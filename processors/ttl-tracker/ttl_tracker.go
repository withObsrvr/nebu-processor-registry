// Package ttl_tracker provides an origin processor that extracts TTL changes from Stellar ledgers.
package ttl_tracker

import (
	"context"
	"fmt"
	"io"

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

// ProcessLedger implements processor.Origin.
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) error {
	sequence := ledger.LedgerSequence()
	closeTime := int64(ledger.LedgerHeaderHistoryEntry().Header.ScpValue.CloseTime)

	reader, err := ingest.NewLedgerTransactionReaderFromLedgerCloseMeta(o.passphrase, ledger)
	if err != nil {
		return fmt.Errorf("error creating transaction reader: %w", err)
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
			return fmt.Errorf("error reading transaction: %w", err)
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

			event := o.processTtlChange(change, sequence, closeTime, txHash, txIndex)
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
			return ctx.Err()
		case o.out <- event:
		}
	}

	return nil
}

func (o *Origin) processTtlChange(
	change ingest.Change,
	sequence uint32,
	closeTime int64,
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
			ClosedAtUnix:     closeTime,
			TxHash:           txHash,
			TransactionIndex: txIndex,
		},
	}
}
