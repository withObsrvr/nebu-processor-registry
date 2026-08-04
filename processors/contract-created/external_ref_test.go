package contract_created

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

const (
	testOwnerAccount = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
	testPassphrase   = "Public Global Stellar Network ; September 2015"
)

// testOwnerContract is derived rather than hardcoded so the strkey checksum is
// always valid.
var testOwnerContract = func() string {
	var raw [32]byte
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	encoded, err := strkey.Encode(strkey.VersionByteContract, raw[:])
	if err != nil {
		panic("encode test contract address: " + err.Error())
	}
	return encoded
}()

// externalRefExecutable builds a CAP-0085 external-reference executable owned
// by the given strkey address (G... account or C... contract).
func externalRefExecutable(t *testing.T, owner, tag string) xdr.ContractExecutable {
	t.Helper()
	return xdr.ContractExecutable{
		Type: xdr.ContractExecutableTypeContractExecutableExternalRef,
		ExternalRef: &xdr.ContractExecutableExternalRef{
			ExecutableOwner: mustScAddress(t, owner),
			Tag:             xdr.ScString(tag),
		},
	}
}

func mustScAddress(t *testing.T, addr string) xdr.ScAddress {
	t.Helper()
	switch {
	case strings.HasPrefix(addr, "G"):
		return xdr.ScAddress{
			Type:      xdr.ScAddressTypeScAddressTypeAccount,
			AccountId: &[]xdr.AccountId{mustAccountID(t, addr)}[0],
		}
	case strings.HasPrefix(addr, "C"):
		decoded, err := strkey.Decode(strkey.VersionByteContract, addr)
		if err != nil {
			t.Fatalf("decode contract address %s: %v", addr, err)
		}
		var cid xdr.ContractId
		copy(cid[:], decoded)
		return xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract, ContractId: &cid}
	default:
		t.Fatalf("unsupported address form: %s", addr)
		return xdr.ScAddress{}
	}
}

func TestExecutableDetailsExternalRef(t *testing.T) {
	for _, tc := range []struct {
		name  string
		owner string
		tag   string
	}{
		{"account owner", testOwnerAccount, "my-external-executable"},
		{"contract owner", testOwnerContract, "vault/v2"},
		{"empty tag", testOwnerAccount, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := executableDetails(externalRefExecutable(t, tc.owner, tc.tag))

			if info.Type != "external_ref" {
				t.Errorf("type = %q, want %q", info.Type, "external_ref")
			}
			if info.Owner != tc.owner {
				t.Errorf("owner = %q, want %q", info.Owner, tc.owner)
			}
			if info.Tag != tc.tag {
				t.Errorf("tag = %q, want %q", info.Tag, tc.tag)
			}
			if info.WasmHash != "" {
				t.Errorf("wasm hash = %q, want empty for external_ref", info.WasmHash)
			}
		})
	}
}

// Protocol 28 must not disturb how the pre-existing arms decode.
func TestExecutableDetailsExistingArmsUnchanged(t *testing.T) {
	var hash xdr.Hash
	for i := range hash {
		hash[i] = byte(i)
	}
	wasm := executableDetails(xdr.ContractExecutable{
		Type:     xdr.ContractExecutableTypeContractExecutableWasm,
		WasmHash: &hash,
	})
	if wasm.Type != "wasm" {
		t.Errorf("wasm type = %q, want %q", wasm.Type, "wasm")
	}
	if wasm.WasmHash != "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f" {
		t.Errorf("wasm hash = %q", wasm.WasmHash)
	}
	if wasm.Owner != "" || wasm.Tag != "" {
		t.Errorf("wasm arm leaked external-ref fields: owner=%q tag=%q", wasm.Owner, wasm.Tag)
	}

	sac := executableDetails(xdr.ContractExecutable{
		Type: xdr.ContractExecutableTypeContractExecutableStellarAsset,
	})
	if sac.Type != "stellar_asset" {
		t.Errorf("sac type = %q, want %q", sac.Type, "stellar_asset")
	}
	if sac.WasmHash != "" || sac.Owner != "" || sac.Tag != "" {
		t.Errorf("stellar_asset arm should carry no payload, got %+v", sac)
	}
}

// key() drives family-profile reuse. Wasm keys on the hash; external-ref
// contracts have no hash and must key on owner+tag instead, otherwise every
// deployment reclassifies from scratch and never caches a profile.
func TestExecutableInfoKey(t *testing.T) {
	tests := []struct {
		name string
		info executableInfo
		want string
	}{
		{
			name: "wasm keys on hash",
			info: executableInfo{Type: "wasm", WasmHash: "abc123"},
			want: "abc123",
		},
		{
			name: "external ref keys on owner and tag",
			info: executableInfo{Type: "external_ref", Owner: testOwnerAccount, Tag: "vault"},
			want: "external_ref:" + testOwnerAccount + ":vault",
		},
		{
			name: "external ref with empty tag still keys",
			info: executableInfo{Type: "external_ref", Owner: testOwnerAccount},
			want: "external_ref:" + testOwnerAccount + ":",
		},
		{
			name: "stellar asset has no key",
			info: executableInfo{Type: "stellar_asset"},
			want: "",
		},
		{
			name: "unknown arm has no key",
			info: executableInfo{Type: "contractexecutablesomethingnew"},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.info.key(); got != tc.want {
				t.Errorf("key() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Two deployments of the same external-ref executable must produce the same
// profile key so the second inherits the first's classification.
func TestExternalRefKeyIsStableAcrossDeployments(t *testing.T) {
	first := executableDetails(externalRefExecutable(t, testOwnerContract, "amm/v1"))
	second := executableDetails(externalRefExecutable(t, testOwnerContract, "amm/v1"))
	other := executableDetails(externalRefExecutable(t, testOwnerContract, "amm/v2"))

	if first.key() == "" {
		t.Fatal("external-ref key must not be empty; an empty key disables profile caching")
	}
	if first.key() != second.key() {
		t.Errorf("same executable produced different keys: %q vs %q", first.key(), second.key())
	}
	if first.key() == other.key() {
		t.Errorf("different tags collided on key %q", first.key())
	}
}

func TestExtractCreateDetailsExternalRef(t *testing.T) {
	exec := externalRefExecutable(t, testOwnerAccount, "external-token")
	preimage := xdr.ContractIdPreimage{
		Type: xdr.ContractIdPreimageTypeContractIdPreimageFromAddress,
		FromAddress: &xdr.ContractIdPreimageFromAddress{
			Address: mustScAddress(t, testOwnerAccount),
			Salt:    xdr.Uint256{1, 2, 3},
		},
	}

	invoke := xdr.InvokeHostFunctionOp{
		HostFunction: xdr.HostFunction{
			Type: xdr.HostFunctionTypeHostFunctionTypeCreateContractV2,
			CreateContractV2: &xdr.CreateContractArgsV2{
				ContractIdPreimage: preimage,
				Executable:         exec,
			},
		},
	}

	details, ok := extractCreateDetails(testPassphrase, ingest.LedgerTransaction{}, invoke)
	if !ok {
		t.Fatal("expected create details to be extracted")
	}
	if details.ExecutableType != "external_ref" {
		t.Errorf("executable type = %q, want %q", details.ExecutableType, "external_ref")
	}
	if details.ExternalRefOwner != testOwnerAccount {
		t.Errorf("owner = %q, want %q", details.ExternalRefOwner, testOwnerAccount)
	}
	if details.ExternalRefTag != "external-token" {
		t.Errorf("tag = %q, want %q", details.ExternalRefTag, "external-token")
	}
	if details.WasmHash != "" {
		t.Errorf("wasm hash = %q, want empty", details.WasmHash)
	}
	if details.ContractID == "" {
		t.Error("expected a derived contract id")
	}
}

// A contract instance holding an external-ref executable must stringify to
// something that identifies the executable, not a bare type name — the value
// feeds Candidate.JoinedLower(), which heuristics match against.
func TestScValToStringExternalRefInstance(t *testing.T) {
	instance := xdr.ScVal{
		Type: xdr.ScValTypeScvContractInstance,
		Instance: &xdr.ScContractInstance{
			Executable: externalRefExecutable(t, testOwnerContract, "lending-pool"),
		},
	}

	got := scValToString(instance)
	if !strings.Contains(got, "lending-pool") {
		t.Errorf("stringified instance %q does not contain the tag", got)
	}
	if !strings.Contains(got, testOwnerContract) {
		t.Errorf("stringified instance %q does not contain the owner", got)
	}
}

// Regression: wasm instances must still stringify to the bare hash.
func TestScValToStringWasmInstanceUnchanged(t *testing.T) {
	var hash xdr.Hash
	hash[0] = 0xab
	instance := xdr.ScVal{
		Type: xdr.ScValTypeScvContractInstance,
		Instance: &xdr.ScContractInstance{
			Executable: xdr.ContractExecutable{
				Type:     xdr.ContractExecutableTypeContractExecutableWasm,
				WasmHash: &hash,
			},
		},
	}

	want := "ab" + strings.Repeat("00", 31)
	if got := scValToString(instance); got != want {
		t.Errorf("wasm instance stringified to %q, want %q", got, want)
	}
}

// The owner and tag must reach the classifier's search corpus.
func TestCandidateJoinedLowerIncludesExternalRef(t *testing.T) {
	c := Candidate{
		ContractID:       "CABC",
		ExecutableType:   "external_ref",
		ExternalRefOwner: testOwnerContract,
		ExternalRefTag:   "Lending-Pool",
	}
	joined := c.JoinedLower()
	if !strings.Contains(joined, strings.ToLower(testOwnerContract)) {
		t.Errorf("JoinedLower() missing owner: %q", joined)
	}
	if !strings.Contains(joined, "lending-pool") {
		t.Errorf("JoinedLower() missing tag: %q", joined)
	}
}
