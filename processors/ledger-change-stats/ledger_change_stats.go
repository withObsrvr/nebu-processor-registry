// Package ledger_change_stats provides a Nebu origin processor that counts
// every ledger entry change in a ledger, split by change type, by the reason
// the change occurred, and by the type of entry that changed — plus the
// entries evicted from the ledger at close.
//
// It walks the full change stream via the ingest change reader, so unlike
// contract-state it sees classic entries (accounts, trustlines, offers,
// claimable balances, liquidity pools) alongside Soroban ones, and includes
// fee, refund, and protocol-upgrade changes rather than operation changes
// only.
package ledger_change_stats

import (
	"context"
	"fmt"
	"io"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
)

const processorName = "ledger-change-stats"

// entryTypeOrder fixes the output ordering of the per-type breakdowns. Ordering
// by the XDR enum rather than by map iteration keeps output byte-identical
// across runs, which replayable pipelines depend on.
var entryTypeOrder = []xdr.LedgerEntryType{
	xdr.LedgerEntryTypeAccount,
	xdr.LedgerEntryTypeTrustline,
	xdr.LedgerEntryTypeOffer,
	xdr.LedgerEntryTypeData,
	xdr.LedgerEntryTypeClaimableBalance,
	xdr.LedgerEntryTypeLiquidityPool,
	xdr.LedgerEntryTypeContractData,
	xdr.LedgerEntryTypeContractCode,
	xdr.LedgerEntryTypeConfigSetting,
	xdr.LedgerEntryTypeTtl,
}

// Origin emits one LedgerChangeStats event per ledger.
type Origin struct {
	passphrase string
	emitter    *processor.Emitter[*LedgerChangeStats]
}

// NewOrigin creates a ledger-change-stats origin processor.
func NewOrigin(passphrase string) *Origin {
	return &Origin{
		passphrase: passphrase,
		emitter:    processor.NewEmitter[*LedgerChangeStats](128),
	}
}

func (o *Origin) Name() string                   { return processorName }
func (o *Origin) Type() processor.Type           { return processor.TypeOrigin }
func (o *Origin) Out() <-chan *LedgerChangeStats { return o.emitter.Out() }
func (o *Origin) Close()                         { o.emitter.Close() }

// ProcessLedger counts the ledger's changes and evictions and emits one event.
//
// A ledger whose change stream cannot be read is skipped with a warning rather
// than emitted partially: a truncated count is indistinguishable from a quiet
// ledger downstream, and silently understating change volume is worse than a
// visible gap.
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) {
	sequence := ledger.LedgerSequence()

	reader, err := ingest.NewLedgerChangeReaderFromLedgerCloseMeta(o.passphrase, ledger)
	if err != nil {
		processor.ReportWarning(ctx, o.Name(), fmt.Errorf("ledger %d: create change reader: %w", sequence, err))
		return
	}
	defer reader.Close()

	accumulator := newChangeAccumulator(sequence, ledger.LedgerCloseTime())

	for {
		change, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			processor.ReportWarning(ctx, o.Name(), fmt.Errorf("ledger %d: read change: %w", sequence, err))
			return
		}
		accumulator.add(change)
	}

	event := accumulator.finish()
	o.countEvictions(ctx, ledger, event)

	select {
	case <-ctx.Done():
		return
	default:
		o.emitter.Emit(event)
	}
}

// changeAccumulator tallies a ledger's change stream. It is separate from
// ProcessLedger so the counting rules can be tested directly against
// constructed changes: restores and evictions are rare enough on mainnet that
// waiting to observe one is not a test strategy.
type changeAccumulator struct {
	event  *LedgerChangeStats
	byType map[xdr.LedgerEntryType]*EntryTypeChangeCounts
}

func newChangeAccumulator(sequence uint32, closedAtUnix int64) *changeAccumulator {
	return &changeAccumulator{
		event: &LedgerChangeStats{
			LedgerSequence: sequence,
			ClosedAtUnix:   closedAtUnix,
		},
		byType: make(map[xdr.LedgerEntryType]*EntryTypeChangeCounts, len(entryTypeOrder)),
	}
}

func (a *changeAccumulator) add(change ingest.Change) {
	counts, ok := a.byType[change.Type]
	if !ok {
		counts = &EntryTypeChangeCounts{EntryType: canonicalEntryType(change.Type)}
		a.byType[change.Type] = counts
	}

	a.event.TotalChanges++
	counts.Total++

	// ChangeType is read from the XDR rather than derived from whether Pre/Post
	// are nil. A restore and a create both present as "no Pre, some Post", so
	// deriving it cannot tell them apart.
	switch change.ChangeType {
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
		a.event.LedgerEntriesCreated++
		counts.Created++
	case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
		a.event.LedgerEntriesUpdated++
		counts.Updated++
	case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
		a.event.LedgerEntriesDeleted++
		counts.Deleted++
	case xdr.LedgerEntryChangeTypeLedgerEntryRestored:
		a.event.LedgerEntriesRestored++
		counts.Restored++
	case xdr.LedgerEntryChangeTypeLedgerEntryState:
		a.event.LedgerEntriesState++
		counts.State++
	}

	switch change.Reason {
	case ingest.LedgerEntryChangeReasonFee:
		a.event.FeeRelatedChanges++
	case ingest.LedgerEntryChangeReasonFeeRefund:
		a.event.FeeRefundRelatedChanges++
	case ingest.LedgerEntryChangeReasonTransaction:
		a.event.TxRelatedChanges++
	case ingest.LedgerEntryChangeReasonOperation:
		a.event.OperationRelatedChanges++
	case ingest.LedgerEntryChangeReasonUpgrade:
		a.event.UpgradeRelatedChanges++
	default:
		a.event.UnknownReasonChanges++
	}
}

func (a *changeAccumulator) finish() *LedgerChangeStats {
	a.event.EntryTypes = orderedEntryTypes(a.byType)
	return a.event
}

// evictionMetaVersions are the LedgerCloseMeta versions that report evicted
// keys. V0 predates eviction entirely.
//
// This is checked before calling EvictedLedgerKeys rather than relying on its
// return, because that method reports "not supported" in two ways that are
// both indistinguishable from a real answer: V0 returns (nil, nil), which reads
// as a genuine zero, and an unrecognised version panics rather than erroring.
var evictionMetaVersions = map[int32]bool{1: true, 2: true}

// countEvictions records entries evicted at ledger close. Eviction is carried
// on LedgerCloseMeta rather than in the change stream, so it has to be read
// separately.
//
// eviction_available stays false unless the meta version genuinely reports
// evictions, so downstream can tell "nothing was evicted" from "this ledger
// cannot say".
func (o *Origin) countEvictions(ctx context.Context, ledger xdr.LedgerCloseMeta, event *LedgerChangeStats) {
	if !evictionMetaVersions[ledger.V] {
		processor.ReportWarning(ctx, o.Name(), fmt.Errorf(
			"ledger %d: LedgerCloseMeta v%d does not report evicted keys",
			ledger.LedgerSequence(), ledger.V))
		return
	}

	keys, err := ledger.EvictedLedgerKeys()
	if err != nil {
		processor.ReportWarning(ctx, o.Name(), fmt.Errorf(
			"ledger %d: read evicted keys: %w", ledger.LedgerSequence(), err))
		return
	}

	event.EvictionAvailable = true
	event.EvictedKeys = uint32(len(keys))
	event.EvictedByType = evictedByType(keys)
}

func evictedByType(keys []xdr.LedgerKey) []*EntryTypeCount {
	if len(keys) == 0 {
		return nil
	}

	counts := make(map[xdr.LedgerEntryType]uint32, len(entryTypeOrder))
	for _, key := range keys {
		counts[key.Type]++
	}

	ordered := make([]*EntryTypeCount, 0, len(counts))
	for _, entryType := range entryTypeOrder {
		if count, ok := counts[entryType]; ok {
			ordered = append(ordered, &EntryTypeCount{
				EntryType: canonicalEntryType(entryType),
				Count:     count,
			})
			delete(counts, entryType)
		}
	}
	// Any entry type the protocol adds later still gets reported.
	for entryType, count := range counts {
		ordered = append(ordered, &EntryTypeCount{
			EntryType: canonicalEntryType(entryType),
			Count:     count,
		})
	}
	return ordered
}

func orderedEntryTypes(byType map[xdr.LedgerEntryType]*EntryTypeChangeCounts) []*EntryTypeChangeCounts {
	ordered := make([]*EntryTypeChangeCounts, 0, len(byType))
	remaining := make(map[xdr.LedgerEntryType]*EntryTypeChangeCounts, len(byType))
	for entryType, counts := range byType {
		remaining[entryType] = counts
	}

	for _, entryType := range entryTypeOrder {
		if counts, ok := remaining[entryType]; ok {
			ordered = append(ordered, counts)
			delete(remaining, entryType)
		}
	}
	// Entry types added by a future protocol are appended rather than dropped.
	for _, counts := range remaining {
		ordered = append(ordered, counts)
	}
	return ordered
}

// canonicalEntryType renders the XDR enum as lowercase snake_case. Unknown
// future types keep their numeric identity instead of being folded into an
// existing bucket.
func canonicalEntryType(entryType xdr.LedgerEntryType) string {
	switch entryType {
	case xdr.LedgerEntryTypeAccount:
		return "account"
	case xdr.LedgerEntryTypeTrustline:
		return "trustline"
	case xdr.LedgerEntryTypeOffer:
		return "offer"
	case xdr.LedgerEntryTypeData:
		return "data"
	case xdr.LedgerEntryTypeClaimableBalance:
		return "claimable_balance"
	case xdr.LedgerEntryTypeLiquidityPool:
		return "liquidity_pool"
	case xdr.LedgerEntryTypeContractData:
		return "contract_data"
	case xdr.LedgerEntryTypeContractCode:
		return "contract_code"
	case xdr.LedgerEntryTypeConfigSetting:
		return "config_setting"
	case xdr.LedgerEntryTypeTtl:
		return "ttl"
	default:
		return fmt.Sprintf("unknown_%d", int32(entryType))
	}
}
