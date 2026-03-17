// Package main provides a standalone CLI for the soroswap-detector transform processor.
//
// This processor checks swap-candidate events for Soroswap contract addresses
// and tags matches with protocol: "soroswap".
//
// Usage:
//
//	token-transfer | swap-candidate | soroswap-detector -q --network testnet
//	token-transfer | swap-candidate | soroswap-detector --contracts-file extra.txt
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

var (
	contractsFile string
	network       string
)

// Known Soroswap contracts by network
var soroswapContracts = map[string]map[string]string{
	"testnet": {
		"CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD": "router",
		"CDP3HMUH6SMS3S7NPGNDJLULCOXXEPSHY4JKUKMBNQMATHDHWXRRJTBY":  "factory",
	},
	"mainnet": {
		// Mainnet contracts will be added when available
	},
}

// contractSet is the resolved set of contract addresses to match against
var contractSet map[string]string

func main() {
	config := cli.TransformConfig{
		Name:        "soroswap-detector",
		Description: "Detect Soroswap DEX swaps in swap-candidate events",
		Version:     version,
	}

	cli.RunTransformCLI(config, detect, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&contractsFile, "contracts-file", "", "File with additional contract addresses (one per line)")
	cmd.Flags().StringVar(&network, "network", "testnet", "Network to use for known contracts (testnet|mainnet)")

	// Pre-run hook to resolve contract set
	origPreRun := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if origPreRun != nil {
			if err := origPreRun(cmd, args); err != nil {
				return err
			}
		}
		return resolveContracts()
	}
}

func resolveContracts() error {
	contractSet = make(map[string]string)

	// Add known contracts for the selected network
	if known, ok := soroswapContracts[network]; ok {
		for addr, role := range known {
			contractSet[addr] = role
		}
	}

	// Load additional contracts from file
	if contractsFile != "" {
		data, err := os.ReadFile(contractsFile)
		if err != nil {
			return fmt.Errorf("failed to read contracts file: %w", err)
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				contractSet[line] = "custom"
			}
		}
	}

	return nil
}

// detect checks a swap-candidate event for Soroswap contract addresses.
func detect(event map[string]interface{}) map[string]interface{} {
	// Lazy init on first call (for when PersistentPreRunE isn't called)
	if contractSet == nil {
		resolveContracts()
	}

	// Only process swap candidates
	schema, _ := event["_schema"].(string)
	if schema != "nebu.swap_candidate.v1" {
		return event // Pass through non-candidates
	}

	// Check contract_addresses array
	var matchedContract string
	var matchedRole string

	if addrs, ok := event["contract_addresses"].([]interface{}); ok {
		for _, addr := range addrs {
			if addrStr, ok := addr.(string); ok {
				if role, found := contractSet[addrStr]; found {
					matchedContract = addrStr
					matchedRole = role
					break
				}
			}
		}
	}

	// Also check legs[*].contract_address
	if matchedContract == "" {
		if legs, ok := event["legs"].([]interface{}); ok {
			for _, leg := range legs {
				if legMap, ok := leg.(map[string]interface{}); ok {
					if addr, ok := legMap["contract_address"].(string); ok {
						if role, found := contractSet[addr]; found {
							matchedContract = addr
							matchedRole = role
							break
						}
					}
				}
			}
		}
	}

	// Tag if matched
	if matchedContract != "" {
		event["protocol"] = "soroswap"
		if matchedRole == "router" {
			event["router_contract"] = matchedContract
		} else {
			event["matched_contract"] = matchedContract
		}
	}

	return event
}
