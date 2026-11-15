package token_transfer

import (
	"encoding/json"
	"math"
	"testing"

	ttpb "github.com/withObsrvr/nebu-processor-registry/processors/token-transfer/proto"
)

// simplifyEvent is the whole HTTP JSON contract and had no test. It is also
// not reachable from the CLI — nothing calls NewServer — so every field it
// renders was unexercised, which is how the muxed id came to be a bare JSON
// number while the NDJSON output renders the same field as a string.
//
// These assert the rendered JSON rather than the struct, because the shape on
// the wire is what a consumer sees: which fields are present, and what type
// each value has.
func TestSimplifyEventJSON(t *testing.T) {
	opIndexThree := uint32(3)
	opIndexZero := uint32(0)

	tests := []struct {
		name string
		ev   *ttpb.TokenTransferEvent
		want map[string]any
		// absent names fields that must not appear at all, which is a
		// different claim from appearing empty.
		absent []string
	}{
		{
			name: "issued transfer carries the asset type and both index forms",
			ev: &ttpb.TokenTransferEvent{
				Meta: &ttpb.EventMeta{
					LedgerSequence: 65447235, ClosedAtUnix: 1794522217,
					ClosedAt: "2026-11-12T22:23:37.000Z", TxHash: "abc",
					TransactionIndex: 164, OperationIndex: 3,
					OperationIndexValue: &opIndexThree, InSuccessfulTx: true,
				},
				Event: &ttpb.TokenTransferEvent_Transfer{Transfer: &ttpb.Transfer{
					From: "GFROM", To: "GTO", AssetCode: "USDC",
					AssetIssuer: "GISSUER", Amount: "30000", AssetType: "issued",
				}},
			},
			want: map[string]any{
				"type": "transfer", "ledger_sequence": 65447235.0,
				"closed_at_unix": 1.794522217e+09,
				"closed_at":      "2026-11-12T22:23:37.000Z",
				"tx_hash":        "abc", "transaction_index": 164.0,
				"operation_index": 3.0, "in_successful_tx": true,
				"from": "GFROM", "to": "GTO", "amount": "30000",
				"asset": map[string]any{"type": "issued", "code": "USDC", "issuer": "GISSUER"},
			},
			absent: []string{"to_muxed_info", "contract_address"},
		},
		{
			// A fee event has no operation index. Rendering 0 would make it
			// indistinguishable from an event at operation 0, which is the
			// distinction operation_index_value exists to keep.
			name: "a fee event omits the operation index entirely",
			ev: &ttpb.TokenTransferEvent{
				Meta: &ttpb.EventMeta{
					LedgerSequence: 65447235, TxHash: "def",
					TransactionIndex: 1, InSuccessfulTx: true,
				},
				Event: &ttpb.TokenTransferEvent_Fee{Fee: &ttpb.Fee{
					From: "GFROM", AssetCode: "XLM", Amount: "200", AssetType: "native",
				}},
			},
			want: map[string]any{
				"type": "fee", "from": "GFROM", "amount": "200",
				"asset": map[string]any{"type": "native", "code": "native"},
			},
			absent: []string{"operation_index", "to", "to_muxed_info"},
		},
		{
			name: "operation index zero is rendered, not dropped",
			ev: &ttpb.TokenTransferEvent{
				Meta: &ttpb.EventMeta{
					TxHash: "ghi", OperationIndex: 0,
					OperationIndexValue: &opIndexZero, InSuccessfulTx: true,
				},
				Event: &ttpb.TokenTransferEvent_Transfer{Transfer: &ttpb.Transfer{
					From: "GFROM", To: "GTO", AssetCode: "XLM", Amount: "1", AssetType: "native",
				}},
			},
			want: map[string]any{"operation_index": 0.0},
		},
		{
			// From a MEMO_TEXT transaction. Verified against pubnet ledger
			// 65447235, where 98 of 101 muxed events are memo text.
			name: "muxed text",
			ev: &ttpb.TokenTransferEvent{
				Meta: &ttpb.EventMeta{
					TxHash: "jkl", InSuccessfulTx: true,
					ToMuxedInfo: &ttpb.MuxedInfo{Content: &ttpb.MuxedInfo_Text{Text: "DREXOVYN"}},
				},
				Event: &ttpb.TokenTransferEvent_Transfer{Transfer: &ttpb.Transfer{
					From: "GFROM", To: "GTO", AssetCode: "XLM", Amount: "1", AssetType: "native",
				}},
			},
			want: map[string]any{"to_muxed_info": map[string]any{"text": "DREXOVYN"}},
		},
		{
			// The id is a uint64 and must cross as a string, matching what
			// protojson emits for the same field. 775078096 is a real value
			// from pubnet ledger 65447235.
			name: "muxed id is a string",
			ev: &ttpb.TokenTransferEvent{
				Meta: &ttpb.EventMeta{
					TxHash: "mno", InSuccessfulTx: true,
					ToMuxedInfo: &ttpb.MuxedInfo{Content: &ttpb.MuxedInfo_Id{Id: 775078096}},
				},
				Event: &ttpb.TokenTransferEvent_Transfer{Transfer: &ttpb.Transfer{
					From: "GFROM", To: "GTO", AssetCode: "XLM", Amount: "1", AssetType: "native",
				}},
			},
			want: map[string]any{"to_muxed_info": map[string]any{"id": "775078096"}},
		},
		{
			// The case a bare number loses: JavaScript's JSON.parse reads
			// this back as 18446744073709552000.
			name: "the largest muxed id survives",
			ev: &ttpb.TokenTransferEvent{
				Meta: &ttpb.EventMeta{
					TxHash: "pqr", InSuccessfulTx: true,
					ToMuxedInfo: &ttpb.MuxedInfo{Content: &ttpb.MuxedInfo_Id{Id: math.MaxUint64}},
				},
				Event: &ttpb.TokenTransferEvent_Transfer{Transfer: &ttpb.Transfer{
					From: "GFROM", To: "GTO", AssetCode: "XLM", Amount: "1", AssetType: "native",
				}},
			},
			want: map[string]any{
				"to_muxed_info": map[string]any{"id": "18446744073709551615"},
			},
		},
		{
			// Bytes render as base64, which is what both encoding/json and
			// proto3 JSON do.
			name: "muxed hash is base64",
			ev: &ttpb.TokenTransferEvent{
				Meta: &ttpb.EventMeta{
					TxHash: "stu", InSuccessfulTx: true,
					ToMuxedInfo: &ttpb.MuxedInfo{Content: &ttpb.MuxedInfo_Hash{
						Hash: []byte{0xde, 0xad, 0xbe, 0xef},
					}},
				},
				Event: &ttpb.TokenTransferEvent_Transfer{Transfer: &ttpb.Transfer{
					From: "GFROM", To: "GTO", AssetCode: "XLM", Amount: "1", AssetType: "native",
				}},
			},
			want: map[string]any{"to_muxed_info": map[string]any{"hash": "3q2+7w=="}},
		},
		{
			name: "mint carries the asset type and no from",
			ev: &ttpb.TokenTransferEvent{
				Meta: &ttpb.EventMeta{TxHash: "vwx", InSuccessfulTx: true},
				Event: &ttpb.TokenTransferEvent_Mint{Mint: &ttpb.Mint{
					To: "GTO", AssetCode: "USDC", AssetIssuer: "GISSUER",
					Amount: "5", AssetType: "issued",
				}},
			},
			want: map[string]any{
				"type": "mint", "to": "GTO", "amount": "5",
				"asset": map[string]any{"type": "issued", "code": "USDC", "issuer": "GISSUER"},
			},
			absent: []string{"from"},
		},
		{
			name: "burn carries the asset type and no to",
			ev: &ttpb.TokenTransferEvent{
				Meta: &ttpb.EventMeta{TxHash: "yz", InSuccessfulTx: true},
				Event: &ttpb.TokenTransferEvent_Burn{Burn: &ttpb.Burn{
					From: "GFROM", AssetCode: "USDC", AssetIssuer: "GISSUER",
					Amount: "6", AssetType: "issued",
				}},
			},
			want:   map[string]any{"type": "burn", "from": "GFROM", "amount": "6"},
			absent: []string{"to"},
		},
		{
			name: "clawback carries the asset type",
			ev: &ttpb.TokenTransferEvent{
				Meta: &ttpb.EventMeta{TxHash: "abc2", InSuccessfulTx: true},
				Event: &ttpb.TokenTransferEvent_Clawback{Clawback: &ttpb.Clawback{
					From: "GFROM", AssetCode: "USDC", AssetIssuer: "GISSUER",
					Amount: "7", AssetType: "issued",
				}},
			},
			want: map[string]any{
				"type": "clawback", "from": "GFROM", "amount": "7",
				"asset": map[string]any{"type": "issued", "code": "USDC", "issuer": "GISSUER"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(simplifyEvent(tt.ev))
			if err != nil {
				t.Fatalf("marshalling the simplified event: %v", err)
			}

			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("the rendered event is not JSON: %v\n%s", err, raw)
			}

			for field, want := range tt.want {
				have, ok := got[field]
				if !ok {
					t.Errorf("%q is missing\n  got: %s", field, raw)
					continue
				}
				if !equalJSON(have, want) {
					t.Errorf("%q = %#v, want %#v\n  got: %s", field, have, want, raw)
				}
			}
			for _, field := range tt.absent {
				if _, ok := got[field]; ok {
					t.Errorf("%q should be absent, got %#v\n  got: %s", field, got[field], raw)
				}
			}
		})
	}
}

// equalJSON compares values decoded from JSON, descending into objects.
func equalJSON(have, want any) bool {
	wantMap, ok := want.(map[string]any)
	if !ok {
		return have == want
	}
	haveMap, ok := have.(map[string]any)
	if !ok || len(haveMap) != len(wantMap) {
		return false
	}
	for key, wantValue := range wantMap {
		if !equalJSON(haveMap[key], wantValue) {
			return false
		}
	}
	return true
}
