package contract_created

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

func TestDeriveContractIDMatchesAssetHelper(t *testing.T) {
	asset, err := xdr.NewAsset(xdr.AssetTypeAssetTypeCreditAlphanum4, xdr.AlphaNum4{
		AssetCode: [4]byte{'U', 'S', 'D', 'C'},
		Issuer:    mustAccountID(t, "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"),
	})
	if err != nil {
		t.Fatalf("new asset: %v", err)
	}

	expected, err := asset.ContractID("Public Global Stellar Network ; September 2015")
	if err != nil {
		t.Fatalf("asset contract id: %v", err)
	}

	preimage := xdr.ContractIdPreimage{
		Type:      xdr.ContractIdPreimageTypeContractIdPreimageFromAsset,
		FromAsset: &asset,
	}
	got := deriveContractID("Public Global Stellar Network ; September 2015", preimage)
	if got == "" {
		t.Fatal("expected derived contract id")
	}

	want, err := strkey.Encode(strkey.VersionByteContract, expected[:])
	if err != nil {
		t.Fatalf("encode expected contract id: %v", err)
	}
	if got != want {
		t.Fatalf("contract id mismatch\n got:  %s\n want: %s", got, want)
	}
}

func TestClassifyNFTLike(t *testing.T) {
	engine := newHeuristicEngine()
	candidate := Candidate{
		ExecutableType:  "wasm",
		ConstructorArgs: []string{"GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF", "SCF Membership", "scf"},
		InitializedState: []*StateEntry{
			{Key: "[Metadata]", Value: "{name:SCF Membership,symbol:scf}"},
			{Key: "NextTokenId", Value: "0"},
		},
	}

	classification := engine.Classify(candidate)
	if classification.Family != "nft_like" {
		t.Fatalf("expected nft_like, got %s (%v)", classification.Family, classification.Reasons)
	}
}

func TestClassifyIdentityRegistryLike(t *testing.T) {
	engine := newHeuristicEngine()
	candidate := Candidate{
		ExecutableType:   "wasm",
		ConstructorArgs:  []string{"GBRPYHIL2C7QZV6XQ7Y3YJ2M2R6M2JXKXX6T7N5JXJ4Q7L4U2M6X7YJH", "Agent Identity", "agent"},
		InitializedState: []*StateEntry{{Key: "[IdentityRegistry]", Value: "CBAR..."}, {Key: "[Metadata]", Value: "{name:Agent Identity,symbol:AGENT}"}, {Key: "[Owner]", Value: "GBRPYHIL2C7QZV6XQ7Y3YJ2M2R6M2JXKXX6T7N5JXJ4Q7L4U2M6X7YJH"}},
	}

	classification := engine.Classify(candidate)
	if classification.Family != "identity_registry_like" {
		t.Fatalf("expected identity_registry_like, got %s (%v)", classification.Family, classification.Reasons)
	}
}

func TestClassifyFungibleLike(t *testing.T) {
	engine := newHeuristicEngine()
	candidate := Candidate{ExecutableType: "stellar_asset", InitializedState: []*StateEntry{{Key: "[Admin]", Value: "GA..."}, {Key: "METADATA", Value: "{}"}, {Key: "[AssetInfo]", Value: "USDC"}}}

	classification := engine.Classify(candidate)
	if classification.Family != "fungible_token_like" {
		t.Fatalf("expected fungible_token_like, got %s", classification.Family)
	}
}

func TestClassifyMultisigLike(t *testing.T) {
	engine := newHeuristicEngine()
	candidate := Candidate{InitializedState: []*StateEntry{{Key: "[Signers,0]", Value: "[]"}, {Key: "[Policies,0]", Value: "[]"}, {Key: "[Ids,[Default]]", Value: "[0]"}, {Key: "[Meta,0]", Value: "{name:multisig}"}, {Key: "[Fingerprint,abc]", Value: "true"}}}

	classification := engine.Classify(candidate)
	if classification.Family != "multisig_like" {
		t.Fatalf("expected multisig_like, got %s (%v)", classification.Family, classification.Reasons)
	}
}

func TestClassifyDefiPoolAdapterLike(t *testing.T) {
	engine := newHeuristicEngine()
	candidate := Candidate{InitializedState: []*StateEntry{{Key: "[BlendPool]", Value: "pool1"}, {Key: "[Token]", Value: "token1"}, {Key: "[SacToken]", Value: "token2"}}}

	classification := engine.Classify(candidate)
	if classification.Family != "defi_pool_adapter_like" {
		t.Fatalf("expected defi_pool_adapter_like, got %s (%v)", classification.Family, classification.Reasons)
	}
}

func TestIdentityRegistryGetsAgentAndNftTags(t *testing.T) {
	engine := newHeuristicEngine()
	candidate := Candidate{
		ConstructorArgs:  []string{"GBRPYHIL2C7QZV6XQ7Y3YJ2M2R6M2JXKXX6T7N5JXJ4Q7L4U2M6X7YJH", "Agent Registry", "AGENT"},
		InitializedState: []*StateEntry{{Key: "[IdentityRegistry]", Value: "reg1"}, {Key: "[Metadata]", Value: "{name:Agent Registry,symbol:AGENT}"}, {Key: "[Owner]", Value: "GBRPYHIL2C7QZV6XQ7Y3YJ2M2R6M2JXKXX6T7N5JXJ4Q7L4U2M6X7YJH"}},
	}

	classification := engine.Classify(candidate)
	if classification.Family != "identity_registry_like" {
		t.Fatalf("expected identity_registry_like, got %s (%v)", classification.Family, classification.Reasons)
	}
	joinedTags := strings.Join(classification.Tags, ",")
	if !strings.Contains(joinedTags, "agent") || !strings.Contains(joinedTags, "nft_like") {
		t.Fatalf("expected agent and nft_like tags, got %v", classification.Tags)
	}
}

func mustAccountID(t *testing.T, addr string) xdr.AccountId {
	t.Helper()
	var account xdr.AccountId
	if err := account.SetAddress(addr); err != nil {
		t.Fatalf("set address: %v", err)
	}
	return account
}
