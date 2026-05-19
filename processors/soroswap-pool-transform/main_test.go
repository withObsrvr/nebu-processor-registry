package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const (
	factory = "CDP3HMUH6SMS3S7NPGNDJLULCOXXEPSHY4JKUKMBNQMATHDHWXRRJTBY"
	tokenA  = "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	tokenB  = "CBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	pool    = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
	other   = "CDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"
)

func runTransform(t *testing.T, input string, cfg config) (string, string, stats, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	st, err := process(strings.NewReader(input), &out, &errOut, cfg)
	return out.String(), errOut.String(), st, err
}

func TestDecodedObjectEvent(t *testing.T) {
	input := `{"network":"testnet","ledger_sequence":2606504,"timestamp":1779041698,"transaction_hash":"42a7","contract_id":"` + factory + `","type":"contract","event_type":"pair_created","topic_decoded":["pair_created"],"data_decoded":{"token_a":"` + tokenB + `","token_b":"` + tokenA + `","pair":"` + pool + `"},"operation_index":0,"event_index":3}` + "\n"
	out, _, st, err := runTransform(t, input, config{Network: "testnet", Factories: []string{factory}, EventNames: defaultEventNames, IncludeRaw: true})
	if err != nil {
		t.Fatal(err)
	}
	if st.Emitted != 1 {
		t.Fatalf("emitted = %d", st.Emitted)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["schema"] != schemaID || rec["pool_contract_id"] != pool || rec["token_a_contract_id"] != tokenB || rec["token_b_contract_id"] != tokenA {
		t.Fatalf("unexpected record: %#v", rec)
	}
	wantKey := tokenA + ":" + tokenB
	if rec["token_pair_key"] != wantKey {
		t.Fatalf("token_pair_key = %v, want %s", rec["token_pair_key"], wantKey)
	}
	if rec["raw_event"] == nil {
		t.Fatal("raw_event missing")
	}
	if rec["ledger_closed_at"] != "2026-05-17T18:14:58Z" {
		t.Fatalf("ledger_closed_at = %v", rec["ledger_closed_at"])
	}
}

func TestArrayAndTopicMixedDecode(t *testing.T) {
	input := `{"contract_id":"` + factory + `","type":"contract","topic_decoded":[{"symbol":"new_pair"},"` + tokenA + `","` + tokenB + `"],"data_decoded":["` + pool + `"],"tx_hash":"abc"}` + "\n"
	out, _, st, err := runTransform(t, input, config{Network: "testnet", Factories: []string{factory}, EventNames: []string{"new_pair"}, IncludeRaw: false})
	if err != nil {
		t.Fatal(err)
	}
	if st.Emitted != 1 {
		t.Fatalf("emitted = %d output=%s", st.Emitted, out)
	}
	var rec map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &rec)
	if rec["raw_event"] != nil {
		t.Fatal("raw_event should be omitted")
	}
	if rec["transaction_hash"] != "abc" {
		t.Fatalf("transaction hash alias not used: %#v", rec)
	}
}

func TestNonMatchingEventsSkipped(t *testing.T) {
	input := `{"contract_id":"` + other + `","type":"contract","event_type":"pair_created","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}` + "\n" +
		`{"contract_id":"` + factory + `","type":"contract","event_type":"transfer","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}` + "\n"
	out, _, st, err := runTransform(t, input, config{Factories: []string{factory}, EventNames: defaultEventNames})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" || st.Emitted != 0 || st.Skipped != 2 {
		t.Fatalf("out=%q stats=%+v", out, st)
	}
}

func TestMalformedAndStrictDecode(t *testing.T) {
	_, _, st, err := runTransform(t, "{bad\n", config{Strict: false})
	if err != nil || st.ParseError != 1 {
		t.Fatalf("default parse handling err=%v stats=%+v", err, st)
	}
	_, _, _, err = runTransform(t, "{bad\n", config{Strict: true})
	if err == nil {
		t.Fatal("expected strict parse error")
	}

	input := `{"contract_id":"` + factory + `","type":"contract","event_type":"pair_created","data_decoded":{"note":"missing ids"}}` + "\n"
	_, _, _, err = runTransform(t, input, config{Factories: []string{factory}, EventNames: defaultEventNames, Strict: true})
	if err == nil {
		t.Fatal("expected strict decode error")
	}
}
