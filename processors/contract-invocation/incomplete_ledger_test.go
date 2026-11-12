package contract_invocation

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

func TestProcessLedgerDeclinesAnIncompleteRead(t *testing.T) {
	// The success map is consulted by hash and a miss resolves to false, so
	// a partial read would report successful invocations as failed. The
	// processor must decline the whole ledger rather than use part of it.
	//
	// The recorded hash below belongs to no envelope in the set, which is
	// what makes the read fail partway through.
	lcm := xdr.LedgerCloseMeta{
		V: 0,
		V0: &xdr.LedgerCloseMetaV0{
			LedgerHeader: xdr.LedgerHeaderHistoryEntry{
				Header: xdr.LedgerHeader{LedgerSeq: 4242},
			},
			TxSet: xdr.TransactionSet{Txs: []xdr.TransactionEnvelope{testEnvelope(t, 1)}},
			TxProcessing: []xdr.TransactionResultMeta{{
				Result: xdr.TransactionResultPair{TransactionHash: xdr.Hash{0xde, 0xad, 0xbe, 0xef}},
			}},
		},
	}

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

	origin.ProcessLedger(ctx, lcm)

	select {
	case <-emitted:
		t.Fatal("events were emitted from an incomplete read")
	case <-time.After(250 * time.Millisecond):
	}

	require.Len(t, reports, 1, "the unreadable transaction must be reported")
	assert.Equal(t, processor.SeverityWarning, reports[0].Severity)
	assert.Contains(t, reports[0].Err.Error(), "4242")
}

func TestProcessLedgerAcceptsAnEmptyLedger(t *testing.T) {
	// A ledger with no transactions is a complete read of nothing, not a
	// failure; reporting it would make every empty ledger look broken.
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
