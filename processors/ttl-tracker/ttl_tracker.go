// Package ttl_tracker provides an origin processor that extracts TTL changes from Stellar ledgers.
package ttl_tracker

import (
	"context"
	"fmt"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
	"github.com/withObsrvr/nebu/pkg/processor/ledgerread"
)

// Origin is an origin processor that extracts TTL (time-to-live) changes.
type Origin struct {
	passphrase string
	out        chan *TtlEvent
}

// NewOrigin creates a new TTL tracker origin processor.
func NewOrigin(passphrase string) *Origin {
	return &Origin{
		passphrase: passphrase,
		out:        make(chan *TtlEvent, 128),
	}
}

// Name implements processor.Processor.
func (o *Origin) Name() string {
	return "stellar/ttl-tracker"
}

// Type implements processor.Processor.
func (o *Origin) Type() processor.Type {
	return processor.TypeOrigin
}

// Out returns the output channel for consuming emitted events.
func (o *Origin) Out() <-chan *TtlEvent {
	return o.out
}

// Close closes the output channel.
func (o *Origin) Close() {
	close(o.out)
}

// ProcessLedger implements processor.Origin. Per-ledger errors are
// reported via processor.ReportWarning; the pipeline continues to
// the next ledger (streams-never-throw).
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) {
	sequence := ledger.LedgerSequence()
	closeTimeMs := processor.LedgerCloseTimeMs(ledger)

	// AllTransactions, not Transactions, because of what `seen` does with
	// the result. Dedup here is last-write-wins, so if the last write for a
	// key lives in a transaction that could not be read, skipping it does
	// not drop the key — it emits the *earlier* value as though it were
	// current. That is a stale fact presented as a fresh one, which is the
	// same failure as a missing map entry falling through to a default, so
	// an incomplete read must decline the ledger. Already reported.
	transactions, ok := ledgerread.AllTransactions(ctx, o.Name(), o.passphrase, ledger)
	if !ok {
		return
	}

	// Track seen entries for dedup within this ledger (last write wins)
	seen := make(map[string]*TtlEvent)

	for _, tx := range transactions {

		txHash := tx.Result.TransactionHash.HexString()
		txIndex := uint32(tx.Index)

		changes, err := tx.GetChanges()
		if err != nil {
			continue
		}

		for _, change := range changes {
			if change.Type != xdr.LedgerEntryTypeTtl {
				continue
			}

			event := o.processTtlChange(change, sequence, closeTimeMs, txHash, txIndex)
			if event != nil {
				// Dedup key: keyHash within this ledger
				seen[event.KeyHash] = event
			}
		}
	}

	// Emit deduplicated events
	for _, event := range seen {
		select {
		case <-ctx.Done():
			return
		case o.out <- event:
		}
	}
}

func (o *Origin) processTtlChange(
	change ingest.Change,
	sequence uint32,
	closeTimeMs int64,
	txHash string,
	txIndex uint32,
) *TtlEvent {
	var operation string
	var ttlEntry *xdr.TtlEntry

	switch change.ChangeType {
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
		operation = "create"
		if change.Post != nil && change.Post.Data.Type == xdr.LedgerEntryTypeTtl {
			entry := change.Post.Data.MustTtl()
			ttlEntry = &entry
		}
	case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
		operation = "update"
		if change.Post != nil && change.Post.Data.Type == xdr.LedgerEntryTypeTtl {
			entry := change.Post.Data.MustTtl()
			ttlEntry = &entry
		}
	case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
		operation = "delete"
		if change.Pre != nil && change.Pre.Data.Type == xdr.LedgerEntryTypeTtl {
			entry := change.Pre.Data.MustTtl()
			ttlEntry = &entry
		}
	default:
		return nil
	}

	if ttlEntry == nil {
		return nil
	}

	// Extract key hash
	keyHash := fmt.Sprintf("%x", ttlEntry.KeyHash[:])

	// Extract expiration ledger
	expirationLedger := uint32(ttlEntry.LiveUntilLedgerSeq)

	return &TtlEvent{
		KeyHash:          keyHash,
		ExpirationLedger: expirationLedger,
		Operation:        operation,
		Meta: &EventMeta{
			LedgerSequence:   sequence,
			ClosedAtUnix:     closeTimeMs / 1000,
			ClosedAt:         processor.FormatCloseTimeRFC3339(closeTimeMs),
			TxHash:           txHash,
			TransactionIndex: txIndex,
		},
	}
}
