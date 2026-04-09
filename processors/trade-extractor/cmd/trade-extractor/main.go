// Package main provides a standalone CLI for the trade-extractor origin processor.
//
// This processor extracts classic DEX orderbook trades from ManageBuyOffer,
// ManageSellOffer, CreatePassiveSellOffer, and PathPayment operations by
// reading ClaimAtom results from transaction metadata.
//
// Usage:
//
//	trade-extractor -q --start-ledger 61696300 --end-ledger 61696400
//	nebu fetch 61696300 61696400 | trade-extractor -q
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
	"github.com/withObsrvr/nebu/pkg/source/rpc"
)

var version = "0.1.0"

var (
	rpcURL      string
	startLedger uint32
	endLedger   uint32
	networkPass string
	quietMode   bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "trade-extractor",
		Short:   "Extract classic DEX trades from Stellar ledgers",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run()
		},
	}

	rootCmd.Flags().StringVar(&rpcURL, "rpc-url", "https://archive-rpc.lightsail.network", "Stellar RPC endpoint")
	rootCmd.Flags().Uint32Var(&startLedger, "start-ledger", 0, "Start ledger sequence")
	rootCmd.Flags().Uint32Var(&endLedger, "end-ledger", 0, "End ledger sequence (0 for unbounded)")
	rootCmd.Flags().StringVar(&networkPass, "network", network.PublicNetworkPassphrase, "Network passphrase or shorthand (mainnet|testnet)")
	rootCmd.Flags().BoolVarP(&quietMode, "quiet", "q", false, "Suppress non-error output")
	rootCmd.Flags().Bool(describeFlagName, false, "Emit machine-readable describe envelope to stdout and exit")

	// Short-circuit into the describe-json protocol before cobra
	// validates required flags — --describe-json must work without
	// --start-ledger or any other mandatory flag set.
	emitDescribeIfRequested(rootCmd, func() processor.DescribeEnvelope {
		return processor.DescribeEnvelope{
			Name:        "trade-extractor",
			Type:        processor.TypeOrigin.String(),
			Version:     version,
			Description: "Extract classic DEX trades from Stellar ledgers",
			Schema: processor.DescribeSchema{
				ID: "nebu.trade_extractor.v1",
			},
		}
	})

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	networkPass = normalizeNetwork(networkPass)

	if startLedger == 0 {
		return fmt.Errorf("--start-ledger is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		if !quietMode {
			fmt.Fprintln(os.Stderr, "\nShutting down...")
		}
		cancel()
		time.Sleep(2 * time.Second)
		os.Exit(1)
	}()

	var src *rpc.LedgerSource
	var err error
	if authHeader := os.Getenv("NEBU_RPC_AUTH"); authHeader != "" {
		src, err = rpc.NewLedgerSourceWithHeaders(rpcURL, map[string]string{"Authorization": authHeader})
	} else {
		src, err = rpc.NewLedgerSource(rpcURL)
	}
	if err != nil {
		return fmt.Errorf("failed to create RPC source: %w", err)
	}
	defer src.Close()

	if !quietMode {
		if endLedger == 0 {
			fmt.Fprintf(os.Stderr, "Streaming trades from ledger %d (unbounded)...\n", startLedger)
		} else {
			fmt.Fprintf(os.Stderr, "Processing ledgers %d to %d...\n", startLedger, endLedger)
		}
	}

	ch := make(chan xdr.LedgerCloseMeta, 4)
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Stream(ctx, startLedger, endLedger, ch)
	}()

	encoder := json.NewEncoder(os.Stdout)
	tradeCount := 0
	ledgerCount := 0

	for lcm := range ch {
		ledgerCount++
		trades := extractTrades(lcm, networkPass)
		for _, trade := range trades {
			if err := encoder.Encode(trade); err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding: %v\n", err)
			}
			tradeCount++
		}

		if !quietMode && ledgerCount%100 == 0 {
			fmt.Fprintf(os.Stderr, "Processed %d ledgers, %d trades (at ledger %d)...\n",
				ledgerCount, tradeCount, lcm.LedgerSequence())
		}
	}

	if err := <-errCh; err != nil && err != context.Canceled {
		return err
	}

	if !quietMode {
		fmt.Fprintf(os.Stderr, "Done. Processed %d ledgers, %d trades\n", ledgerCount, tradeCount)
	}

	return nil
}

type tradeEvent struct {
	Schema         string     `json:"_schema"`
	NebuVersion    string     `json:"_nebu_version"`
	LedgerSequence uint32     `json:"ledger_sequence"`
	TimestampUnix  int64      `json:"timestamp_unix"`
	TxHash         string     `json:"tx_hash"`
	OpIndex        int        `json:"operation_index"`
	TradeType      string     `json:"trade_type"`
	Seller         string     `json:"seller"`
	Buyer          string     `json:"buyer"`
	SoldAsset      assetJSON  `json:"sold_asset"`
	SoldAmount     string     `json:"sold_amount"`
	BoughtAsset    assetJSON  `json:"bought_asset"`
	BoughtAmount   string     `json:"bought_amount"`
	OfferID        int64      `json:"offer_id,omitempty"`
	PoolID         string     `json:"pool_id,omitempty"`
	InSuccessfulTx bool       `json:"in_successful_tx"`
}

type assetJSON struct {
	Code   string `json:"code"`
	Issuer string `json:"issuer,omitempty"`
}

func extractTrades(lcm xdr.LedgerCloseMeta, passphrase string) []tradeEvent {
	var trades []tradeEvent

	seq := lcm.LedgerSequence()
	closeTime := lcm.LedgerCloseTime()

	reader, err := ingest.NewLedgerTransactionReaderFromLedgerCloseMeta(passphrase, lcm)
	if err != nil {
		if !quietMode {
			fmt.Fprintf(os.Stderr, "Warning: could not read ledger %d: %v\n", seq, err)
		}
		return nil
	}
	defer reader.Close()

	for {
		tx, err := reader.Read()
		if err != nil {
			break
		}

		successful := tx.Result.Successful()
		txHash := tx.Result.TransactionHash.HexString()

		// Get the source account for buyer identification
		sourceAccount := tx.Envelope.SourceAccount().ToAccountId()
		buyer := sourceAccount.Address()

		// Iterate over operation results to find trades
		results, ok := tx.Result.OperationResults()
		if !ok {
			continue
		}

		ops := tx.Envelope.Operations()

		for opIdx, result := range results {
			// Get the operation source (may override tx source)
			opBuyer := buyer
			if opIdx < len(ops) && ops[opIdx].SourceAccount != nil {
				opBuyer = ops[opIdx].SourceAccount.Address()
			}

			claims := extractClaimAtoms(result)
			for _, claim := range claims {
				trade := claimToTrade(claim, seq, closeTime, txHash, opIdx, opBuyer, successful, result)
				if trade != nil {
					trades = append(trades, *trade)
				}
			}
		}
	}

	return trades
}

func extractClaimAtoms(result xdr.OperationResult) []claimWithType {
	if result.Code != xdr.OperationResultCodeOpInner {
		return nil
	}

	tr := result.MustTr()
	var claims []claimWithType

	switch tr.Type {
	case xdr.OperationTypeManageSellOffer:
		if success, ok := tr.GetManageSellOfferResult(); ok && success.Code == xdr.ManageSellOfferResultCodeManageSellOfferSuccess {
			for _, c := range success.MustSuccess().OffersClaimed {
				claims = append(claims, claimWithType{claim: c, tradeType: "orderbook"})
			}
		}
	case xdr.OperationTypeManageBuyOffer:
		if success, ok := tr.GetManageBuyOfferResult(); ok && success.Code == xdr.ManageBuyOfferResultCodeManageBuyOfferSuccess {
			for _, c := range success.MustSuccess().OffersClaimed {
				claims = append(claims, claimWithType{claim: c, tradeType: "orderbook"})
			}
		}
	case xdr.OperationTypeCreatePassiveSellOffer:
		if success, ok := tr.GetCreatePassiveSellOfferResult(); ok && success.Code == xdr.ManageSellOfferResultCodeManageSellOfferSuccess {
			for _, c := range success.MustSuccess().OffersClaimed {
				claims = append(claims, claimWithType{claim: c, tradeType: "orderbook"})
			}
		}
	case xdr.OperationTypePathPaymentStrictReceive:
		if success, ok := tr.GetPathPaymentStrictReceiveResult(); ok && success.Code == xdr.PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveSuccess {
			for _, c := range success.MustSuccess().Offers {
				claims = append(claims, claimWithType{claim: c, tradeType: "path_payment"})
			}
		}
	case xdr.OperationTypePathPaymentStrictSend:
		if success, ok := tr.GetPathPaymentStrictSendResult(); ok && success.Code == xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendSuccess {
			for _, c := range success.MustSuccess().Offers {
				claims = append(claims, claimWithType{claim: c, tradeType: "path_payment"})
			}
		}
	}

	return claims
}

type claimWithType struct {
	claim     xdr.ClaimAtom
	tradeType string
}

func claimToTrade(cwt claimWithType, seq uint32, closeTime int64, txHash string, opIdx int, buyer string, successful bool, _ xdr.OperationResult) *tradeEvent {
	claim := cwt.claim

	trade := &tradeEvent{
		Schema:         "nebu.dex_trade.v1",
		NebuVersion:    version,
		LedgerSequence: seq,
		TimestampUnix:  closeTime,
		TxHash:         txHash,
		OpIndex:        opIdx,
		TradeType:      cwt.tradeType,
		Buyer:          buyer,
		InSuccessfulTx: successful,
	}

	switch claim.Type {
	case xdr.ClaimAtomTypeClaimAtomTypeOrderBook:
		ob := claim.MustOrderBook()
		trade.Seller = ob.SellerId.Address()
		trade.SoldAsset = xdrAssetToJSON(ob.AssetSold)
		trade.SoldAmount = fmt.Sprintf("%d", ob.AmountSold)
		trade.BoughtAsset = xdrAssetToJSON(ob.AssetBought)
		trade.BoughtAmount = fmt.Sprintf("%d", ob.AmountBought)
		trade.OfferID = int64(ob.OfferId)

	case xdr.ClaimAtomTypeClaimAtomTypeV0:
		v0 := claim.MustV0()
		sellerKey := xdr.AccountId{}
		sellerKey.SetAddress(strkey.MustEncode(strkey.VersionByteAccountID, v0.SellerEd25519[:]))
		trade.Seller = sellerKey.Address()
		trade.SoldAsset = xdrAssetToJSON(v0.AssetSold)
		trade.SoldAmount = fmt.Sprintf("%d", v0.AmountSold)
		trade.BoughtAsset = xdrAssetToJSON(v0.AssetBought)
		trade.BoughtAmount = fmt.Sprintf("%d", v0.AmountBought)
		trade.OfferID = int64(v0.OfferId)

	case xdr.ClaimAtomTypeClaimAtomTypeLiquidityPool:
		lp := claim.MustLiquidityPool()
		trade.TradeType = "liquidity_pool"
		trade.PoolID = xdr.Hash(lp.LiquidityPoolId).HexString()
		trade.SoldAsset = xdrAssetToJSON(lp.AssetSold)
		trade.SoldAmount = fmt.Sprintf("%d", lp.AmountSold)
		trade.BoughtAsset = xdrAssetToJSON(lp.AssetBought)
		trade.BoughtAmount = fmt.Sprintf("%d", lp.AmountBought)

	default:
		return nil
	}

	return trade
}

func xdrAssetToJSON(asset xdr.Asset) assetJSON {
	if asset.Type == xdr.AssetTypeAssetTypeNative {
		return assetJSON{Code: "XLM"}
	}

	code := ""
	issuer := ""

	switch asset.Type {
	case xdr.AssetTypeAssetTypeCreditAlphanum4:
		a4 := asset.MustAlphaNum4()
		code = strings.TrimRight(string(a4.AssetCode[:]), "\x00")
		issuer = a4.Issuer.Address()
	case xdr.AssetTypeAssetTypeCreditAlphanum12:
		a12 := asset.MustAlphaNum12()
		code = strings.TrimRight(string(a12.AssetCode[:]), "\x00")
		issuer = a12.Issuer.Address()
	}

	return assetJSON{Code: code, Issuer: issuer}
}

func normalizeNetwork(s string) string {
	switch s {
	case "mainnet", "pubnet":
		return network.PublicNetworkPassphrase
	case "testnet":
		return network.TestNetworkPassphrase
	default:
		return s
	}
}
