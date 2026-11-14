// Package main provides a standalone CLI for the trade-extractor origin
// processor.
//
// It extracts classic DEX trades from ManageBuyOffer, ManageSellOffer,
// CreatePassiveSellOffer and PathPayment operations by reading the ClaimAtoms
// their results record.
//
// Usage:
//
//	trade-extractor --last 100
//	trade-extractor -q --start-ledger 61696300 --end-ledger 61696400
//	nebu fetch 61696300 61696400 | trade-extractor -q
//
// The third line is in this comment because it has always been here, and it
// did not work: the hand-rolled command this replaces created an RPC source
// unconditionally and required --start-ledger, so piping ledgers in did
// nothing. Going through the shared origin CLI makes all three true, and
// brings --last, the execution policy, coverage reporting and the status
// stream with it.
package main

import (
	trade_extractor "github.com/withObsrvr/nebu-processor-registry/processors/trade-extractor"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.2.0"

func main() {
	config := cli.OriginConfig{
		Name:        "trade-extractor",
		Description: "Extract classic DEX trades from Stellar ledgers",
		Version:     version,
		SchemaID:    trade_extractor.SchemaID,
	}

	cli.RunGenericOriginCLI(config, func(networkPass string) cli.GenericOriginProcessor[trade_extractor.TradeEvent] {
		return trade_extractor.NewTradesOrigin(networkPass)
	})
}
