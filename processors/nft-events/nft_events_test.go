package nft_events

import "testing"

func TestActionForMethod(t *testing.T) {
	cases := map[string]string{
		"transfer":        "transfer",
		"transfer_from":   "transfer",
		"approve":         "approve",
		"approve_for_all": "approve_for_all",
		"mint":            "mint",
		"burn":            "burn",
		"owner_of":        "detect",
	}

	for in, want := range cases {
		if got := actionForMethod(in); got != want {
			t.Fatalf("actionForMethod(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeuristicEngineInvocation(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindInvocation, MethodName: "owner_of", ActionHint: "detect", RawTopics: []string{"42"}})
	if cls.Standard != "sep_50" {
		t.Fatalf("expected sep_50, got %q", cls.Standard)
	}
	if cls.Source != "heuristic_framework" {
		t.Fatalf("expected heuristic_framework, got %q", cls.Source)
	}
	if cls.Confidence < minimumConfidence(CandidateKindInvocation) {
		t.Fatalf("expected high enough confidence, got %f", cls.Confidence)
	}
}

func TestHeuristicEngineState(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindState, ActionHint: "approve_for_all", KeyParts: []string{"operator"}, ValueParts: []string{"GABC"}})
	if cls.Standard != "sep_50" {
		t.Fatalf("expected sep_50, got %q", cls.Standard)
	}
	if cls.Source != "heuristic_framework" {
		t.Fatalf("expected heuristic_framework, got %q", cls.Source)
	}
	if cls.Confidence < 0.5 {
		t.Fatalf("expected strong confidence, got %f", cls.Confidence)
	}
}

func TestFungiblePenalty(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindEvent, ActionHint: "transfer", RawTopics: []string{"transfer", "GA", "GB", "USDC:GISSUER"}})
	if cls.Confidence >= minimumConfidence(CandidateKindEvent) {
		t.Fatalf("expected fungible penalty to keep confidence low, got %f", cls.Confidence)
	}
}

func TestTemporaryMetadataStatePenalty(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindState, ActionHint: "metadata_update", KeyParts: []string{"metadata"}, ValueParts: []string{"sequence", "stake"}, Durability: "temporary"})
	if cls.Confidence >= minimumConfidence(CandidateKindState) {
		t.Fatalf("expected temporary generic metadata state to stay below threshold, got %f", cls.Confidence)
	}
}

func TestMinimumConfidence(t *testing.T) {
	if got := minimumConfidence(CandidateKindInvocation); got != 0.55 {
		t.Fatalf("unexpected invocation threshold: %f", got)
	}
	if got := minimumConfidence(CandidateKindEvent); got != 0.65 {
		t.Fatalf("unexpected event threshold: %f", got)
	}
	if got := minimumConfidence(CandidateKindState); got != 0.80 {
		t.Fatalf("unexpected state threshold: %f", got)
	}
}

func TestTransferAloneIsWeak(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindInvocation, MethodName: "transfer", ActionHint: "transfer", RawTopics: []string{"GAAA", "GBBB", "1"}, TokenID: "1"})
	if cls.Confidence >= minimumConfidence(CandidateKindInvocation) {
		t.Fatalf("expected plain transfer to remain below invocation threshold, got %f", cls.Confidence)
	}
}

func TestMintIsStronger(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindInvocation, MethodName: "mint", ActionHint: "mint", RawTopics: []string{"GAAA", "42"}, TokenID: "42"})
	if cls.Confidence < minimumConfidence(CandidateKindInvocation) {
		t.Fatalf("expected mint to cross invocation threshold, got %f", cls.Confidence)
	}
}

func TestOwnerOfIsStrongSignal(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindInvocation, MethodName: "owner_of", ActionHint: "detect", RawTopics: []string{"0"}, TokenID: "0"})
	if cls.Confidence < minimumConfidence(CandidateKindInvocation) {
		t.Fatalf("expected owner_of to cross threshold, got %f", cls.Confidence)
	}
}

func TestCustomTraitMethodSignal(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindInvocation, MethodName: "set_trait", ActionHint: "detect", RawTopics: []string{"0", "role", "3"}, TokenID: "0"})
	if cls.Confidence < minimumConfidence(CandidateKindInvocation) {
		t.Fatalf("expected set_trait to cross threshold, got %f", cls.Confidence)
	}
}

func TestLooksTokenIDLike(t *testing.T) {
	if !looksTokenIDLike("42") {
		t.Fatalf("expected numeric token id to match")
	}
	if !looksTokenIDLike("0x00000001") {
		t.Fatalf("expected hex token id to match")
	}
	if looksTokenIDLike("USDC:GISSUER") {
		t.Fatalf("did not expect asset string to look like token id")
	}
}

func TestConfirmedProfilePromotesWeakTransfer(t *testing.T) {
	o := NewOrigin("test")
	strong := Classification{Standard: "sep_50", Implementation: "unknown", Confidence: 0.8, RuleIDs: []string{"method.sep50", "token_id.shape"}}
	o.rememberContractProfile(Candidate{ContractID: "CABC", MethodName: "owner_of", Kind: CandidateKindInvocation}, strong)
	weak := Classification{Standard: "custom_nft", Implementation: "unknown", Confidence: 0.30}
	promoted := o.applyContractProfile(Candidate{ContractID: "CABC", MethodName: "transfer", ActionHint: "transfer", Kind: CandidateKindInvocation}, weak)
	if promoted.Confidence < minimumConfidence(CandidateKindInvocation) {
		t.Fatalf("expected promoted transfer to cross threshold, got %f", promoted.Confidence)
	}
}

func TestExactOwnerStateRule(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindState, ActionHint: "owner_update", KeyParts: []string{"Owner", "0"}, ValueParts: []string{"GABC"}, TokenID: "0", Durability: "persistent"})
	if cls.Confidence < minimumConfidence(CandidateKindState) {
		t.Fatalf("expected exact owner state to cross state threshold, got %f", cls.Confidence)
	}
}

func TestExtractStateTokenID(t *testing.T) {
	if got := extractStateTokenID([]string{"Owner", "0"}, []string{"GABC"}, ""); got != "0" {
		t.Fatalf("expected token id 0 from Owner key, got %q", got)
	}
	if got := extractStateTokenID([]string{"Approval", "42"}, []string{"GABC"}, ""); got != "42" {
		t.Fatalf("expected token id 42 from Approval key, got %q", got)
	}
	if got := extractStateTokenID([]string{"Balance", "GABC"}, []string{"1"}, ""); got != "1" {
		t.Fatalf("expected fallback token-like value 1, got %q", got)
	}
}

func TestFungibleMethodPenalty(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindInvocation, MethodName: "total_supply"})
	if cls.Confidence >= minimumConfidence(CandidateKindInvocation) {
		t.Fatalf("expected total_supply to stay below NFT threshold, got %f", cls.Confidence)
	}
}

func TestFungibleStatePenalty(t *testing.T) {
	engine := newHeuristicEngine()
	cls := engine.Classify(Candidate{Kind: CandidateKindState, KeyParts: []string{"TotalSupply"}, Durability: "persistent"})
	if cls.Confidence >= minimumConfidence(CandidateKindState) {
		t.Fatalf("expected TotalSupply state to stay below NFT threshold, got %f", cls.Confidence)
	}
}

func TestExtractMetadataURI(t *testing.T) {
	uri, storage := extractMetadataURI("foo", "ipfs://bafy.../42.json")
	if uri == "" || storage != "ipfs" {
		t.Fatalf("expected ipfs uri, got uri=%q storage=%q", uri, storage)
	}
}
