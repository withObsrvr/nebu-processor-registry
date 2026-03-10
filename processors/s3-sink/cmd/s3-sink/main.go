// Package main provides a standalone CLI for the s3-sink processor.
//
// s3-sink writes buffered JSONL files to S3-compatible object storage,
// partitioned by time. Supports gzip compression and configurable flush
// intervals.
//
// Usage:
//
//	# Write to S3
//	token-transfer --start-ledger 60200000 --follow | \
//	  s3-sink --bucket my-data-lake --prefix stellar/transfers
//
//	# Write to MinIO
//	token-transfer --start-ledger 60200000 --follow | \
//	  s3-sink --bucket my-bucket --endpoint http://minio:9000
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

const version = "0.1.0"

var (
	// S3 settings
	bucketName    string
	prefix        string
	endpoint      string
	region        string
	partition     string
	format        string
	flushInterval time.Duration
	flushSize     int

	// State
	s3Client    *s3.Client
	buffer      bytes.Buffer
	gzWriter    *gzip.Writer
	bufferMu    sync.Mutex
	eventCount  int
	currentHour time.Time
	flushTicker *time.Ticker
	ctx         context.Context
	cancel      context.CancelFunc
)

func main() {
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	setupCleanup()

	cliConfig := cli.SinkConfig{
		Name:        "s3-sink",
		Description: "Write JSONL events to S3-compatible object storage",
		Version:     version,
	}

	cli.RunSinkCLI(cliConfig, processEvent, addFlags)

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
	if flushTicker != nil {
		flushTicker.Stop()
	}
	bufferMu.Lock()
	if s3Client != nil && eventCount > 0 {
		if err := flushBufferLocked(); err != nil {
			fmt.Fprintf(os.Stderr, "Error flushing final buffer: %v\n", err)
		}
	}
	bufferMu.Unlock()
	cancel()
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&bucketName, "bucket", getEnvOrDefault("S3_BUCKET", ""),
		"S3 bucket name (or set S3_BUCKET env)")
	cmd.Flags().StringVar(&prefix, "prefix", "",
		"Key prefix (e.g., stellar/transfers)")
	cmd.Flags().StringVar(&endpoint, "endpoint", getEnvOrDefault("S3_ENDPOINT", ""),
		"Custom S3 endpoint URL (for MinIO/R2)")
	cmd.Flags().StringVar(&region, "region", getEnvOrDefault("AWS_REGION", "us-east-1"),
		"AWS region")
	cmd.Flags().StringVar(&partition, "partition", "hourly",
		"Time partitioning: 'hourly' or 'daily'")
	cmd.Flags().StringVar(&format, "format", "jsonl.gz",
		"Output format: 'jsonl' or 'jsonl.gz'")
	cmd.Flags().DurationVar(&flushInterval, "flush-interval", 5*time.Minute,
		"Flush interval for writing files")
	cmd.Flags().IntVar(&flushSize, "flush-size", 64*1024*1024,
		"Flush when buffer exceeds this size in bytes (default: 64MB)")

	cmd.MarkFlagRequired("bucket")
}

func processEvent(event map[string]interface{}) error {
	// Lazy init S3 client
	if s3Client == nil {
		if err := initS3Client(); err != nil {
			return err
		}
		startFlushTicker()
	}

	// Marshal event
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	bufferMu.Lock()
	defer bufferMu.Unlock()

	// Write to buffer
	if useGzip() {
		if gzWriter == nil {
			gzWriter = gzip.NewWriter(&buffer)
		}
		if _, err := gzWriter.Write(data); err != nil {
			return fmt.Errorf("gzip write failed: %w", err)
		}
		if _, err := gzWriter.Write([]byte("\n")); err != nil {
			return fmt.Errorf("gzip write failed: %w", err)
		}
	} else {
		buffer.Write(data)
		buffer.Write([]byte("\n"))
	}
	eventCount++

	// Flush if buffer exceeds size limit
	if buffer.Len() >= flushSize {
		return flushBufferLocked()
	}

	return nil
}

func flushBufferLocked() error {
	if eventCount == 0 {
		return nil
	}

	// Close gzip writer to finalize
	if gzWriter != nil {
		if err := gzWriter.Close(); err != nil {
			return fmt.Errorf("gzip close failed: %w", err)
		}
		gzWriter = nil
	}

	// Generate object key
	key := generateKey()

	// Upload to S3
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(key),
		Body:        bytes.NewReader(buffer.Bytes()),
		ContentType: aws.String(contentType()),
	})
	if err != nil {
		return fmt.Errorf("failed to upload to s3://%s/%s: %w", bucketName, key, err)
	}

	fmt.Fprintf(os.Stderr, "Uploaded %d events to s3://%s/%s (%d bytes)\n",
		eventCount, bucketName, key, buffer.Len())

	// Reset buffer
	buffer.Reset()
	eventCount = 0

	return nil
}

func generateKey() string {
	now := time.Now().UTC()
	id := uuid.New().String()[:8]

	var timePath string
	switch partition {
	case "daily":
		timePath = now.Format("year=2006/month=01/day=02")
	default: // hourly
		timePath = now.Format("year=2006/month=01/day=02/hour=15")
	}

	ext := "jsonl"
	if useGzip() {
		ext = "jsonl.gz"
	}

	if prefix != "" {
		return fmt.Sprintf("%s/%s/events-%s.%s", prefix, timePath, id, ext)
	}
	return fmt.Sprintf("%s/events-%s.%s", timePath, id, ext)
}

func useGzip() bool {
	return format == "jsonl.gz"
}

func contentType() string {
	if useGzip() {
		return "application/gzip"
	}
	return "application/x-ndjson"
}

func initS3Client() error {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Opts := []func(*s3.Options){}

	if endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		})

		// For custom endpoints, use env credentials if available
		accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
		secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
		if accessKey != "" && secretKey != "" {
			cfg.Credentials = credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")
		}
	}

	s3Client = s3.NewFromConfig(cfg, s3Opts...)
	return nil
}

func startFlushTicker() {
	flushTicker = time.NewTicker(flushInterval)
	go func() {
		for {
			select {
			case <-flushTicker.C:
				bufferMu.Lock()
				if eventCount > 0 {
					if err := flushBufferLocked(); err != nil {
						fmt.Fprintf(os.Stderr, "Error flushing buffer: %v\n", err)
					}
				}
				bufferMu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
