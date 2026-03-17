package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// helper to build a protojson token-transfer event
func makeTransferEvent(ledgerSeq int, txHash, from, to, assetCode, assetIssuer, amount, contractAddr string, inSuccessful bool) string {
	event := map[string]interface{}{
		"_schema":       "nebu.token_transfer.v1",
		"_nebu_version": "0.3.0",
		"meta": map[string]interface{}{
			"ledgerSequence":  ledgerSeq,
			"closedAtUnix":    "1707123456",
			"txHash":          txHash,
			"transactionIndex": 0,
			"operationIndex":  0,
			"contractAddress": contractAddr,
			"inSuccessfulTx":  inSuccessful,
		},
		"transfer": map[string]interface{}{
			"from":        from,
			"to":          to,
			"assetCode":   assetCode,
			"assetIssuer": assetIssuer,
			"amount":      amount,
		},
	}
	b, _ := json.Marshal(event)
	return string(b)
}

func makeFeeEvent(ledgerSeq int, txHash, from, amount string) string {
	event := map[string]interface{}{
		"_schema":       "nebu.token_transfer.v1",
		"_nebu_version": "0.3.0",
		"meta": map[string]interface{}{
			"ledgerSequence":  ledgerSeq,
			"closedAtUnix":    "1707123456",
			"txHash":          txHash,
			"transactionIndex": 0,
			"operationIndex":  0,
			"contractAddress": "",
			"inSuccessfulTx":  true,
		},
		"fee": map[string]interface{}{
			"from":        from,
			"assetCode":   "XLM",
			"assetIssuer": "",
			"amount":      amount,
		},
	}
	b, _ := json.Marshal(event)
	return string(b)
}

func makeFlatTransferEvent(ledgerSeq int, txHash, from, to, assetCode, assetIssuer, amount string) string {
	event := map[string]interface{}{
		"_schema":          "nebu.token_transfer.v1",
		"_nebu_version":    "0.3.0",
		"ledger_sequence":  ledgerSeq,
		"tx_hash":          txHash,
		"type":             "transfer",
		"from":             from,
		"to":               to,
		"amount":           amount,
		"contract_address": "",
		"asset": map[string]interface{}{
			"code":   assetCode,
			"issuer": assetIssuer,
		},
	}
	b, _ := json.Marshal(event)
	return string(b)
}

func TestDetectSwap_SimpleSwap(t *testing.T) {
	// A simple swap: TRADER sends USDC to POOL, POOL sends XLM to TRADER
	group := &txGroup{
		txHash:         "tx1",
		ledgerSequence: 60200000,
		timestampUnix:  1707123456,
		inSuccessfulTx: true,
		contracts:      map[string]bool{"CA_POOL": true},
		legs: []transferLeg{
			{From: "TRADER", To: "POOL", Asset: assetInfo{Code: "USDC", Issuer: "GA_ISSUER"}, Amount: "1000000000", ContractAddress: "CA_POOL"},
			{From: "POOL", To: "TRADER", Asset: assetInfo{Code: "XLM"}, Amount: "500000000", ContractAddress: "CA_POOL"},
		},
	}

	result := detectSwap(group)
	if result == nil {
		t.Fatal("expected swap candidate, got nil")
	}

	if result["_schema"] != "nebu.swap_candidate.v1" {
		t.Errorf("expected schema nebu.swap_candidate.v1, got %v", result["_schema"])
	}
	if result["pivot_address"] != "TRADER" {
		t.Errorf("expected pivot TRADER, got %v", result["pivot_address"])
	}
	if result["tx_hash"] != "tx1" {
		t.Errorf("expected tx_hash tx1, got %v", result["tx_hash"])
	}

	legs := result["legs"].([]map[string]interface{})
	if len(legs) != 2 {
		t.Errorf("expected 2 legs, got %d", len(legs))
	}
}

func TestDetectSwap_MultiHop(t *testing.T) {
	// Multi-hop: TRADER → POOL_A → POOL_B → TRADER
	group := &txGroup{
		txHash:         "tx2",
		ledgerSequence: 60200001,
		timestampUnix:  1707123460,
		inSuccessfulTx: true,
		contracts:      map[string]bool{"CA_A": true, "CA_B": true},
		legs: []transferLeg{
			{From: "TRADER", To: "POOL_A", Asset: assetInfo{Code: "USDC", Issuer: "GA_ISSUER"}, Amount: "1000000000"},
			{From: "POOL_A", To: "POOL_B", Asset: assetInfo{Code: "INTERMEDIATE"}, Amount: "2000000000"},
			{From: "POOL_B", To: "TRADER", Asset: assetInfo{Code: "XLM"}, Amount: "500000000"},
		},
	}

	result := detectSwap(group)
	if result == nil {
		t.Fatal("expected swap candidate, got nil")
	}

	// TRADER should be pivot (sends USDC, receives XLM)
	if result["pivot_address"] != "TRADER" {
		t.Errorf("expected pivot TRADER, got %v", result["pivot_address"])
	}

	legs := result["legs"].([]map[string]interface{})
	if len(legs) != 3 {
		t.Errorf("expected 3 legs, got %d", len(legs))
	}

	hopCount := result["hop_count"].(int)
	if hopCount != 2 {
		t.Errorf("expected hop_count 2, got %d", hopCount)
	}
}

func TestDetectSwap_NotASwap(t *testing.T) {
	// Two transfers with same asset — not a swap
	group := &txGroup{
		txHash:         "tx3",
		ledgerSequence: 60200002,
		timestampUnix:  1707123470,
		inSuccessfulTx: true,
		contracts:      map[string]bool{},
		legs: []transferLeg{
			{From: "ALICE", To: "BOB", Asset: assetInfo{Code: "USDC", Issuer: "GA_ISSUER"}, Amount: "1000000000"},
			{From: "BOB", To: "CHARLIE", Asset: assetInfo{Code: "USDC", Issuer: "GA_ISSUER"}, Amount: "500000000"},
		},
	}

	result := detectSwap(group)
	if result != nil {
		t.Error("expected nil for non-swap, got result")
	}
}

func TestDetectSwap_SingleTransfer(t *testing.T) {
	// Only one transfer — below min-transfers threshold
	group := &txGroup{
		txHash:         "tx4",
		ledgerSequence: 60200003,
		timestampUnix:  1707123480,
		inSuccessfulTx: true,
		contracts:      map[string]bool{},
		legs: []transferLeg{
			{From: "ALICE", To: "BOB", Asset: assetInfo{Code: "USDC", Issuer: "GA_ISSUER"}, Amount: "1000000000"},
		},
	}

	// With 2+ transfers required, single transfer should not even be checked
	// But if it is, detectSwap should return nil (no pivot found)
	result := detectSwap(group)
	if result != nil {
		t.Error("expected nil for single transfer, got result")
	}
}

func TestExtractTransferFields_Protojson(t *testing.T) {
	input := makeTransferEvent(60200000, "txhash1", "FROM_ADDR", "TO_ADDR", "USDC", "GA_ISSUER", "1000", "CA_CONTRACT", true)

	var event map[string]interface{}
	json.Unmarshal([]byte(input), &event)

	txHash, ledgerSeq, ts, inSuccessful, leg, isFee := extractTransferFields(event)

	if txHash != "txhash1" {
		t.Errorf("expected txhash1, got %s", txHash)
	}
	if ledgerSeq != 60200000 {
		t.Errorf("expected ledger 60200000, got %f", ledgerSeq)
	}
	if ts == 0 {
		t.Error("expected non-zero timestamp")
	}
	if !inSuccessful {
		t.Error("expected inSuccessfulTx=true")
	}
	if isFee {
		t.Error("expected isFee=false")
	}
	if leg == nil {
		t.Fatal("expected non-nil leg")
	}
	if leg.From != "FROM_ADDR" {
		t.Errorf("expected FROM_ADDR, got %s", leg.From)
	}
	if leg.To != "TO_ADDR" {
		t.Errorf("expected TO_ADDR, got %s", leg.To)
	}
	if leg.Asset.Code != "USDC" {
		t.Errorf("expected USDC, got %s", leg.Asset.Code)
	}
	if leg.Asset.Issuer != "GA_ISSUER" {
		t.Errorf("expected GA_ISSUER, got %s", leg.Asset.Issuer)
	}
	if leg.ContractAddress != "CA_CONTRACT" {
		t.Errorf("expected CA_CONTRACT, got %s", leg.ContractAddress)
	}
}

func TestExtractTransferFields_FlatFormat(t *testing.T) {
	input := makeFlatTransferEvent(60200000, "txhash2", "FROM_ADDR", "TO_ADDR", "XLM", "", "5000")

	var event map[string]interface{}
	json.Unmarshal([]byte(input), &event)

	txHash, ledgerSeq, _, _, leg, isFee := extractTransferFields(event)

	if txHash != "txhash2" {
		t.Errorf("expected txhash2, got %s", txHash)
	}
	if ledgerSeq != 60200000 {
		t.Errorf("expected ledger 60200000, got %f", ledgerSeq)
	}
	if isFee {
		t.Error("expected isFee=false")
	}
	if leg == nil {
		t.Fatal("expected non-nil leg")
	}
	if leg.Asset.Code != "XLM" {
		t.Errorf("expected XLM, got %s", leg.Asset.Code)
	}
}

func TestExtractTransferFields_Fee(t *testing.T) {
	input := makeFeeEvent(60200000, "txhash3", "ALICE", "100")

	var event map[string]interface{}
	json.Unmarshal([]byte(input), &event)

	_, _, _, _, _, isFee := extractTransferFields(event)

	if !isFee {
		t.Error("expected isFee=true for fee event")
	}
}

func TestFlushBuffer_EmitsOnlySwaps(t *testing.T) {
	minTransfers = 2

	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)

	txBuffer := map[string]*txGroup{
		"swap_tx": {
			txHash:         "swap_tx",
			ledgerSequence: 100,
			timestampUnix:  1000,
			inSuccessfulTx: true,
			contracts:      map[string]bool{},
			legs: []transferLeg{
				{From: "TRADER", To: "POOL", Asset: assetInfo{Code: "USDC", Issuer: "GA"}, Amount: "100"},
				{From: "POOL", To: "TRADER", Asset: assetInfo{Code: "XLM"}, Amount: "200"},
			},
		},
		"non_swap_tx": {
			txHash:         "non_swap_tx",
			ledgerSequence: 100,
			timestampUnix:  1000,
			inSuccessfulTx: true,
			contracts:      map[string]bool{},
			legs: []transferLeg{
				{From: "A", To: "B", Asset: assetInfo{Code: "USDC", Issuer: "GA"}, Amount: "100"},
				{From: "B", To: "C", Asset: assetInfo{Code: "USDC", Issuer: "GA"}, Amount: "100"},
			},
		},
	}

	count := flushBuffer(txBuffer, encoder)

	if count != 1 {
		t.Errorf("expected 1 candidate emitted, got %d", count)
	}

	// Verify the output
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 output line, got %d", len(lines))
	}

	var output map[string]interface{}
	json.Unmarshal([]byte(lines[0]), &output)

	if output["_schema"] != "nebu.swap_candidate.v1" {
		t.Errorf("expected schema nebu.swap_candidate.v1, got %v", output["_schema"])
	}
	if output["tx_hash"] != "swap_tx" {
		t.Errorf("expected tx_hash swap_tx, got %v", output["tx_hash"])
	}

	// Buffer should be empty after flush
	if len(txBuffer) != 0 {
		t.Errorf("expected empty buffer after flush, got %d entries", len(txBuffer))
	}
}

func TestDetectSwap_FailedTransaction(t *testing.T) {
	// Failed tx swaps should still be detected but marked
	group := &txGroup{
		txHash:         "failed_tx",
		ledgerSequence: 60200005,
		timestampUnix:  1707123500,
		inSuccessfulTx: false,
		contracts:      map[string]bool{},
		legs: []transferLeg{
			{From: "TRADER", To: "POOL", Asset: assetInfo{Code: "USDC", Issuer: "GA"}, Amount: "100"},
			{From: "POOL", To: "TRADER", Asset: assetInfo{Code: "XLM"}, Amount: "200"},
		},
	}

	result := detectSwap(group)
	if result == nil {
		t.Fatal("expected swap candidate for failed tx")
	}

	if result["in_successful_tx"] != false {
		t.Error("expected in_successful_tx=false")
	}
}
