// Package main provides a standalone CLI for the asset-filter transform processor.
//
// This processor filters token transfer events by asset code and optionally issuer.
// It generalizes usdc-filter to work with any Stellar asset.
//
// Usage:
//
//	# Filter for AQUA transfers
//	token-transfer --start-ledger 60200000 | asset-filter --code AQUA
//
//	# Filter for XLM (native asset)
//	token-transfer --start-ledger 60200000 | asset-filter --native
//
//	# Filter by code and issuer
//	token-transfer --start-ledger 60200000 | asset-filter --code USDC --issuer GA5Z...
package main

import (
	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

var (
	assetCodes  []string
	assetIssuer string
	nativeOnly  bool
)

func main() {
	config := cli.TransformConfig{
		Name:        "asset-filter",
		Description: "Filter token transfer events by asset code",
		Version:     version,
	}

	cli.RunTransformCLI(config, filterByAsset, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&assetCodes, "code", nil, "Asset code to filter for (repeatable, e.g., USDC, AQUA)")
	cmd.Flags().StringVar(&assetIssuer, "issuer", "", "Asset issuer to filter for (optional, narrows match)")
	cmd.Flags().BoolVar(&nativeOnly, "native", false, "Filter for native XLM only")
}

// filterByAsset filters events to only include those matching specified assets.
func filterByAsset(event map[string]interface{}) map[string]interface{} {
	// Build a set of codes to match
	codeSet := make(map[string]bool, len(assetCodes))
	for _, c := range assetCodes {
		codeSet[c] = true
	}

	// Check each event type that carries asset info
	for _, key := range []string{"transfer", "mint", "burn", "clawback"} {
		nested, ok := event[key].(map[string]interface{})
		if !ok {
			continue
		}

		asset, ok := nested["asset"].(map[string]interface{})
		if !ok {
			continue
		}

		if matchesAsset(asset, codeSet) {
			return event
		}
	}

	return nil
}

func matchesAsset(asset map[string]interface{}, codeSet map[string]bool) bool {
	// Check for native XLM
	if nativeOnly {
		if native, ok := asset["native"].(map[string]interface{}); ok && native != nil {
			return true
		}
		// Also check type field
		if assetType, ok := asset["type"].(string); ok && assetType == "native" {
			return true
		}
		return false
	}

	// No codes specified and not native-only means pass everything through
	if len(codeSet) == 0 && !nativeOnly {
		return true
	}

	// Check issued asset (protojson format: asset.issuedAsset.assetCode)
	issuedAsset, ok := asset["issuedAsset"].(map[string]interface{})
	if !ok {
		return false
	}

	code, ok := issuedAsset["assetCode"].(string)
	if !ok {
		return false
	}

	if !codeSet[code] {
		return false
	}

	// Optionally check issuer
	if assetIssuer != "" {
		issuer, ok := issuedAsset["issuer"].(string)
		if !ok || issuer != assetIssuer {
			return false
		}
	}

	return true
}
