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
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/metrics"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
	"github.com/withObsrvr/nebu/pkg/toid"
)

const version = "0.2.0"

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
	ctx         context.Context
	cancel      context.CancelFunc
	flushTicker *time.Ticker
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
	if db != nil && len(batch) > 0 {
		if err := flushBatch(); err != nil {
			fmt.Fprintf(os.Stderr, "Error flushing final batch: %v\n", err)
		}
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

	// Add to batch
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
		return flushBatch()
	}

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

// ensureTable creates the events table if it doesn't exist
func ensureTable() error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGINT PRIMARY KEY,
			event_type TEXT,
			ledger_sequence BIGINT,
			data JSONB NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`, tableName)

	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Add ledger_sequence column to existing tables that don't have it
	alterQuery := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS ledger_sequence BIGINT`, tableName)
	if _, err := db.ExecContext(ctx, alterQuery); err != nil {
		return fmt.Errorf("failed to add ledger_sequence column: %w", err)
	}

	// Create indexes
	indexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_data ON %s USING GIN (data)", tableName, tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_event_type ON %s (event_type) WHERE event_type IS NOT NULL", tableName, tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_created_at ON %s (created_at)", tableName, tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_ledger_sequence ON %s (ledger_sequence)", tableName, tableName),
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// flushBatch writes the current batch to PostgreSQL, retrying with exponential backoff on failure.
func flushBatch() error {
	if len(batch) == 0 {
		return nil
	}

	if maxRetries == 0 {
		err := flushBatchOnce()
		if err == nil && recorder != nil {
			recorder.RecordBatchFlush()
		}
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = flushBatchOnce()
		if lastErr == nil {
			if recorder != nil {
				recorder.RecordBatchFlush()
			}
			return nil
		}
		if attempt < maxRetries {
			if recorder != nil {
				recorder.RecordRetry()
			}
			backoff := time.Duration(attempt+1) * retryBackoff
			fmt.Fprintf(os.Stderr, "Batch flush failed (attempt %d/%d), retrying in %v: %v\n",
				attempt+1, maxRetries, backoff, lastErr)
			time.Sleep(backoff)
		}
	}
	return fmt.Errorf("batch flush failed after %d attempts: %w", maxRetries, lastErr)
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
	`, tableName)

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
		`, tableName, tableName)
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
				if len(batch) > 0 {
					if err := flushBatch(); err != nil {
						fmt.Fprintf(os.Stderr, "Error flushing batch: %v\n", err)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// getEnvOrDefault gets environment variable or returns default
func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
