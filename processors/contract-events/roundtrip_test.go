package contract_events

import (
	"encoding/base64"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// TestScValBase64RoundTrips is the property DATA-9 exists for: a consumer
// must be able to re-run this processor's decode from the XDR it publishes
// and check the result. If the base64 does not decode back to the same
// ScVal, the published raw value is decoration rather than evidence.
func TestScValBase64RoundTrips(t *testing.T) {
	symbol := xdr.ScSymbol("fee")
	i128 := xdr.Int128Parts{Hi: 0, Lo: 100}

	tests := []struct {
		name string
		val  xdr.ScVal
	}{
		{"symbol", xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &symbol}},
		{"i128", xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &i128}},
		{"void", xdr.ScVal{Type: xdr.ScValTypeScvVoid}},
		{"bool", xdr.ScVal{Type: xdr.ScValTypeScvBool, B: boolPtr(true)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := scValBase64(tt.val)
			if encoded == "" {
				t.Fatal("expected non-empty base64")
			}

			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("published XDR is not valid base64: %v", err)
			}

			var back xdr.ScVal
			if err := back.UnmarshalBinary(raw); err != nil {
				t.Fatalf("published XDR does not unmarshal: %v", err)
			}
			if back.Type != tt.val.Type {
				t.Fatalf("round trip changed type: got %v, want %v", back.Type, tt.val.Type)
			}

			// And it must agree with what this processor decoded from it.
			if got, want := convertXdrScValToProto(back), convertXdrScValToProto(tt.val); got.String() != want.String() {
				t.Fatalf("decode of published XDR disagrees with the emitted decode:\n got  %s\n want %s", got, want)
			}
		})
	}
}

// TestScValBase64MatchesObservedOutput pins a value captured from real
// pubnet ledger 60200000, so a change in encoding is visible rather than
// silent.
func TestScValBase64MatchesObservedOutput(t *testing.T) {
	symbol := xdr.ScSymbol("fee")
	val := xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &symbol}
	const observed = "AAAADwAAAANmZWUA"

	if got := scValBase64(val); got != observed {
		t.Fatalf("got %q, want %q (captured from pubnet ledger 60200000)", got, observed)
	}
}

func TestClosedAtRFC3339(t *testing.T) {
	// Fixed three decimals, Z suffix — DATA-3, and uniform width so values
	// sort lexicographically.
	if got, want := closedAtRFC3339(1765158311000), "2025-12-08T01:45:11.000Z"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := closedAtRFC3339(1765158311123), "2025-12-08T01:45:11.123Z"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func boolPtr(b bool) *bool { return &b }
