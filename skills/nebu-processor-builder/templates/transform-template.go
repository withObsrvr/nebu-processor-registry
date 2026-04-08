// Package main provides a standalone CLI for the {NAME} processor.
//
// {DESCRIPTION}
package main

import (
	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

// Custom flags for this processor
var (
	// TODO: Add processor-specific flags here
	// Example:
	// minAmount int64
	// assetCode string
)

func main() {
	config := cli.TransformConfig{
		Name:        "{NAME}",
		Description: "{DESCRIPTION}",
		Version:     version,
		// Optional: declare the canonical schema this transform
		// operates on. Pass-through filters set this to their
		// upstream's schema ID (e.g., "nebu.token_transfer.v1").
		// SchemaID: "nebu.{NAME_UNDERSCORED}.v1",
	}

	cli.RunTransformCLI(config, transformEvent, addFlags)
}

// transformEvent processes a single event. Return:
//   - nil   to filter the event out of the output stream
//   - event to pass it through (modified or unmodified)
//
// There is no error return: transforms run on potentially millions of
// events and one bad event must not halt the pipeline. Log
// recoverable issues to stderr and return nil to skip them.
func transformEvent(event map[string]interface{}) map[string]interface{} {
	// TODO: Implement your transform logic here

	// Example: Filter by field value
	// if value, ok := event["field"].(string); ok {
	//     if value != "expected" {
	//         return nil // Filter out
	//     }
	// }

	// Example: Enrich event
	// event["enriched_field"] = "value"

	return event
}

// addFlags adds custom flags to the command
func addFlags(cmd *cobra.Command) {
	// TODO: Add your flags here
	// Example:
	// cmd.Flags().Int64Var(&minAmount, "min", 0, "Minimum amount threshold")
	// cmd.Flags().StringVar(&assetCode, "asset", "", "Filter by asset code")
}
