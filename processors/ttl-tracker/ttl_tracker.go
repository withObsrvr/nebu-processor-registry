// Package ttl_tracker provides an origin processor that extracts TTL changes from Stellar ledgers.
package ttl_tracker

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
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

	reader, err := ingest.NewLedgerTransactionReaderFromLedgerCloseMeta(o.passphrase, ledger)
	if err != nil {
		processor.ReportWarning(ctx, o.Name(),
			fmt.Errorf("ledger %d: create tx reader: %w", sequence, err))
		return
	}
	defer reader.Close()

	// Track seen entries for dedup within this ledger (last write wins)
	seen := make(map[string]*TtlEvent)

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
			ClosedAt:         closedAtRFC3339(closeTimeMs),
			TxHash:           txHash,
			TransactionIndex: txIndex,
		},
	}
}

// closedAtRFC3339 renders a millisecond Unix timestamp as the RFC 3339 UTC
// string the envelope's closed_at field carries (DATA-3).
//
// Fixed-width with three decimal places rather than time.RFC3339Nano, which
// drops trailing zeros: uniform width keeps values lexicographically
// sortable and lets a human line them up.
func closedAtRFC3339(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
