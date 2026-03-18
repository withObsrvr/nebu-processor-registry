// Package main provides a standalone CLI for the aquarius-detector transform processor.
//
// This processor checks swap-candidate events for the Aquarius DEX router
// address and tags matches with protocol: "aquarius".
//
// Aquarius uses a monolithic router (single contract handles all swaps),
// so there are no separate pool contracts to track. Detection checks
// legs[*].from and legs[*].to against the router address.
//
// Usage:
//
//	token-transfer | swap-candidate | aquarius-detector -q --network mainnet
package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

var network string

// Known Aquarius router contracts by network.
// Aquarius uses a monolithic router — no factory or pool contracts needed.
var aquariusContracts = map[string]string{
	"mainnet": "CBQDHNBFBZYE4MKPWBSJOPIYLW4SFSXAXUTSXJN76GNKYVYPCKWC6QUK",
	"testnet": "CDGX6Q3ZZIDSX2N3SHBORWUIEG2ZZEBAAMYARAXTT7M5L6IXKNJMT3GB",
}

// routerAddr holds the resolved router address for the selected network.
var routerAddr string

func main() {
	config := cli.TransformConfig{
		Name:        "aquarius-detector",
		Description: "Detect Aquarius DEX swaps in swap-candidate events",
		Version:     version,
	}

	cli.RunTransformCLI(config, detect, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&network, "network", "mainnet", "Network for router contract (testnet|mainnet)")

	origPreRun := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if origPreRun != nil {
			if err := origPreRun(cmd, args); err != nil {
				return err
			}
		}
		addr, ok := aquariusContracts[network]
		if !ok {
			return fmt.Errorf("unsupported network %q; supported networks: testnet, mainnet", network)
		}
		routerAddr = addr
		return nil
	}
}

// detect checks a swap-candidate event for the Aquarius router address.
// The router appears as from/to in transfer legs because it is the intermediary
// that receives one token and sends another in a swap.
func detect(event map[string]interface{}) map[string]interface{} {
	if routerAddr == "" {
		routerAddr = aquariusContracts[network]
	}

	schema, _ := event["_schema"].(string)
	if schema != "nebu.swap_candidate.v1" {
		return event
	}

	matched := false

	// Check 1: router in contract_addresses
	if addrs, ok := event["contract_addresses"].([]interface{}); ok {
		for _, addr := range addrs {
			if addrStr, ok := addr.(string); ok && addrStr == routerAddr {
				matched = true
				break
			}
		}
	}

	// Check 2: router in legs from/to
	if !matched {
		if legs, ok := event["legs"].([]interface{}); ok {
			for _, leg := range legs {
				legMap, ok := leg.(map[string]interface{})
				if !ok {
					continue
				}
				if from, ok := legMap["from"].(string); ok && from == routerAddr {
					matched = true
					break
				}
				if to, ok := legMap["to"].(string); ok && to == routerAddr {
					matched = true
					break
				}
			}
		}
	}

	// Check 3: router in legs contract_address (fallback)
	if !matched {
		if legs, ok := event["legs"].([]interface{}); ok {
			for _, leg := range legs {
				legMap, ok := leg.(map[string]interface{})
				if !ok {
					continue
				}
				if addr, ok := legMap["contract_address"].(string); ok && addr == routerAddr {
					matched = true
					break
				}
			}
		}
	}

	if matched {
		event["protocol"] = "aquarius"
		event["router_contract"] = routerAddr
	}

	return event
}
