// Package contract_state provides an origin processor that extracts contract data state changes from Stellar ledgers.
package contract_state

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
)

// Origin is an origin processor that extracts contract data state changes.
type Origin struct {
	passphrase string
	out        chan *ContractStateEvent
}

// NewOrigin creates a new contract state origin processor.
func NewOrigin(passphrase string) *Origin {
	return &Origin{
		passphrase: passphrase,
		out:        make(chan *ContractStateEvent, 128),
	}
}

// Name implements processor.Processor.
func (o *Origin) Name() string {
	return "stellar/contract-state"
}

// Type implements processor.Processor.
func (o *Origin) Type() processor.Type {
	return processor.TypeOrigin
}

// Out returns the output channel for consuming emitted events.
func (o *Origin) Out() <-chan *ContractStateEvent {
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
	seen := make(map[string]*ContractStateEvent)

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
			if change.Type != xdr.LedgerEntryTypeContractData {
				continue
			}

			event := o.processContractDataChange(change, sequence, closeTime, txHash, txIndex)
			if event != nil {
				// Dedup key: contractId + ledgerKeyHash
				dedupKey := event.ContractId + "|" + event.LedgerKeyHash
				seen[dedupKey] = event
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

func (o *Origin) processContractDataChange(
	change ingest.Change,
	sequence uint32,
	closeTime int64,
	txHash string,
	txIndex uint32,
) *ContractStateEvent {
	var operation string
	var contractData *xdr.ContractDataEntry
	var keyHash string

	switch change.ChangeType {
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
		operation = "create"
		if change.Post != nil && change.Post.Data.Type == xdr.LedgerEntryTypeContractData {
			cd := change.Post.Data.MustContractData()
			contractData = &cd
		}
	case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
		operation = "update"
		if change.Post != nil && change.Post.Data.Type == xdr.LedgerEntryTypeContractData {
			cd := change.Post.Data.MustContractData()
			contractData = &cd
		}
	case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
		operation = "delete"
		if change.Pre != nil && change.Pre.Data.Type == xdr.LedgerEntryTypeContractData {
			cd := change.Pre.Data.MustContractData()
			contractData = &cd
		}
	default:
		return nil
	}

	if contractData == nil {
		return nil
	}

	// Extract contract ID
	contractIDBytes := contractData.Contract.ContractId
	if contractIDBytes == nil {
		return nil
	}
	contractID, err := strkey.Encode(strkey.VersionByteContract, contractIDBytes[:])
	if err != nil {
		return nil
	}

	// Compute ledger key hash
	keyXDR, err := contractData.Key.MarshalBinary()
	if err != nil {
		return nil
	}
	hash := sha256.Sum256(keyXDR)
	keyHash = fmt.Sprintf("%x", hash[:])

	// Extract key symbol if the key is a simple symbol
	keySymbol := extractKeySymbol(contractData.Key)

	// Encode key and value as base64 XDR
	keyB64 := base64.StdEncoding.EncodeToString(keyXDR)

	var valB64 string
	if operation != "delete" {
		valXDR, err := contractData.Val.MarshalBinary()
		if err == nil {
			valB64 = base64.StdEncoding.EncodeToString(valXDR)
		}
	}

	// Determine durability
	durability := Durability_PERSISTENT
	if contractData.Durability == xdr.ContractDataDurabilityTemporary {
		durability = Durability_TEMPORARY
	}

	return &ContractStateEvent{
		ContractId:   contractID,
		LedgerKeyHash: keyHash,
		Durability:   durability,
		Operation:    operation,
		KeyXdr:       keyB64,
		ValXdr:       valB64,
		KeySymbol:    keySymbol,
		Meta: &EventMeta{
			LedgerSequence: sequence,
			ClosedAtUnix:   closeTime,
			TxHash:         txHash,
			TransactionIndex: txIndex,
		},
	}
}

// extractKeySymbol returns the symbol string if the key is a simple ScValTypeScvSymbol.
func extractKeySymbol(key xdr.ScVal) string {
	if key.Type == xdr.ScValTypeScvSymbol {
		return string(key.MustSym())
	}
	return ""
}
