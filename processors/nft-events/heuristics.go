package nft_events

import "strings"

type Evidence struct {
	RuleID               string
	Standard             string
	Implementation       string
	ClassificationSource string
	ConfidenceDelta      float64
	Tags                 []string
	Reasons              []string
}

type Heuristic interface {
	ID() string
	Evaluate(c Candidate) []Evidence
}

type Classification struct {
	Standard       string
	Implementation string
	Source         string
	Confidence     float64
	RuleIDs        []string
	Reasons        []string
	Tags           []string
}

type heuristicEngine struct {
	rules []Heuristic
}

func newHeuristicEngine() *heuristicEngine {
	return &heuristicEngine{rules: []Heuristic{
		sep50MethodRule{},
		approveForAllRule{},
		tokenURIRule{},
		mintBurnRule{},
		transferFromRule{},
		customCollectionMethodRule{},
		customTraitMethodRule{},
		metadataTripletRule{},
		tokenIDShapeRule{},
		openzeppelinRule{},
		stateExactOwnerRule{},
		stateExactApprovalForAllRule{},
		ownerStateRule{},
		metadataStateRule{},
		collectionMetadataRule{},
		genericTransferRule{},
		genericApproveRule{},
		balancePenaltyRule{},
		ftMethodPenaltyRule{},
		ftStatePenaltyRule{},
		ftAmountShapePenaltyRule{},
		fungibleAssetPenaltyRule{},
		nativeAssetPenaltyRule{},
		temporaryStatePenaltyRule{},
		genericMetadataStatePenaltyRule{},
		stakingPatternPenaltyRule{},
	}}
}

func minimumConfidence(kind CandidateKind) float64 {
	switch kind {
	case CandidateKindInvocation:
		return 0.55
	case CandidateKindEvent:
		return 0.65
	case CandidateKindState:
		return 0.80
	default:
		return 0.70
	}
}

func (e *heuristicEngine) Classify(c Candidate) Classification {
	cls := Classification{
		Standard:       "unknown",
		Implementation: "unknown",
		Source:         "heuristic_framework",
		Confidence:     0.25,
	}
	standardScores := map[string]float64{}
	implScores := map[string]float64{}

	for _, rule := range e.rules {
		for _, ev := range rule.Evaluate(c) {
			cls.RuleIDs = append(cls.RuleIDs, ev.RuleID)
			cls.Reasons = append(cls.Reasons, ev.Reasons...)
			cls.Tags = append(cls.Tags, ev.Tags...)
			if ev.Standard != "" {
				standardScores[ev.Standard] += ev.ConfidenceDelta
			}
			if ev.Implementation != "" && ev.Implementation != "unknown" {
				implScores[ev.Implementation] += ev.ConfidenceDelta
			}
		}
	}

	bestStd, bestStdScore := "unknown", 0.0
	for k, v := range standardScores {
		if v > bestStdScore {
			bestStd, bestStdScore = k, v
		}
	}
	bestImpl, bestImplScore := "unknown", 0.0
	for k, v := range implScores {
		if v > bestImplScore {
			bestImpl, bestImplScore = k, v
		}
	}

	if bestStdScore > 0 {
		cls.Standard = bestStd
		cls.Confidence += bestStdScore
	}
	if bestImplScore > 0 {
		cls.Implementation = bestImpl
	}
	if cls.Confidence < 0 {
		cls.Confidence = 0
	}
	if cls.Confidence > 0.99 {
		cls.Confidence = 0.99
	}
	cls.RuleIDs = uniqueStrings(cls.RuleIDs)
	cls.Reasons = uniqueStrings(cls.Reasons)
	cls.Tags = uniqueStrings(cls.Tags)
	return cls
}

type sep50MethodRule struct{}

func (sep50MethodRule) ID() string { return "method.sep50" }
func (sep50MethodRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindInvocation {
		return nil
	}
	m := strings.ToLower(c.MethodName)
	if _, ok := sep50MethodNames[m]; !ok {
		return nil
	}
	delta := 0.10
	if m == "owner_of" || m == "token_uri" || m == "token_uri_of" || m == "approve_for_all" || m == "get_approved" || m == "is_approved_for_all" {
		delta = 0.55
	} else if m == "mint" || m == "burn" || m == "burn_from" || m == "transfer_from" {
		delta = 0.35
	} else if m == "transfer" || m == "approve" || m == "name" || m == "symbol" {
		delta = 0.08
	}
	return []Evidence{{RuleID: "method.sep50", Standard: "sep_50", ClassificationSource: "heuristic_framework", ConfidenceDelta: delta, Tags: []string{"method", "sep50"}, Reasons: []string{"SEP-50 style method detected: " + m}}}
}

type approveForAllRule struct{}

func (approveForAllRule) ID() string { return "nft.approve_for_all" }
func (approveForAllRule) Evaluate(c Candidate) []Evidence {
	if strings.ToLower(c.ActionHint) != "approve_for_all" && strings.ToLower(c.MethodName) != "approve_for_all" && !containsAny(strings.ToLower(strings.Join(append(c.KeyParts, c.ValueParts...), " ")), "approve_for_all", "operator") {
		return nil
	}
	return []Evidence{{RuleID: "nft.approve_for_all", Standard: "sep_50", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.45, Tags: []string{"approval", "operator"}, Reasons: []string{"approve_for_all/operator approval is a strong NFT signal"}}}
}

type tokenURIRule struct{}

func (tokenURIRule) ID() string { return "nft.token_uri" }
func (tokenURIRule) Evaluate(c Candidate) []Evidence {
	joined := strings.ToLower(strings.Join(append(append([]string{}, c.RawTopics...), c.RawData), " "))
	if c.MetadataURI == "" && !containsAny(joined, "token_uri", "metadata", "uri") {
		return nil
	}
	return []Evidence{{RuleID: "nft.token_uri", Standard: "sep_50", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.35, Tags: []string{"metadata"}, Reasons: []string{"token metadata URI signal detected"}}}
}

type openzeppelinRule struct{}

func (openzeppelinRule) ID() string { return "impl.openzeppelin" }
func (openzeppelinRule) Evaluate(c Candidate) []Evidence {
	joined := strings.ToLower(strings.Join(allCandidateParts(c), " "))
	if !containsAny(joined, "openzeppelin", "non_fungible", "non-fungible") {
		return nil
	}
	return []Evidence{{RuleID: "impl.openzeppelin", Standard: "custom_nft", Implementation: "openzeppelin", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.4, Tags: []string{"implementation"}, Reasons: []string{"OpenZeppelin NFT marker detected"}}}
}

type stateExactOwnerRule struct{}

func (stateExactOwnerRule) ID() string { return "state.exact_owner" }
func (stateExactOwnerRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindState || len(c.KeyParts) == 0 {
		return nil
	}
	if c.KeyParts[0] != "Owner" {
		return nil
	}
	return []Evidence{{RuleID: "state.exact_owner", Standard: "sep_50", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.57, Tags: []string{"state", "owner"}, Reasons: []string{"exact NFT owner(token_id) storage key detected"}}}
}

type stateExactApprovalForAllRule struct{}

func (stateExactApprovalForAllRule) ID() string { return "state.exact_approval_for_all" }
func (stateExactApprovalForAllRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindState || len(c.KeyParts) == 0 {
		return nil
	}
	if c.KeyParts[0] != "ApprovalForAll" {
		return nil
	}
	return []Evidence{{RuleID: "state.exact_approval_for_all", Standard: "sep_50", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.52, Tags: []string{"state", "approval"}, Reasons: []string{"exact NFT approval_for_all storage key detected"}}}
}

type ownerStateRule struct{}

func (ownerStateRule) ID() string { return "state.owner" }
func (ownerStateRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindState {
		return nil
	}
	joined := strings.ToLower(strings.Join(append(c.KeyParts, c.ValueParts...), " "))
	if !containsAny(joined, "owner", "holder") {
		return nil
	}
	return []Evidence{{RuleID: "state.owner", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.28, Tags: []string{"state", "ownership"}, Reasons: []string{"owner-like state pattern detected"}}}
}

type metadataStateRule struct{}

func (metadataStateRule) ID() string { return "state.metadata" }
func (metadataStateRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindState {
		return nil
	}
	joined := strings.ToLower(strings.Join(append(c.KeyParts, c.ValueParts...), " "))
	if !containsAny(joined, "token_uri", "metadata", "uri") {
		return nil
	}
	return []Evidence{{RuleID: "state.metadata", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.25, Tags: []string{"state", "metadata"}, Reasons: []string{"metadata-like state pattern detected"}}}
}

type collectionMetadataRule struct{}

func (collectionMetadataRule) ID() string { return "state.collection_metadata" }
func (collectionMetadataRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindState {
		return nil
	}
	joined := strings.ToLower(strings.Join(append(c.KeyParts, c.ValueParts...), " "))
	if !containsAny(joined, "name", "symbol") {
		return nil
	}
	return []Evidence{{RuleID: "state.collection_metadata", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.18, Tags: []string{"collection"}, Reasons: []string{"collection metadata state detected"}}}
}

type mintBurnRule struct{}

func (mintBurnRule) ID() string { return "method.mint_burn" }
func (mintBurnRule) Evaluate(c Candidate) []Evidence {
	m := strings.ToLower(c.MethodName)
	a := strings.ToLower(c.ActionHint)
	if m != "mint" && m != "burn" && m != "burn_from" && a != "mint" && a != "burn" {
		return nil
	}
	return []Evidence{{RuleID: "method.mint_burn", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.32, Tags: []string{"lifecycle"}, Reasons: []string{"mint/burn lifecycle method detected"}}}
}

type transferFromRule struct{}

func (transferFromRule) ID() string { return "method.transfer_from" }
func (transferFromRule) Evaluate(c Candidate) []Evidence {
	if strings.ToLower(c.MethodName) != "transfer_from" {
		return nil
	}
	return []Evidence{{RuleID: "method.transfer_from", Standard: "sep_50", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.26, Tags: []string{"transfer"}, Reasons: []string{"transfer_from is stronger than plain transfer for NFT-like contracts"}}}
}

type customCollectionMethodRule struct{}

func (customCollectionMethodRule) ID() string { return "method.custom_collection" }
func (customCollectionMethodRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindInvocation {
		return nil
	}
	m := strings.ToLower(c.MethodName)
	if m != "get_total_minted" && m != "get_max_supply" && m != "next_token_id" && m != "update_uri" && m != "trait_metadata_uri" {
		return nil
	}
	return []Evidence{{RuleID: "method.custom_collection", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.28, Tags: []string{"collection"}, Reasons: []string{"collection-level NFT method detected: " + m}}}
}

type customTraitMethodRule struct{}

func (customTraitMethodRule) ID() string { return "method.custom_trait" }
func (customTraitMethodRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindInvocation {
		return nil
	}
	m := strings.ToLower(c.MethodName)
	if m != "set_trait" && m != "trait_value" && m != "trait_values" && m != "governance" && m != "clawback" {
		return nil
	}
	return []Evidence{{RuleID: "method.custom_trait", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.26, Tags: []string{"trait"}, Reasons: []string{"custom NFT trait/governance method detected: " + m}}}
}

type metadataTripletRule struct{}

func (metadataTripletRule) ID() string { return "metadata.triplet" }
func (metadataTripletRule) Evaluate(c Candidate) []Evidence {
	joined := strings.ToLower(strings.Join(allCandidateParts(c), " "))
	if containsAny(joined, "name") && containsAny(joined, "symbol") && (c.MetadataURI != "" || containsAny(joined, "token_uri", "uri", "https://", "ipfs://", "ar://")) {
		return []Evidence{{RuleID: "metadata.triplet", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.35, Tags: []string{"metadata", "collection"}, Reasons: []string{"name + symbol + uri metadata triplet detected"}}}
	}
	return nil
}

type tokenIDShapeRule struct{}

func (tokenIDShapeRule) ID() string { return "token_id.shape" }
func (tokenIDShapeRule) Evaluate(c Candidate) []Evidence {
	if c.TokenID == "" {
		return nil
	}
	if looksTokenIDLike(c.TokenID) {
		return []Evidence{{RuleID: "token_id.shape", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.18, Tags: []string{"token_id"}, Reasons: []string{"token-id-like argument/value detected"}}}
	}
	return nil
}

type genericTransferRule struct{}

func (genericTransferRule) ID() string { return "event.generic_transfer" }
func (genericTransferRule) Evaluate(c Candidate) []Evidence {
	if strings.ToLower(c.ActionHint) != "transfer" && strings.ToLower(c.ActionHint) != "mint" && strings.ToLower(c.ActionHint) != "burn" && strings.ToLower(c.MethodName) != "transfer" && strings.ToLower(c.MethodName) != "transfer_from" {
		return nil
	}
	return []Evidence{{RuleID: "event.generic_transfer", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.03, Tags: []string{"transfer"}, Reasons: []string{"generic transfer signal detected"}}}
}

type genericApproveRule struct{}

func (genericApproveRule) ID() string { return "event.generic_approve" }
func (genericApproveRule) Evaluate(c Candidate) []Evidence {
	if strings.ToLower(c.ActionHint) != "approve" && strings.ToLower(c.MethodName) != "approve" {
		return nil
	}
	return []Evidence{{RuleID: "event.generic_approve", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: 0.02, Tags: []string{"approval"}, Reasons: []string{"generic approve signal detected"}}}
}

type balancePenaltyRule struct{}

func (balancePenaltyRule) ID() string { return "penalty.balance_only" }
func (balancePenaltyRule) Evaluate(c Candidate) []Evidence {
	if strings.ToLower(c.MethodName) != "balance" {
		return nil
	}
	return []Evidence{{RuleID: "penalty.balance_only", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: -0.15, Tags: []string{"penalty", "balance"}, Reasons: []string{"balance alone is weak and common outside NFTs"}}}
}

type ftMethodPenaltyRule struct{}

func (ftMethodPenaltyRule) ID() string { return "penalty.ft_method" }
func (ftMethodPenaltyRule) Evaluate(c Candidate) []Evidence {
	m := strings.ToLower(c.MethodName)
	switch m {
	case "total_supply", "allowance", "decimals":
		return []Evidence{{RuleID: "penalty.ft_method", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: -0.65, Tags: []string{"penalty", "fungible"}, Reasons: []string{"fungible-token method detected: " + m}}}
	}
	return nil
}

type ftStatePenaltyRule struct{}

func (ftStatePenaltyRule) ID() string { return "penalty.ft_state" }
func (ftStatePenaltyRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindState || len(c.KeyParts) == 0 {
		return nil
	}
	switch c.KeyParts[0] {
	case "TotalSupply", "Allowance":
		return []Evidence{{RuleID: "penalty.ft_state", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: -0.70, Tags: []string{"penalty", "fungible"}, Reasons: []string{"fungible-token storage key detected: " + c.KeyParts[0]}}}
	}
	return nil
}

type ftAmountShapePenaltyRule struct{}

func (ftAmountShapePenaltyRule) ID() string { return "penalty.ft_amount_shape" }
func (ftAmountShapePenaltyRule) Evaluate(c Candidate) []Evidence {
	m := strings.ToLower(c.MethodName)
	a := strings.ToLower(c.ActionHint)
	if m == "mint" || m == "burn" || m == "transfer" || m == "transfer_from" || a == "mint" || a == "burn" || a == "transfer" {
		joined := strings.ToLower(strings.Join(allCandidateParts(c), " "))
		if containsAny(joined, "total_supply", "allowance", "decimals") {
			return []Evidence{{RuleID: "penalty.ft_amount_shape", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: -0.45, Tags: []string{"penalty", "amount"}, Reasons: []string{"amount-oriented FT context detected around transfer/mint/burn"}}}
		}
	}
	return nil
}

type fungibleAssetPenaltyRule struct{}

func (fungibleAssetPenaltyRule) ID() string { return "penalty.fungible_asset" }
func (fungibleAssetPenaltyRule) Evaluate(c Candidate) []Evidence {
	joined := strings.Join(allCandidateParts(c), " ")
	for _, part := range strings.Fields(joined) {
		if strings.Count(part, ":") == 1 && strings.HasPrefix(strings.Split(part, ":")[1], "G") {
			return []Evidence{{RuleID: "penalty.fungible_asset", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: -0.55, Tags: []string{"penalty", "fungible"}, Reasons: []string{"asset-code:issuer marker suggests fungible token, not NFT"}}}
		}
	}
	return nil
}

type nativeAssetPenaltyRule struct{}

func (nativeAssetPenaltyRule) ID() string { return "penalty.native_asset" }
func (nativeAssetPenaltyRule) Evaluate(c Candidate) []Evidence {
	joined := strings.ToLower(strings.Join(allCandidateParts(c), " "))
	if !containsAny(joined, " native ", "[native", " native]", "native") {
		return nil
	}
	return []Evidence{{RuleID: "penalty.native_asset", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: -0.65, Tags: []string{"penalty", "native"}, Reasons: []string{"native asset marker strongly suggests non-NFT activity"}}}
}

type temporaryStatePenaltyRule struct{}

func (temporaryStatePenaltyRule) ID() string { return "penalty.temporary_state" }
func (temporaryStatePenaltyRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindState || c.Durability != "temporary" {
		return nil
	}
	return []Evidence{{RuleID: "penalty.temporary_state", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: -0.20, Tags: []string{"penalty", "temporary"}, Reasons: []string{"temporary contract state is a weaker NFT ownership/metadata signal"}}}
}

type genericMetadataStatePenaltyRule struct{}

func (genericMetadataStatePenaltyRule) ID() string { return "penalty.generic_metadata_state" }
func (genericMetadataStatePenaltyRule) Evaluate(c Candidate) []Evidence {
	if c.Kind != CandidateKindState {
		return nil
	}
	joined := strings.ToLower(strings.Join(append(c.KeyParts, c.ValueParts...), " "))
	if !containsAny(joined, "metadata", "uri") {
		return nil
	}
	if containsAny(joined, "token_uri", "image", "animation_url", "attributes", "trait") {
		return nil
	}
	return []Evidence{{RuleID: "penalty.generic_metadata_state", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: -0.22, Tags: []string{"penalty", "metadata"}, Reasons: []string{"generic metadata/uri state without token-specific markers is weak evidence"}}}
}

type stakingPatternPenaltyRule struct{}

func (stakingPatternPenaltyRule) ID() string { return "penalty.staking_pattern" }
func (stakingPatternPenaltyRule) Evaluate(c Candidate) []Evidence {
	joined := strings.ToLower(strings.Join(allCandidateParts(c), " "))
	if !containsAny(joined, "stake", "staking", "sequence", "gap", "zeros", "reward") {
		return nil
	}
	return []Evidence{{RuleID: "penalty.staking_pattern", Standard: "custom_nft", ClassificationSource: "heuristic_framework", ConfidenceDelta: -0.30, Tags: []string{"penalty", "staking"}, Reasons: []string{"staking/account-tracking pattern suggests non-NFT state"}}}
}

func allCandidateParts(c Candidate) []string {
	parts := append([]string{}, c.RawTopics...)
	parts = append(parts, c.RawData)
	parts = append(parts, c.KeyParts...)
	parts = append(parts, c.ValueParts...)
	if c.MethodName != "" {
		parts = append(parts, c.MethodName)
	}
	if c.ActionHint != "" {
		parts = append(parts, c.ActionHint)
	}
	if c.MetadataURI != "" {
		parts = append(parts, c.MetadataURI)
	}
	return parts
}

func looksTokenIDLike(v string) bool {
	if v == "" {
		return false
	}
	lower := strings.ToLower(v)
	if strings.HasPrefix(lower, "0x") {
		return true
	}
	digits := 0
	for _, r := range v {
		if r >= '0' && r <= '9' {
			digits++
			continue
		}
		if r == '-' || r == '_' {
			continue
		}
		return false
	}
	return digits > 0
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
