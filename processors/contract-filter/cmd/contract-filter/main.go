// Package main provides a standalone CLI for the contract-filter transform processor.
//
// This processor filters events by contract ID(s). It reads JSON events from stdin
// and writes filtered events to stdout, passing through only events that match
// one or more specified contract IDs.
//
// Usage:
//
//	# Filter for a specific contract
//	contract-events --start-ledger 60200000 | contract-filter --contract CABC...XYZ
//
//	# Filter for multiple contracts
//	contract-events --start-ledger 60200000 | contract-filter --contract CABC...XYZ --contract CDEF...123
//
//	# Filter using a file of contract IDs
//	contract-events --start-ledger 60200000 | contract-filter --contract-file contracts.txt
package main

import (
	"bufio"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

var (
	contractIDs     []string
	contractFile    string
	contractSet     map[string]bool
	contractsLoaded bool
)

func main() {
	config := cli.TransformConfig{
		Name:        "contract-filter",
		Description: "Filter events by contract ID",
		Version:     version,
	}

	cli.RunTransformCLI(config, filterByContract, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&contractIDs, "contract", nil, "Contract ID to filter for (repeatable)")
	cmd.Flags().StringVar(&contractFile, "contract-file", "", "File containing contract IDs (one per line)")
}

func loadContracts() {
	if contractsLoaded {
		return
	}
	contractsLoaded = true
	contractSet = make(map[string]bool)

	for _, id := range contractIDs {
		contractSet[strings.TrimSpace(id)] = true
	}

	if contractFile != "" {
		f, err := os.Open(contractFile)
		if err != nil {
			return
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				contractSet[line] = true
			}
		}
	}
}

// filterByContract filters events to only include those matching specified contract IDs.
// Returns the event if it matches, nil otherwise.
func filterByContract(event map[string]interface{}) map[string]interface{} {
	loadContracts()

	if len(contractSet) == 0 {
		return event
	}

	// Check common contract ID field locations
	if matchesContractID(event) {
		return event
	}

	return nil
}

func matchesContractID(event map[string]interface{}) bool {
	// Direct contractId field
	if id, ok := getStringField(event, "contractId"); ok && contractSet[id] {
		return true
	}
	if id, ok := getStringField(event, "contract_id"); ok && contractSet[id] {
		return true
	}

	// Nested in transfer/mint/burn/clawback
	for _, key := range []string{"transfer", "mint", "burn", "clawback"} {
		if nested, ok := event[key].(map[string]interface{}); ok {
			if id, ok := getStringField(nested, "from"); ok && contractSet[id] {
				return true
			}
			if id, ok := getStringField(nested, "to"); ok && contractSet[id] {
				return true
			}
		}
	}

	// Meta field
	if meta, ok := event["meta"].(map[string]interface{}); ok {
		if id, ok := getStringField(meta, "contractAddress"); ok && contractSet[id] {
			return true
		}
		if id, ok := getStringField(meta, "contract_address"); ok && contractSet[id] {
			return true
		}
	}

	// State changes
	if changes, ok := event["stateChanges"].([]interface{}); ok {
		for _, change := range changes {
			if changeMap, ok := change.(map[string]interface{}); ok {
				if id, ok := getStringField(changeMap, "contractId"); ok && contractSet[id] {
					return true
				}
				if id, ok := getStringField(changeMap, "contract_id"); ok && contractSet[id] {
					return true
				}
			}
		}
	}

	return false
}

func getStringField(m map[string]interface{}, key string) (string, bool) {
	val, ok := m[key]
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}
