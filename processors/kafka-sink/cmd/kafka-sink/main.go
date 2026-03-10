// Package main provides a standalone CLI for the kafka-sink processor.
//
// kafka-sink publishes JSON events to Kafka topics. Supports key extraction
// for partitioning, compression, and SASL authentication.
//
// Usage:
//
//	# Publish to Kafka
//	token-transfer --start-ledger 60200000 --follow | \
//	  kafka-sink --brokers kafka:9092 --topic stellar-transfers
//
//	# With partition key
//	token-transfer --start-ledger 60200000 --follow | \
//	  kafka-sink --brokers kafka:9092 --topic transfers --key transfer.asset.issuedAsset.assetCode
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

const version = "0.1.0"

var (
	// Connection
	brokers string
	topic   string

	// Partitioning
	keyField string

	// Producer settings
	compression string
	batchSize   int
	acks        string

	// Auth
	saslUser     string
	saslPassword string

	// State
	writer *kafka.Writer
	ctx    context.Context
	cancel context.CancelFunc
)

func main() {
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	setupCleanup()

	config := cli.SinkConfig{
		Name:        "kafka-sink",
		Description: "Publish JSON events to Kafka topics",
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
	if writer != nil {
		writer.Close()
	}
	cancel()
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&brokers, "brokers", getEnvOrDefault("KAFKA_BROKERS", "localhost:9092"),
		"Kafka broker addresses (comma-separated)")
	cmd.Flags().StringVar(&topic, "topic", "",
		"Kafka topic to publish to")
	cmd.Flags().StringVar(&keyField, "key", "",
		"Field path for partition key (dot notation, e.g., meta.txHash)")
	cmd.Flags().StringVar(&compression, "compression", "snappy",
		"Compression: none, gzip, snappy, lz4")
	cmd.Flags().IntVar(&batchSize, "batch-size", 100,
		"Producer batch size")
	cmd.Flags().StringVar(&acks, "acks", "all",
		"Required acks: 0, 1, or all")
	cmd.Flags().StringVar(&saslUser, "sasl-user", getEnvOrDefault("KAFKA_SASL_USER", ""),
		"SASL username (or set KAFKA_SASL_USER)")
	cmd.Flags().StringVar(&saslPassword, "sasl-password", getEnvOrDefault("KAFKA_SASL_PASSWORD", ""),
		"SASL password (or set KAFKA_SASL_PASSWORD)")

	cmd.MarkFlagRequired("topic")
}

func processEvent(event map[string]interface{}) error {
	// Lazy init writer
	if writer == nil {
		if err := initWriter(); err != nil {
			return err
		}
	}

	// Marshal event
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Extract partition key
	var key []byte
	if keyField != "" {
		if keyVal := resolveField(event, keyField); keyVal != "" {
			key = []byte(keyVal)
		}
	}

	msg := kafka.Message{
		Value: data,
		Key:   key,
	}

	if err := writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("failed to write to kafka: %w", err)
	}

	return nil
}

func initWriter() error {
	brokerList := strings.Split(brokers, ",")
	for i := range brokerList {
		brokerList[i] = strings.TrimSpace(brokerList[i])
	}

	var codec kafka.Compression
	switch compression {
	case "gzip":
		codec = kafka.Gzip
	case "snappy":
		codec = kafka.Snappy
	case "lz4":
		codec = kafka.Lz4
	default:
		codec = 0 // no compression
	}

	requiredAcks := kafka.RequireAll
	switch acks {
	case "0":
		requiredAcks = kafka.RequireNone
	case "1":
		requiredAcks = kafka.RequireOne
	}

	transport := &kafka.Transport{}

	// Configure SASL if credentials provided
	if saslUser != "" && saslPassword != "" {
		transport.SASL = plain.Mechanism{
			Username: saslUser,
			Password: saslPassword,
		}
	}

	writer = &kafka.Writer{
		Addr:         kafka.TCP(brokerList...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    batchSize,
		RequiredAcks: requiredAcks,
		Compression:  codec,
		Transport:    transport,
	}

	return nil
}

func resolveField(event map[string]interface{}, path string) string {
	parts := strings.Split(path, ".")
	current := event
	for i, part := range parts {
		val, ok := current[part]
		if !ok {
			return ""
		}
		if i == len(parts)-1 {
			return fmt.Sprintf("%v", val)
		}
		current, ok = val.(map[string]interface{})
		if !ok {
			return ""
		}
	}
	return ""
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
