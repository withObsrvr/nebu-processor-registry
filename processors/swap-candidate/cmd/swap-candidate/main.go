// Package main provides a standalone CLI for the swap-candidate transform processor.
//
// This processor buffers token-transfer events by tx_hash, detects swap patterns
// (2+ counter-directional transfers through a common "pivot" address), and emits
// swap candidate events.
//
// Uses a custom stdin loop (not RunTransformCLI) because it must buffer events
// by tx_hash and flush on ledger boundaries + EOF.
//
// Usage:
//
//	token-transfer --start-ledger 60200000 | swap-candidate -q
//	token-transfer --start-ledger 60200000 | swap-candidate --min-transfers 3
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var version = "0.1.0"

var (
	minTransfers int
	quietMode    bool
)

// transferLeg represents a single token transfer within a transaction.
type transferLeg struct {
	From            string      `json:"from"`
	To              string      `json:"to"`
	Asset           assetInfo   `json:"asset"`
	Amount          string      `json:"amount"`
	ContractAddress string      `json:"contract_address,omitempty"`
}

type assetInfo struct {
	Code   string `json:"code"`
	Issuer string `json:"issuer,omitempty"`
}

// txGroup holds buffered transfers for a single transaction.
type txGroup struct {
	txHash         string
	ledgerSequence float64
	timestampUnix  float64
	inSuccessfulTx bool
	legs           []transferLeg
	contracts      map[string]bool
}

func main() {
	rootCmd := &cobra.Command{
		Use:     "swap-candidate",
		Short:   "Detect swap patterns in token transfer events",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run()
		},
	}

	rootCmd.Flags().IntVar(&minTransfers, "min-transfers", 2, "Minimum transfers per tx to consider as swap candidate")
	rootCmd.Flags().BoolVarP(&quietMode, "quiet", "q", false, "Suppress non-error output")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		if !quietMode {
			fmt.Fprintln(os.Stderr, "\nShutting down...")
		}
		os.Exit(0)
	}()

	if !quietMode {
		fmt.Fprintln(os.Stderr, "Reading token-transfer events from stdin...")
	}

	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer for large events
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	encoder := json.NewEncoder(os.Stdout)

	// Buffer: tx_hash -> txGroup
	txBuffer := make(map[string]*txGroup)
	var currentLedger float64
	eventCount := 0
	outputCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			if !quietMode {
				fmt.Fprintf(os.Stderr, "Warning: failed to parse JSON: %v\n", err)
			}
			continue
		}

		eventCount++

		// Extract fields from event
		txHash, ledgerSeq, ts, inSuccessful, leg, isFee := extractTransferFields(event)
		if txHash == "" || isFee {
			continue
		}

		// Detect ledger boundary — flush previous ledger's buffer
		if ledgerSeq != currentLedger && currentLedger != 0 {
			outputCount += flushBuffer(txBuffer, encoder)
		}
		currentLedger = ledgerSeq

		// Add to buffer
		group, exists := txBuffer[txHash]
		if !exists {
			group = &txGroup{
				txHash:         txHash,
				ledgerSequence: ledgerSeq,
				timestampUnix:  ts,
				inSuccessfulTx: inSuccessful,
				contracts:      make(map[string]bool),
			}
			txBuffer[txHash] = group
		}
		if leg != nil {
			group.legs = append(group.legs, *leg)
			if leg.ContractAddress != "" {
				group.contracts[leg.ContractAddress] = true
			}
		}

		if !quietMode && eventCount%1000 == 0 {
			fmt.Fprintf(os.Stderr, "Processed %d events (%d candidates)...\n", eventCount, outputCount)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stdin: %w", err)
	}

	// EOF flush: drain remaining buffers
	outputCount += flushBuffer(txBuffer, encoder)

	if !quietMode {
		fmt.Fprintf(os.Stderr, "Processed %d events (%d candidates)\n", eventCount, outputCount)
	}

	return nil
}

// flushBuffer processes all buffered tx groups, emits swap candidates, and clears the buffer.
func flushBuffer(txBuffer map[string]*txGroup, encoder *json.Encoder) int {
	count := 0
	for txHash, group := range txBuffer {
		if len(group.legs) >= minTransfers {
			if candidate := detectSwap(group); candidate != nil {
				if err := encoder.Encode(candidate); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding output: %v\n", err)
				} else {
					count++
				}
			}
		}
		delete(txBuffer, txHash)
	}
	return count
}

// detectSwap checks if a tx group contains a swap pattern.
// A swap is detected when any address has both inbound and outbound transfers
// with different assets.
func detectSwap(group *txGroup) map[string]interface{} {
	// Build address -> directions map
	type direction struct {
		assetKey  string
		direction string // "in" or "out"
	}

	addressDirs := make(map[string][]direction)

	for _, leg := range group.legs {
		assetKey := leg.Asset.Code
		if leg.Asset.Issuer != "" {
			assetKey += ":" + leg.Asset.Issuer
		}

		// "from" address has an outbound transfer
		addressDirs[leg.From] = append(addressDirs[leg.From], direction{assetKey: assetKey, direction: "out"})
		// "to" address has an inbound transfer
		addressDirs[leg.To] = append(addressDirs[leg.To], direction{assetKey: assetKey, direction: "in"})
	}

	// Find pivot: an address that has both inbound and outbound with different assets
	var pivotAddress string
	for addr, dirs := range addressDirs {
		inAssets := make(map[string]bool)
		outAssets := make(map[string]bool)

		for _, d := range dirs {
			if d.direction == "in" {
				inAssets[d.assetKey] = true
			} else {
				outAssets[d.assetKey] = true
			}
		}

		if len(inAssets) == 0 || len(outAssets) == 0 {
			continue
		}

		// Check if any inbound asset differs from any outbound asset
		for inAsset := range inAssets {
			for outAsset := range outAssets {
				if inAsset != outAsset {
					pivotAddress = addr
					break
				}
			}
			if pivotAddress != "" {
				break
			}
		}
		if pivotAddress != "" {
			break
		}
	}

	if pivotAddress == "" {
		return nil
	}

	// Build output
	legs := make([]map[string]interface{}, 0, len(group.legs))
	for _, leg := range group.legs {
		legMap := map[string]interface{}{
			"from":   leg.From,
			"to":     leg.To,
			"asset":  buildAssetMap(leg.Asset),
			"amount": leg.Amount,
		}
		if leg.ContractAddress != "" {
			legMap["contract_address"] = leg.ContractAddress
		}
		legs = append(legs, legMap)
	}

	contracts := make([]string, 0, len(group.contracts))
	for c := range group.contracts {
		contracts = append(contracts, c)
	}

	// Count hops: number of intermediate addresses between trader's out and in
	hopCount := len(group.legs) - 1
	if hopCount < 1 {
		hopCount = 1
	}

	return map[string]interface{}{
		"_schema":            "nebu.swap_candidate.v1",
		"_nebu_version":      "1.0.0",
		"ledger_sequence":    group.ledgerSequence,
		"tx_hash":            group.txHash,
		"timestamp_unix":     group.timestampUnix,
		"pivot_address":      pivotAddress,
		"in_successful_tx":   group.inSuccessfulTx,
		"legs":               legs,
		"hop_count":          hopCount,
		"contract_addresses": contracts,
	}
}

func buildAssetMap(a assetInfo) map[string]interface{} {
	m := map[string]interface{}{"code": a.Code}
	if a.Issuer != "" {
		m["issuer"] = a.Issuer
	}
	return m
}

// extractTransferFields extracts relevant fields from a token-transfer event.
// Handles both protojson format (from RunProtoOriginCLI) and flat SCHEMA.md format.
func extractTransferFields(event map[string]interface{}) (txHash string, ledgerSeq, timestamp float64, inSuccessful bool, leg *transferLeg, isFee bool) {
	inSuccessful = true // default

	// --- Extract metadata ---
	// Protojson format: meta.txHash, meta.ledgerSequence, meta.closedAtUnix, meta.contractAddress
	var contractAddress string
	if meta, ok := event["meta"].(map[string]interface{}); ok {
		txHash = toString(meta["txHash"])
		if txHash == "" {
			txHash = toString(meta["tx_hash"])
		}
		ledgerSeq = toFloat64(meta["ledgerSequence"])
		if ledgerSeq == 0 {
			ledgerSeq = toFloat64(meta["ledger_sequence"])
		}
		timestamp = toFloat64(meta["closedAtUnix"])
		if timestamp == 0 {
			timestamp = toFloat64(meta["closed_at_unix"])
		}
		contractAddress = toString(meta["contractAddress"])
		if contractAddress == "" {
			contractAddress = toString(meta["contract_address"])
		}
		if v, ok := meta["inSuccessfulTx"]; ok {
			inSuccessful = toBool(v)
		} else if v, ok := meta["in_successful_tx"]; ok {
			inSuccessful = toBool(v)
		}
	}

	// Flat format: top-level tx_hash, ledger_sequence
	if txHash == "" {
		txHash = toString(event["tx_hash"])
	}
	if ledgerSeq == 0 {
		ledgerSeq = toFloat64(event["ledger_sequence"])
	}
	if timestamp == 0 {
		timestamp = toFloat64(event["timestamp_unix"])
	}
	if contractAddress == "" {
		contractAddress = toString(event["contract_address"])
	}

	if txHash == "" {
		return
	}

	// --- Detect event type and extract transfer leg ---

	// Check for fee events — skip them
	if _, ok := event["fee"]; ok {
		isFee = true
		return
	}
	if eventType, ok := event["type"].(string); ok && eventType == "fee" {
		isFee = true
		return
	}

	// Try protojson format: event["transfer"], event["mint"], etc.
	if transfer, ok := event["transfer"].(map[string]interface{}); ok {
		leg = &transferLeg{
			From:            toString(transfer["from"]),
			To:              toString(transfer["to"]),
			Amount:          toString(transfer["amount"]),
			ContractAddress: contractAddress,
		}
		leg.Asset = extractAsset(transfer, event)
		return
	}

	// Mint events: to address receives tokens (from is implicit — issuer/contract)
	if mint, ok := event["mint"].(map[string]interface{}); ok {
		leg = &transferLeg{
			From:            contractAddress, // Mints come from the contract
			To:              toString(mint["to"]),
			Amount:          toString(mint["amount"]),
			ContractAddress: contractAddress,
		}
		leg.Asset = extractAsset(mint, event)
		return
	}

	// Burn events: from address sends tokens (to is implicit — destroyed)
	if burn, ok := event["burn"].(map[string]interface{}); ok {
		leg = &transferLeg{
			From:            toString(burn["from"]),
			To:              contractAddress, // Burns go to the contract
			Amount:          toString(burn["amount"]),
			ContractAddress: contractAddress,
		}
		leg.Asset = extractAsset(burn, event)
		return
	}

	// Flat format: top-level from, to, amount with type == "transfer"
	if eventType, ok := event["type"].(string); ok && eventType == "transfer" {
		leg = &transferLeg{
			From:            toString(event["from"]),
			To:              toString(event["to"]),
			Amount:          toString(event["amount"]),
			ContractAddress: contractAddress,
		}
		leg.Asset = extractAssetFlat(event)
		return
	}

	return
}

// extractAsset extracts asset info from a protojson event field.
// Handles multiple formats:
//   - Protojson flat: assetCode, assetIssuer on the event type object
//   - Nested: asset.issuedAsset.assetCode (asset-filter format)
//   - Flat SCHEMA.md: asset.code, asset.issuer on the event
func extractAsset(eventTypeObj map[string]interface{}, event map[string]interface{}) assetInfo {
	// Format 1: Protojson flat fields on the event type object (transfer.assetCode)
	if code := toString(eventTypeObj["assetCode"]); code != "" {
		return assetInfo{Code: code, Issuer: toString(eventTypeObj["assetIssuer"])}
	}
	if code := toString(eventTypeObj["asset_code"]); code != "" {
		return assetInfo{Code: code, Issuer: toString(eventTypeObj["asset_issuer"])}
	}

	// Format 2: Nested asset object on the event type (transfer.asset.issuedAsset.assetCode)
	if asset, ok := eventTypeObj["asset"].(map[string]interface{}); ok {
		// Check for issuedAsset wrapper
		if issued, ok := asset["issuedAsset"].(map[string]interface{}); ok {
			return assetInfo{Code: toString(issued["assetCode"]), Issuer: toString(issued["issuer"])}
		}
		// Check for native
		if _, ok := asset["native"]; ok {
			return assetInfo{Code: "XLM"}
		}
		// Direct code/issuer on asset object
		if code := toString(asset["code"]); code != "" {
			return assetInfo{Code: code, Issuer: toString(asset["issuer"])}
		}
	}

	// Format 3: Top-level asset object on the event (event.asset.code)
	if asset, ok := event["asset"].(map[string]interface{}); ok {
		if code := toString(asset["code"]); code != "" {
			return assetInfo{Code: code, Issuer: toString(asset["issuer"])}
		}
	}

	return assetInfo{}
}

// extractAssetFlat extracts asset info from the flat SCHEMA.md format.
func extractAssetFlat(event map[string]interface{}) assetInfo {
	if asset, ok := event["asset"].(map[string]interface{}); ok {
		return assetInfo{
			Code:   toString(asset["code"]),
			Issuer: toString(asset["issuer"]),
		}
	}
	return assetInfo{}
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case json.Number:
		f, _ := val.Float64()
		return f
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	}
	return 0
}

func toBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true"
	}
	return false
}
