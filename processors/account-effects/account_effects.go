// Package account_effects provides an origin processor that extracts account-level effects from Stellar ledgers.
package account_effects

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
)

// Origin extracts account-level effects (non-payment operations) from ledgers.
type Origin struct {
	passphrase string
	out        chan *AccountEffect
}

// NewOrigin creates a new account effects origin processor.
func NewOrigin(passphrase string) *Origin {
	return &Origin{
		passphrase: passphrase,
		out:        make(chan *AccountEffect, 128),
	}
}

func (o *Origin) Name() string                    { return "stellar/account-effects" }
func (o *Origin) Type() processor.Type            { return processor.TypeOrigin }
func (o *Origin) Out() <-chan *AccountEffect       { return o.out }
func (o *Origin) Close()                          { close(o.out) }

// ProcessLedger extracts account effects from the ledger. Per-ledger
// errors are reported via processor.ReportWarning; the pipeline
// continues (streams-never-throw).
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) {
	sequence := ledger.LedgerSequence()
	closeTime := int64(ledger.LedgerHeaderHistoryEntry().Header.ScpValue.CloseTime)

	reader, err := ingest.NewLedgerTransactionReaderFromLedgerCloseMeta(o.passphrase, ledger)
	if err != nil {
		processor.ReportWarning(ctx, o.Name(),
			fmt.Errorf("ledger %d: create tx reader: %w", sequence, err))
		return
	}
	defer reader.Close()

	for {
		tx, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			processor.ReportWarning(ctx, o.Name(),
				fmt.Errorf("ledger %d: read tx: %w", sequence, err))
			return
		}

		txHash := tx.Result.TransactionHash.HexString()
		txIndex := uint32(tx.Index)
		successful := tx.Result.Successful()

		for opIndex, op := range tx.Envelope.Operations() {
			account := sourceAccount(op, tx)
			meta := &EventMeta{
				LedgerSequence:   sequence,
				ClosedAtUnix:     closeTime,
				TxHash:           txHash,
				TransactionIndex: txIndex,
				OperationIndex:   uint32(opIndex),
				InSuccessfulTx:   successful,
			}

			effects := o.extractEffects(op, account, meta)
			for _, effect := range effects {
				select {
				case <-ctx.Done():
					return
				case o.out <- effect:
				}
			}
		}

		// Extract effects from ledger changes (trustline, offer, data lifecycle)
		changeEffects := o.extractChangeEffects(tx, sequence, closeTime, txHash, txIndex, successful)
		for _, effect := range changeEffects {
			select {
			case <-ctx.Done():
				return
			case o.out <- effect:
			}
		}
	}
}

func (o *Origin) extractEffects(op xdr.Operation, account string, meta *EventMeta) []*AccountEffect {
	var effects []*AccountEffect

	switch op.Body.Type {
	case xdr.OperationTypeCreateAccount:
		createOp := op.Body.MustCreateAccountOp()
		effects = append(effects, &AccountEffect{
			Type:    "account_created",
			Account: createOp.Destination.Address(),
			Details: toJSON(map[string]interface{}{
				"funder":          account,
				"startingBalance": fmt.Sprintf("%d", createOp.StartingBalance),
			}),
			Meta: meta,
		})

	case xdr.OperationTypeSetOptions:
		setOpts := op.Body.MustSetOptionsOp()
		if setOpts.HomeDomain != nil {
			effects = append(effects, &AccountEffect{
				Type:    "home_domain_updated",
				Account: account,
				Details: toJSON(map[string]interface{}{
					"homeDomain": string(*setOpts.HomeDomain),
				}),
				Meta: meta,
			})
		}
		if setOpts.InflationDest != nil {
			effects = append(effects, &AccountEffect{
				Type:    "inflation_destination_updated",
				Account: account,
				Details: toJSON(map[string]interface{}{
					"inflationDest": setOpts.InflationDest.Address(),
				}),
				Meta: meta,
			})
		}
		if setOpts.SetFlags != nil || setOpts.ClearFlags != nil {
			details := map[string]interface{}{}
			if setOpts.SetFlags != nil {
				details["setFlags"] = uint32(*setOpts.SetFlags)
			}
			if setOpts.ClearFlags != nil {
				details["clearFlags"] = uint32(*setOpts.ClearFlags)
			}
			effects = append(effects, &AccountEffect{
				Type:    "flags_updated",
				Account: account,
				Details: toJSON(details),
				Meta:    meta,
			})
		}
		if setOpts.LowThreshold != nil || setOpts.MedThreshold != nil || setOpts.HighThreshold != nil {
			details := map[string]interface{}{}
			if setOpts.LowThreshold != nil {
				details["lowThreshold"] = uint32(*setOpts.LowThreshold)
			}
			if setOpts.MedThreshold != nil {
				details["medThreshold"] = uint32(*setOpts.MedThreshold)
			}
			if setOpts.HighThreshold != nil {
				details["highThreshold"] = uint32(*setOpts.HighThreshold)
			}
			effects = append(effects, &AccountEffect{
				Type:    "thresholds_updated",
				Account: account,
				Details: toJSON(details),
				Meta:    meta,
			})
		}
		if setOpts.Signer != nil {
			details := map[string]interface{}{
				"signerKey":    setOpts.Signer.Key.Address(),
				"signerWeight": setOpts.Signer.Weight,
			}
			effectType := "signer_updated"
			if setOpts.Signer.Weight == 0 {
				effectType = "signer_removed"
			}
			effects = append(effects, &AccountEffect{
				Type:    effectType,
				Account: account,
				Details: toJSON(details),
				Meta:    meta,
			})
		}

	case xdr.OperationTypeChangeTrust:
		// Trustline effects (created/updated/removed) are derived from ledger
		// entry changes in extractChangeEffects to avoid duplicate events.

	case xdr.OperationTypeManageSellOffer:
		offerOp := op.Body.MustManageSellOfferOp()
		effectType := "offer_created"
		if offerOp.OfferId != 0 && offerOp.Amount == 0 {
			effectType = "offer_removed"
		} else if offerOp.OfferId != 0 {
			effectType = "offer_updated"
		}
		effects = append(effects, &AccountEffect{
			Type:    effectType,
			Account: account,
			Details: toJSON(map[string]interface{}{
				"offerId": int64(offerOp.OfferId),
				"amount":  fmt.Sprintf("%d", offerOp.Amount),
			}),
			Meta: meta,
		})

	case xdr.OperationTypeManageBuyOffer:
		offerOp := op.Body.MustManageBuyOfferOp()
		effectType := "offer_created"
		if offerOp.OfferId != 0 && offerOp.BuyAmount == 0 {
			effectType = "offer_removed"
		} else if offerOp.OfferId != 0 {
			effectType = "offer_updated"
		}
		effects = append(effects, &AccountEffect{
			Type:    effectType,
			Account: account,
			Details: toJSON(map[string]interface{}{
				"offerId":   int64(offerOp.OfferId),
				"buyAmount": fmt.Sprintf("%d", offerOp.BuyAmount),
			}),
			Meta: meta,
		})

	case xdr.OperationTypeManageData:
		dataOp := op.Body.MustManageDataOp()
		effectType := "data_updated"
		if dataOp.DataValue == nil {
			effectType = "data_removed"
		}
		effects = append(effects, &AccountEffect{
			Type:    effectType,
			Account: account,
			Details: toJSON(map[string]interface{}{
				"name": string(dataOp.DataName),
			}),
			Meta: meta,
		})
	}

	return effects
}

func (o *Origin) extractChangeEffects(
	tx ingest.LedgerTransaction,
	sequence uint32,
	closeTime int64,
	txHash string,
	txIndex uint32,
	successful bool,
) []*AccountEffect {
	var effects []*AccountEffect

	changes, err := tx.GetChanges()
	if err != nil {
		return effects
	}

	for _, change := range changes {
		meta := &EventMeta{
			LedgerSequence:   sequence,
			ClosedAtUnix:     closeTime,
			TxHash:           txHash,
			TransactionIndex: txIndex,
			InSuccessfulTx:   successful,
		}

		switch change.Type {
		case xdr.LedgerEntryTypeTrustline:
			switch change.ChangeType {
			case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
				if change.Post != nil {
					tl := change.Post.Data.MustTrustLine()
					details := map[string]interface{}{
						"limit": fmt.Sprintf("%d", tl.Limit),
					}
					assetCode, assetIssuer := trustlineAssetInfo(tl.Asset)
					if assetCode != "" {
						details["assetCode"] = assetCode
						details["assetIssuer"] = assetIssuer
					}
					effects = append(effects, &AccountEffect{
						Type:    "trustline_created",
						Account: tl.AccountId.Address(),
						Details: toJSON(details),
						Meta:    meta,
					})
				}
			case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
				if change.Post != nil {
					tl := change.Post.Data.MustTrustLine()
					details := map[string]interface{}{
						"limit": fmt.Sprintf("%d", tl.Limit),
					}
					assetCode, assetIssuer := trustlineAssetInfo(tl.Asset)
					if assetCode != "" {
						details["assetCode"] = assetCode
						details["assetIssuer"] = assetIssuer
					}
					effects = append(effects, &AccountEffect{
						Type:    "trustline_updated",
						Account: tl.AccountId.Address(),
						Details: toJSON(details),
						Meta:    meta,
					})
				}
			case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
				if change.Pre != nil {
					tl := change.Pre.Data.MustTrustLine()
					details := map[string]interface{}{}
					assetCode, assetIssuer := trustlineAssetInfo(tl.Asset)
					if assetCode != "" {
						details["assetCode"] = assetCode
						details["assetIssuer"] = assetIssuer
					}
					effects = append(effects, &AccountEffect{
						Type:    "trustline_removed",
						Account: tl.AccountId.Address(),
						Details: toJSON(details),
						Meta:    meta,
					})
				}
			}
		}
	}

	return effects
}

func trustlineAssetInfo(asset xdr.TrustLineAsset) (string, string) {
	switch asset.Type {
	case xdr.AssetTypeAssetTypeCreditAlphanum4:
		a := asset.MustAlphaNum4()
		return strings.TrimRight(string(a.AssetCode[:]), "\x00"), a.Issuer.Address()
	case xdr.AssetTypeAssetTypeCreditAlphanum12:
		a := asset.MustAlphaNum12()
		return strings.TrimRight(string(a.AssetCode[:]), "\x00"), a.Issuer.Address()
	}
	return "", ""
}

func sourceAccount(op xdr.Operation, tx ingest.LedgerTransaction) string {
	if op.SourceAccount != nil {
		return op.SourceAccount.ToAccountId().Address()
	}
	return tx.Envelope.SourceAccount().ToAccountId().Address()
}

func toJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
