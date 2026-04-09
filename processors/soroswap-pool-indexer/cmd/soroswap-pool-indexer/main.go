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
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/stellar/go-stellar-sdk/clients/rpcclient"
	"github.com/stellar/go-stellar-sdk/network"
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/withObsrvr/nebu/pkg/processor"
)

var version = "0.2.0"

var (
	rpcURL          string
	startLedger     uint32
	endLedger       uint32
	networkPass     string
	quietMode       bool
	factoryContract string
	pageLimit       uint
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
			return run()
		},
	}

	rootCmd.Flags().StringVar(&rpcURL, "rpc-url", "https://archive-rpc.lightsail.network", "Stellar Soroban RPC endpoint")
	rootCmd.Flags().Uint32Var(&startLedger, "start-ledger", 0, "Start ledger sequence")
	rootCmd.Flags().Uint32Var(&endLedger, "end-ledger", 0, "End ledger sequence (0 = use RPC latest)")
	rootCmd.Flags().StringVar(&networkPass, "network", "mainnet", "Network (mainnet|testnet) or full passphrase")
	rootCmd.Flags().BoolVarP(&quietMode, "quiet", "q", false, "Suppress non-error output")
	rootCmd.Flags().StringVar(&factoryContract, "factory", "", "Soroswap factory contract address (auto-detected from --network)")
	rootCmd.Flags().UintVar(&pageLimit, "page-limit", 100, "Events per RPC page request")
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

	if startLedger == 0 {
		return fmt.Errorf("--start-ledger is required")
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
	addr := val.MustAddress()
	switch addr.Type {
	case xdr.ScAddressTypeScAddressTypeContract:
		contractID := addr.MustContractId()
		encoded, err := strkey.Encode(strkey.VersionByteContract, contractID[:])
		if err != nil {
			return ""
		}
		return encoded
	case xdr.ScAddressTypeScAddressTypeAccount:
		accountID := addr.MustAccountId()
		raw := accountID.MustEd25519()
		encoded, err := strkey.Encode(strkey.VersionByteAccountID, raw[:])
		if err != nil {
			return ""
		}
		return encoded
	}
	return ""
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
