package ledger_change_stats

import (
	"slices"
	"testing"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Entry types the protocol adds after entryTypeOrder was written fall through
// to a fallback path. That path used to range a map directly, so output order
// varied run to run on exactly the ledgers where a new type first appears.
func TestUnknownEntryTypesOrderDeterministically(t *testing.T) {
	build := func() []string {
		acc := newChangeAccumulator(100, 0)
		// Two hypothetical future types, added high-then-low on purpose.
		acc.add(change(xdr.LedgerEntryType(98), xdr.LedgerEntryChangeTypeLedgerEntryUpdated, ingest.LedgerEntryChangeReasonOperation))
		acc.add(change(xdr.LedgerEntryType(97), xdr.LedgerEntryChangeTypeLedgerEntryUpdated, ingest.LedgerEntryChangeReasonOperation))
		acc.add(change(xdr.LedgerEntryTypeAccount, xdr.LedgerEntryChangeTypeLedgerEntryUpdated, ingest.LedgerEntryChangeReasonOperation))

		names := make([]string, 0, 3)
		for _, counts := range acc.finish().EntryTypes {
			names = append(names, counts.EntryType)
		}
		return names
	}

	// Known types first in enum order, then unknown ones by enum value.
	want := []string{"account", "unknown_97", "unknown_98"}
	for run := range 30 {
		if got := build(); !slices.Equal(got, want) {
			t.Fatalf("run %d: order = %v, want %v", run, got, want)
		}
	}
}

func TestEvictedUnknownTypesOrderDeterministically(t *testing.T) {
	keys := []xdr.LedgerKey{
		{Type: xdr.LedgerEntryType(98)},
		{Type: xdr.LedgerEntryTypeContractData},
		{Type: xdr.LedgerEntryType(97)},
	}

	want := []string{"contract_data", "unknown_97", "unknown_98"}
	for run := range 30 {
		got := make([]string, 0, 3)
		for _, c := range evictedByType(keys) {
			got = append(got, c.EntryType)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("run %d: order = %v, want %v", run, got, want)
		}
	}
}

func change(entryType xdr.LedgerEntryType, changeType xdr.LedgerEntryChangeType, reason ingest.LedgerEntryChangeReason) ingest.Change {
	return ingest.Change{Type: entryType, ChangeType: changeType, Reason: reason}
}

// Every change type must land in its own bucket. Restored is the one that
// matters most: the original implementation derived the change type from
// whether Pre/Post were nil, which cannot distinguish a restore from a create,
// so restorations were counted as creates.
func TestAccumulatorCountsEveryChangeType(t *testing.T) {
	acc := newChangeAccumulator(100, 1765158311)

	acc.add(change(xdr.LedgerEntryTypeContractData, xdr.LedgerEntryChangeTypeLedgerEntryCreated, ingest.LedgerEntryChangeReasonOperation))
	acc.add(change(xdr.LedgerEntryTypeContractData, xdr.LedgerEntryChangeTypeLedgerEntryUpdated, ingest.LedgerEntryChangeReasonOperation))
	acc.add(change(xdr.LedgerEntryTypeContractData, xdr.LedgerEntryChangeTypeLedgerEntryRemoved, ingest.LedgerEntryChangeReasonOperation))
	acc.add(change(xdr.LedgerEntryTypeContractData, xdr.LedgerEntryChangeTypeLedgerEntryRestored, ingest.LedgerEntryChangeReasonOperation))
	acc.add(change(xdr.LedgerEntryTypeContractData, xdr.LedgerEntryChangeTypeLedgerEntryState, ingest.LedgerEntryChangeReasonOperation))

	event := acc.finish()

	if event.LedgerEntriesCreated != 1 {
		t.Errorf("created = %d, want 1", event.LedgerEntriesCreated)
	}
	if event.LedgerEntriesUpdated != 1 {
		t.Errorf("updated = %d, want 1", event.LedgerEntriesUpdated)
	}
	if event.LedgerEntriesDeleted != 1 {
		t.Errorf("deleted = %d, want 1", event.LedgerEntriesDeleted)
	}
	if event.LedgerEntriesRestored != 1 {
		t.Errorf("restored = %d, want 1 — a restore must not be counted as a create", event.LedgerEntriesRestored)
	}
	if event.LedgerEntriesState != 1 {
		t.Errorf("state = %d, want 1", event.LedgerEntriesState)
	}
	if event.TotalChanges != 5 {
		t.Errorf("totalChanges = %d, want 5", event.TotalChanges)
	}
}

// Every change type must land in exactly one bucket, so the five sum to
// totalChanges. This is what justifies keeping the state counter even though
// the ingest reader elides state entries: without it, a change counted in the
// total would belong to no bucket.
func TestChangeTypeBucketsSumToTotal(t *testing.T) {
	acc := newChangeAccumulator(100, 0)

	types := []xdr.LedgerEntryChangeType{
		xdr.LedgerEntryChangeTypeLedgerEntryCreated,
		xdr.LedgerEntryChangeTypeLedgerEntryUpdated,
		xdr.LedgerEntryChangeTypeLedgerEntryRemoved,
		xdr.LedgerEntryChangeTypeLedgerEntryRestored,
		xdr.LedgerEntryChangeTypeLedgerEntryState,
	}
	for i, changeType := range types {
		for range i + 1 {
			acc.add(change(xdr.LedgerEntryTypeContractData, changeType, ingest.LedgerEntryChangeReasonOperation))
		}
	}

	event := acc.finish()
	sum := event.LedgerEntriesCreated + event.LedgerEntriesUpdated + event.LedgerEntriesDeleted +
		event.LedgerEntriesRestored + event.LedgerEntriesState
	if sum != event.TotalChanges {
		t.Errorf("change-type buckets sum to %d but totalChanges = %d", sum, event.TotalChanges)
	}
	if event.TotalChanges != 15 {
		t.Errorf("totalChanges = %d, want 15", event.TotalChanges)
	}
}

// State snapshots are not mutations and must never inflate a "how much changed"
// figure derived from created+updated+deleted.
func TestStateChangesExcludedFromMutationTotals(t *testing.T) {
	acc := newChangeAccumulator(100, 0)
	for range 7 {
		acc.add(change(xdr.LedgerEntryTypeAccount, xdr.LedgerEntryChangeTypeLedgerEntryState, ingest.LedgerEntryChangeReasonTransaction))
	}
	event := acc.finish()

	mutations := event.LedgerEntriesCreated + event.LedgerEntriesUpdated + event.LedgerEntriesDeleted + event.LedgerEntriesRestored
	if mutations != 0 {
		t.Errorf("mutation total = %d, want 0", mutations)
	}
	if event.LedgerEntriesState != 7 {
		t.Errorf("state = %d, want 7", event.LedgerEntriesState)
	}
}

// Fee refunds and upgrades were dropped entirely by the original three-case
// switch, so their changes counted toward no reason at all.
func TestAccumulatorCountsEveryReason(t *testing.T) {
	acc := newChangeAccumulator(100, 0)

	reasons := []ingest.LedgerEntryChangeReason{
		ingest.LedgerEntryChangeReasonFee,
		ingest.LedgerEntryChangeReasonFeeRefund,
		ingest.LedgerEntryChangeReasonTransaction,
		ingest.LedgerEntryChangeReasonOperation,
		ingest.LedgerEntryChangeReasonUpgrade,
		ingest.LedgerEntryChangeReasonUnknown,
	}
	for _, reason := range reasons {
		acc.add(change(xdr.LedgerEntryTypeAccount, xdr.LedgerEntryChangeTypeLedgerEntryUpdated, reason))
	}

	event := acc.finish()

	checks := map[string]uint32{
		"fee":     event.FeeRelatedChanges,
		"refund":  event.FeeRefundRelatedChanges,
		"tx":      event.TxRelatedChanges,
		"op":      event.OperationRelatedChanges,
		"upgrade": event.UpgradeRelatedChanges,
		"unknown": event.UnknownReasonChanges,
	}
	for name, got := range checks {
		if got != 1 {
			t.Errorf("%s reason count = %d, want 1", name, got)
		}
	}

	sum := uint32(0)
	for _, got := range checks {
		sum += got
	}
	if sum != event.TotalChanges {
		t.Errorf("reason counts sum to %d but totalChanges = %d; a change fell into no bucket", sum, event.TotalChanges)
	}
}

func TestEntryTypeBreakdown(t *testing.T) {
	acc := newChangeAccumulator(100, 0)

	acc.add(change(xdr.LedgerEntryTypeAccount, xdr.LedgerEntryChangeTypeLedgerEntryUpdated, ingest.LedgerEntryChangeReasonFee))
	acc.add(change(xdr.LedgerEntryTypeAccount, xdr.LedgerEntryChangeTypeLedgerEntryUpdated, ingest.LedgerEntryChangeReasonFee))
	acc.add(change(xdr.LedgerEntryTypeContractData, xdr.LedgerEntryChangeTypeLedgerEntryCreated, ingest.LedgerEntryChangeReasonOperation))
	acc.add(change(xdr.LedgerEntryTypeTtl, xdr.LedgerEntryChangeTypeLedgerEntryRestored, ingest.LedgerEntryChangeReasonOperation))

	event := acc.finish()

	got := make(map[string]*EntryTypeChangeCounts, len(event.EntryTypes))
	for _, counts := range event.EntryTypes {
		got[counts.EntryType] = counts
	}

	if got["account"] == nil || got["account"].Updated != 2 || got["account"].Total != 2 {
		t.Errorf("account counts wrong: %+v", got["account"])
	}
	if got["contract_data"] == nil || got["contract_data"].Created != 1 {
		t.Errorf("contract_data counts wrong: %+v", got["contract_data"])
	}
	if got["ttl"] == nil || got["ttl"].Restored != 1 {
		t.Errorf("ttl counts wrong: %+v", got["ttl"])
	}
	if len(event.EntryTypes) != 3 {
		t.Errorf("entryTypes length = %d, want 3 (only types that saw a change)", len(event.EntryTypes))
	}
}

// Output ordering must follow the XDR enum, not map iteration, or replayable
// pipelines produce different bytes for identical input.
func TestEntryTypeOrderingIsDeterministic(t *testing.T) {
	build := func() []string {
		acc := newChangeAccumulator(100, 0)
		// Added in reverse enum order on purpose.
		acc.add(change(xdr.LedgerEntryTypeTtl, xdr.LedgerEntryChangeTypeLedgerEntryUpdated, ingest.LedgerEntryChangeReasonOperation))
		acc.add(change(xdr.LedgerEntryTypeContractData, xdr.LedgerEntryChangeTypeLedgerEntryUpdated, ingest.LedgerEntryChangeReasonOperation))
		acc.add(change(xdr.LedgerEntryTypeOffer, xdr.LedgerEntryChangeTypeLedgerEntryUpdated, ingest.LedgerEntryChangeReasonOperation))
		acc.add(change(xdr.LedgerEntryTypeAccount, xdr.LedgerEntryChangeTypeLedgerEntryUpdated, ingest.LedgerEntryChangeReasonOperation))

		event := acc.finish()
		names := make([]string, 0, len(event.EntryTypes))
		for _, counts := range event.EntryTypes {
			names = append(names, counts.EntryType)
		}
		return names
	}

	want := []string{"account", "offer", "contract_data", "ttl"}
	for run := range 20 {
		got := build()
		if len(got) != len(want) {
			t.Fatalf("run %d: length = %d, want %d", run, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("run %d: order = %v, want %v", run, got, want)
			}
		}
	}
}

func TestEvictedByType(t *testing.T) {
	keys := []xdr.LedgerKey{
		{Type: xdr.LedgerEntryTypeContractData},
		{Type: xdr.LedgerEntryTypeContractData},
		{Type: xdr.LedgerEntryTypeTtl},
	}

	got := evictedByType(keys)
	if len(got) != 2 {
		t.Fatalf("length = %d, want 2", len(got))
	}
	// Enum order: contract_data (6) before ttl (9).
	if got[0].EntryType != "contract_data" || got[0].Count != 2 {
		t.Errorf("first = %+v, want contract_data x2", got[0])
	}
	if got[1].EntryType != "ttl" || got[1].Count != 1 {
		t.Errorf("second = %+v, want ttl x1", got[1])
	}
}

func TestEvictedByTypeEmpty(t *testing.T) {
	if got := evictedByType(nil); got != nil {
		t.Errorf("evictedByType(nil) = %v, want nil", got)
	}
}

func TestCanonicalEntryType(t *testing.T) {
	cases := map[xdr.LedgerEntryType]string{
		xdr.LedgerEntryTypeAccount:          "account",
		xdr.LedgerEntryTypeTrustline:        "trustline",
		xdr.LedgerEntryTypeOffer:            "offer",
		xdr.LedgerEntryTypeData:             "data",
		xdr.LedgerEntryTypeClaimableBalance: "claimable_balance",
		xdr.LedgerEntryTypeLiquidityPool:    "liquidity_pool",
		xdr.LedgerEntryTypeContractData:     "contract_data",
		xdr.LedgerEntryTypeContractCode:     "contract_code",
		xdr.LedgerEntryTypeConfigSetting:    "config_setting",
		xdr.LedgerEntryTypeTtl:              "ttl",
	}

	for entryType, want := range cases {
		if got := canonicalEntryType(entryType); got != want {
			t.Errorf("canonicalEntryType(%v) = %q, want %q", entryType, got, want)
		}
	}
}

// An entry type added by a future protocol must keep its identity rather than
// be folded into an existing bucket.
func TestCanonicalEntryTypeUnknown(t *testing.T) {
	got := canonicalEntryType(xdr.LedgerEntryType(99))
	if got == "" {
		t.Fatal("unknown entry type produced an empty string")
	}
	for _, known := range []string{"account", "trustline", "contract_data", "ttl"} {
		if got == known {
			t.Fatalf("unknown entry type folded into known bucket %q", known)
		}
	}
}
