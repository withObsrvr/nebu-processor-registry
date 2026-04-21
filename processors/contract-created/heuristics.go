package contract_created

import (
	"sort"
	"strings"
)

type Evidence struct {
	RuleID          string
	Family          string
	ConfidenceDelta float64
	Reasons         []string
	Tags            []string
}

type Heuristic interface {
	ID() string
	Evaluate(c Candidate) []Evidence
}

type FamilyScore struct {
	Family     string
	Confidence float64
	Reasons    []string
	Tags       []string
	RuleIDs    []string
}

type Classification struct {
	Family     string
	Confidence float64
	Reasons    []string
	Tags       []string
	RuleIDs    []string
	Candidates []FamilyScore
}

type heuristicEngine struct {
	rules []Heuristic
}

func newHeuristicEngine() *heuristicEngine {
	return &heuristicEngine{rules: []Heuristic{
		stellarAssetRule{},
		sacStateRule{},
		wasmMemoryRule{},
		multisigStateRule{},
		smartAccountRule{},
		identityRegistryRule{},
		nftConstructorRule{},
		nftMetadataRule{},
		authCredentialRule{},
		defiRouterRule{},
		defiPoolAdapterRule{},
		vaultAdminRule{},
	}}
}

func (e *heuristicEngine) Classify(c Candidate) Classification {
	familyScores := map[string]float64{}
	reasonsByFamily := map[string][]string{}
	tagsByFamily := map[string][]string{}
	rulesByFamily := map[string][]string{}

	for _, rule := range e.rules {
		for _, ev := range rule.Evaluate(c) {
			if ev.Family == "" {
				continue
			}
			familyScores[ev.Family] += ev.ConfidenceDelta
			reasonsByFamily[ev.Family] = append(reasonsByFamily[ev.Family], ev.Reasons...)
			tagsByFamily[ev.Family] = append(tagsByFamily[ev.Family], ev.Tags...)
			rulesByFamily[ev.Family] = append(rulesByFamily[ev.Family], ev.RuleID)
		}
	}

	candidates := make([]FamilyScore, 0, len(familyScores))
	for family, score := range familyScores {
		if score < 0 {
			score = 0
		}
		if score > 0.99 {
			score = 0.99
		}
		candidates = append(candidates, FamilyScore{
			Family:     family,
			Confidence: score,
			Reasons:    uniqueStrings(reasonsByFamily[family]),
			Tags:       uniqueStrings(tagsByFamily[family]),
			RuleIDs:    uniqueStrings(rulesByFamily[family]),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Confidence == candidates[j].Confidence {
			return familyPriority(candidates[i].Family) < familyPriority(candidates[j].Family)
		}
		return candidates[i].Confidence > candidates[j].Confidence
	})

	if len(candidates) == 0 {
		return Classification{
			Family:     "generic_contract",
			Confidence: 0.25,
			Reasons:    []string{"create-contract host function confirmed with initialized state"},
		}
	}

	top := candidates[0]
	if top.Confidence < 0.55 {
		return Classification{
			Family:     "generic_contract",
			Confidence: 0.25,
			Reasons:    []string{"create-contract host function confirmed with initialized state"},
			Candidates: candidates,
		}
	}
	return Classification{
		Family:     top.Family,
		Confidence: top.Confidence,
		Reasons:    top.Reasons,
		Tags:       top.Tags,
		RuleIDs:    top.RuleIDs,
		Candidates: candidates,
	}
}

func familyPriority(f string) int {
	switch f {
	case "fungible_token_like":
		return 0
	case "multisig_like":
		return 1
	case "smart_account_like":
		return 2
	case "identity_registry_like":
		return 3
	case "auth_credential_like":
		return 4
	case "defi_router_like":
		return 5
	case "defi_pool_adapter_like":
		return 6
	case "vault_admin_like":
		return 7
	case "nft_like":
		return 8
	default:
		return 99
	}
}

type stellarAssetRule struct{}

func (stellarAssetRule) ID() string { return "family.ft.stellar_asset" }
func (stellarAssetRule) Evaluate(c Candidate) []Evidence {
	if c.ExecutableType != "stellar_asset" {
		return nil
	}
	return []Evidence{{RuleID: "family.ft.stellar_asset", Family: "fungible_token_like", ConfidenceDelta: 0.95, Reasons: []string{"executable type is stellar_asset"}, Tags: []string{"token", "sac"}}}
}

type sacStateRule struct{}

func (sacStateRule) ID() string { return "family.ft.sac_state" }
func (sacStateRule) Evaluate(c Candidate) []Evidence {
	matches := 0
	for _, k := range []string{"[AssetInfo]", "METADATA", "[Admin]", "[TotalSupply]"} {
		if c.HasKey(k) {
			matches++
		}
	}
	if matches < 3 {
		return nil
	}
	return []Evidence{{RuleID: "family.ft.sac_state", Family: "fungible_token_like", ConfidenceDelta: 0.75, Reasons: []string{"state matches SAC metadata/admin pattern"}, Tags: []string{"token", "metadata", "admin"}}}
}

type wasmMemoryRule struct{}

func (wasmMemoryRule) ID() string { return "family.memory.wasm" }
func (wasmMemoryRule) Evaluate(c Candidate) []Evidence {
	if len(c.KnownFamilyHints) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]Evidence, 0, len(c.KnownFamilyHints))
	for _, family := range c.KnownFamilyHints {
		if family == "" {
			continue
		}
		if _, ok := seen[family]; ok {
			continue
		}
		seen[family] = struct{}{}
		out = append(out, Evidence{RuleID: "family.memory.wasm", Family: family, ConfidenceDelta: 0.35, Reasons: []string{"previous deployments with this wasm hash matched the same family"}, Tags: append([]string{"wasm_memory"}, c.KnownTags...)})
	}
	return out
}

type multisigStateRule struct{}

func (multisigStateRule) ID() string { return "family.multisig.state" }
func (multisigStateRule) Evaluate(c Candidate) []Evidence {
	matches := c.CountMatchingKeyPrefixes("[Signers,", "[Policies,", "[Fingerprint,")
	if c.HasKey("[Ids,[Default]]") {
		matches++
	}
	if c.HasKey("[Meta,0]") {
		matches++
	}
	if c.HasKey("[NextId]") {
		matches++
	}
	if c.HasKey("[Count]") {
		matches++
	}
	if matches < 4 {
		return nil
	}
	return []Evidence{{RuleID: "family.multisig.state", Family: "multisig_like", ConfidenceDelta: 0.93, Reasons: []string{"initialized state includes signer, policy, and fingerprint/account metadata patterns"}, Tags: []string{"signers", "policies", "account_abstraction"}}}
}

type smartAccountRule struct{}

func (smartAccountRule) ID() string { return "family.smart_account" }
func (smartAccountRule) Evaluate(c Candidate) []Evidence {
	matches := 0
	for _, k := range []string{"[Admin]", "[Owner]", "[Threshold]", "[Recovery]", "[OneSigId]"} {
		if c.HasKey(k) {
			matches++
		}
	}
	if c.HasKeyPrefix("[RoleAccounts,") || c.HasKeyPrefix("[HasRole,") {
		matches++
	}
	if matches < 2 {
		return nil
	}
	return []Evidence{{RuleID: "family.smart_account", Family: "smart_account_like", ConfidenceDelta: 0.72, Reasons: []string{"admin/owner/recovery or role-account management pattern detected"}, Tags: []string{"account", "admin", "roles"}}}
}

type identityRegistryRule struct{}

func (identityRegistryRule) ID() string { return "family.identity_registry" }
func (identityRegistryRule) Evaluate(c Candidate) []Evidence {
	if !c.HasKey("[IdentityRegistry]") && !(c.HasKey("[Metadata]") && c.HasKey("[Owner]") && looksLikeNFTConstructor(c.ConstructorArgs)) {
		return nil
	}
	tags := []string{"registry", "identity", "owner"}
	joined := c.JoinedLower()
	if strings.Contains(joined, "agent") {
		tags = append(tags, "agent")
	}
	if looksLikeNFTConstructor(c.ConstructorArgs) || c.HasKey("[Metadata]") {
		tags = append(tags, "nft_like")
	}
	return []Evidence{{RuleID: "family.identity_registry", Family: "identity_registry_like", ConfidenceDelta: 1.10, Reasons: []string{"identity registry ownership/metadata pattern detected"}, Tags: uniqueStrings(tags)}}
}

type nftConstructorRule struct{}

func (nftConstructorRule) ID() string { return "family.nft.constructor" }
func (nftConstructorRule) Evaluate(c Candidate) []Evidence {
	if !looksLikeNFTConstructor(c.ConstructorArgs) {
		return nil
	}
	return []Evidence{{RuleID: "family.nft.constructor", Family: "nft_like", ConfidenceDelta: 0.60, Reasons: []string{"constructor shape resembles owner + name + symbol"}, Tags: []string{"nft_like", "collection"}}}
}

type nftMetadataRule struct{}

func (nftMetadataRule) ID() string { return "family.nft.metadata" }
func (nftMetadataRule) Evaluate(c Candidate) []Evidence {
	if c.HasKey("NextTokenId") || (c.HasKey("[Metadata]") && c.Contains("name:", "symbol:")) {
		return []Evidence{{RuleID: "family.nft.metadata", Family: "nft_like", ConfidenceDelta: 0.45, Reasons: []string{"NFT-style metadata initialized in contract state"}, Tags: []string{"nft_like", "metadata"}}}
	}
	return nil
}

type authCredentialRule struct{}

func (authCredentialRule) ID() string { return "family.auth_credential" }
func (authCredentialRule) Evaluate(c Candidate) []Evidence {
	if c.HasKey("init") && c.HasKeyPrefix("[Secp256r1,") {
		return []Evidence{{RuleID: "family.auth_credential", Family: "auth_credential_like", ConfidenceDelta: 0.94, Reasons: []string{"initialized state includes secp256r1 credential registration pattern"}, Tags: []string{"auth", "credential", "passkey"}}}
	}
	return nil
}

type defiRouterRule struct{}

func (defiRouterRule) ID() string { return "family.defi_router" }
func (defiRouterRule) Evaluate(c Candidate) []Evidence {
	routerMatches := 0
	for _, k := range []string{"[SoroswapRouter]", "[AquaRouter]", "[PhoenixMultihop]"} {
		if c.HasKey(k) {
			routerMatches++
		}
	}
	if routerMatches < 2 {
		return nil
	}
	tags := []string{"defi", "router"}
	if c.HasKey("[BlendPool]") {
		tags = append(tags, "pool")
	}
	return []Evidence{{RuleID: "family.defi_router", Family: "defi_router_like", ConfidenceDelta: 0.90, Reasons: []string{"initialized state references multiple DeFi routers or multihop routing components"}, Tags: tags}}
}

type defiPoolAdapterRule struct{}

func (defiPoolAdapterRule) ID() string { return "family.defi_pool_adapter" }
func (defiPoolAdapterRule) Evaluate(c Candidate) []Evidence {
	matches := 0
	for _, k := range []string{"[BlendPool]", "[Token]", "[SacToken]"} {
		if c.HasKey(k) {
			matches++
		}
	}
	if matches < 2 {
		return nil
	}
	return []Evidence{{RuleID: "family.defi_pool_adapter", Family: "defi_pool_adapter_like", ConfidenceDelta: 0.96, Reasons: []string{"pool and token adapter state detected"}, Tags: []string{"defi", "pool", "adapter"}}}
}

type vaultAdminRule struct{}

func (vaultAdminRule) ID() string { return "family.vault_admin" }
func (vaultAdminRule) Evaluate(c Candidate) []Evidence {
	matches := 0
	for _, k := range []string{"[VaultWasmHash]", "[Vaults]", "[RoleAdmin,upgrader]", "[WasmHashChangeCooldownSecs]"} {
		if c.HasKey(k) {
			matches++
		}
	}
	if matches < 2 {
		return nil
	}
	return []Evidence{{RuleID: "family.vault_admin", Family: "vault_admin_like", ConfidenceDelta: 0.92, Reasons: []string{"vault/admin upgrade management pattern detected"}, Tags: []string{"vault", "admin", "upgradeable"}}}
}
