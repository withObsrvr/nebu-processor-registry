# contract-created heuristics framework

This document explains:

1. what heuristic rules are
2. how the contract-created processor uses them
3. what is required to add a new rule
4. current rule families and examples

---

## Why heuristics exist

Stellar contract family discovery is not fully deterministic.

At contract creation time, we often have strong signals, but not always perfect signals:

- create host function type
- executable type
- constructor arguments
- initialized state keys
- initialized state values
- repeated wasm hashes
- deployer patterns

A heuristic rule lets us convert those signals into explainable evidence such as:

- `multisig_like`
- `identity_registry_like`
- `fungible_token_like`
- `nft_like`
- `defi_router_like`

Instead of hardcoding one giant classification function, the processor uses a modular rule engine.

---

## What a heuristic rule is

A heuristic rule is a small unit of logic that:

- inspects a normalized contract creation candidate
- decides whether it sees a meaningful pattern
- emits evidence for one family

In code, a rule implements:

```go
type Heuristic interface {
    ID() string
    Evaluate(c Candidate) []Evidence
}
```

### Candidate

A `Candidate` is the normalized creation-time view of a contract. It includes fields like:

- `ContractID`
- `Deployer`
- `PreimageAddress`
- `WasmHash`
- `ExecutableType`
- `CreateHostFunctionType`
- `ConstructorName`
- `ConstructorArgs`
- `InitializedState`
- `TTLToLedger`
- `KnownFamilyHints`
- `KnownTags`

### Evidence

A rule emits `Evidence`:

```go
type Evidence struct {
    RuleID          string
    Family          string
    ConfidenceDelta float64
    Reasons         []string
    Tags            []string
}
```

This means:

- `RuleID`: unique identifier for the rule
- `Family`: the family this rule supports
- `ConfidenceDelta`: score contribution
- `Reasons`: human-readable explanation
- `Tags`: secondary descriptors

---

## How classification works

The engine:

1. builds a `Candidate`
2. runs all rules against it
3. aggregates evidence by family
4. sorts families by confidence
5. chooses the top family as `familyHint`
6. keeps the others in `familyCandidates`

This produces output like:

- `familyHint`
- `familyConfidence`
- `familyReasons`
- `familyTags`
- `familyCandidates`

This makes the result:

- modular
- explainable
- testable
- extensible

---

## What is needed to create a new heuristic rule

A good rule should have four things.

### 1. A clearly defined target family

Decide what family the rule is trying to detect.

Examples:

- `multisig_like`
- `smart_account_like`
- `identity_registry_like`
- `defi_pool_adapter_like`

Do not create a rule unless you know which family it supports.

### 2. A stable signal

A good rule should use signals that are reasonably stable across deployments.

Good signals:

- exact initialized state keys
- key prefixes
- constructor shapes
- executable type
- repeated protocol-specific state patterns

Less reliable signals:

- arbitrary freeform strings alone
- deployer alone
- one-off values without surrounding context

### 3. A confidence score

Every rule should contribute a score that reflects how strong the signal is.

Typical guidance:

- `0.90+` very strong signature
- `0.70-0.89` strong family signal
- `0.40-0.69` useful supporting signal
- `0.10-0.39` weak supporting signal

Examples:

- `executableType == stellar_asset` → very strong for `fungible_token_like`
- `[Signers,*] + [Policies,*] + [Fingerprint,*]` → very strong for `multisig_like`
- constructor shaped like `owner, name, symbol` → supporting signal for `nft_like`

### 4. Explainable reasons and tags

Each rule should emit reasons a human can understand.

Good reason examples:

- `initialized state includes signer, policy, and fingerprint/account metadata patterns`
- `identity registry ownership/metadata pattern detected`
- `pool and token adapter state detected`

Tags should be short descriptors, for example:

- `registry`
- `identity`
- `nft_like`
- `router`
- `pool`
- `admin`

---

## How to add a new rule

Rules currently live in:

- `processors/contract-created/heuristics.go`

### Step 1: add a rule type

Example skeleton:

```go
type myNewRule struct{}

func (myNewRule) ID() string { return "family.my_new_rule" }

func (myNewRule) Evaluate(c Candidate) []Evidence {
    if !c.HasKey("[SomeKey]") {
        return nil
    }

    return []Evidence{{
        RuleID:          "family.my_new_rule",
        Family:          "some_family_like",
        ConfidenceDelta: 0.85,
        Reasons:         []string{"some family-specific state pattern detected"},
        Tags:            []string{"some_tag"},
    }}
}
```

### Step 2: register it in the engine

Add it in `newHeuristicEngine()`:

```go
func newHeuristicEngine() *heuristicEngine {
    return &heuristicEngine{rules: []Heuristic{
        ...
        myNewRule{},
    }}
}
```

### Step 3: add tests

Update:

- `processors/contract-created/contract_created_test.go`

A new rule should have at least one positive test.

Preferably also add a negative test if the pattern could be confused with another family.

---

## Helper methods available on Candidate

The `Candidate` type provides useful helpers.

### `Keys()`
Returns initialized state keys.

### `Values()`
Returns constructor args and state values.

### `JoinedLower()`
Returns a lowercase concatenation of candidate text fields.
Useful for light text matching.

### `HasKey(key string)`
Checks for an exact key.

### `HasKeyPrefix(prefix string)`
Checks for key prefix patterns such as:

- `[Signers,`
- `[Fingerprint,`
- `[RoleAccounts,`

### `Contains(parts ...string)`
Checks whether any provided substring exists in the combined candidate text.

### `CountMatchingKeyPrefixes(prefixes ...string)`
Counts how many key-prefix patterns are present.
Useful for family signature rules.

---

## Current implemented families

### `fungible_token_like`
Typical signals:

- `ExecutableType == stellar_asset`
- `[AssetInfo]`
- `METADATA`
- `[Admin]`
- `[TotalSupply]`

### `multisig_like`
Typical signals:

- `[Signers,*]`
- `[Policies,*]`
- `[Ids,[Default]]`
- `[Meta,0]`
- `[Fingerprint,*]`
- `[NextId]`
- `[Count]`

### `smart_account_like`
Typical signals:

- `[Threshold]`
- `[OneSigId]`
- `[Recovery]`
- `[Admin]`
- role/account management patterns

### `identity_registry_like`
Typical signals:

- `[IdentityRegistry]`
- `[Metadata]`
- `[Owner]`
- constructor shape like `owner, name, symbol`

Often tagged with:

- `agent`
- `nft_like`

### `nft_like`
Typical signals:

- constructor shape like `owner, name, symbol`
- NFT-style metadata state
- token metadata style patterns

### `auth_credential_like`
Typical signals:

- `init`
- `[Secp256r1,*]`

### `defi_router_like`
Typical signals:

- `[SoroswapRouter]`
- `[AquaRouter]`
- `[PhoenixMultihop]`
- optionally `[BlendPool]`

### `defi_pool_adapter_like`
Typical signals:

- `[BlendPool]`
- `[Token]`
- `[SacToken]`

### `vault_admin_like`
Typical signals:

- `[VaultWasmHash]`
- `[Vaults]`
- `[RoleAdmin,upgrader]`
- `[WasmHashChangeCooldownSecs]`

### `generic_contract`
Fallback when no strong family wins.

---

## Best practices for new rules

### Prefer structural signals over text guesses

Prefer:

- exact key matches
- key prefixes
- executable type
- constructor shape

Over:

- loose text contains checks alone

### Keep rules small

A rule should do one thing well.

If a family has multiple kinds of evidence, use multiple rules instead of one giant rule.

### Make rules composable

A family should often be supported by several rules.

Example:

- one rule for constructor shape
- one rule for state shape
- one rule for known wasm reuse

### Avoid overfitting to a single contract

A rule should represent a reusable pattern, not just one exact contract instance.

### Write reasons for humans

If someone sees the event JSON, they should understand why the family was chosen.

---

## When to create a new family vs a new tag

Create a **new family** when the contract has a distinct application role.

Examples:

- `multisig_like`
- `identity_registry_like`
- `defi_router_like`

Create a **tag** when the signal is secondary or cross-cutting.

Examples:

- `nft_like`
- `agent`
- `admin`
- `pool`
- `upgradeable`

A good rule of thumb:

- family = primary role
- tag = capability or trait

---

## Future directions

Potential future extensions:

- persistent wasm family memory across runs
- declarative rule definitions from YAML or CEL
- family confidence thresholds by category
- negative evidence / penalties
- protocol-specific rule packs

For now, the framework is code-first by design because it is:

- type-safe
- easy to test
- easy to refactor
- easy to explain
