# STL-1: which processors implement a CAP or SEP

**STL-1** of the OBSRVR Engineering Standard: "A processor or dataset that
implements a CAP or SEP MUST name it, with the version or commit, in its
schema metadata. When the spec is a Draft, it MUST also pin the revision it
follows."

The rule is conditional — it binds a processor that *implements* a spec, and
asks nothing of one that reads ledger metadata directly. So the first job is
deciding which processors are in scope, and that is a judgement about intent
that cannot be read off the code.

This file records the determinations so the question is not re-litigated, and
marks the ones still open.

## Recorded in `description.yml`

| Processor | Spec | Basis |
|---|---|---|
| `token-transfer` | **CAP-67** Unified Asset Events (Final, protocol 23) | §11 of the standard names it directly: "Canonical source for asset movements: token-transfer processors" |
| `token-transfer` | **SEP-41** Soroban Token Interface (Draft) | `contract-events/README.md` describes this processor as the SEP-41 token-event one. **Revision unpinned** — see below |
| `contract-created` | **CAP-85** External-reference contract executables (Final, protocol 28) | Its own README: handles executables "(CAP-0085, Protocol 28), which have no wasm hash" |

## Open — needs the implementer's determination

- **`token-transfer` / SEP-41 revision.** STL-1 requires a Draft's revision to
  be pinned. Which revision the implementation follows is not inferable from
  the code; it needs whoever wrote it. Recorded as `revision: unconfirmed` so
  the gap is machine-detectable rather than invisible.
- **`nft-events` / SEP-50.** Describes itself as extracting "NFT-*like*"
  events and filtering "*likely* SEP-50 contracts". That is a heuristic over
  NFT-shaped contracts rather than an implementation of the spec. If the
  intent is to implement SEP-50, it needs the entry and a revision pin; if the
  intent is heuristic detection, STL-1 does not bind and this line is the
  record of that.

## Determined **not** to implement a spec

Both of these name a spec in their docs, which is why they look like
candidates. Neither claim is an implementation claim, and reading them as one
would put false metadata in the registry:

- **`contract-events`** — its README draws the opposite contrast: "Unlike the
  `token-transfer` processor which focuses on SEP-41 token events,
  `contract-events` captures all contract activity." It is deliberately
  general-purpose.
- **`ledger-change-stats`** — its CAP-0062 reference is a caveat about
  protocol behaviour ("Under CAP-0062, ..."), explaining a quirk a detector
  author should know about. It does not implement CAP-62.

## Not in scope

The remaining processors read ledger metadata, transaction results or contract
data directly rather than implementing a published interface:
`account-effects`, `contract-invocation`, `contract-state`,
`liquidity-pool-events`, `soroban-tx-resources`, `ttl-tracker`,
`validator-analytics`, and every transform and sink. STL-1 does not bind them.

Revisit this file whenever a processor starts decoding by a published
interface rather than by observation — that is the moment STL-1 starts to
apply.
