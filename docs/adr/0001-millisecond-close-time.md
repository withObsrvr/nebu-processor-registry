# 0001. Carry ledger close time at millisecond precision

- **Status:** Accepted
- **Date:** 2026-10-29
- **Owner:** Tillman Mosley III
- **Affected interfaces:** the `EventMeta` message in every proto-based
  processor; the `closedAtUnix` field every JSON consumer reads
- **Change class (VER-3):** Additive
- **Context:** [nebu's Protocol 30 impact review](https://github.com/withObsrvr/nebu/blob/main/docs/protocol-30-impact-review.md)

## Context and problem statement

Protocol 30's CAP-88 gives ledger close times millisecond resolution.
Consecutive ledgers can now close **within the same whole second**.

Eleven protos in this registry define `int64 closed_at_unix` — Unix
**seconds** — and fifteen processors read or emit it. Two of them filter on
it: `time-window` ("requires meta.closedAtUnix so filtering uses real ledger
close time") and `aggregator`. From the first same-second ledger pair, those
two silently put events in the wrong bucket. They do not error. Nothing in
the output looks wrong.

Every construction site also truncates on the way in. All sixteen derive the
value from something second-resolution —
`ledger.LedgerHeaderHistoryEntry().Header.ScpValue.CloseTime`,
`ledger.LedgerCloseTime()`, or `ClosedAt().Unix()` — so the precision is lost
before it reaches the schema, independently of what the schema can hold.

Until Protocol 30 this cost nothing: across pubnet ledgers 60200000-60200060,
all 61 close times are distinct and 5-7 seconds apart. Seconds were lossless
because ledgers were seconds apart. That stops being true at the vote.

## Decision

Add a new field to each `EventMeta`:

```proto
// RFC 3339 UTC close time with millisecond precision.
string closed_at = N;
```

rendered in JSON as `closedAt`, e.g. `"2026-10-28T06:52:40.000Z"`.

`closed_at_unix` keeps its name, its type and its units. It is deprecated,
not changed.

Three supporting decisions:

1. **RFC 3339 string, not an integer of milliseconds.** DATA-3 requires
   timestamps to be "RFC 3339 in UTC with a `Z` suffix", so an
   `int64 closed_at_ms` would add precision while still not satisfying the
   rule, and we would have to revisit it. The format is fixed-width with
   exactly three decimal places — `2006-01-02T15:04:05.000Z07:00` rather than
   RFC3339Nano, which drops trailing zeros — so values sort lexicographically
   and line up when read by eye.
2. **The value comes from `processor.LedgerCloseTimeMs`** (nebu v0.6.17),
   which reads the millisecond value out of the `StellarValueExt` union. Two
   arms carry it, `SIGNED_MS` and `EMPTY_TX_SET_MS`, and an implementation
   that handles only the first silently degrades to seconds on every empty
   Protocol 30 ledger. That is not something each processor should
   re-implement; nebu owns it and tests all five discriminants.
3. **Processors thread milliseconds internally.** The `closeTime int64`
   parameter passed through each processor's helpers becomes `closeTimeMs`,
   and `closed_at_unix` is derived from it by integer division. One value
   flows through, so the two fields cannot disagree.

## Consequences

**Good:** same-second ledgers are distinguishable, which is the whole point.
DATA-3 is satisfied for the first time. Existing consumers are untouched.

**Bad:** two timestamps in the envelope until the deprecation completes, and
`closed_at_unix` remains the field most consumers reach for first, so the
wrong one stays the obvious one for a while.

**Neutral:** additive — a minor release, no schema-ID bump. Consumers must
ignore unknown fields, which the processor contract already requires.

## Why not change `closed_at_unix` in place

Reinterpreting it as milliseconds keeps the wire type and changes the meaning,
which VER-4 calls out precisely: "Adding a protobuf field keeps old decoders
working, but if it changes what an existing field means, the change is
Semantic." Semantic changes are treated as breaking and need a schema-ID bump,
a parity test and a replay decision.

It is also not available in the time we have. VER-6 requires a deprecated
interface to keep working for 90 days and two minor releases, whichever is
longer. Counted from this decision that lands well past the pubnet vote on
2026-11-23, so the breaking path could not be completed compliantly before the
ambiguity starts arriving.

## Deprecation

`closed_at_unix` is deprecated from the release that carries this change. Per
VER-6 it keeps working for at least 90 days and two minor releases — so not
before **2027-01-27** — and in practice until every known consumer has moved.
`time-window` and `aggregator` move in the same change that adds the field;
they are the two that are actually wrong without it.

## Tests

Each processor that populates `closed_at` is checked for a value that round
trips to the same instant as `closed_at_unix`, and for millisecond precision
surviving where the ledger carries it. `time-window` is tested against two
events inside one whole second, which is the case it gets wrong today.
