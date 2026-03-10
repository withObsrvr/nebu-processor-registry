// Package liquidity_pool_events provides an origin processor that extracts liquidity pool operations from Stellar ledgers.
package liquidity_pool_events

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
)

// Origin extracts liquidity pool operations from ledgers.
type Origin struct {
	passphrase string
	out        chan *LiquidityPoolEvent
}

// NewOrigin creates a new liquidity pool events origin processor.
func NewOrigin(passphrase string) *Origin {
	return &Origin{
		passphrase: passphrase,
		out:        make(chan *LiquidityPoolEvent, 128),
	}
}

func (o *Origin) Name() string                            { return "stellar/liquidity-pool-events" }
func (o *Origin) Type() processor.Type                    { return processor.TypeOrigin }
func (o *Origin) Out() <-chan *LiquidityPoolEvent         { return o.out }
func (o *Origin) Close()                                  { close(o.out) }

// ProcessLedger extracts LP operations from the ledger.
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) error {
	sequence := ledger.LedgerSequence()
	closeTime := int64(ledger.LedgerHeaderHistoryEntry().Header.ScpValue.CloseTime)

	// Build tx success map
	txSuccessMap := make(map[string]bool)
	reader, err := ingest.NewLedgerTransactionReaderFromLedgerCloseMeta(o.passphrase, ledger)
	if err != nil {
		return fmt.Errorf("error creating transaction reader: %w", err)
	}
	defer reader.Close()

	for {
		tx, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		txSuccessMap[tx.Result.TransactionHash.HexString()] = tx.Result.Successful()
	}

	// Re-create reader to process operations
	reader, err = ingest.NewLedgerTransactionReaderFromLedgerCloseMeta(o.passphrase, ledger)
	if err != nil {
		return err
	}
	defer reader.Close()

	for {
		tx, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		txHash := tx.Result.TransactionHash.HexString()
		txIndex := uint32(tx.Index)
		successful := txSuccessMap[txHash]

		for opIndex, op := range tx.Envelope.Operations() {
			var event *LiquidityPoolEvent

			switch op.Body.Type {
			case xdr.OperationTypeLiquidityPoolDeposit:
				event = o.processDeposit(tx, opIndex, op, sequence, closeTime, txHash, txIndex, successful)
			case xdr.OperationTypeLiquidityPoolWithdraw:
				event = o.processWithdraw(tx, opIndex, op, sequence, closeTime, txHash, txIndex, successful)
			}

			if event != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case o.out <- event:
				}
			}
		}

		// Check for path payments routed through LPs (trades)
		events := o.processPathPaymentTrades(tx, sequence, closeTime, txHash, txIndex, successful)
		for _, event := range events {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case o.out <- event:
			}
		}
	}

	return nil
}

func (o *Origin) processDeposit(
	tx ingest.LedgerTransaction,
	opIndex int,
	op xdr.Operation,
	sequence uint32,
	closeTime int64,
	txHash string,
	txIndex uint32,
	successful bool,
) *LiquidityPoolEvent {
	deposit := op.Body.MustLiquidityPoolDepositOp()
	poolID := hex.EncodeToString(deposit.LiquidityPoolId[:])

	account := sourceAccount(op, tx)

	event := &LiquidityPoolEvent{
		Type:    "deposit",
		PoolId:  poolID,
		Account: account,
		Assets: []*PoolAsset{
			{Amount: "0"},
			{Amount: "0"},
		},
		Meta: &EventMeta{
			LedgerSequence:   sequence,
			ClosedAtUnix:     closeTime,
			TxHash:           txHash,
			TransactionIndex: txIndex,
			OperationIndex:   uint32(opIndex),
			InSuccessfulTx:   successful,
		},
	}

	// Extract actual amounts and pool info from ledger changes
	o.enrichFromChanges(tx, poolID, event)

	return event
}

func (o *Origin) processWithdraw(
	tx ingest.LedgerTransaction,
	opIndex int,
	op xdr.Operation,
	sequence uint32,
	closeTime int64,
	txHash string,
	txIndex uint32,
	successful bool,
) *LiquidityPoolEvent {
	withdraw := op.Body.MustLiquidityPoolWithdrawOp()
	poolID := hex.EncodeToString(withdraw.LiquidityPoolId[:])

	account := sourceAccount(op, tx)

	event := &LiquidityPoolEvent{
		Type:    "withdraw",
		PoolId:  poolID,
		Account: account,
		Shares:  fmt.Sprintf("%d", withdraw.Amount),
		Assets: []*PoolAsset{
			{Amount: "0"},
			{Amount: "0"},
		},
		Meta: &EventMeta{
			LedgerSequence:   sequence,
			ClosedAtUnix:     closeTime,
			TxHash:           txHash,
			TransactionIndex: txIndex,
			OperationIndex:   uint32(opIndex),
			InSuccessfulTx:   successful,
		},
	}

	o.enrichFromChanges(tx, poolID, event)

	return event
}

func (o *Origin) processPathPaymentTrades(
	tx ingest.LedgerTransaction,
	sequence uint32,
	closeTime int64,
	txHash string,
	txIndex uint32,
	successful bool,
) []*LiquidityPoolEvent {
	var events []*LiquidityPoolEvent

	changes, err := tx.GetChanges()
	if err != nil {
		return events
	}

	for _, change := range changes {
		if change.Type != xdr.LedgerEntryTypeLiquidityPool {
			continue
		}
		if change.Pre == nil || change.Post == nil {
			continue
		}

		preLp := change.Pre.Data.MustLiquidityPool()
		postLp := change.Post.Data.MustLiquidityPool()

		preBody := preLp.Body.MustConstantProduct()
		postBody := postLp.Body.MustConstantProduct()

		// If reserves changed but it's not a deposit/withdraw op, it's a trade
		preA := int64(preBody.ReserveA)
		preB := int64(preBody.ReserveB)
		postA := int64(postBody.ReserveA)
		postB := int64(postBody.ReserveB)

		// Skip if no change
		if preA == postA && preB == postB {
			continue
		}

		// Skip if total shares changed (that's a deposit/withdraw, not a trade)
		if preBody.TotalPoolShares != postBody.TotalPoolShares {
			continue
		}

		poolID := hex.EncodeToString(preLp.LiquidityPoolId[:])

		assetA := assetToPoolAsset(preBody.Params.AssetA)
		assetB := assetToPoolAsset(preBody.Params.AssetB)

		events = append(events, &LiquidityPoolEvent{
			Type:    "trade",
			PoolId:  poolID,
			Account: "", // trades via path payment don't have a direct LP account
			ReservesBefore: []*ReserveAmount{
				{AssetType: assetA.AssetType, AssetCode: assetA.AssetCode, AssetIssuer: assetA.AssetIssuer, Amount: fmt.Sprintf("%d", preA)},
				{AssetType: assetB.AssetType, AssetCode: assetB.AssetCode, AssetIssuer: assetB.AssetIssuer, Amount: fmt.Sprintf("%d", preB)},
			},
			ReservesAfter: []*ReserveAmount{
				{AssetType: assetA.AssetType, AssetCode: assetA.AssetCode, AssetIssuer: assetA.AssetIssuer, Amount: fmt.Sprintf("%d", postA)},
				{AssetType: assetB.AssetType, AssetCode: assetB.AssetCode, AssetIssuer: assetB.AssetIssuer, Amount: fmt.Sprintf("%d", postB)},
			},
			Meta: &EventMeta{
				LedgerSequence:   sequence,
				ClosedAtUnix:     closeTime,
				TxHash:           txHash,
				TransactionIndex: txIndex,
				InSuccessfulTx:   successful,
			},
		})
	}

	return events
}

func (o *Origin) enrichFromChanges(tx ingest.LedgerTransaction, poolID string, event *LiquidityPoolEvent) {
	changes, err := tx.GetChanges()
	if err != nil {
		return
	}

	for _, change := range changes {
		if change.Type != xdr.LedgerEntryTypeLiquidityPool {
			continue
		}

		var preBody, postBody *xdr.LiquidityPoolConstantProductParameters
		var preReserveA, preReserveB, postReserveA, postReserveB int64

		if change.Pre != nil {
			lp := change.Pre.Data.MustLiquidityPool()
			if hex.EncodeToString(lp.LiquidityPoolId[:]) != poolID {
				continue
			}
			cp := lp.Body.MustConstantProduct()
			preBody = &cp.Params
			preReserveA = int64(cp.ReserveA)
			preReserveB = int64(cp.ReserveB)
		}

		if change.Post != nil {
			lp := change.Post.Data.MustLiquidityPool()
			if hex.EncodeToString(lp.LiquidityPoolId[:]) != poolID {
				continue
			}
			cp := lp.Body.MustConstantProduct()
			postBody = &cp.Params
			postReserveA = int64(cp.ReserveA)
			postReserveB = int64(cp.ReserveB)

			if event.Type == "deposit" {
				event.Shares = fmt.Sprintf("%d", cp.TotalPoolShares)
			}
		}

		// Enrich asset info and compute actual amounts from reserve deltas
		params := postBody
		if params == nil {
			params = preBody
		}
		if params != nil {
			assetA := assetToPoolAsset(params.AssetA)
			assetB := assetToPoolAsset(params.AssetB)
			if len(event.Assets) >= 2 {
				event.Assets[0].AssetType = assetA.AssetType
				event.Assets[0].AssetCode = assetA.AssetCode
				event.Assets[0].AssetIssuer = assetA.AssetIssuer
				event.Assets[1].AssetType = assetB.AssetType
				event.Assets[1].AssetCode = assetB.AssetCode
				event.Assets[1].AssetIssuer = assetB.AssetIssuer

				// Compute actual amounts from reserve deltas
				if change.Pre != nil && change.Post != nil {
					deltaA := postReserveA - preReserveA
					deltaB := postReserveB - preReserveB
					if deltaA < 0 {
						deltaA = -deltaA
					}
					if deltaB < 0 {
						deltaB = -deltaB
					}
					event.Assets[0].Amount = fmt.Sprintf("%d", deltaA)
					event.Assets[1].Amount = fmt.Sprintf("%d", deltaB)
				}
			}
		}

		// Set reserves before/after
		if change.Pre != nil {
			assetA := assetToPoolAsset(preBody.AssetA)
			assetB := assetToPoolAsset(preBody.AssetB)
			event.ReservesBefore = []*ReserveAmount{
				{AssetType: assetA.AssetType, AssetCode: assetA.AssetCode, AssetIssuer: assetA.AssetIssuer, Amount: fmt.Sprintf("%d", preReserveA)},
				{AssetType: assetB.AssetType, AssetCode: assetB.AssetCode, AssetIssuer: assetB.AssetIssuer, Amount: fmt.Sprintf("%d", preReserveB)},
			}
		}
		if change.Post != nil {
			assetA := assetToPoolAsset(postBody.AssetA)
			assetB := assetToPoolAsset(postBody.AssetB)
			event.ReservesAfter = []*ReserveAmount{
				{AssetType: assetA.AssetType, AssetCode: assetA.AssetCode, AssetIssuer: assetA.AssetIssuer, Amount: fmt.Sprintf("%d", postReserveA)},
				{AssetType: assetB.AssetType, AssetCode: assetB.AssetCode, AssetIssuer: assetB.AssetIssuer, Amount: fmt.Sprintf("%d", postReserveB)},
			}
		}

		break
	}
}

func sourceAccount(op xdr.Operation, tx ingest.LedgerTransaction) string {
	if op.SourceAccount != nil {
		return op.SourceAccount.ToAccountId().Address()
	}
	return tx.Envelope.SourceAccount().ToAccountId().Address()
}

func assetToPoolAsset(asset xdr.Asset) *PoolAsset {
	pa := &PoolAsset{}
	switch asset.Type {
	case xdr.AssetTypeAssetTypeNative:
		pa.AssetType = "native"
		pa.AssetCode = "XLM"
	case xdr.AssetTypeAssetTypeCreditAlphanum4:
		pa.AssetType = "credit_alphanum4"
		a := asset.MustAlphaNum4()
		pa.AssetCode = strings.TrimRight(string(a.AssetCode[:]), "\x00")
		pa.AssetIssuer = a.Issuer.Address()
	case xdr.AssetTypeAssetTypeCreditAlphanum12:
		pa.AssetType = "credit_alphanum12"
		a := asset.MustAlphaNum12()
		pa.AssetCode = strings.TrimRight(string(a.AssetCode[:]), "\x00")
		pa.AssetIssuer = a.Issuer.Address()
	}
	return pa
}
