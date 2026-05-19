package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
)

const (
	factory = "CDP3HMUH6SMS3S7NPGNDJLULCOXXEPSHY4JKUKMBNQMATHDHWXRRJTBY"
	tokenA  = "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	tokenB  = "CBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	pool    = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
	other   = "CDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"
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
	if rec["_schema"] != schemaID || rec["_nebu_version"] != version || rec["pool_contract_id"] != pool || rec["token_a_contract_id"] != tokenB || rec["token_b_contract_id"] != tokenA {
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

func TestNetworkPassphraseAlias(t *testing.T) {
	input := `{"network_passphrase":"Test SDF Network ; September 2015","contract_id":"` + factory + `","type":"contract","event_type":"pair_created","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}` + "\n"
	out, _, st, err := runTransform(t, input, config{Factories: []string{factory}, EventNames: defaultEventNames, IncludeRaw: false})
	if err != nil {
		t.Fatal(err)
	}
	if st.Emitted != 1 {
		t.Fatalf("emitted = %d", st.Emitted)
	}
	var rec map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &rec)
	if rec["network"] != "testnet" {
		t.Fatalf("network = %v", rec["network"])
	}
}

func TestNormalizeNetworkPassphrases(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Public Global Stellar Network ; September 2015", "pubnet"},
		{"Test SDF Network ; September 2015", "testnet"},
		{"Test SDF Future Network ; October 2022", "futurenet"},
		{"mainnet", "pubnet"},
		{"pubnet", "pubnet"},
		{"testnet", "testnet"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeNetwork(tc.in); got != tc.want {
			t.Errorf("normalizeNetwork(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBareSymbolInTopicDecoded(t *testing.T) {
	input := `{"contract_id":"` + factory + `","type":"contract","topic_decoded":["new_pair","` + tokenA + `","` + tokenB + `"],"data_decoded":["` + pool + `"]}` + "\n"
	_, _, st, err := runTransform(t, input, config{Factories: []string{factory}, EventNames: defaultEventNames})
	if err != nil {
		t.Fatal(err)
	}
	if st.Emitted != 1 {
		t.Fatalf("bare-string symbol in topic_decoded[0] should match: %+v", st)
	}
}

func TestMultiLineStreamMixedEmissions(t *testing.T) {
	rec1 := `{"contract_id":"` + factory + `","type":"contract","event_type":"pair_created","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}`
	skip := `{"contract_id":"` + factory + `","type":"contract","event_type":"transfer","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}`
	rec2 := `{"contract_id":"` + factory + `","type":"contract","event_type":"new_pair","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}`
	out, _, st, err := runTransform(t, rec1+"\n"+skip+"\n"+rec2+"\n", config{Factories: []string{factory}, EventNames: defaultEventNames})
	if err != nil {
		t.Fatal(err)
	}
	if st.Read != 3 || st.Emitted != 2 || st.Skipped != 1 {
		t.Fatalf("expected read=3 emitted=2 skipped=1, got %+v", st)
	}
	if got := strings.Count(strings.TrimSpace(out), "\n") + 1; got != 2 {
		t.Fatalf("expected 2 output lines, got %d", got)
	}
}

func TestDuplicatePoolEmittedTwice(t *testing.T) {
	row := `{"contract_id":"` + factory + `","type":"contract","event_type":"pair_created","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}` + "\n"
	_, _, st, err := runTransform(t, row+row, config{Factories: []string{factory}, EventNames: defaultEventNames})
	if err != nil {
		t.Fatal(err)
	}
	if st.Emitted != 2 {
		t.Fatalf("transform does not dedupe; expected 2 emissions, got %d", st.Emitted)
	}
}

func TestMissingEventNameCounter(t *testing.T) {
	input := `{"contract_id":"` + factory + `","type":"contract","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}` + "\n"
	_, _, st, err := runTransform(t, input, config{Factories: []string{factory}, EventNames: defaultEventNames})
	if err != nil {
		t.Fatal(err)
	}
	if st.MissingEventName != 1 || st.Emitted != 0 {
		t.Fatalf("expected MissingEventName=1 Emitted=0, got %+v", st)
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

func TestQuietSuppressesVerboseDiagnostics(t *testing.T) {
	_, errOut, _, _ := runTransform(t, "{bad\n", config{Verbose: true})
	if !strings.Contains(errOut, "parse error") {
		t.Fatalf("verbose should emit parse error; got %q", errOut)
	}
	_, errOut, _, _ = runTransform(t, "{bad\n", config{Verbose: true, Quiet: true})
	if errOut != "" {
		t.Fatalf("quiet should suppress verbose diagnostics; got %q", errOut)
	}
}

func TestParseFlagsRequiresFactory(t *testing.T) {
	if _, _, err := parseFlags(nil); err == nil {
		t.Fatal("expected error when no network and no factory")
	}
	if _, _, err := parseFlags([]string{"--network", "sandbox"}); err == nil {
		t.Fatal("expected error when network has no known factory and none provided")
	}
	cfg, _, err := parseFlags([]string{"--network", "testnet"})
	if err != nil {
		t.Fatalf("known network should resolve factory: %v", err)
	}
	if len(cfg.Factories) == 0 {
		t.Fatal("testnet should auto-populate factory allowlist")
	}
	if _, _, err := parseFlags([]string{"--factory", factory}); err != nil {
		t.Fatalf("explicit factory should be accepted: %v", err)
	}
}

func TestFactoryAllowlistRejectsUnknownContract(t *testing.T) {
	input := `{"contract_id":"` + other + `","type":"contract","event_type":"pair_created","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}` + "\n"
	out, _, st, err := runTransform(t, input, config{Factories: []string{factory}, EventNames: defaultEventNames})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" || st.Emitted != 0 {
		t.Fatalf("non-factory contract must not emit: out=%q stats=%+v", out, st)
	}
	_, _, st, err = runTransform(t, input, config{EventNames: defaultEventNames})
	if err != nil {
		t.Fatal(err)
	}
	if st.Emitted != 0 {
		t.Fatalf("empty allowlist must not emit: %+v", st)
	}
}

func TestParseFlagsInvalidFactory(t *testing.T) {
	if _, _, err := parseFlags([]string{"--factory", "not-a-contract-id"}); err == nil {
		t.Fatal("expected error for invalid factory ID")
	}
}

func TestParseFlagsOmitRawOverridesIncludeRaw(t *testing.T) {
	cfg, _, err := parseFlags([]string{"--factory", factory, "--include-raw", "--omit-raw"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IncludeRaw {
		t.Fatal("--omit-raw must win over --include-raw")
	}
}

func TestPrintDescribeIncludesAllFlags(t *testing.T) {
	var buf bytes.Buffer
	if err := printDescribe(&buf); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Name   string
		Type   string
		Schema struct{ ID string }
		Flags  []struct {
			Name        string
			Description string
		}
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("describe output is not valid JSON: %v", err)
	}
	if env.Name != "soroswap-pool-transform" || env.Type != "transform" || env.Schema.ID != schemaID {
		t.Fatalf("envelope basics wrong: %+v", env)
	}
	wantFlags := []string{"network", "factory", "event-name", "include-raw", "omit-raw", "strict", "stats", "verbose", "quiet"}
	got := make(map[string]bool, len(env.Flags))
	for _, f := range env.Flags {
		got[f.Name] = true
	}
	for _, w := range wantFlags {
		if !got[w] {
			t.Errorf("describe envelope missing flag %q", w)
		}
	}
}

func TestEncodeBrokenPipeIsCleanExit(t *testing.T) {
	input := `{"network":"testnet","contract_id":"` + factory + `","type":"contract","event_type":"pair_created","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}` + "\n"
	closedWriter := &errWriter{err: io.ErrClosedPipe}
	st, err := process(strings.NewReader(input), closedWriter, io.Discard, config{Factories: []string{factory}, EventNames: defaultEventNames})
	if err != nil {
		t.Fatalf("EPIPE should produce a clean exit, got %v", err)
	}
	if st.Emitted != 0 {
		t.Fatalf("Emitted should not increment on encode failure, got %d", st.Emitted)
	}
}

type errWriter struct{ err error }

func (w *errWriter) Write(_ []byte) (int, error) { return 0, w.err }

func TestHelpFlagReturnsHelpRequested(t *testing.T) {
	_, _, err := parseFlags([]string{"--help"})
	var help *helpRequested
	if !errors.As(err, &help) {
		t.Fatalf("--help should produce helpRequested, got %v", err)
	}
	if !strings.Contains(help.usage, "factory") {
		t.Fatalf("help usage missing flag list: %q", help.usage)
	}
	if errors.Is(err, flag.ErrHelp) && !errors.As(err, &help) {
		t.Fatal("flag.ErrHelp should be wrapped as helpRequested, not bare")
	}
}

func TestFirstInt64HandlesStringNumeric(t *testing.T) {
	got := firstInt64(map[string]any{"ledger_sequence": "12345"}, "ledger_sequence")
	if got == nil || *got != 12345 {
		t.Fatalf("string-encoded int should parse, got %v", got)
	}
	if firstInt64(map[string]any{"x": "not-a-number"}, "x") != nil {
		t.Fatal("non-numeric string should return nil")
	}
	if firstInt64(map[string]any{"x": true}, "x") != nil {
		t.Fatal("unsupported type should return nil")
	}
}

func TestCamelCaseUpstreamFieldsAccepted(t *testing.T) {
	input := `{"networkPassphrase":"Test SDF Network ; September 2015","ledgerSequence":2606504,"transactionHash":"42a7","contractId":"` + factory + `","type":"CONTRACT","eventType":"pair_created","topicDecoded":[{"symbolValue":"pair_created"}],"dataDecoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"},"operationIndex":0,"eventIndex":3}` + "\n"
	out, _, st, err := runTransform(t, input, config{Factories: []string{factory}, EventNames: defaultEventNames, IncludeRaw: false})
	if err != nil {
		t.Fatal(err)
	}
	if st.Emitted != 1 {
		t.Fatalf("camelCase upstream row should emit, got %+v", st)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["network"] != "testnet" {
		t.Fatalf("networkPassphrase alias failed: %v", rec["network"])
	}
	if rec["transaction_hash"] != "42a7" {
		t.Fatalf("transactionHash alias failed: %v", rec["transaction_hash"])
	}
	if v, _ := rec["ledger_sequence"].(float64); int64(v) != 2606504 {
		t.Fatalf("ledgerSequence alias failed: %v", rec["ledger_sequence"])
	}
	if v, _ := rec["event_index"].(float64); int64(v) != 3 {
		t.Fatalf("eventIndex alias failed: %v", rec["event_index"])
	}
}

func TestFactoryNeverAppearsInTokenSlot(t *testing.T) {
	input := `{"contract_id":"` + factory + `","type":"contract","event_type":"pair_created","data_decoded":{"token_a":"` + factory + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}` + "\n"
	_, _, st, err := runTransform(t, input, config{Factories: []string{factory}, EventNames: defaultEventNames})
	if err != nil {
		t.Fatal(err)
	}
	if st.Emitted != 0 || st.DecodeError != 1 {
		t.Fatalf("factory landing in token slot should fail decode: %+v", st)
	}
}

func TestRejectedEventNameCounter(t *testing.T) {
	input := `{"contract_id":"` + factory + `","type":"contract","event_type":"transfer","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}` + "\n"
	_, _, st, err := runTransform(t, input, config{Factories: []string{factory}, EventNames: defaultEventNames})
	if err != nil {
		t.Fatal(err)
	}
	if st.RejectedEventName != 1 || st.MissingEventName != 0 || st.Emitted != 0 {
		t.Fatalf("wrong-name event should increment RejectedEventName only: %+v", st)
	}
}

func TestOptionalNumericFieldsOmittedWhenAbsent(t *testing.T) {
	input := `{"contract_id":"` + factory + `","type":"contract","event_type":"pair_created","data_decoded":{"token_a":"` + tokenA + `","token_b":"` + tokenB + `","pair":"` + pool + `"}}` + "\n"
	out, _, _, err := runTransform(t, input, config{Factories: []string{factory}, EventNames: defaultEventNames, IncludeRaw: false})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ledger_sequence", "operation_index", "event_index"} {
		if strings.Contains(out, field) {
			t.Errorf("%s should be omitted when absent from input; output: %s", field, out)
		}
	}
}

func TestFallbackDecodeIsDeterministic(t *testing.T) {
	input := `{"contract_id":"` + factory + `","type":"contract","event_type":"new_pair","data_decoded":{"alpha":"` + tokenA + `","beta":"` + tokenB + `","gamma":"` + pool + `"}}` + "\n"
	cfg := config{Factories: []string{factory}, EventNames: defaultEventNames}
	first, _, _, err := runTransform(t, input, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("fallback path should emit a record")
	}
	for i := 0; i < 50; i++ {
		got, _, _, err := runTransform(t, input, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Fatalf("non-deterministic output on iteration %d:\nfirst=%s\ngot  =%s", i, first, got)
		}
	}
}
