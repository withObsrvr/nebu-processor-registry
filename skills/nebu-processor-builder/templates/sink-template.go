// Package main provides a standalone CLI for the {NAME} processor.
//
// {DESCRIPTION}
package main

import (
	"encoding/json"
	"sync"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

// Connection/configuration variables
var (
	// TODO: Add sink-specific configuration
	// Example:
	// dsn        string  // Database connection string
	// outputFile string  // File path
	// apiURL     string  // API endpoint

	// Connection state (lazy initialized)
	// connection interface{}
	// connOnce   sync.Once
)

func main() {
	config := cli.SinkConfig{
		Name:        "{NAME}",
		Description: "{DESCRIPTION}",
		Version:     version,
		// Optional: declare the canonical schema this sink expects.
		// Generic sinks (that accept any JSON shape) leave this empty.
		// SchemaID: "nebu.token_transfer.v1",
	}

	cli.RunSinkCLI(config, processEvent, addFlags)
}

// processEvent handles a single event.
//
// Returning a non-nil error causes the CLI helper to log the failure
// as a warning and continue to the next event (streams-never-throw).
// For unrecoverable conditions (dropped DB connection, revoked
// credentials), call os.Exit or panic with a clear message — errors
// returned from here are NOT treated as fatal.
func processEvent(event map[string]interface{}) error {
	// TODO: Implement your sink logic here

	// Example: Lazy initialize connection
	// connOnce.Do(func() {
	//     connection = initializeConnection()
	// })

	// Example: Write to destination
	// return writeToDestination(event)

	// Example: Batch events (see sink-processors.md for full pattern)
	// return addToBatch(event)

	// Placeholder implementation - marshal to verify JSON is valid
	_, err := json.Marshal(event)
	if err != nil {
		return err
	}

	// TODO: Write data to your sink (e.g., database, file, message queue)

	return nil
}

// addFlags adds custom flags to the command
func addFlags(cmd *cobra.Command) {
	// TODO: Add your flags here
	// Example:
	// cmd.Flags().StringVar(&dsn, "dsn", "", "Database connection string (required)")
	// cobra.MarkFlagRequired(cmd.Flags(), "dsn")

	// cmd.Flags().StringVar(&outputFile, "out", "output.jsonl", "Output file path")
	// cmd.Flags().StringVar(&apiURL, "url", "", "API endpoint URL")
}

// Example helper functions:

// func initializeConnection() interface{} {
//     // TODO: Initialize your connection (database, message queue, file, etc.)
//     return nil
// }

// func writeToDestination(event map[string]interface{}) error {
//     // TODO: Write event to destination
//     return nil
// }
