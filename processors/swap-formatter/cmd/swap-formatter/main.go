// Package main provides a standalone CLI for the swap-formatter transform processor.
//
// This processor converts nebu.dex_swap.v1 events into human-readable text
// suitable for display in block explorers like Prism.
//
// Usage:
//
//	token-transfer -q | swap-candidate -q | swap-normalizer -q | swap-formatter -q
//	token-transfer -q | swap-candidate -q | soroswap-detector -q | swap-normalizer -q | swap-formatter -q --json
package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

var (
	jsonOutput   bool
	longAddrs    bool
	addrLen      int
)

func main() {
	config := cli.TransformConfig{
		Name:        "swap-formatter",
		Description: "Format dex_swap events into human-readable text",
		Version:     version,
	}

	cli.RunTransformCLI(config, format, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output JSON with added 'display' field instead of plain text")
	cmd.Flags().BoolVar(&longAddrs, "long", false, "Show full addresses instead of truncated")
	cmd.Flags().IntVar(&addrLen, "addr-len", 4, "Characters to show at start/end of truncated addresses")
}

func format(event map[string]interface{}) map[string]interface{} {
	schema, _ := event["_schema"].(string)
	if schema != "nebu.dex_swap.v1" {
		if jsonOutput {
			return event
		}
		return nil
	}

	trader, _ := event["trader"].(string)
	soldAmount := toDecimal(event["sold_amount"], 7)
	boughtAmount := toDecimal(event["bought_amount"], 7)
	soldAsset := assetCode(event["sold_asset"])
	boughtAsset := assetCode(event["bought_asset"])
	protocol, _ := event["protocol"].(string)
	hopCount := toInt(event["hop_count"])

	addr := truncAddr(trader)

	// Build display string
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s swapped %s %s → %s %s", addr, soldAmount, soldAsset, boughtAmount, boughtAsset)

	if protocol != "" && protocol != "unknown" {
		fmt.Fprintf(&sb, " at %s", titleCase(protocol))
	}

	if hopCount > 1 {
		fmt.Fprintf(&sb, " (%d hops)", hopCount)
	}

	display := sb.String()

	if jsonOutput {
		event["display"] = display
		event["display_sold"] = soldAmount + " " + soldAsset
		event["display_bought"] = boughtAmount + " " + boughtAsset

		// Effective rate: how much bought per 1 sold
		soldF := parseStroops(event["sold_amount"])
		boughtF := parseStroops(event["bought_amount"])
		if soldF > 0 && boughtF > 0 {
			rate := boughtF / soldF
			event["effective_rate"] = formatNumber(rate, 6)
			event["display_rate"] = fmt.Sprintf("1 %s = %s %s", soldAsset, formatNumber(rate, 4), boughtAsset)
		}

		// Route: ordered asset path
		if route, ok := event["route"].([]interface{}); ok && len(route) > 0 {
			parts := make([]string, 0, len(route))
			for _, r := range route {
				if s, ok := r.(string); ok {
					parts = append(parts, s)
				}
			}
			event["display_route"] = strings.Join(parts, " → ")
		}

		return event
	}

	// Plain text mode — emit just the text line as a JSON string
	// so it stays valid in a pipeline
	return map[string]interface{}{
		"_schema": "nebu.dex_swap_display.v1",
		"display": display,
		"tx_hash": event["tx_hash"],
		"ledger_sequence": event["ledger_sequence"],
		"protocol": protocol,
	}
}

func truncAddr(addr string) string {
	if longAddrs || len(addr) <= addrLen*2+3 {
		return addr
	}
	return addr[:addrLen] + "..." + addr[len(addr)-addrLen:]
}

func assetCode(v interface{}) string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return "?"
	}
	code, _ := m["code"].(string)
	if code == "" {
		return "?"
	}
	return code
}

func toDecimal(v interface{}, decimals int) string {
	raw := ""
	switch val := v.(type) {
	case string:
		raw = val
	case float64:
		raw = strconv.FormatFloat(val, 'f', 0, 64)
	default:
		raw = fmt.Sprintf("%v", v)
	}

	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return raw
	}

	divisor := math.Pow10(decimals)
	result := f / divisor

	if result == 0 {
		return "0"
	}

	// Format with appropriate precision
	if result >= 1000 {
		return formatNumber(result, 2)
	}
	if result >= 1 {
		return formatNumber(result, 4)
	}
	if result >= 0.01 {
		return formatNumber(result, 6)
	}
	return fmt.Sprintf("%g", result)
}

func formatNumber(f float64, maxDecimals int) string {
	s := strconv.FormatFloat(f, 'f', maxDecimals, 64)
	// Trim trailing zeros after decimal point
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	// Add commas for thousands
	parts := strings.Split(s, ".")
	intPart := parts[0]
	if len(intPart) > 3 {
		var result []byte
		for i, c := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				result = append(result, ',')
			}
			result = append(result, byte(c))
		}
		intPart = string(result)
	}
	if len(parts) == 2 {
		return intPart + "." + parts[1]
	}
	return intPart
}

func parseStroops(v interface{}) float64 {
	raw := ""
	switch val := v.(type) {
	case string:
		raw = val
	case float64:
		return val / 1e7
	default:
		raw = fmt.Sprintf("%v", v)
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return f / 1e7
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	}
	return 0
}

func titleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
