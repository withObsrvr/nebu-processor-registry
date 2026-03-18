// Package main provides a standalone CLI for the soroswap-detector transform processor.
//
// This processor checks swap-candidate events for Soroswap pool addresses
// and tags matches with protocol: "soroswap".
//
// Soroswap's router invokes pool contracts, which invoke token contracts.
// Token-transfer events show the token contract as contractAddress, and the
// pool address as the from/to in transfer legs. Detection therefore checks
// legs[*].from and legs[*].to against known pool addresses.
//
// Usage:
//
//	token-transfer | swap-candidate | soroswap-detector -q --network mainnet
//	token-transfer | swap-candidate | soroswap-detector --pools-file pools.txt
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.2.0"

var (
	poolsFile string
	network   string
)

// Known Soroswap infrastructure contracts by network.
// These are checked against contract_addresses for completeness,
// but pool addresses (loaded via --pools-file) are the primary match target.
var soroswapInfra = map[string]map[string]string{
	"testnet": {
		"CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD": "router",
		"CDP3HMUH6SMS3S7NPGNDJLULCOXXEPSHY4JKUKMBNQMATHDHWXRRJTBY":  "factory",
	},
	"mainnet": {
		"CAG5LRYQ5JVEUI5TEID72EYOVX44TTUJT5BQR2J6J77FH65PCCFAJDDH": "router",
		"CA4HEQTL2WPEUYKYKCDOHCDNIV4QHNJ7EL4J4NQ6VADP7SYHVRYZ7AW2": "factory",
	},
}

// infraSet holds router/factory addresses (checked against contract_addresses)
var infraSet map[string]string

// poolSet holds known Soroswap pool addresses (checked against legs from/to)
var poolSet map[string]bool

func main() {
	config := cli.TransformConfig{
		Name:        "soroswap-detector",
		Description: "Detect Soroswap DEX swaps in swap-candidate events",
		Version:     version,
	}

	cli.RunTransformCLI(config, detect, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&poolsFile, "pools-file", "", "File with Soroswap pool addresses (one per line)")
	cmd.Flags().StringVar(&network, "network", "mainnet", "Network for known infrastructure contracts (testnet|mainnet)")

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
	infraSet = make(map[string]string)
	poolSet = make(map[string]bool)

	// Add known infrastructure contracts for the selected network
	if known, ok := soroswapInfra[network]; ok {
		for addr, role := range known {
			infraSet[addr] = role
		}
	}

	// Load pool addresses from file
	if poolsFile != "" {
		data, err := os.ReadFile(poolsFile)
		if err != nil {
			return fmt.Errorf("failed to read pools file: %w", err)
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				poolSet[line] = true
			}
		}
	}

	return nil
}

// detect checks a swap-candidate event for Soroswap pool addresses in the
// transfer legs. Soroswap pools appear as from/to addresses (not contractAddress)
// because the pool invokes the token contract for transfers.
func detect(event map[string]interface{}) map[string]interface{} {
	if infraSet == nil {
		resolveContracts()
	}

	schema, _ := event["_schema"].(string)
	if schema != "nebu.swap_candidate.v1" {
		return event
	}

	var matchedAddr string
	var matchedRole string

	// Check 1: infrastructure contracts in contract_addresses
	// (covers cases where router/factory appears as contractAddress)
	if addrs, ok := event["contract_addresses"].([]interface{}); ok {
		for _, addr := range addrs {
			if addrStr, ok := addr.(string); ok {
				if role, found := infraSet[addrStr]; found {
					matchedAddr = addrStr
					matchedRole = role
					break
				}
			}
		}
	}

	// Check 2: pool addresses in legs from/to
	// This is the primary detection path. Soroswap pools are the intermediary
	// that receives one token and sends another in a swap.
	if matchedAddr == "" {
		if legs, ok := event["legs"].([]interface{}); ok {
			for _, leg := range legs {
				legMap, ok := leg.(map[string]interface{})
				if !ok {
					continue
				}
				if from, ok := legMap["from"].(string); ok && poolSet[from] {
					matchedAddr = from
					matchedRole = "pool"
					break
				}
				if to, ok := legMap["to"].(string); ok && poolSet[to] {
					matchedAddr = to
					matchedRole = "pool"
					break
				}
			}
		}
	}

	// Check 3: infrastructure contracts in legs from/to and contract_address
	// (fallback — unlikely but covers edge cases)
	if matchedAddr == "" {
		if legs, ok := event["legs"].([]interface{}); ok {
			for _, leg := range legs {
				legMap, ok := leg.(map[string]interface{})
				if !ok {
					continue
				}
				for _, field := range []string{"from", "to", "contract_address"} {
					if addr, ok := legMap[field].(string); ok {
						if role, found := infraSet[addr]; found {
							matchedAddr = addr
							matchedRole = role
							break
						}
					}
				}
				if matchedAddr != "" {
					break
				}
			}
		}
	}

	if matchedAddr != "" {
		event["protocol"] = "soroswap"
		switch matchedRole {
		case "router":
			event["router_contract"] = matchedAddr
		case "pool":
			event["pool_contract"] = matchedAddr
		default:
			event["matched_contract"] = matchedAddr
		}
	}

	return event
}
