package validatoridentityenricher

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/withObsrvr/nebu/pkg/version"
)

const (
	// InputSchema is emitted by the validator-analytics origin processor.
	InputSchema = "nebu.validator_analytics.v1"
	// OutputSchema adds validatorIdentity while preserving every input fact.
	OutputSchema = "nebu.validator_analytics_enriched.v1"
)

// LookupStatus records whether an identity source resolved the validator.
type LookupStatus string

const (
	StatusResolved    LookupStatus = "resolved"
	StatusNotFound    LookupStatus = "not_found"
	StatusUnavailable LookupStatus = "unavailable"
)

// NodeIdentity is the stable identity subset copied from Radar or a snapshot.
// Volatile health, availability, and trust metrics deliberately do not belong
// in this record.
type NodeIdentity struct {
	PublicKey      string `json:"publicKey,omitempty"`
	Name           string `json:"name,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`
	Alias          string `json:"alias,omitempty"`
	HomeDomain     string `json:"homeDomain,omitempty"`
	OrganizationID string `json:"organizationId,omitempty"`
	DateUpdated    string `json:"dateUpdated,omitempty"`
}

// LookupResult is returned by an identity source. Errors are represented as a
// status and reason so a failed enrichment never drops a ledger event.
type LookupResult struct {
	Status LookupStatus
	Node   NodeIdentity
	Reason string
}

// Resolver looks up one validator public key for one Stellar network.
type Resolver interface {
	Resolve(ctx context.Context, networkName, publicKey string) LookupResult
	Source() string
	TemporalBasis() string
}

// ValidatorIdentity is embedded into an enriched validator analytics record.
type ValidatorIdentity struct {
	Status          LookupStatus `json:"status"`
	Reason          string       `json:"reason,omitempty"`
	PublicKey       string       `json:"publicKey"`
	Name            string       `json:"name,omitempty"`
	DisplayName     string       `json:"displayName,omitempty"`
	Alias           string       `json:"alias,omitempty"`
	HomeDomain      string       `json:"homeDomain,omitempty"`
	OrganizationID  string       `json:"organizationId,omitempty"`
	Source          string       `json:"source"`
	SourceUpdatedAt string       `json:"sourceUpdatedAt,omitempty"`
	ResolvedAt      string       `json:"resolvedAt"`
	TemporalBasis   string       `json:"temporalBasis"`
}

// Enricher adds validator identity to validator-analytics events.
type Enricher struct {
	resolver Resolver
	timeout  time.Duration
	network  string
	now      func() time.Time
	warn     func(string)

	warnMu sync.Mutex
	warned map[string]struct{}
}

// Options configure an Enricher. Network may be auto, mainnet, or testnet.
type Options struct {
	Resolver Resolver
	Timeout  time.Duration
	Network  string
	Now      func() time.Time
	Warn     func(string)
}

// NewEnricher validates options and constructs an identity transform.
func NewEnricher(opts Options) (*Enricher, error) {
	if opts.Resolver == nil {
		return nil, fmt.Errorf("resolver is required")
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("timeout must be positive")
	}
	networkName := strings.ToLower(strings.TrimSpace(opts.Network))
	if networkName == "" {
		networkName = "auto"
	}
	switch networkName {
	case "auto", "mainnet", "testnet":
	default:
		return nil, fmt.Errorf("network must be auto, mainnet, or testnet")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Enricher{
		resolver: opts.Resolver,
		timeout:  opts.Timeout,
		network:  networkName,
		now:      opts.Now,
		warn:     opts.Warn,
		warned:   make(map[string]struct{}),
	}, nil
}

// Transform enriches a validator analytics event. Events with a different
// schema pass through untouched so the processor remains pipe-composable.
func (e *Enricher) Transform(event map[string]interface{}) map[string]interface{} {
	schema, _ := event["_schema"].(string)
	if schema != InputSchema {
		return event
	}

	publicKey, _ := event["validatorAddress"].(string)
	publicKey = strings.TrimSpace(publicKey)
	identity := ValidatorIdentity{
		Status:        StatusUnavailable,
		PublicKey:     publicKey,
		Source:        e.resolver.Source(),
		ResolvedAt:    e.now().UTC().Format(time.RFC3339Nano),
		TemporalBasis: e.resolver.TemporalBasis(),
	}

	attributionAvailable, _ := event["validatorAttributionAvailable"].(bool)
	switch {
	case !attributionAvailable:
		identity.Reason = "validator_attribution_unavailable"
	case !strkey.IsValidEd25519PublicKey(publicKey):
		identity.Reason = "invalid_validator_address"
	default:
		networkName, ok := e.resolveNetwork(event)
		if !ok {
			identity.Reason = "network_unrecognized"
			break
		}

		ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
		result := e.resolver.Resolve(ctx, networkName, publicKey)
		cancel()

		identity.Status = result.Status
		identity.Reason = result.Reason
		if result.Status == StatusResolved {
			identity.Name = result.Node.Name
			identity.DisplayName = result.Node.DisplayName
			identity.Alias = result.Node.Alias
			identity.HomeDomain = result.Node.HomeDomain
			identity.OrganizationID = result.Node.OrganizationID
			identity.SourceUpdatedAt = result.Node.DateUpdated
		}
	}

	if identity.Status == StatusUnavailable && identity.Reason != "validator_attribution_unavailable" {
		e.warnOnce(publicKey+"\x00"+identity.Reason, fmt.Sprintf("validator %s identity unavailable: %s", publicKey, identity.Reason))
	}

	event["_schema"] = OutputSchema
	event["_nebu_version"] = version.Version
	event["validatorIdentity"] = identity
	return event
}

func (e *Enricher) resolveNetwork(event map[string]interface{}) (string, bool) {
	if e.network != "auto" {
		return e.network, true
	}
	passphrase, _ := event["networkPassphrase"].(string)
	switch strings.TrimSpace(passphrase) {
	case network.PublicNetworkPassphrase:
		return "mainnet", true
	case network.TestNetworkPassphrase:
		return "testnet", true
	default:
		return "", false
	}
}

func (e *Enricher) warnOnce(key, message string) {
	if e.warn == nil {
		return
	}
	e.warnMu.Lock()
	defer e.warnMu.Unlock()
	if _, ok := e.warned[key]; ok {
		return
	}
	e.warned[key] = struct{}{}
	e.warn(message)
}
