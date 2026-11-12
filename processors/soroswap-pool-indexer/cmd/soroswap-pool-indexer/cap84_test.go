// CAP-84 (muxed contract addresses, SEP-23 "W" strkey) regression tests.
//
// Before Protocol 30 these conversions switched on the account and contract
// arms of xdr.ScAddress only. A muxed contract address fell through that
// switch, so the address was emitted as empty/void/nil rather than as an
// error — the silent-wrong failure mode. Where the code instead read
// addr.ContractId directly, a muxed address made that read nil: a dropped
// record where the nil was guarded, a panic where it was not.
package main

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

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

func TestExtractAddressRendersMuxedContract(t *testing.T) {
	muxed, muxedAddr, _ := muxedContractFixture(t, 42)

	// An empty return here is indistinguishable downstream from an absent
	// pool address, which is why the muxed arm must not fall through.
	if got := extractAddress(addressScVal(muxed)); got != muxedAddr {
		t.Errorf("got %q, want %q", got, muxedAddr)
	}
}

func TestExtractAddressRendersPlainContract(t *testing.T) {
	plain, plainAddr := plainContractFixture(t)

	if got := extractAddress(addressScVal(plain)); got != plainAddr {
		t.Errorf("got %q, want %q", got, plainAddr)
	}
}
