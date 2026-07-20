// Package validator_analytics provides a Nebu origin processor that extracts
// deterministic validator and activity facts from Stellar ledgers.
package validator_analytics

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
)

const processorName = "stellar/validator-analytics"

// Origin emits one ValidatorLedgerAnalytics event for every input ledger.
type Origin struct {
	networkPassphrase string
	out               chan *ValidatorLedgerAnalytics
}

// NewOrigin creates a validator analytics origin processor.
func NewOrigin(networkPassphrase string) *Origin {
	return &Origin{
		networkPassphrase: networkPassphrase,
		out:               make(chan *ValidatorLedgerAnalytics, 128),
	}
}

func (o *Origin) Name() string                          { return processorName }
func (o *Origin) Type() processor.Type                  { return processor.TypeOrigin }
func (o *Origin) Out() <-chan *ValidatorLedgerAnalytics { return o.out }
func (o *Origin) Close()                                { close(o.out) }

// ProcessLedger extracts one per-ledger event. Validator address encoding
// failures are reported as warnings, but the remaining facts are still emitted.
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) {
	event := summarizeLedger(ledger, o.networkPassphrase)

	if signature, ok := ledger.LedgerHeaderHistoryEntry().Header.ScpValue.Ext.GetLcValueSignature(); ok {
		address, err := signature.NodeId.GetAddress()
		if err != nil {
			processor.ReportWarning(ctx, o.Name(), fmt.Errorf("ledger %d: encode validator address: %w", ledger.LedgerSequence(), err))
		} else {
			event.ValidatorAddress = address
			event.LedgerCloseSignature = base64.StdEncoding.EncodeToString(signature.Signature)
			event.ValidatorAttributionAvailable = true
		}
	}

	select {
	case <-ctx.Done():
		return
	case o.out <- event:
	}
}

type transactionFacts struct {
	successful     bool
	operationTypes []xdr.OperationType
}

type activitySummary struct {
	transactionCount           uint32
	successfulTransactionCount uint32
	operationCount             uint32
	successfulOperationCount   uint32
	operationCategories        *OperationCategoryCounts
	successfulCategories       *OperationCategoryCounts
}

func summarizeLedger(ledger xdr.LedgerCloseMeta, networkPassphrase string) *ValidatorLedgerAnalytics {
	header := ledger.LedgerHeaderHistoryEntry().Header
	envelopes := ledger.TransactionEnvelopes()
	transactions := make([]transactionFacts, 0, len(envelopes))

	for index, envelope := range envelopes {
		operations := envelope.Operations()
		facts := transactionFacts{
			successful:     ledger.TransactionResultPair(index).Result.Successful(),
			operationTypes: make([]xdr.OperationType, 0, len(operations)),
		}
		for _, operation := range operations {
			facts.operationTypes = append(facts.operationTypes, operation.Body.Type)
		}
		transactions = append(transactions, facts)
	}

	activity := summarizeActivity(transactions)
	return &ValidatorLedgerAnalytics{
		LedgerSequence:                ledger.LedgerSequence(),
		ClosedAtUnix:                  ledger.LedgerCloseTime(),
		LedgerHash:                    ledger.LedgerHash().HexString(),
		PreviousLedgerHash:            ledger.PreviousLedgerHash().HexString(),
		ProtocolVersion:               uint32(header.LedgerVersion),
		NetworkPassphrase:             networkPassphrase,
		TransactionCount:              activity.transactionCount,
		SuccessfulTransactionCount:    activity.successfulTransactionCount,
		FailedTransactionCount:        activity.transactionCount - activity.successfulTransactionCount,
		OperationCount:                activity.operationCount,
		SuccessfulOperationCount:      activity.successfulOperationCount,
		OperationCategories:           activity.operationCategories,
		SuccessfulOperationCategories: activity.successfulCategories,
	}
}

func summarizeActivity(transactions []transactionFacts) activitySummary {
	summary := activitySummary{
		transactionCount:     uint32(len(transactions)),
		operationCategories:  &OperationCategoryCounts{},
		successfulCategories: &OperationCategoryCounts{},
	}

	for _, transaction := range transactions {
		if transaction.successful {
			summary.successfulTransactionCount++
		}
		for _, operationType := range transaction.operationTypes {
			summary.operationCount++
			incrementCategory(summary.operationCategories, classifyOperation(operationType))
			if transaction.successful {
				summary.successfulOperationCount++
				incrementCategory(summary.successfulCategories, classifyOperation(operationType))
			}
		}
	}

	return summary
}

type operationCategory uint8

const (
	categoryAccountCreation operationCategory = iota
	categoryPayments
	categoryOffersAndAMMs
	categoryTrustlines
	categoryClaimableBalances
	categorySponsorship
	categorySoroban
	categoryOther
)

func classifyOperation(operationType xdr.OperationType) operationCategory {
	switch operationType {
	case xdr.OperationTypeCreateAccount:
		return categoryAccountCreation
	case xdr.OperationTypePayment,
		xdr.OperationTypePathPaymentStrictReceive,
		xdr.OperationTypePathPaymentStrictSend:
		return categoryPayments
	case xdr.OperationTypeManageSellOffer,
		xdr.OperationTypeCreatePassiveSellOffer,
		xdr.OperationTypeManageBuyOffer,
		xdr.OperationTypeLiquidityPoolDeposit,
		xdr.OperationTypeLiquidityPoolWithdraw:
		return categoryOffersAndAMMs
	case xdr.OperationTypeChangeTrust,
		xdr.OperationTypeAllowTrust,
		xdr.OperationTypeSetTrustLineFlags:
		return categoryTrustlines
	case xdr.OperationTypeCreateClaimableBalance,
		xdr.OperationTypeClaimClaimableBalance,
		xdr.OperationTypeClawbackClaimableBalance:
		return categoryClaimableBalances
	case xdr.OperationTypeBeginSponsoringFutureReserves,
		xdr.OperationTypeEndSponsoringFutureReserves,
		xdr.OperationTypeRevokeSponsorship:
		return categorySponsorship
	case xdr.OperationTypeInvokeHostFunction,
		xdr.OperationTypeExtendFootprintTtl,
		xdr.OperationTypeRestoreFootprint:
		return categorySoroban
	default:
		return categoryOther
	}
}

func incrementCategory(counts *OperationCategoryCounts, category operationCategory) {
	switch category {
	case categoryAccountCreation:
		counts.AccountCreation++
	case categoryPayments:
		counts.Payments++
	case categoryOffersAndAMMs:
		counts.OffersAndAmms++
	case categoryTrustlines:
		counts.Trustlines++
	case categoryClaimableBalances:
		counts.ClaimableBalances++
	case categorySponsorship:
		counts.Sponsorship++
	case categorySoroban:
		counts.Soroban++
	case categoryOther:
		counts.Other++
	}
}
