package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	schemaID = "nebu.soroswap_pool.v1"
	version  = "1.0.0"
)

var contractIDRE = regexp.MustCompile(`^C[A-Z2-7]{55}$`)

var knownFactories = map[string][]string{
	"mainnet": {"CA4HEQTL2WPEUYKYKCDOHCDNIV4QHNJ7EL4J4NQ6VADP7SYHVRYZ7AW2"},
	"pubnet":  {"CA4HEQTL2WPEUYKYKCDOHCDNIV4QHNJ7EL4J4NQ6VADP7SYHVRYZ7AW2"},
	"testnet": {"CDP3HMUH6SMS3S7NPGNDJLULCOXXEPSHY4JKUKMBNQMATHDHWXRRJTBY"},
}

var defaultEventNames = []string{"new_pair", "pair_created", "create_pair"}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	v = strings.TrimSpace(v)
	if v != "" {
		*s = append(*s, v)
	}
	return nil
}

type config struct {
	Network      string
	Factories    []string
	EventNames   []string
	IncludeRaw   bool
	Strict       bool
	StatsEnabled bool
	Verbose      bool
	Quiet        bool
}

type stats struct {
	Read             int
	ParseError       int
	MatchedFactory   int
	MatchedEventName int
	Emitted          int
	Skipped          int
	DecodeError      int
}

type poolRecord struct {
	Schema            string         `json:"_schema"`
	NebuVersion       string         `json:"_nebu_version"`
	Network           string         `json:"network,omitempty"`
	Protocol          string         `json:"protocol"`
	FactoryContractID string         `json:"factory_contract_id"`
	PoolContractID    string         `json:"pool_contract_id"`
	TokenAContractID  string         `json:"token_a_contract_id"`
	TokenBContractID  string         `json:"token_b_contract_id"`
	TokenPairKey      string         `json:"token_pair_key"`
	LedgerSequence    any            `json:"ledger_sequence,omitempty"`
	LedgerClosedAt    string         `json:"ledger_closed_at,omitempty"`
	TransactionHash   string         `json:"transaction_hash,omitempty"`
	OperationIndex    any            `json:"operation_index,omitempty"`
	EventIndex        any            `json:"event_index,omitempty"`
	FactoryEventName  string         `json:"factory_event_name"`
	SourceContractID  string         `json:"source_contract_id"`
	DiscoveryMethod   string         `json:"discovery_method"`
	RawEvent          map[string]any `json:"raw_event,omitempty"`
}

type decodedPool struct {
	TokenA string
	TokenB string
	Pool   string
}

func main() {
	cfg, describe, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if describe {
		printDescribe()
		return
	}
	st, err := process(os.Stdin, os.Stdout, os.Stderr, cfg)
	if cfg.StatsEnabled {
		fmt.Fprintf(os.Stderr, "soroswap-pool-transform stats: read=%d parse_error=%d matched_factory=%d matched_event_name=%d emitted=%d skipped=%d decode_error=%d\n", st.Read, st.ParseError, st.MatchedFactory, st.MatchedEventName, st.Emitted, st.Skipped, st.DecodeError)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func parseFlags(args []string) (config, bool, error) {
	var factories, eventNames stringList
	cfg := config{IncludeRaw: true}
	fs := flag.NewFlagSet("soroswap-pool-transform", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.Network, "network", "", "network name (pubnet, mainnet, testnet, futurenet, sandbox)")
	fs.Var(&factories, "factory", "Soroswap factory contract ID allowlist (repeatable)")
	fs.Var(&eventNames, "event-name", "accepted pool creation event symbol (repeatable)")
	fs.BoolVar(&cfg.IncludeRaw, "include-raw", true, "include full raw event evidence")
	omitRaw := fs.Bool("omit-raw", false, "omit raw_event from output")
	fs.BoolVar(&cfg.Strict, "strict", false, "exit non-zero on malformed JSON or undecodable matching events")
	fs.BoolVar(&cfg.StatsEnabled, "stats", false, "print summary counts to stderr")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "print per-error diagnostics to stderr")
	fs.BoolVar(&cfg.Quiet, "quiet", false, "suppress non-error diagnostics")
	fs.BoolVar(&cfg.Quiet, "q", false, "suppress non-error diagnostics")
	describe := fs.Bool("describe-json", false, "print registry/schema description JSON and exit")
	if err := fs.Parse(args); err != nil {
		return cfg, false, err
	}
	if *describe {
		return cfg, true, nil
	}
	if *omitRaw {
		cfg.IncludeRaw = false
	}
	cfg.Network = normalizeNetwork(cfg.Network)
	cfg.Factories = factories
	if len(cfg.Factories) == 0 && cfg.Network != "" {
		cfg.Factories = append(cfg.Factories, knownFactories[cfg.Network]...)
	}
	for _, f := range cfg.Factories {
		if !isContractID(f) {
			return cfg, false, fmt.Errorf("invalid --factory contract id: %s", f)
		}
	}
	cfg.EventNames = eventNames
	if len(cfg.EventNames) == 0 {
		cfg.EventNames = defaultEventNames
	}
	if cfg.Strict && cfg.Network != "" && len(cfg.Factories) == 0 {
		return cfg, false, fmt.Errorf("no known Soroswap factory for network %q; pass --factory", cfg.Network)
	}
	return cfg, *describe, nil
}

func process(r io.Reader, w io.Writer, errw io.Writer, cfg config) (stats, error) {
	var st stats
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(w)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		st.Read++
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			st.ParseError++
			st.Skipped++
			if cfg.Verbose {
				fmt.Fprintf(errw, "line %d: parse error: %v\n", st.Read, err)
			}
			if cfg.Strict {
				return st, fmt.Errorf("line %d: parse error: %w", st.Read, err)
			}
			continue
		}
		if !contractApplicationEvent(row) {
			st.Skipped++
			continue
		}
		sourceContract := firstString(row, "contract_id", "contract", "source_contract_id")
		if !factoryAllowed(sourceContract, cfg.Factories) {
			st.Skipped++
			continue
		}
		st.MatchedFactory++
		eventName := eventName(row)
		if !acceptedEventName(eventName, cfg.EventNames) {
			st.Skipped++
			continue
		}
		st.MatchedEventName++
		pool, err := decodePool(row)
		if err != nil {
			st.DecodeError++
			st.Skipped++
			if cfg.Verbose {
				fmt.Fprintf(errw, "line %d: decode error: %v\n", st.Read, err)
			}
			if cfg.Strict {
				return st, fmt.Errorf("line %d: decode error: %w", st.Read, err)
			}
			continue
		}
		network := normalizeNetwork(firstString(row, "network", "network_passphrase"))
		if network == "" {
			network = cfg.Network
		}
		rec := poolRecord{
			Schema: schemaID, NebuVersion: version, Network: network, Protocol: "soroswap",
			FactoryContractID: sourceContract, PoolContractID: pool.Pool,
			TokenAContractID: pool.TokenA, TokenBContractID: pool.TokenB,
			TokenPairKey:   pairKey(pool.TokenA, pool.TokenB),
			LedgerSequence: firstAny(row, "ledger_sequence", "ledger"), LedgerClosedAt: ledgerClosedAt(row),
			TransactionHash: firstString(row, "transaction_hash", "tx_hash"), OperationIndex: firstAny(row, "operation_index"), EventIndex: firstAny(row, "event_index"),
			FactoryEventName: eventName, SourceContractID: sourceContract, DiscoveryMethod: "contract-events",
		}
		if cfg.IncludeRaw {
			rec.RawEvent = row
		}
		if err := enc.Encode(rec); err != nil {
			return st, err
		}
		st.Emitted++
	}
	if err := scanner.Err(); err != nil {
		return st, err
	}
	return st, nil
}

func contractApplicationEvent(row map[string]any) bool {
	if typ := firstString(row, "type"); typ != "" {
		return typ == "contract"
	}
	return true
}

func factoryAllowed(contract string, factories []string) bool {
	if contract == "" {
		return false
	}
	if len(factories) == 0 {
		return true
	}
	for _, f := range factories {
		if contract == f {
			return true
		}
	}
	return false
}

func eventName(row map[string]any) string {
	if s := firstString(row, "event_type"); s != "" {
		return s
	}
	for _, key := range []string{"topic_decoded", "topics"} {
		if a, ok := row[key].([]any); ok && len(a) > 0 {
			if s := scalarSymbol(a[0]); s != "" {
				return s
			}
		}
	}
	return ""
}

func acceptedEventName(name string, accepted []string) bool {
	for _, a := range accepted {
		if name == a {
			return true
		}
	}
	return false
}

func decodePool(row map[string]any) (decodedPool, error) {
	if p, ok := decodeObject(row); ok {
		return p, nil
	}
	for _, key := range []string{"data_decoded", "data"} {
		if p, ok := decodeObjectValue(row[key]); ok {
			return p, nil
		}
	}
	for _, key := range []string{"data_decoded", "data"} {
		ids := collectContractIDs(row[key])
		if len(ids) >= 3 {
			return decodedPool{ids[0], ids[1], ids[2]}, nil
		}
	}
	ids := append(collectContractIDs(row["topic_decoded"]), collectContractIDs(row["topics"])...)
	ids = append(ids, collectContractIDs(row["data_decoded"])...)
	ids = append(ids, collectContractIDs(row["data"])...)
	ids = uniqueContracts(ids)
	if len(ids) >= 3 {
		return decodedPool{ids[0], ids[1], ids[2]}, nil
	}
	return decodedPool{}, errors.New("could not find token_a, token_b, and pool contract ids")
}

func decodeObjectValue(v any) (decodedPool, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return decodedPool{}, false
	}
	return decodeObject(m)
}

func decodeObject(m map[string]any) (decodedPool, bool) {
	var p decodedPool
	for k, v := range m {
		n := normKey(k)
		s := firstContractID(v)
		if s == "" {
			continue
		}
		switch n {
		case "tokena", "token0":
			p.TokenA = s
		case "tokenb", "token1":
			p.TokenB = s
		case "pair", "pool", "pairaddress", "pooladdress", "paircontractid", "poolcontractid":
			p.Pool = s
		}
	}
	return p, isContractID(p.TokenA) && isContractID(p.TokenB) && isContractID(p.Pool)
}

func firstContractID(v any) string {
	ids := collectContractIDs(v)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func collectContractIDs(v any) []string {
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			if isContractID(t) {
				out = append(out, t)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for _, key := range []string{"contract_id", "contractId", "address", "value", "strkey", "symbol", "sym"} {
				if s, ok := t[key].(string); ok && isContractID(s) {
					out = append(out, s)
					return
				}
			}
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

func uniqueContracts(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func scalarSymbol(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		for _, k := range []string{"symbol", "sym", "value"} {
			if s, ok := t[k].(string); ok {
				return s
			}
		}
	}
	return ""
}

func isContractID(s string) bool { return contractIDRE.MatchString(s) }
func normKey(s string) string {
	return strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(s))
}

func normalizeNetwork(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "mainnet", "public", "pubnet", "public global stellar network ; september 2015":
		return "pubnet"
	case "test", "testnet", "test sdf network ; september 2015":
		return "testnet"
	case "future", "futurenet", "test sdf future network ; october 2022":
		return "futurenet"
	}
	return s
}

func pairKey(a, b string) string { x := []string{a, b}; sort.Strings(x); return x[0] + ":" + x[1] }

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func firstAny(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func ledgerClosedAt(m map[string]any) string {
	if s := firstString(m, "ledger_closed_at", "closed_at"); s != "" {
		return s
	}
	if v, ok := m["timestamp"]; ok {
		switch t := v.(type) {
		case float64:
			return time.Unix(int64(t), 0).UTC().Format(time.RFC3339)
		case int64:
			return time.Unix(t, 0).UTC().Format(time.RFC3339)
		}
	}
	return ""
}

type describeEnvelope struct {
	Name        string           `json:"name"`
	Type        string           `json:"type"`
	Version     string           `json:"version"`
	Description string           `json:"description"`
	Schema      describeSchema   `json:"schema"`
	Flags       []describeFlag   `json:"flags"`
	Examples    []map[string]any `json:"examples"`
}

type describeSchema struct {
	ID string `json:"id"`
}

type describeFlag struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Default     string `json:"default"`
}

func printDescribe() {
	desc := describeEnvelope{
		Name:        "soroswap-pool-transform",
		Type:        "transform",
		Version:     version,
		Description: "Transform contract-events JSONL into normalized Soroswap pool discovery records without querying RPC.",
		Schema:      describeSchema{ID: schemaID},
		Flags: []describeFlag{
			{Name: "network", Type: "string", Description: "network name (pubnet, mainnet, testnet, futurenet, sandbox)", Default: ""},
			{Name: "factory", Type: "stringArray", Description: "Soroswap factory contract ID allowlist (repeatable)", Default: ""},
			{Name: "event-name", Type: "stringArray", Description: "accepted pool creation event symbol (repeatable)", Default: strings.Join(defaultEventNames, ",")},
			{Name: "include-raw", Type: "bool", Description: "include full raw event evidence", Default: "true"},
			{Name: "omit-raw", Type: "bool", Description: "omit raw_event from output", Default: "false"},
			{Name: "strict", Type: "bool", Description: "exit non-zero on malformed JSON or undecodable matching events", Default: "false"},
			{Name: "stats", Type: "bool", Description: "print summary counts to stderr", Default: "false"},
			{Name: "verbose", Type: "bool", Description: "print per-error diagnostics to stderr", Default: "false"},
			{Name: "quiet", Type: "bool", Description: "suppress non-error diagnostics", Default: "false"},
		},
		Examples: []map[string]any{
			{"description": "Historical archive backfill", "command": "nebu fetch --network pubnet --mode archive --start-ledger 50000000 --end-ledger 51000000 | contract-events | soroswap-pool-transform --network pubnet"},
		},
	}
	b, _ := json.MarshalIndent(desc, "", "  ")
	fmt.Println(string(b))
}
