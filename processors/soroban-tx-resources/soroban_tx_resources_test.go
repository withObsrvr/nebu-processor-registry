package soroban_tx_resources

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestCamelToScreamingSnake(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"PaymentSuccess", "PAYMENT_SUCCESS"},
		{"InvokeHostFunctionTrapped", "INVOKE_HOST_FUNCTION_TRAPPED"},
		{"BadSeq", "BAD_SEQ"},
		{"Success", "SUCCESS"},
		{"", ""},
		// Runs of capitals stay together, and a new word after the run splits.
		{"ExtendFootprintTTLSuccess", "EXTEND_FOOTPRINT_TTL_SUCCESS"},
		{"TTL", "TTL"},
	}

	for _, tc := range cases {
		if got := camelToScreamingSnake(tc.in); got != tc.want {
			t.Errorf("camelToScreamingSnake(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalTransactionResultCode(t *testing.T) {
	cases := []struct {
		in   xdr.TransactionResultCode
		want string
	}{
		{xdr.TransactionResultCodeTxSuccess, "tx_SUCCESS"},
		{xdr.TransactionResultCodeTxFailed, "tx_FAILED"},
		{xdr.TransactionResultCodeTxBadSeq, "tx_BAD_SEQ"},
		{xdr.TransactionResultCodeTxInsufficientFee, "tx_INSUFFICIENT_FEE"},
		{xdr.TransactionResultCodeTxFeeBumpInnerFailed, "tx_FEE_BUMP_INNER_FAILED"},
		{xdr.TransactionResultCodeTxBadMinSeqAgeOrGap, "tx_BAD_MIN_SEQ_AGE_OR_GAP"},
	}

	for _, tc := range cases {
		if got := canonicalTransactionResultCode(tc.in); got != tc.want {
			t.Errorf("canonicalTransactionResultCode(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// An unrecognised code must keep its identity rather than be folded into an
// existing bucket — a mislabelled failure code is worse than an obviously
// unknown one.
func TestCanonicalTransactionResultCodeUnknown(t *testing.T) {
	got := canonicalTransactionResultCode(xdr.TransactionResultCode(-9999))
	if got == "tx_SUCCESS" || got == "tx_FAILED" {
		t.Fatalf("unknown code was folded into a known bucket: %q", got)
	}
	if got == "" {
		t.Fatal("unknown code produced an empty string")
	}
}

func TestCanonicalOperationResultCodeInner(t *testing.T) {
	result := xdr.OperationResult{
		Code: xdr.OperationResultCodeOpInner,
		Tr: &xdr.OperationResultTr{
			Type: xdr.OperationTypeInvokeHostFunction,
			InvokeHostFunctionResult: &xdr.InvokeHostFunctionResult{
				Code: xdr.InvokeHostFunctionResultCodeInvokeHostFunctionTrapped,
			},
		},
	}

	want := "op_INVOKE_HOST_FUNCTION_TRAPPED"
	if got := canonicalOperationResultCode(result); got != want {
		t.Errorf("canonicalOperationResultCode = %q, want %q", got, want)
	}
}

// Classic operations resolve through the same generic path as Soroban ones.
func TestCanonicalOperationResultCodeClassic(t *testing.T) {
	result := xdr.OperationResult{
		Code: xdr.OperationResultCodeOpInner,
		Tr: &xdr.OperationResultTr{
			Type: xdr.OperationTypePayment,
			PaymentResult: &xdr.PaymentResult{
				Code: xdr.PaymentResultCodePaymentNoTrust,
			},
		},
	}

	want := "op_PAYMENT_NO_TRUST"
	if got := canonicalOperationResultCode(result); got != want {
		t.Errorf("canonicalOperationResultCode = %q, want %q", got, want)
	}
}

func TestCanonicalOperationResultCodeOuter(t *testing.T) {
	result := xdr.OperationResult{Code: xdr.OperationResultCodeOpBadAuth}

	want := "op_BAD_AUTH"
	if got := canonicalOperationResultCode(result); got != want {
		t.Errorf("canonicalOperationResultCode = %q, want %q", got, want)
	}
}

// A nil inner arm must not panic; it reports the unset arm instead.
func TestCanonicalOperationResultCodeNilArm(t *testing.T) {
	result := xdr.OperationResult{
		Code: xdr.OperationResultCodeOpInner,
		Tr:   &xdr.OperationResultTr{Type: xdr.OperationTypePayment},
	}

	if got := canonicalOperationResultCode(result); got == "" {
		t.Fatal("nil inner arm produced an empty string")
	}
}

func TestScErrorFactsContractCode(t *testing.T) {
	code := xdr.Uint32(4)
	scErr := xdr.ScError{
		Type:         xdr.ScErrorTypeSceContract,
		ContractCode: &code,
	}

	errorType, errorCode, present := scErrorFacts(scErr)
	if !present {
		t.Fatal("expected an error to be reported")
	}
	if errorType != "contract" {
		t.Errorf("errorType = %q, want %q", errorType, "contract")
	}
	if errorCode != 4 {
		t.Errorf("errorCode = %d, want 4", errorCode)
	}
}

// Non-contract errors carry no contract code; the type must still be reported
// so a host fault is distinguishable from a contract declining to proceed.
func TestScErrorFactsNonContract(t *testing.T) {
	scErr := xdr.ScError{Type: xdr.ScErrorTypeSceStorage}

	errorType, errorCode, present := scErrorFacts(scErr)
	if !present {
		t.Fatal("expected an error to be reported")
	}
	if errorType != "storage" {
		t.Errorf("errorType = %q, want %q", errorType, "storage")
	}
	if errorCode != 0 {
		t.Errorf("errorCode = %d, want 0", errorCode)
	}
}

func TestMaxFeePrefersFeeBumpBid(t *testing.T) {
	inner := xdr.TransactionV1Envelope{
		Tx: xdr.Transaction{Fee: xdr.Uint32(100)},
	}
	envelope := xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTxFeeBump,
		FeeBump: &xdr.FeeBumpTransactionEnvelope{
			Tx: xdr.FeeBumpTransaction{
				Fee: xdr.Int64(5000),
				InnerTx: xdr.FeeBumpTransactionInnerTx{
					Type: xdr.EnvelopeTypeEnvelopeTypeTx,
					V1:   &inner,
				},
			},
		},
	}

	if got := maxFee(envelope); got != 5000 {
		t.Errorf("maxFee = %d, want 5000 (the outer bid)", got)
	}
}

// A classic V0 envelope predates Soroban and must report no footprint rather
// than a zero-valued one that would read as "declared nothing".
func TestSorobanDataAbsentForV0(t *testing.T) {
	envelope := xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTxV0,
		V0:   &xdr.TransactionV0Envelope{},
	}

	if _, ok := sorobanData(envelope); ok {
		t.Error("expected no Soroban data on a V0 envelope")
	}
}
