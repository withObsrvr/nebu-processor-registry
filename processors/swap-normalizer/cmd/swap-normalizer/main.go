// Package main provides a standalone CLI for the swap-normalizer transform processor.
//
// This processor normalizes swap-candidate events into a unified nebu.dex_swap.v1
// schema for downstream consumers.
//
// Usage:
//
//	token-transfer | swap-candidate | soroswap-detector | swap-normalizer -q
package main

import (
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

func main() {
	config := cli.TransformConfig{
		Name:        "swap-normalizer",
		Description: "Normalize swap candidates into unified dex_swap events",
		Version:     version,
	}

	cli.RunTransformCLI(config, normalize, nil)
}

// normalize converts a swap-candidate event into a dex_swap event.
func normalize(event map[string]interface{}) map[string]interface{} {
	schema, _ := event["_schema"].(string)
	if schema != "nebu.swap_candidate.v1" {
		return event // Pass through non-candidates
	}

	pivotAddress, _ := event["pivot_address"].(string)
	if pivotAddress == "" {
		return nil // Invalid candidate
	}

	legs, ok := event["legs"].([]interface{})
	if !ok || len(legs) < 2 {
		return nil // Need at least 2 legs
	}

	// Find sold asset (what the pivot sent out) and bought asset (what the pivot received)
	var soldAsset map[string]interface{}
	var soldAmount string
	var boughtAsset map[string]interface{}
	var boughtAmount string

	for _, legRaw := range legs {
		leg, ok := legRaw.(map[string]interface{})
		if !ok {
			continue
		}

		from, _ := leg["from"].(string)
		to, _ := leg["to"].(string)
		asset, _ := leg["asset"].(map[string]interface{})
		amount, _ := leg["amount"].(string)

		if from == pivotAddress && soldAsset == nil {
			// Pivot's first outbound = sold
			soldAsset = copyAsset(asset)
			soldAmount = amount
		}
		if to == pivotAddress {
			// Pivot's last inbound = bought (keep overwriting)
			boughtAsset = copyAsset(asset)
			boughtAmount = amount
		}
	}

	if soldAsset == nil || boughtAsset == nil {
		return nil
	}

	// Build normalized output
	result := map[string]interface{}{
		"_schema":          "nebu.dex_swap.v1",
		"_nebu_version":    "1.0.0",
		"ledger_sequence":  event["ledger_sequence"],
		"tx_hash":          event["tx_hash"],
		"timestamp_unix":   event["timestamp_unix"],
		"trader":           pivotAddress,
		"sold_asset":       soldAsset,
		"sold_amount":      soldAmount,
		"bought_asset":     boughtAsset,
		"bought_amount":    boughtAmount,
		"in_successful_tx": event["in_successful_tx"],
		"hop_count":        event["hop_count"],
	}

	// Carry forward protocol attribution
	if protocol, ok := event["protocol"].(string); ok {
		result["protocol"] = protocol
	} else {
		result["protocol"] = "unknown"
	}

	if router, ok := event["router_contract"].(string); ok {
		result["router_contract"] = router
	}

	return result
}

func copyAsset(asset map[string]interface{}) map[string]interface{} {
	if asset == nil {
		return nil
	}
	result := make(map[string]interface{})
	for k, v := range asset {
		result[k] = v
	}
	return result
}
