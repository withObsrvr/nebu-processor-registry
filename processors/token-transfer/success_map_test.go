package token_transfer

import (
	"context"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/withObsrvr/nebu/pkg/processor"
)

const testPassphrase = network.TestNetworkPassphrase

func testEnvelope(t *testing.T, seqNum xdr.SequenceNumber) xdr.TransactionEnvelope {
	t.Helper()

	source := xdr.MustAddress("GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF")
	return xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: source.ToMuxedAccount(),
				Fee:           100,
				SeqNum:        seqNum,
				Operations:    []xdr.Operation{},
			},
		},
	}
}

func txHash(t *testing.T, envelope xdr.TransactionEnvelope) xdr.Hash {
	t.Helper()

	hash, err := network.HashTransactionInEnvelope(envelope, testPassphrase)
	require.NoError(t, err)
	return xdr.Hash(hash)
}

// ledgerWithUnreadableTransaction records a hash that belongs to no envelope
// in the set, which is what makes the reader fail partway through the ledger.
func ledgerWithUnreadableTransaction(t *testing.T, sequence uint32) xdr.LedgerCloseMeta {
	t.Helper()

	good := testEnvelope(t, 1)
	return xdr.LedgerCloseMeta{
		V: 0,
		V0: &xdr.LedgerCloseMetaV0{
			LedgerHeader: xdr.LedgerHeaderHistoryEntry{
				Header: xdr.LedgerHeader{LedgerSeq: xdr.Uint32(sequence)},
			},
			TxSet: xdr.TransactionSet{Txs: []xdr.TransactionEnvelope{good}},
			TxProcessing: []xdr.TransactionResultMeta{{
				Result: xdr.TransactionResultPair{TransactionHash: xdr.Hash{0xde, 0xad, 0xbe, 0xef}},
			}},
		},
	}
}

func TestProcessLedgerDoesNotEmitFromAnIncompleteSuccessMap(t *testing.T) {
	// The regression. The success-map loop broke on every reader error, so a
	// mid-ledger decode failure truncated the map — and a transaction missing
	// from the map falls through to `successful := true`. The result was not
	// lost data but wrong data: failed transfers reported as successful, with
	// no warning anywhere.
	//
	// The read now goes through ledgerread.AllTransactions, so the wording of
	// the warning belongs to nebu rather than to this processor. What is
	// asserted here is what this processor is responsible for: that an
	// incomplete read is reported, names the ledger, and emits nothing.
	origin := NewOrigin(testPassphrase)
	defer origin.Close()

	var reports []processor.ErrorReport
	ctx := processor.WithReporter(context.Background(),
		processor.ReporterFunc(func(report processor.ErrorReport) {
			reports = append(reports, report)
		}))

	emitted := make(chan struct{}, 1)
	go func() {
		for range origin.Out() {
			select {
			case emitted <- struct{}{}:
			default:
			}
		}
	}()

	origin.ProcessLedger(ctx, ledgerWithUnreadableTransaction(t, 4242))

	select {
	case <-emitted:
		t.Fatal("events were emitted from an incomplete success map")
	case <-time.After(250 * time.Millisecond):
	}

	require.Len(t, reports, 1, "an unreadable transaction must be reported, not swallowed as EOF")
	assert.Equal(t, processor.SeverityWarning, reports[0].Severity)
	assert.Equal(t, origin.Name(), reports[0].Processor)
	assert.Contains(t, reports[0].Err.Error(), "4242", "the report must name the ledger")
}

func TestProcessLedgerReportsNothingForAnEmptyLedger(t *testing.T) {
	// End of ledger is not a failure: a ledger with no transactions must not
	// produce a warning, or every clean run becomes noisy.
	origin := NewOrigin(testPassphrase)
	defer origin.Close()

	var reports []processor.ErrorReport
	ctx := processor.WithReporter(context.Background(),
		processor.ReporterFunc(func(report processor.ErrorReport) {
			reports = append(reports, report)
		}))

	go func() {
		for range origin.Out() {
		}
	}()

	origin.ProcessLedger(ctx, xdr.LedgerCloseMeta{
		V: 0,
		V0: &xdr.LedgerCloseMetaV0{
			LedgerHeader: xdr.LedgerHeaderHistoryEntry{
				Header: xdr.LedgerHeader{LedgerSeq: 4243},
			},
		},
	})

	assert.Empty(t, reports)
}
