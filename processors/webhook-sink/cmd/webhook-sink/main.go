// Package main provides a standalone CLI for the webhook-sink processor.
//
// webhook-sink POSTs JSON events to an HTTP endpoint with configurable headers,
// authentication, batching, and retry with exponential backoff.
//
// Usage:
//
//	# Post events to a webhook
//	token-transfer --start-ledger 60200000 --follow | \
//	  webhook-sink --url https://example.com/webhook
//
//	# With authentication
//	token-transfer --start-ledger 60200000 --follow | \
//	  webhook-sink --url https://example.com/webhook --auth-header "Bearer token123"
//
//	# Batched delivery
//	token-transfer --start-ledger 60200000 --follow | \
//	  webhook-sink --url https://example.com/webhook --batch-size 100
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

const version = "0.1.0"

var (
	// Endpoint settings
	webhookURL string
	method     string
	headers    []string
	authHeader string
	timeout    int

	// Batching
	batchSize     int
	flushInterval time.Duration

	// Retry
	maxRetries   int
	retryBackoff time.Duration

	// State
	client      *http.Client
	batch       []map[string]interface{}
	batchMu     sync.Mutex
	flushTicker *time.Ticker
	done        chan struct{}
)

func main() {
	done = make(chan struct{})
	setupCleanup()

	config := cli.SinkConfig{
		Name:        "webhook-sink",
		Description: "POST JSON events to an HTTP endpoint",
		Version:     version,
	}

	cli.RunSinkCLI(config, processEvent, addFlags)

	cleanup()
}

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

func cleanup() {
	close(done)
	if flushTicker != nil {
		flushTicker.Stop()
	}
	batchMu.Lock()
	if len(batch) > 0 {
		if err := flushBatchLocked(); err != nil {
			fmt.Fprintf(os.Stderr, "Error flushing final batch: %v\n", err)
		}
	}
	batchMu.Unlock()
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&webhookURL, "url", getEnvOrDefault("WEBHOOK_URL", ""),
		"Webhook URL (or set WEBHOOK_URL env)")
	cmd.Flags().StringVar(&method, "method", "POST",
		"HTTP method (POST or PUT)")
	cmd.Flags().StringArrayVar(&headers, "header", nil,
		"HTTP header as key:value (repeatable)")
	cmd.Flags().StringVar(&authHeader, "auth-header", getEnvOrDefault("WEBHOOK_AUTH", ""),
		"Authorization header value (or set WEBHOOK_AUTH env)")
	cmd.Flags().IntVar(&timeout, "timeout", 10,
		"HTTP request timeout in seconds")
	cmd.Flags().IntVar(&batchSize, "batch-size", 1,
		"Number of events to batch per request (1 = send individually)")
	cmd.Flags().DurationVar(&flushInterval, "flush-interval", 5*time.Second,
		"Flush interval for partial batches")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 3,
		"Max retry attempts per request")
	cmd.Flags().DurationVar(&retryBackoff, "retry-backoff", 2*time.Second,
		"Base backoff between retries")

	cmd.MarkFlagRequired("url")
}

func processEvent(event map[string]interface{}) error {
	// Lazy init HTTP client
	if client == nil {
		client = &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		}
		if batchSize > 1 {
			startFlushTicker()
		}
	}

	// No batching — send immediately
	if batchSize <= 1 {
		return sendSingle(event)
	}

	// Batching mode
	batchMu.Lock()
	batch = append(batch, event)
	if len(batch) >= batchSize {
		err := flushBatchLocked()
		batchMu.Unlock()
		return err
	}
	batchMu.Unlock()
	return nil
}

func sendSingle(event map[string]interface{}) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}
	return sendWithRetry(data)
}

func flushBatchLocked() error {
	if len(batch) == 0 {
		return nil
	}

	data, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %w", err)
	}

	err = sendWithRetry(data)
	if err == nil {
		batch = batch[:0]
	}
	return err
}

func sendWithRetry(data []byte) error {
	totalAttempts := maxRetries + 1
	var lastErr error

	for attempt := 1; attempt <= totalAttempts; attempt++ {
		lastErr = sendRequest(data)
		if lastErr == nil {
			return nil
		}
		if attempt < totalAttempts {
			backoff := exponentialBackoff(attempt, retryBackoff)
			fmt.Fprintf(os.Stderr, "Webhook %s failed (attempt %d/%d), retrying in %v: %v\n",
				method, attempt, totalAttempts, backoff, lastErr)
			time.Sleep(backoff)
		}
	}
	return fmt.Errorf("webhook %s failed after %d attempts: %w", method, totalAttempts, lastErr)
}

func sendRequest(data []byte) error {
	req, err := http.NewRequest(method, webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add auth header
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	// Add custom headers
	for _, h := range headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, webhookURL)
	}

	return nil
}

func startFlushTicker() {
	if flushInterval <= 0 {
		flushInterval = 5 * time.Second
	}
	flushTicker = time.NewTicker(flushInterval)
	go func() {
		for {
			select {
			case <-flushTicker.C:
				batchMu.Lock()
				if len(batch) > 0 {
					if err := flushBatchLocked(); err != nil {
						fmt.Fprintf(os.Stderr, "Error flushing batch: %v\n", err)
					}
				}
				batchMu.Unlock()
			case <-done:
				return
			}
		}
	}()
}

func exponentialBackoff(attempt int, base time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	backoff := base * time.Duration(math.Pow(2, float64(attempt-1)))
	jitterMax := int64(backoff) / 4
	if jitterMax <= 0 {
		jitterMax = 1
	}
	jitter := time.Duration(rand.Int63n(jitterMax))
	return backoff + jitter
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
