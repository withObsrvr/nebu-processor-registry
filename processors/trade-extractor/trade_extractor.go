// Package trade_extractor provides an origin processor that extracts classic
// DEX trades from Stellar ledgers.
//
// Trades are read out of operation *results* rather than operations: a
// ManageBuyOffer, ManageSellOffer, CreatePassiveSellOffer or PathPayment
// operation records what it actually matched as ClaimAtoms, and a ClaimAtom is
// one trade against one counterparty. An operation can match many offers, so
// one operation can produce many trades.
//
// The extraction used to live in the command's main package alongside a
// hand-rolled cobra command, which is why trade-extractor had no --last, no
// stdin or file input, no execution policy and no coverage reporting while
// every other origin had all four. It lives here so the command can be the
// same twenty lines every other origin's is.
package trade_extractor

import (
	"context"
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
	"github.com/withObsrvr/nebu/pkg/processor/ledgerread"
)

// SchemaID identifies the events this processor emits. It is the identifier
// in description.yml and SCHEMA.md, and the one the events themselves have
// always carried; the --describe-json envelope used to report
// nebu.trade_extractor.v1, which no event has ever had. Naming it once here
// means the describe envelope and the emitted events cannot disagree again.
const SchemaID = "nebu.dex_trade.v1"

// Asset is an asset as it appears on a trade: XLM with no issuer, or a code
// and the issuer that controls it.
type Asset struct {
	Code   string `json:"code"`
	Issuer string `json:"issuer,omitempty"`
}

// TradeEvent is one claimed offer: one counterparty, one asset pair, one
// direction.
//
// It deliberately has no _schema or _nebu_version field. The CLI wraps every
// event with those, and with the network, from one place; the struct used to
// carry its own and set _nebu_version to the *processor's* version, so every
// trade reported "0.1.0" where the field names the nebu that produced it.
type TradeEvent struct {
	LedgerSequence uint32 `json:"ledger_sequence"`
	TimestampUnix  int64  `json:"timestamp_unix"`
	// Timestamp is the close time at the resolution Protocol 30 records it.
	// timestamp_unix stays for compatibility, but whole seconds cannot order
	// two trades in the same ledger, and trades are exactly the events whose
	// order matters.
	Timestamp      string `json:"timestamp"`
	TxHash         string `json:"tx_hash"`
	OpIndex        int    `json:"operation_index"`
	TradeType      string `json:"trade_type"`
	Seller         string `json:"seller"`
	Buyer          string `json:"buyer"`
	SoldAsset      Asset  `json:"sold_asset"`
	SoldAmount     string `json:"sold_amount"`
	BoughtAsset    Asset  `json:"bought_asset"`
	BoughtAmount   string `json:"bought_amount"`
	OfferID        int64  `json:"offer_id,omitempty"`
	PoolID         string `json:"pool_id,omitempty"`
	InSuccessfulTx bool   `json:"in_successful_tx"`
}

// TradesOrigin extracts trades from ledgers.
type TradesOrigin struct {
	networkPassphrase string
	out               chan *TradeEvent
}

// NewTradesOrigin creates a trade-extractor origin for one network.
func NewTradesOrigin(networkPassphrase string) *TradesOrigin {
	return &TradesOrigin{
		networkPassphrase: networkPassphrase,
		out:               make(chan *TradeEvent, 128),
	}
}

// ProcessLedger implements processor.Origin. Read failures are reported
// through the Reporter on ctx and the ledger continues, per
// streams-never-throw.
func (p *TradesOrigin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) {
	closeTimeMs := processor.LedgerCloseTimeMs(ledger)
	sequence := ledger.LedgerSequence()

	// ledgerread.Transactions reports a transaction it cannot read and carries
	// on, where the loop this replaces abandoned the rest of the ledger: it
	// returned the trades collected so far, so a decode failure early in a
	// busy ledger dropped every later trade with only a warning on stderr and
	// nothing the execution policy could see. Trades are emitted one by one
	// rather than collected into an index, so skipping the unreadable one is
	// the right trade here -- AllTransactions is for the index shape.
	for tx := range ledgerread.Transactions(ctx, p.Name(), p.networkPassphrase, ledger) {
		results, ok := tx.Result.OperationResults()
		if !ok {
			continue
		}

		successful := tx.Result.Successful()
		txHash := tx.Result.TransactionHash.HexString()
		buyer := tx.Envelope.SourceAccount().ToAccountId().Address()
		operations := tx.Envelope.Operations()

		for opIndex, result := range results {
			// An operation may name its own source account, which makes it
			// the buyer rather than the transaction's source.
			opBuyer := buyer
			if opIndex < len(operations) && operations[opIndex].SourceAccount != nil {
				opBuyer = operations[opIndex].SourceAccount.Address()
			}

			for _, claimed := range claimsOf(result) {
				trade := claimed.toTrade(sequence, closeTimeMs, txHash, opIndex, opBuyer, successful)
				if trade == nil {
					// An arm this processor does not know how to read. Report
					// it: a trade type silently dropped looks the same
					// downstream as a ledger that held no trades.
					processor.ReportWarning(ctx, p.Name(), fmt.Errorf(
						"ledger %d, tx %s, operation %d: unsupported claim type %s",
						sequence, txHash, opIndex, claimed.claim.Type))
					continue
				}

				select {
				case <-ctx.Done():
					return
				case p.out <- trade:
				}
			}
		}
	}
}

// Out implements cli.GenericOriginProcessor.
func (p *TradesOrigin) Out() <-chan *TradeEvent { return p.out }

// Close closes the output channel.
func (p *TradesOrigin) Close() { close(p.out) }

// Name returns the processor name.
func (p *TradesOrigin) Name() string { return "trade-extractor" }

// Type returns the processor type.
func (p *TradesOrigin) Type() processor.Type { return processor.TypeOrigin }

// claim pairs a ClaimAtom with the kind of trade the operation that produced
// it was making: the atom itself cannot tell an orderbook match from a path
// payment hop.
type claim struct {
	claim     xdr.ClaimAtom
	tradeType string
}

// claimsOf returns every offer one operation result claimed.
func claimsOf(result xdr.OperationResult) []claim {
	if result.Code != xdr.OperationResultCodeOpInner {
		return nil
	}

	inner := result.MustTr()
	var claims []claim

	appendAll := func(atoms []xdr.ClaimAtom, tradeType string) {
		for _, atom := range atoms {
			claims = append(claims, claim{claim: atom, tradeType: tradeType})
		}
	}

	switch inner.Type {
	case xdr.OperationTypeManageSellOffer:
		if res, ok := inner.GetManageSellOfferResult(); ok && res.Code == xdr.ManageSellOfferResultCodeManageSellOfferSuccess {
			appendAll(res.MustSuccess().OffersClaimed, "orderbook")
		}
	case xdr.OperationTypeManageBuyOffer:
		if res, ok := inner.GetManageBuyOfferResult(); ok && res.Code == xdr.ManageBuyOfferResultCodeManageBuyOfferSuccess {
			appendAll(res.MustSuccess().OffersClaimed, "orderbook")
		}
	case xdr.OperationTypeCreatePassiveSellOffer:
		if res, ok := inner.GetCreatePassiveSellOfferResult(); ok && res.Code == xdr.ManageSellOfferResultCodeManageSellOfferSuccess {
			appendAll(res.MustSuccess().OffersClaimed, "orderbook")
		}
	case xdr.OperationTypePathPaymentStrictReceive:
		if res, ok := inner.GetPathPaymentStrictReceiveResult(); ok && res.Code == xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveSuccess {
			appendAll(res.MustSuccess().Offers, "path_payment")
		}
	case xdr.OperationTypePathPaymentStrictSend:
		if res, ok := inner.GetPathPaymentStrictSendResult(); ok && res.Code == xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendSuccess {
			appendAll(res.MustSuccess().Offers, "path_payment")
		}
	}

	return claims
}

// toTrade renders one claimed offer as an event, or nil for a claim arm this
// processor does not read.
func (c claim) toTrade(sequence uint32, closeTimeMs int64, txHash string, opIndex int, buyer string, successful bool) *TradeEvent {
	trade := &TradeEvent{
		LedgerSequence: sequence,
		TimestampUnix:  closeTimeMs / 1000,
		Timestamp:      processor.FormatCloseTimeRFC3339(closeTimeMs),
		TxHash:         txHash,
		OpIndex:        opIndex,
		TradeType:      c.tradeType,
		Buyer:          buyer,
		InSuccessfulTx: successful,
	}

	switch c.claim.Type {
	case xdr.ClaimAtomTypeClaimAtomTypeOrderBook:
		book := c.claim.MustOrderBook()
		trade.Seller = book.SellerId.Address()
		trade.SoldAsset = assetOf(book.AssetSold)
		trade.SoldAmount = fmt.Sprintf("%d", book.AmountSold)
		trade.BoughtAsset = assetOf(book.AssetBought)
		trade.BoughtAmount = fmt.Sprintf("%d", book.AmountBought)
		trade.OfferID = int64(book.OfferId)

	case xdr.ClaimAtomTypeClaimAtomTypeV0:
		v0 := c.claim.MustV0()
		seller := xdr.AccountId{}
		seller.SetAddress(strkey.MustEncode(strkey.VersionByteAccountID, v0.SellerEd25519[:]))
		trade.Seller = seller.Address()
		trade.SoldAsset = assetOf(v0.AssetSold)
		trade.SoldAmount = fmt.Sprintf("%d", v0.AmountSold)
		trade.BoughtAsset = assetOf(v0.AssetBought)
		trade.BoughtAmount = fmt.Sprintf("%d", v0.AmountBought)
		trade.OfferID = int64(v0.OfferId)

	case xdr.ClaimAtomTypeClaimAtomTypeLiquidityPool:
		pool := c.claim.MustLiquidityPool()
		// A pool trade has no seller account and no offer id; the operation
		// that reached it decides nothing about which pool answered.
		trade.TradeType = "liquidity_pool"
		trade.PoolID = xdr.Hash(pool.LiquidityPoolId).HexString()
		trade.SoldAsset = assetOf(pool.AssetSold)
		trade.SoldAmount = fmt.Sprintf("%d", pool.AmountSold)
		trade.BoughtAsset = assetOf(pool.AssetBought)
		trade.BoughtAmount = fmt.Sprintf("%d", pool.AmountBought)

	default:
		return nil
	}

	return trade
}

// assetOf renders an XDR asset as the code and issuer pair the schema uses.
func assetOf(asset xdr.Asset) Asset {
	switch asset.Type {
	case xdr.AssetTypeAssetTypeNative:
		return Asset{Code: "XLM"}
	case xdr.AssetTypeAssetTypeCreditAlphanum4:
		code := asset.MustAlphaNum4()
		return Asset{
			Code:   strings.TrimRight(string(code.AssetCode[:]), "\x00"),
			Issuer: code.Issuer.Address(),
		}
	case xdr.AssetTypeAssetTypeCreditAlphanum12:
		code := asset.MustAlphaNum12()
		return Asset{
			Code:   strings.TrimRight(string(code.AssetCode[:]), "\x00"),
			Issuer: code.Issuer.Address(),
		}
	default:
		return Asset{}
	}
}
