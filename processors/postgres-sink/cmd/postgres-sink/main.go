// Package main provides a standalone CLI for the postgres-sink processor.
//
// postgres-sink stores nebu events in PostgreSQL using JSONB for flexible schema.
// It provides automatic TOID generation for idempotency and batched COPY for performance.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/metrics"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
	"github.com/withObsrvr/nebu/pkg/toid"
)

const version = "0.2.0"

// identifierRegexp validates SQL identifiers.
var identifierRegexp = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

var (
	// Connection settings
	dsn string

	// Table settings
	tableName string
	batchSize int

	// Conflict resolution
	conflictMode string // "ignore", "update", or "update-if-newer"

	// Retry settings
	maxRetries   int
	retryBackoff time.Duration

	// Metrics
	metricsPort int
	recorder    *metrics.Recorder

	// State
	db          *sql.DB
	batch       []batchEvent
	batchMu     sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	flushTicker *time.Ticker

	// Quoted table name for SQL (set once during ensureTable)
	quotedTable string
)

type batchEvent struct {
	id             int64
	eventType      *string
	ledgerSequence *int64
	data           []byte
}

func main() {
	// Setup context and graceful shutdown
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	setupCleanup()

	config := cli.SinkConfig{
		Name:        "postgres-sink",
		Description: "Store events in PostgreSQL with JSONB schema",
		Version:     version,
	}

	cli.RunSinkCLI(config, processEvent, addFlags)

	// Cleanup on normal exit
	cleanup()
}

// setupCleanup registers signal handlers for graceful shutdown
func setupCleanup() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Fprintln(os.Stderr, "\nReceived shutdown signal, flushing...")
		cleanup()
		os.Exit(0)
	}()
}

// cleanup ensures database connection is properly closed and batch is flushed
func cleanup() {
	// Stop the ticker first
	if flushTicker != nil {
		flushTicker.Stop()
	}

	// Flush any pending events BEFORE canceling context
	batchMu.Lock()
	if db != nil && len(batch) > 0 {
		if err := flushBatchLocked(); err != nil {
			fmt.Fprintf(os.Stderr, "Error flushing final batch: %v\n", err)
		}
	}
	batchMu.Unlock()

	// Shutdown metrics server
	if recorder != nil {
		recorder.Shutdown()
	}

	// Now cancel context to stop ticker goroutine
	cancel()

	// Close database connection
	if db != nil {
		db.Close()
	}
}

// addFlags adds custom flags to the command
func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&dsn, "dsn", getEnvOrDefault("POSTGRES_DSN", ""),
		"PostgreSQL connection string (or set POSTGRES_DSN env)")
	cmd.Flags().StringVar(&tableName, "table", "events",
		"Table name for storing events")
	cmd.Flags().IntVar(&batchSize, "batch-size", 1000,
		"Number of events to batch before COPY")
	cmd.Flags().StringVar(&conflictMode, "conflict", "ignore",
		"Conflict resolution: 'ignore', 'update', or 'update-if-newer'")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 5,
		"Max retry attempts for batch flush (0 = no retry)")
	cmd.Flags().DurationVar(&retryBackoff, "retry-backoff", 5*time.Second,
		"Base backoff between retries")
	cmd.Flags().IntVar(&metricsPort, "metrics-port", 0,
		"Port to expose Prometheus metrics on (0 = disabled)")

	cmd.MarkFlagRequired("dsn")
}

// processEvent handles each incoming event
func processEvent(event map[string]interface{}) error {
	// Lazy connect on first event
	if db == nil {
		if err := connect(); err != nil {
			return err
		}
		if err := ensureTable(); err != nil {
			return err
		}
		startFlushTicker()
		// Start metrics server if configured
		if metricsPort > 0 {
			recorder = metrics.NewRecorder("postgres_sink")
			go func() {
				if err := recorder.Serve(metricsPort); err != nil {
					fmt.Fprintf(os.Stderr, "Metrics server error: %v\n", err)
				}
			}()
		}
	}

	// Generate or extract TOID
	var id int64
	var err error

	// Check if event already has a pre-calculated TOID
	if toidVal, ok := event["toid"]; ok {
		switch v := toidVal.(type) {
		case float64:
			id = int64(v)
		case int64:
			id = v
		case int:
			id = int64(v)
		default:
			return fmt.Errorf("invalid toid type: %T", toidVal)
		}
	} else if idVal, ok := event["id"]; ok {
		// Also support "id" field
		switch v := idVal.(type) {
		case float64:
			id = int64(v)
		case int64:
			id = v
		case int:
			id = int64(v)
		default:
			return fmt.Errorf("invalid id type: %T", idVal)
		}
	} else {
		// Auto-generate TOID from meta fields
		id, err = toid.FromEvent(event)
		if err != nil {
			return fmt.Errorf("failed to generate TOID: %w", err)
		}
	}

	// Extract event type if present
	eventType := extractEventType(event)

	// Extract ledger sequence if present
	ledgerSeq := extractLedgerSequence(event)

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Add to batch (synchronized)
	batchMu.Lock()
	batch = append(batch, batchEvent{
		id:             id,
		eventType:      eventType,
		ledgerSequence: ledgerSeq,
		data:           data,
	})

	// Record metrics
	if recorder != nil {
		recorder.RecordEventsProcessed(1)
		if ledgerSeq != nil {
			recorder.SetCurrentLedger(*ledgerSeq)
		}
	}

	// Flush if batch is full
	if len(batch) >= batchSize {
		err := flushBatchLocked()
		batchMu.Unlock()
		return err
	}
	batchMu.Unlock()

	return nil
}

// extractLedgerSequence extracts the ledger sequence number from an event.
// Supports: meta.ledgerSequence (token-transfer), top-level ledgerSequence (contract-events),
// meta.ledger_sequence, top-level ledger_sequence.
func extractLedgerSequence(event map[string]interface{}) *int64 {
	// Try meta.ledgerSequence (token-transfer format)
	if meta, ok := event["meta"].(map[string]interface{}); ok {
		if v := toInt64(meta["ledgerSequence"]); v != nil {
			return v
		}
		if v := toInt64(meta["ledger_sequence"]); v != nil {
			return v
		}
	}
	// Try top-level ledgerSequence (contract-events format)
	if v := toInt64(event["ledgerSequence"]); v != nil {
		return v
	}
	// Try top-level ledger_sequence
	if v := toInt64(event["ledger_sequence"]); v != nil {
		return v
	}
	return nil
}

// toInt64 converts a JSON number value to *int64.
func toInt64(val interface{}) *int64 {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case float64:
		i := int64(v)
		return &i
	case int64:
		return &v
	case int:
		i := int64(v)
		return &i
	}
	return nil
}

// extractEventType extracts the event type from an event.
// Supports multiple formats:
//   - Custom jq: "event_type" or "function_name" field
//   - contract-events: "eventType" field (e.g., "transfer", "fee")
//   - contract-invocation: "functionName" field (e.g., "work", "transfer")
//   - protobuf oneof: field name indicates type (e.g., has "transfer" field)
//   - simple: "type" field (e.g., {"type": "transfer"})
func extractEventType(event map[string]interface{}) *string {
	// Try custom jq "event_type" field first (snake_case convention)
	if t, ok := event["event_type"].(string); ok && t != "" && t != "unknown" {
		result := t
		return &result
	}

	// Try contract-events "eventType" field (camelCase)
	if t, ok := event["eventType"].(string); ok && t != "" && t != "unknown" {
		result := t
		return &result
	}

	// Try contract-invocation "functionName" field
	if t, ok := event["functionName"].(string); ok && t != "" {
		result := t
		return &result
	}

	// Try custom jq "function_name" field (snake_case)
	if t, ok := event["function_name"].(string); ok && t != "" {
		result := t
		return &result
	}

	// Try protobuf oneof fields (token-transfer, etc.)
	// Check for common event type fields
	oneofFields := []string{"transfer", "mint", "burn", "clawback", "fee", "payment", "invoke"}
	for _, field := range oneofFields {
		if _, exists := event[field]; exists {
			result := field
			return &result
		}
	}

	// Fall back to simple "type" field (but skip enum values like "CONTRACT")
	if t, ok := event["type"].(string); ok && t != "CONTRACT" && t != "SYSTEM" && t != "DIAGNOSTIC" {
		result := t
		return &result
	}

	return nil
}

// connect establishes connection to PostgreSQL
func connect() error {
	var err error
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings for production
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

// safeIndexName derives a safe index name from a table name by stripping non-identifier characters.
func safeIndexName(table, suffix string) string {
	safe := regexp.MustCompile(`[^a-zA-Z0-9_]`).ReplaceAllString(table, "_")
	return fmt.Sprintf("idx_%s_%s", safe, suffix)
}

// ensureTable creates the events table if it doesn't exist
func ensureTable() error {
	// Validate and quote table name
	if !identifierRegexp.MatchString(tableName) {
		return fmt.Errorf("invalid table name %q: must match [a-zA-Z_][a-zA-Z0-9_]*", tableName)
	}
	quotedTable = pq.QuoteIdentifier(tableName)

	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGINT PRIMARY KEY,
			event_type TEXT,
			ledger_sequence BIGINT,
			data JSONB NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`, quotedTable)

	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Add ledger_sequence column to existing tables that don't have it
	alterQuery := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS ledger_sequence BIGINT`, quotedTable)
	if _, err := db.ExecContext(ctx, alterQuery); err != nil {
		return fmt.Errorf("failed to add ledger_sequence column: %w", err)
	}

	// Create indexes using safe index names
	indexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s USING GIN (data)", pq.QuoteIdentifier(safeIndexName(tableName, "data")), quotedTable),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (event_type) WHERE event_type IS NOT NULL", pq.QuoteIdentifier(safeIndexName(tableName, "event_type")), quotedTable),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (created_at)", pq.QuoteIdentifier(safeIndexName(tableName, "created_at")), quotedTable),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (ledger_sequence)", pq.QuoteIdentifier(safeIndexName(tableName, "ledger_sequence")), quotedTable),
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// flushBatch acquires the batch mutex and flushes with retry.
func flushBatch() error {
	batchMu.Lock()
	defer batchMu.Unlock()
	return flushBatchLocked()
}

// flushBatchLocked flushes the batch with retry. Caller must hold batchMu.
func flushBatchLocked() error {
	if len(batch) == 0 {
		return nil
	}

	totalAttempts := maxRetries + 1
	if maxRetries == 0 {
		totalAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= totalAttempts; attempt++ {
		lastErr = flushBatchOnce()
		if lastErr == nil {
			if recorder != nil {
				recorder.RecordBatchFlush()
			}
			return nil
		}
		if attempt < totalAttempts {
			if recorder != nil {
				recorder.RecordRetry()
			}
			backoff := exponentialBackoff(attempt, retryBackoff)
			fmt.Fprintf(os.Stderr, "Batch flush failed (attempt %d/%d), retrying in %v: %v\n",
				attempt, totalAttempts, backoff, lastErr)
			time.Sleep(backoff)
		}
	}
	return fmt.Errorf("batch flush failed after %d attempts: %w", totalAttempts, lastErr)
}

// flushBatchOnce performs a single attempt to write the batch to PostgreSQL.
func flushBatchOnce() error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, getUpsertQuery())
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, evt := range batch {
		_, err := stmt.ExecContext(ctx, evt.id, evt.eventType, evt.ledgerSequence, evt.data)
		if err != nil {
			return fmt.Errorf("failed to insert event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Clear batch only on success
	batch = batch[:0]

	return nil
}

// getUpsertQuery returns the appropriate INSERT query based on conflict mode
func getUpsertQuery() string {
	base := fmt.Sprintf(`
		INSERT INTO %s (id, event_type, ledger_sequence, data)
		VALUES ($1, $2, $3, $4)
	`, quotedTable)

	switch conflictMode {
	case "update":
		return base + `
			ON CONFLICT (id) DO UPDATE SET
				event_type = EXCLUDED.event_type,
				ledger_sequence = EXCLUDED.ledger_sequence,
				data = EXCLUDED.data,
				created_at = NOW()
		`
	case "update-if-newer":
		return base + fmt.Sprintf(`
			ON CONFLICT (id) DO UPDATE SET
				event_type = EXCLUDED.event_type,
				ledger_sequence = EXCLUDED.ledger_sequence,
				data = EXCLUDED.data,
				created_at = NOW()
			WHERE EXCLUDED.ledger_sequence > %s.ledger_sequence
				OR %s.ledger_sequence IS NULL
		`, quotedTable, quotedTable)
	case "ignore":
		fallthrough
	default:
		return base + ` ON CONFLICT (id) DO NOTHING`
	}
}

// startFlushTicker starts a ticker to flush batches periodically
func startFlushTicker() {
	flushTicker = time.NewTicker(1 * time.Second)
	go func() {
		for {
			select {
			case <-flushTicker.C:
				if err := flushBatch(); err != nil {
					fmt.Fprintf(os.Stderr, "Error flushing batch: %v\n", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// exponentialBackoff returns a duration with exponential growth and jitter.
// Formula: base * 2^(attempt-1) + random jitter up to 25% of the backoff.
func exponentialBackoff(attempt int, base time.Duration) time.Duration {
	backoff := base * time.Duration(math.Pow(2, float64(attempt-1)))
	// Add up to 25% jitter
	jitter := time.Duration(rand.Int63n(int64(backoff) / 4))
	return backoff + jitter
}

// getEnvOrDefault gets environment variable or returns default
func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
