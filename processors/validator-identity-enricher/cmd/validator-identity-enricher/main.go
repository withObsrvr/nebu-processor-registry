package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	validatoridentityenricher "github.com/withObsrvr/nebu-processor-registry/processors/validator-identity-enricher"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

var (
	identitySource  string
	networkName     string
	snapshotPath    string
	radarBaseURL    string
	identityAt      string
	lookupTimeout   time.Duration
	cacheSize       int
	failureCacheTTL time.Duration
	retries         int
	retryDelay      time.Duration
	runtimeEnricher *validatoridentityenricher.Enricher
)

func main() {
	config := cli.TransformConfig{
		Name:        "validator-identity-enricher",
		Description: "Enrich validator analytics records with cached Radar or snapshot identity",
		Version:     version,
		SchemaID:    validatoridentityenricher.OutputSchema,
	}
	cli.RunTransformCLI(config, transformEvent, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&identitySource, "source", "radar", "Identity source: radar or snapshot")
	cmd.Flags().StringVar(&networkName, "network", "auto", "Stellar network: auto, mainnet, or testnet")
	cmd.Flags().StringVar(&snapshotPath, "snapshot", "", "Radar node snapshot file (required for --source snapshot)")
	cmd.Flags().StringVar(&radarBaseURL, "radar-url", "", "Override the Radar API base URL")
	cmd.Flags().StringVar(&identityAt, "identity-at", "", "Resolve Radar identity at a fixed RFC3339 time")
	cmd.Flags().DurationVar(&lookupTimeout, "timeout", time.Second, "Maximum time for one identity resolution including retries")
	cmd.Flags().IntVar(&cacheSize, "cache-size", 4096, "Maximum distinct validator identities cached in memory")
	cmd.Flags().DurationVar(&failureCacheTTL, "failure-cache-ttl", 30*time.Second, "How long unavailable results suppress repeat lookups")
	cmd.Flags().IntVar(&retries, "retries", 1, "Retries for transient Radar failures")
	cmd.Flags().DurationVar(&retryDelay, "retry-delay", 50*time.Millisecond, "Delay between Radar retries")

	cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		enricher, err := buildEnricher()
		if err != nil {
			return err
		}
		runtimeEnricher = enricher
		return nil
	}
}

func buildEnricher() (*validatoridentityenricher.Enricher, error) {
	var resolver validatoridentityenricher.Resolver
	switch strings.ToLower(strings.TrimSpace(identitySource)) {
	case "radar":
		var at *time.Time
		if strings.TrimSpace(identityAt) != "" {
			parsed, err := time.Parse(time.RFC3339, identityAt)
			if err != nil {
				return nil, fmt.Errorf("parse --identity-at: %w", err)
			}
			at = &parsed
		}
		radar, err := validatoridentityenricher.NewRadarResolver(validatoridentityenricher.RadarOptions{
			Client:     &http.Client{},
			BaseURL:    strings.TrimSpace(radarBaseURL),
			At:         at,
			Retries:    retries,
			RetryDelay: retryDelay,
			UserAgent:  "validator-identity-enricher/" + version,
		})
		if err != nil {
			return nil, err
		}
		resolver = radar
	case "snapshot":
		if strings.TrimSpace(snapshotPath) == "" {
			return nil, fmt.Errorf("--snapshot is required with --source snapshot")
		}
		if strings.TrimSpace(identityAt) != "" {
			return nil, fmt.Errorf("--identity-at is only valid with --source radar")
		}
		snapshot, err := validatoridentityenricher.LoadSnapshot(snapshotPath)
		if err != nil {
			return nil, err
		}
		resolver = snapshot
	default:
		return nil, fmt.Errorf("--source must be radar or snapshot")
	}

	cached, err := validatoridentityenricher.NewCachedResolver(resolver, validatoridentityenricher.CacheOptions{
		MaxEntries:     cacheSize,
		UnavailableTTL: failureCacheTTL,
	})
	if err != nil {
		return nil, err
	}
	return validatoridentityenricher.NewEnricher(validatoridentityenricher.Options{
		Resolver: cached,
		Timeout:  lookupTimeout,
		Network:  networkName,
		Warn: func(message string) {
			fmt.Fprintf(os.Stderr, "[validator-identity-enricher] warning: %s\n", message)
		},
	})
}

func transformEvent(event map[string]interface{}) map[string]interface{} {
	if runtimeEnricher == nil {
		return event
	}
	return runtimeEnricher.Transform(event)
}
