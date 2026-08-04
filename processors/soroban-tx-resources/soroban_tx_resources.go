// Package soroban_tx_resources provides a Nebu origin processor that extracts
// per-transaction Soroban resource footprints, decoded result codes, fees, and
// envelope sizes from Stellar ledgers.
//
// It exists because no other origin carries these facts. contract-invocation
// reports whether a call succeeded but not why, and reports what state changed
// but not what resources were reserved. The distinction matters: the protocol
// charges a ledger's capacity against the entries DECLARED in each
// transaction's readWrite footprint, not against the entries observed to
// change. Only the declared footprint can answer "was this ledger write-bound".
package soroban_tx_resources

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
)

const processorName = "stellar/soroban-tx-resources"

// Origin emits one SorobanTxResources event per transaction, in apply order.
type Origin struct {
	networkPassphrase string
	out               chan *SorobanTxResources
}

// NewOrigin creates a soroban-tx-resources origin processor.
func NewOrigin(networkPassphrase string) *Origin {
	return &Origin{
		networkPassphrase: networkPassphrase,
		out:               make(chan *SorobanTxResources, 256),
	}
}

func (o *Origin) Name() string                    { return processorName }
func (o *Origin) Type() processor.Type            { return processor.TypeOrigin }
func (o *Origin) Out() <-chan *SorobanTxResources { return o.out }
func (o *Origin) Close()                          { close(o.out) }

// ProcessLedger emits one event per transaction in the ledger's transaction
// set, failed transactions included. A transaction whose envelope cannot be
// marshaled still emits, with envelope_size_bytes left at zero and a warning
// reported — a missing size is better than a dropped transaction, because a
// dropped one silently understates ledger utilisation.
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) {
	envelopes := ledger.TransactionEnvelopes()

	for index := range envelopes {
		event := o.buildEvent(ctx, ledger, index, envelopes[index])

		select {
		case <-ctx.Done():
			return
		case o.out <- event:
		}
	}
}

func (o *Origin) buildEvent(
	ctx context.Context,
	ledger xdr.LedgerCloseMeta,
	index int,
	envelope xdr.TransactionEnvelope,
) *SorobanTxResources {
	resultPair := ledger.TransactionResultPair(index)

	event := &SorobanTxResources{
		Successful:        resultPair.Successful(),
		ResultCode:        canonicalTransactionResultCode(resultPair.Result.Result.Code),
		SourceAccount:     envelope.SourceAccount().ToAccountId().Address(),
		OperationCount:    envelope.OperationsCount(),
		IsFeeBump:         envelope.IsFeeBump(),
		FeeChargedStroops: int64(resultPair.Result.FeeCharged),
		MaxFeeStroops:     maxFee(envelope),
		Meta: &EventMeta{
			LedgerSequence:   ledger.LedgerSequence(),
			ClosedAtUnix:     ledger.LedgerCloseTime(),
			TxHash:           ledger.TransactionHash(index).HexString(),
			TransactionIndex: uint32(index),
		},
	}

	if results, ok := resultPair.OperationResults(); ok {
		event.OperationResultCodes = make([]string, 0, len(results))
		for _, result := range results {
			event.OperationResultCodes = append(event.OperationResultCodes, canonicalOperationResultCode(result))
		}
	}

	if size, err := envelopeSizeBytes(envelope); err != nil {
		processor.ReportWarning(ctx, o.Name(), fmt.Errorf(
			"ledger %d tx %d: marshal envelope: %w", ledger.LedgerSequence(), index, err))
	} else {
		event.EnvelopeSizeBytes = size
	}

	if data, ok := sorobanData(envelope); ok {
		event.IsSoroban = true
		event.ResourceFeeStroops = int64(data.ResourceFee)
		event.Instructions = uint32(data.Resources.Instructions)
		event.DiskReadBytes = uint32(data.Resources.DiskReadBytes)
		event.WriteBytes = uint32(data.Resources.WriteBytes)
		event.ReadOnlyEntries = uint32(len(data.Resources.Footprint.ReadOnly))
		event.ReadWriteEntries = uint32(len(data.Resources.Footprint.ReadWrite))
	}

	applyMeta := ledger.TxApplyProcessing(index)
	if errorType, errorCode, present := contractError(&applyMeta); present {
		event.ContractErrorPresent = true
		event.ContractErrorType = errorType
		event.ContractErrorCode = errorCode
	}

	return event
}

// maxFee returns the fee bid. For a fee bump the outer bid is what the network
// considered, so it takes precedence over the inner transaction's fee.
func maxFee(envelope xdr.TransactionEnvelope) int64 {
	if envelope.IsFeeBump() {
		return envelope.FeeBumpFee()
	}
	return int64(envelope.Fee())
}

func envelopeSizeBytes(envelope xdr.TransactionEnvelope) (uint32, error) {
	encoded, err := envelope.MarshalBinary()
	if err != nil {
		return 0, err
	}
	return uint32(len(encoded)), nil
}

// sorobanData returns the SorobanTransactionData for V1 and fee-bump-wrapped V1
// transactions. V0 envelopes predate Soroban and never carry it.
func sorobanData(envelope xdr.TransactionEnvelope) (xdr.SorobanTransactionData, bool) {
	switch envelope.Type {
	case xdr.EnvelopeTypeEnvelopeTypeTx:
		if envelope.V1 == nil {
			return xdr.SorobanTransactionData{}, false
		}
		return envelope.V1.Tx.Ext.GetSorobanData()
	case xdr.EnvelopeTypeEnvelopeTypeTxFeeBump:
		if envelope.FeeBump == nil || envelope.FeeBump.Tx.InnerTx.V1 == nil {
			return xdr.SorobanTransactionData{}, false
		}
		return envelope.FeeBump.Tx.InnerTx.V1.Tx.Ext.GetSorobanData()
	default:
		return xdr.SorobanTransactionData{}, false
	}
}

// contractError scans the transaction's diagnostic events for an ScError and
// returns the first one found. The first is used rather than the last because
// a contract that errors typically unwinds, and later events describe the
// unwinding rather than the cause.
func contractError(meta *xdr.TransactionMeta) (errorType string, errorCode uint32, present bool) {
	events, err := meta.GetDiagnosticEvents()
	if err != nil {
		return "", 0, false
	}

	for _, event := range events {
		body, ok := event.Event.Body.GetV0()
		if !ok {
			continue
		}
		for _, topic := range body.Topics {
			if scErr, ok := topic.GetError(); ok {
				return scErrorFacts(scErr)
			}
		}
		if scErr, ok := body.Data.GetError(); ok {
			return scErrorFacts(scErr)
		}
	}
	return "", 0, false
}

func scErrorFacts(scErr xdr.ScError) (string, uint32, bool) {
	errorType := canonicalScErrorType(scErr.Type)
	if scErr.ContractCode != nil {
		return errorType, uint32(*scErr.ContractCode), true
	}
	return errorType, 0, true
}

// canonicalTransactionResultCode renders the Horizon-style string operators
// recognise, e.g. "tx_BAD_SEQ". The SDK's own String() returns a Go identifier
// ("TransactionResultCodeTxBadSeq"), which is not what anyone reads in a UI or
// stores in a projection.
func canonicalTransactionResultCode(code xdr.TransactionResultCode) string {
	return canonicalEnumCode(code, "tx")
}

// canonicalOperationResultCode resolves the operation result, descending into
// the inner union when the outer code is opINNER.
//
// The inner arm is resolved generically rather than with a switch over the
// 25-odd operation types: every arm carries a Code field whose String() follows
// the same generated naming convention, so one reflective lookup handles
// Soroban and classic alike and will not silently miss an operation type added
// by a future protocol.
func canonicalOperationResultCode(result xdr.OperationResult) string {
	if result.Code != xdr.OperationResultCodeOpInner || result.Tr == nil {
		return canonicalEnumCode(result.Code, "op")
	}

	armName, ok := result.Tr.ArmForSwitch(int32(result.Tr.Type))
	if !ok {
		return fmt.Sprintf("op_UNKNOWN_TYPE_%d", int32(result.Tr.Type))
	}

	arm := reflect.ValueOf(*result.Tr).FieldByName(armName)
	if !arm.IsValid() || arm.Kind() != reflect.Ptr || arm.IsNil() {
		return fmt.Sprintf("op_UNSET_%s", camelToScreamingSnake(armName))
	}

	codeField := arm.Elem().FieldByName("Code")
	if !codeField.IsValid() {
		return fmt.Sprintf("op_NO_CODE_%s", camelToScreamingSnake(armName))
	}

	stringer, ok := codeField.Interface().(fmt.Stringer)
	if !ok {
		return fmt.Sprintf("op_NO_CODE_%s", camelToScreamingSnake(armName))
	}
	return canonicalEnumCode(stringer, "op")
}

// canonicalEnumCode turns a generated XDR enum into "<prefix>_SCREAMING_SNAKE".
//
// The generated String() returns the Go constant name, which always begins with
// the enum's own type name — "PaymentResultCode" + "PaymentSuccess". Stripping
// that prefix leaves the meaningful part. The discriminator that duplicates the
// output prefix ("Tx" for tx codes, "Op" for op codes) is dropped too, so
// TransactionResultCodeTxBadSeq becomes tx_BAD_SEQ rather than tx_TX_BAD_SEQ.
//
// Unknown values keep their numeric identity instead of being folded into an
// existing bucket, because a silently mislabelled failure code is worse than an
// obviously unrecognised one.
func canonicalEnumCode(value fmt.Stringer, prefix string) string {
	name := value.String()
	if name == "" {
		return fmt.Sprintf("%s_UNKNOWN_%v", prefix, value)
	}

	typeName := reflect.TypeOf(value).Name()
	trimmed := strings.TrimPrefix(name, typeName)
	if trimmed == "" || trimmed == name {
		// The convention did not hold; surface the raw name rather than guess.
		return fmt.Sprintf("%s_%s", prefix, camelToScreamingSnake(name))
	}

	// Drop a leading discriminator that would duplicate the prefix.
	switch prefix {
	case "tx":
		trimmed = strings.TrimPrefix(trimmed, "Tx")
	case "op":
		trimmed = strings.TrimPrefix(trimmed, "Op")
	}

	return fmt.Sprintf("%s_%s", prefix, camelToScreamingSnake(trimmed))
}

// camelToScreamingSnake converts "InvokeHostFunctionTrapped" to
// "INVOKE_HOST_FUNCTION_TRAPPED". Runs of capitals are kept together so "TTL"
// does not become "T_T_L".
func camelToScreamingSnake(input string) string {
	runes := []rune(input)
	var out strings.Builder
	out.Grow(len(runes) + 8)

	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			next := rune(0)
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			// Boundary when leaving a lowercase run, or when a run of capitals
			// ends and a new word starts (e.g. "TTLSuccess" -> "TTL_SUCCESS").
			if !unicode.IsUpper(prev) || (next != 0 && unicode.IsLower(next)) {
				out.WriteByte('_')
			}
		}
		out.WriteRune(unicode.ToUpper(r))
	}
	return out.String()
}

func canonicalScErrorType(errorType xdr.ScErrorType) string {
	switch errorType {
	case xdr.ScErrorTypeSceContract:
		return "contract"
	case xdr.ScErrorTypeSceWasmVm:
		return "wasm_vm"
	case xdr.ScErrorTypeSceContext:
		return "context"
	case xdr.ScErrorTypeSceStorage:
		return "storage"
	case xdr.ScErrorTypeSceObject:
		return "object"
	case xdr.ScErrorTypeSceCrypto:
		return "crypto"
	case xdr.ScErrorTypeSceEvents:
		return "events"
	case xdr.ScErrorTypeSceBudget:
		return "budget"
	case xdr.ScErrorTypeSceValue:
		return "value"
	case xdr.ScErrorTypeSceAuth:
		return "auth"
	default:
		return fmt.Sprintf("unknown_%d", int32(errorType))
	}
}
