// CAP-84 (muxed contract addresses, SEP-23 "W" strkey) regression tests.
//
// Before Protocol 30 these conversions switched on the account and contract
// arms of xdr.ScAddress only. A muxed contract address fell through that
// switch, so the address was emitted as empty/void/nil rather than as an
// error — the silent-wrong failure mode. Where the code instead read
// addr.ContractId directly, a muxed address made that read nil: a dropped
// record where the nil was guarded, a panic where it was not.
package nft_events

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// testAccountAddress is an arbitrary valid G-address; only its validity
// matters here.
const testAccountAddress = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"

// muxedContractFixture builds a CAP-84 muxed contract ScAddress together with
// the W-strkey it must render as and the C-strkey of the contract it
// multiplexes. Both expected strings come from the SDK's own encoder rather
// than a hand-assembled byte layout, so these tests pin behavior instead of
// re-deriving it.
func muxedContractFixture(t *testing.T, muxID uint64) (xdr.ScAddress, string, string) {
	t.Helper()

	var raw xdr.ContractId
	for i := range raw {
		raw[i] = byte(i + 1)
	}

	contractAddr, err := strkey.Encode(strkey.VersionByteContract, raw[:])
	if err != nil {
		t.Fatalf("encode contract address: %v", err)
	}

	var mc strkey.MuxedContract
	mc.SetID(muxID)
	if err := mc.SetContractID(contractAddr); err != nil {
		t.Fatalf("set muxed contract id: %v", err)
	}
	muxedAddr, err := mc.Address()
	if err != nil {
		t.Fatalf("encode muxed contract address: %v", err)
	}

	addr := xdr.ScAddress{
		Type: xdr.ScAddressTypeScAddressTypeMuxedContract,
		MuxedContract: &xdr.MuxedContract{
			Id:         xdr.Uint64(muxID),
			ContractId: raw,
		},
	}
	return addr, muxedAddr, contractAddr
}

// plainContractFixture builds the same contract as a plain C-address, so a
// test can assert that muxed and plain forms resolve to the same contract.
func plainContractFixture(t *testing.T) (xdr.ScAddress, string) {
	t.Helper()

	var raw xdr.ContractId
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	contractAddr, err := strkey.Encode(strkey.VersionByteContract, raw[:])
	if err != nil {
		t.Fatalf("encode contract address: %v", err)
	}
	return xdr.ScAddress{
		Type:       xdr.ScAddressTypeScAddressTypeContract,
		ContractId: &raw,
	}, contractAddr
}

func addressScVal(addr xdr.ScAddress) xdr.ScVal {
	return xdr.ScVal{Type: xdr.ScValTypeScvAddress, Address: &addr}
}

func TestContractIDFromScAddress(t *testing.T) {
	muxed, _, muxedContract := muxedContractFixture(t, 42)
	plain, plainContract := plainContractFixture(t)

	var accountID xdr.AccountId
	if err := accountID.SetAddress(testAccountAddress); err != nil {
		t.Fatalf("set account address: %v", err)
	}

	tests := []struct {
		name   string
		addr   xdr.ScAddress
		want   string
		wantOK bool
	}{
		{name: "plain contract", addr: plain, want: plainContract, wantOK: true},
		// The point of the muxed case: a W-address identifies the same
		// contract as its C-address, so contract-scoped matching keeps
		// working instead of silently failing to match.
		{name: "muxed contract resolves to the multiplexed contract", addr: muxed, want: muxedContract, wantOK: true},
		{name: "account is not a contract", addr: xdr.ScAddress{
			Type:      xdr.ScAddressTypeScAddressTypeAccount,
			AccountId: &accountID,
		}, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := contractIDFromScAddress(tt.addr)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContractIDFromScAddressAgreesForMuxedAndPlain(t *testing.T) {
	muxed, _, _ := muxedContractFixture(t, 7)
	plain, _ := plainContractFixture(t)

	fromMuxed, ok := contractIDFromScAddress(muxed)
	if !ok {
		t.Fatal("muxed contract address did not resolve")
	}
	fromPlain, ok := contractIDFromScAddress(plain)
	if !ok {
		t.Fatal("plain contract address did not resolve")
	}
	if fromMuxed != fromPlain {
		t.Errorf("muxed resolved to %q, plain to %q; want the same contract", fromMuxed, fromPlain)
	}
}

func TestScValToStringRendersMuxedContract(t *testing.T) {
	muxed, muxedAddr, _ := muxedContractFixture(t, 42)

	if got := scValToString(addressScVal(muxed)); got != muxedAddr {
		t.Errorf("got %q, want %q", got, muxedAddr)
	}
}
