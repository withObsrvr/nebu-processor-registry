// Package contract_state provides an origin processor that extracts contract data state changes from Stellar ledgers.
package contract_state

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
	"github.com/withObsrvr/nebu/pkg/processor/ledgerread"
)

// contractIDFromScAddress returns the C-address of the contract an ScAddress
// designates: the address itself for a plain contract, or the multiplexed
// contract for a CAP-84 muxed contract address. Use this wherever a contract
// is being identified rather than displayed, so a W-address still resolves to
// the contract it multiplexes.
//
// Reading addr.ContractId directly is what this replaces: each arm of the
// union populates only its own pointer, so that read is nil on a muxed
// address.
func contractIDFromScAddress(addr xdr.ScAddress) (string, bool) {
	var raw xdr.ContractId
	switch addr.Type {
	case xdr.ScAddressTypeScAddressTypeContract:
		raw = addr.MustContractId()
	case xdr.ScAddressTypeScAddressTypeMuxedContract:
		raw = addr.MustMuxedContract().ContractId
	default:
		return "", false
	}
	encoded, err := strkey.Encode(strkey.VersionByteContract, raw[:])
	if err != nil {
		return "", false
	}
	return encoded, true
}

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

// ProcessLedger implements processor.Origin. Per-ledger errors are
// reported via processor.ReportWarning; the pipeline continues
// (streams-never-throw).
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
	seen := make(map[string]*ContractStateEvent)

	for _, tx := range transactions {

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

			event := o.processContractDataChange(change, sequence, closeTimeMs, txHash, txIndex)
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
			return
		case o.out <- event:
		}
	}
}

func (o *Origin) processContractDataChange(
	change ingest.Change,
	sequence uint32,
	closeTimeMs int64,
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

	// Extract contract ID. Reading Contract.ContractId directly is nil for a
	// CAP-84 muxed contract address, which the old nil guard turned into a
	// silently dropped state entry.
	contractID, ok := contractIDFromScAddress(contractData.Contract)
	if !ok {
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
		ContractId:    contractID,
		LedgerKeyHash: keyHash,
		Durability:    durability,
		Operation:     operation,
		KeyXdr:        keyB64,
		ValXdr:        valB64,
		KeySymbol:     keySymbol,
		Meta: &EventMeta{
			LedgerSequence:   sequence,
			ClosedAtUnix:     closeTimeMs / 1000,
			ClosedAt:         processor.FormatCloseTimeRFC3339(closeTimeMs),
			TxHash:           txHash,
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
