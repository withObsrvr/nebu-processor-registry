// Package main provides a standalone CLI for the soroswap-pool-indexer.
//
// This processor queries the Soroban RPC getEvents endpoint for Soroswap factory
// "new_pair" events and emits pool addresses with their token pairs.
//
// Usage:
//
//	# Build full pool list (uses getEvents RPC, not getLedgers)
//	soroswap-pool-indexer --start-ledger 50759000 --network mainnet | \
//	  jq -r .pair_address > soroswap-pools-mainnet.txt
//
//	# With auth
//	NEBU_RPC_AUTH="Api-Key xxx" soroswap-pool-indexer \
//	  --rpc-url "https://rpc-pubnet.nodeswithobsrvr.co" \
//	  --start-ledger 50759000 --network mainnet
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/stellar/go-stellar-sdk/clients/rpcclient"
	"github.com/stellar/go-stellar-sdk/network"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/withObsrvr/nebu/pkg/processor"
	"github.com/withObsrvr/nebu/pkg/source"
	nebuRPC "github.com/withObsrvr/nebu/pkg/source/rpc"
)

var version = "0.2.0"

var (
	rpcURL            string
	startLedger       uint32
	endLedger         uint32
	networkPass       string
	quietMode         bool
	programStatusMode string
	factoryContract   string
	pageLimit         uint
	lastLedgers       uint32
)

// Known Soroswap factory addresses by network shorthand
var knownFactories = map[string]string{
	"mainnet": "CA4HEQTL2WPEUYKYKCDOHCDNIV4QHNJ7EL4J4NQ6VADP7SYHVRYZ7AW2",
	"testnet": "CDP3HMUH6SMS3S7NPGNDJLULCOXXEPSHY4JKUKMBNQMATHDHWXRRJTBY",
}

func main() {
	rootCmd := &cobra.Command{
		Use:     "soroswap-pool-indexer",
		Short:   "Index Soroswap pool creation events from the factory contract via getEvents RPC",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithProgramStatus("soroswap-pool-indexer", programStatusMode, quietMode, run)
		},
	}

	rootCmd.Flags().StringVar(&rpcURL, "rpc-url", "https://archive-rpc.lightsail.network", "Stellar Soroban RPC endpoint")
	rootCmd.Flags().Uint32Var(&startLedger, "start-ledger", 0, "Start ledger sequence")
	rootCmd.Flags().Uint32Var(&endLedger, "end-ledger", 0, "End ledger sequence (0 = use RPC latest)")
	rootCmd.Flags().StringVar(&networkPass, "network", "mainnet", "Network (mainnet|testnet) or full passphrase")
	rootCmd.Flags().BoolVarP(&quietMode, "quiet", "q", false, "Suppress non-error output")
	rootCmd.Flags().StringVar(&programStatusMode, "program-status", programStatusDefault(), "Program status reporting: auto, always, or never")
	rootCmd.Flags().StringVar(&factoryContract, "factory", "", "Soroswap factory contract address (auto-detected from --network)")
	rootCmd.Flags().UintVar(&pageLimit, "page-limit", 100, "Events per RPC page request")
	rootCmd.Flags().Uint32Var(&lastLedgers, "last", 0, "Index the most recent N ledgers instead of an explicit range (or set NEBU_LAST)")
	rootCmd.Flags().Bool(describeFlagName, false, "Emit machine-readable describe envelope to stdout and exit")

	// Short-circuit into the describe-json protocol before cobra
	// validates required flags — --describe-json must work without
	// --start-ledger or any other mandatory flag set.
	emitDescribeIfRequested(rootCmd, func() processor.DescribeEnvelope {
		return processor.DescribeEnvelope{
			Name:        "soroswap-pool-indexer",
			Type:        processor.TypeOrigin.String(),
			Version:     version,
			Description: "Index Soroswap pool creation events from the factory contract via getEvents RPC",
			Schema: processor.DescribeSchema{
				ID: "nebu.soroswap_pool.v1",
			},
		}
	})

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	networkPass = normalizeNetwork(networkPass)

	// Auto-detect factory
	if factoryContract == "" {
		for name, addr := range knownFactories {
			if networkPass == normalizeNetwork(name) {
				factoryContract = addr
				break
			}
		}
		if factoryContract == "" {
			return fmt.Errorf("--factory is required (or use --network mainnet|testnet)")
		}
	}

	if last, err := resolveLastFlag(); err != nil {
		return err
	} else if last != 0 {
		if startLedger != 0 || endLedger != 0 {
			return fmt.Errorf("--last and an explicit ledger range are mutually exclusive; " +
				"--last asks the source where the range is")
		}
		start, end, err := lastLedgerRange(context.Background(), last)
		if err != nil {
			return err
		}
		startLedger, endLedger = start, end
	}

	if startLedger == 0 {
		return fmt.Errorf("--start-ledger or --last is required")
	}

	if !quietMode {
		fmt.Fprintf(os.Stderr, "Querying factory %s for new_pair events via getEvents\n", factoryContract)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		markProgramStatusCanceled("soroswap-pool-indexer", programStatusMode, quietMode)
		if !quietMode {
			fmt.Fprintln(os.Stderr, "\nShutting down...")
		}
		cancel()
	}()

	// Create RPC client with optional auth
	var httpClient *http.Client
	if authHeader := os.Getenv("NEBU_RPC_AUTH"); authHeader != "" {
		httpClient = &http.Client{
			Transport: &headerTransport{
				base:   http.DefaultTransport,
				header: "Authorization",
				value:  authHeader,
			},
		}
	}

	client := rpcclient.NewClient(rpcURL, httpClient)
	defer client.Close()

	// Build the topic filter for "new_pair" events
	// Factory events have topic[0] = "SoroswapFactory" (string), topic[1] = "new_pair" (symbol)
	// Use wildcards to match any factory event, then filter client-side
	encoder := json.NewEncoder(os.Stdout)
	poolCount := 0
	cursor := ""
	currentStart := startLedger

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		req := protocol.GetEventsRequest{
			Filters: []protocol.EventFilter{
				{
					EventType:   protocol.EventTypeSet{protocol.EventTypeContract: nil},
					ContractIDs: []string{factoryContract},
				},
			},
			Pagination: &protocol.PaginationOptions{
				Limit: pageLimit,
			},
		}

		if cursor != "" {
			c, err := protocol.ParseCursor(cursor)
			if err != nil {
				return fmt.Errorf("invalid cursor: %w", err)
			}
			req.Pagination.Cursor = &c
		} else {
			req.StartLedger = currentStart
			if endLedger > 0 {
				req.EndLedger = endLedger
			}
		}

		resp, err := client.GetEvents(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			return fmt.Errorf("getEvents error: %w", err)
		}

		if !quietMode && len(resp.Events) > 0 {
			firstLedger := resp.Events[0].Ledger
			lastLedger := resp.Events[len(resp.Events)-1].Ledger
			fmt.Fprintf(os.Stderr, "Got %d events (ledgers %d-%d), %d pools so far\n",
				len(resp.Events), firstLedger, lastLedger, poolCount)
		}

		for _, event := range resp.Events {
			pool := parseEventInfo(event)
			if pool != nil {
				if err := encoder.Encode(pool); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding: %v\n", err)
				}
				poolCount++
			}
		}

		// Check if we got fewer events than the limit — we're done
		if len(resp.Events) < int(pageLimit) {
			break
		}

		cursor = resp.Cursor
		if cursor == "" {
			break
		}
	}

	if !quietMode {
		fmt.Fprintf(os.Stderr, "Done. Found %d pools\n", poolCount)
	}

	return nil
}

type poolEvent struct {
	Schema         string `json:"_schema"`
	NebuVersion    string `json:"_nebu_version"`
	LedgerSequence int32  `json:"ledger_sequence"`
	TimestampUnix  int64  `json:"timestamp_unix,omitempty"`
	TxHash         string `json:"tx_hash"`
	FactoryAddress string `json:"factory_address"`
	PairAddress    string `json:"pair_address"`
	Token0         string `json:"token_0"`
	Token1         string `json:"token_1"`
}

// parseEventInfo parses a getEvents EventInfo for a "new_pair" event.
func parseEventInfo(event protocol.EventInfo) *poolEvent {
	// Check topics for "new_pair"
	isNewPair := false
	for _, topicXDR := range event.TopicXDR {
		scVal, err := decodeScVal(topicXDR)
		if err != nil {
			continue
		}
		if scVal.Type == xdr.ScValTypeScvSymbol && string(*scVal.Sym) == "new_pair" {
			isNewPair = true
			break
		}
	}

	if !isNewPair {
		return nil
	}

	// Parse the value field — contains the map with token_0, token_1, pair
	if event.ValueXDR == "" {
		return nil
	}
	dataVal, err := decodeScVal(event.ValueXDR)
	if err != nil {
		return nil
	}

	pool := &poolEvent{
		Schema:         "nebu.soroswap_pool.v1",
		NebuVersion:    version,
		LedgerSequence: event.Ledger,
		TxHash:         event.TransactionHash,
		FactoryAddress: event.ContractID,
	}

	// Parse timestamp
	if event.LedgerClosedAt != "" {
		if t, err := time.Parse(time.RFC3339, event.LedgerClosedAt); err == nil {
			pool.TimestampUnix = t.Unix()
		}
	}

	// Extract addresses from the map value
	scMap, ok := dataVal.GetMap()
	if !ok || scMap == nil {
		return nil
	}

	for _, entry := range *scMap {
		if entry.Key.Type != xdr.ScValTypeScvSymbol {
			continue
		}
		sym := string(*entry.Key.Sym)
		addr := extractAddress(entry.Val)
		if addr == "" {
			continue
		}
		switch sym {
		case "token_0":
			pool.Token0 = addr
		case "token_1":
			pool.Token1 = addr
		case "pair":
			pool.PairAddress = addr
		}
	}

	if pool.PairAddress == "" {
		return nil
	}

	return pool
}

func decodeScVal(b64 string) (xdr.ScVal, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return xdr.ScVal{}, err
	}
	var val xdr.ScVal
	err = val.UnmarshalBinary(data)
	return val, err
}

func extractAddress(val xdr.ScVal) string {
	if val.Type != xdr.ScValTypeScvAddress {
		return ""
	}
	// xdr.ScAddress.String covers every arm the protocol defines, including
	// the CAP-84 muxed contract (W...) and the muxed account (M...) that the
	// previous contract/account switch returned "" for. A pool address that
	// reads as "" is indistinguishable from an absent one downstream.
	encoded, err := val.MustAddress().String()
	if err != nil {
		return ""
	}
	return encoded
}

// resolveLastFlag reads --last, falling back to NEBU_LAST, the way the shared
// origin CLI does for every other processor.
func resolveLastFlag() (uint32, error) {
	if lastLedgers != 0 {
		return lastLedgers, nil
	}
	raw := os.Getenv("NEBU_LAST")
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("NEBU_LAST=%q is not a ledger count: %w", raw, err)
	}
	return uint32(parsed), nil
}

// lastLedgerRange turns a count of ledgers into the range the source can
// serve, through the same window types and the same arithmetic the other
// processors use, so "the last 100 ledgers" cannot come to mean something
// different here. This processor reads events from the getEvents RPC rather
// than ledgers from a stream, so it cannot use the shared origin CLI that
// would otherwise provide this; it can still agree with it.
func lastLedgerRange(ctx context.Context, last uint32) (uint32, uint32, error) {
	var (
		src *nebuRPC.LedgerSource
		err error
	)
	if authHeader := os.Getenv("NEBU_RPC_AUTH"); authHeader != "" {
		src, err = nebuRPC.NewLedgerSourceWithHeaders(rpcURL, map[string]string{"Authorization": authHeader})
	} else {
		src, err = nebuRPC.NewLedgerSource(rpcURL)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create RPC source: %w", err)
	}
	defer func() {
		// Close releases nothing for a source that never streamed, but the
		// contract allows calling it and ignoring a nil error.
		_ = src.Close()
	}()

	window, err := src.LedgerWindow(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("could not ask %s for its ledger range: %w", rpcURL, err)
	}

	start, end, err := source.ResolveLast(window, last)
	if err != nil {
		return 0, 0, fmt.Errorf("%w (the source reports ledgers %s)", err, window)
	}

	// Here the reported window is a hard limit, which is the opposite of the
	// ledger-streaming case: nebu treats a request below the reported oldest
	// as a note and proceeds, because an archive-backed endpoint reports a
	// window of its last few dozen ledgers and then serves history millions
	// deep. getEvents does not. Asked for a start below its window it
	// answers
	//
	//	[-32600] startLedger must be within the ledger range: 65473982 - 65474045
	//
	// so proceeding would guarantee a failure with a worse message than this
	// one. Refusing rather than clamping keeps the promise --last makes
	// everywhere else: it never returns fewer ledgers than asked.
	if !window.Covers(start) {
		retained := window.Latest - window.Oldest + 1
		return 0, 0, fmt.Errorf(
			"--last %d needs ledgers %d-%d, but getEvents on %s only retains %s (%d ledgers); "+
				"ask for at most %d, or use an endpoint with deeper event retention",
			last, start, end, rpcURL, window, retained, retained)
	}

	fmt.Fprintf(os.Stderr, "Resolved --last %d to ledgers %d-%d (source reports %s)\n",
		last, start, end, window)
	return start, end, nil
}

func normalizeNetwork(s string) string {
	switch s {
	case "mainnet", "pubnet":
		return network.PublicNetworkPassphrase
	case "testnet":
		return network.TestNetworkPassphrase
	default:
		return s
	}
}

// headerTransport adds a custom header to all HTTP requests.
type headerTransport struct {
	base   http.RoundTripper
	header string
	value  string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set(t.header, t.value)
	return t.base.RoundTrip(req)
}
