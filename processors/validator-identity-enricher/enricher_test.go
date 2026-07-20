package validatoridentityenricher

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/network"
)

const testValidator = "GCGB2S2KGYARPVIA37HYZXVRM2YZUEXA6S33ZU5BUDC6THSB62LZSTYH"

type stubResolver struct {
	result   LookupResult
	calls    int
	networks []string
	wait     bool
}

func (s *stubResolver) Resolve(ctx context.Context, networkName, _ string) LookupResult {
	s.calls++
	s.networks = append(s.networks, networkName)
	if s.wait {
		<-ctx.Done()
		return LookupResult{Status: StatusUnavailable, Reason: "radar_request_timeout"}
	}
	return s.result
}

func (s *stubResolver) Source() string        { return "radar" }
func (s *stubResolver) TemporalBasis() string { return "current" }

func TestEnricherResolvedIdentityPreservesAnalyticsFacts(t *testing.T) {
	resolver := &stubResolver{result: LookupResult{
		Status: StatusResolved,
		Node: NodeIdentity{
			PublicKey:      testValidator,
			Name:           "SDF 1",
			DisplayName:    "Stellar Development Foundation 1",
			Alias:          "sdf1",
			HomeDomain:     "www.stellar.org",
			OrganizationID: "organization-1",
			DateUpdated:    "2026-07-15T21:06:29.862Z",
		},
	}}
	fixedNow := time.Date(2026, 7, 20, 15, 4, 5, 6, time.UTC)
	enricher, err := NewEnricher(Options{
		Resolver: resolver,
		Timeout:  time.Second,
		Network:  "auto",
		Now:      func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}
	event := analyticsEvent()
	event["ledgerSequence"] = float64(3_707_457)
	event["operationCount"] = float64(19)

	got := enricher.Transform(event)

	if got["_schema"] != OutputSchema {
		t.Fatalf("schema = %v, want %s", got["_schema"], OutputSchema)
	}
	if got["ledgerSequence"] != float64(3_707_457) || got["operationCount"] != float64(19) {
		t.Fatalf("analytics facts were changed: %#v", got)
	}
	identity, ok := got["validatorIdentity"].(ValidatorIdentity)
	if !ok {
		t.Fatalf("validatorIdentity type = %T", got["validatorIdentity"])
	}
	if identity.Status != StatusResolved || identity.Name != "SDF 1" || identity.Alias != "sdf1" {
		t.Fatalf("identity = %#v", identity)
	}
	if identity.Source != "radar" || identity.TemporalBasis != "current" {
		t.Fatalf("identity provenance = %#v", identity)
	}
	if identity.ResolvedAt != "2026-07-20T15:04:05.000000006Z" {
		t.Fatalf("resolvedAt = %q", identity.ResolvedAt)
	}
	if resolver.calls != 1 || len(resolver.networks) != 1 || resolver.networks[0] != "mainnet" {
		t.Fatalf("resolver calls=%d networks=%v", resolver.calls, resolver.networks)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal enriched event: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal enriched event: %v", err)
	}
	wireIdentity, ok := wire["validatorIdentity"].(map[string]interface{})
	if !ok || wireIdentity["homeDomain"] != "www.stellar.org" || wireIdentity["organizationId"] != "organization-1" {
		t.Fatalf("wire identity = %#v", wire["validatorIdentity"])
	}
}

func TestEnricherSoftDependencyStates(t *testing.T) {
	tests := []struct {
		name       string
		modify     func(map[string]interface{})
		result     LookupResult
		wantStatus LookupStatus
		wantReason string
		wantCalls  int
	}{
		{
			name:       "Radar not found",
			result:     LookupResult{Status: StatusNotFound, Reason: "radar_not_found"},
			wantStatus: StatusNotFound,
			wantReason: "radar_not_found",
			wantCalls:  1,
		},
		{
			name: "attribution unavailable",
			modify: func(event map[string]interface{}) {
				event["validatorAttributionAvailable"] = false
				event["validatorAddress"] = ""
			},
			wantStatus: StatusUnavailable,
			wantReason: "validator_attribution_unavailable",
			wantCalls:  0,
		},
		{
			name: "invalid validator",
			modify: func(event map[string]interface{}) {
				event["validatorAddress"] = "not-a-validator"
			},
			wantStatus: StatusUnavailable,
			wantReason: "invalid_validator_address",
			wantCalls:  0,
		},
		{
			name: "unknown network",
			modify: func(event map[string]interface{}) {
				event["networkPassphrase"] = "private network"
			},
			wantStatus: StatusUnavailable,
			wantReason: "network_unrecognized",
			wantCalls:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &stubResolver{result: tt.result}
			enricher, err := NewEnricher(Options{Resolver: resolver, Timeout: time.Second, Network: "auto"})
			if err != nil {
				t.Fatalf("NewEnricher: %v", err)
			}
			event := analyticsEvent()
			if tt.modify != nil {
				tt.modify(event)
			}
			got := enricher.Transform(event)
			identity := got["validatorIdentity"].(ValidatorIdentity)
			if identity.Status != tt.wantStatus || identity.Reason != tt.wantReason {
				t.Fatalf("identity = %#v", identity)
			}
			if resolver.calls != tt.wantCalls {
				t.Fatalf("resolver calls = %d, want %d", resolver.calls, tt.wantCalls)
			}
			if got["_schema"] != OutputSchema {
				t.Fatalf("soft failure dropped schema enrichment: %#v", got)
			}
		})
	}
}

func TestEnricherPassesThroughOtherSchemas(t *testing.T) {
	resolver := &stubResolver{}
	enricher, err := NewEnricher(Options{Resolver: resolver, Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}
	event := map[string]interface{}{"_schema": "nebu.other.v1", "value": "unchanged"}
	got := enricher.Transform(event)
	if got["_schema"] != "nebu.other.v1" || got["validatorIdentity"] != nil || resolver.calls != 0 {
		t.Fatalf("unexpected pass-through result: %#v calls=%d", got, resolver.calls)
	}
}

func TestEnricherBoundsLookupAndWarnsOnce(t *testing.T) {
	resolver := &stubResolver{wait: true}
	var warnings []string
	enricher, err := NewEnricher(Options{
		Resolver: resolver,
		Timeout:  10 * time.Millisecond,
		Network:  "mainnet",
		Warn:     func(message string) { warnings = append(warnings, message) },
	})
	if err != nil {
		t.Fatalf("NewEnricher: %v", err)
	}

	for i := 0; i < 2; i++ {
		got := enricher.Transform(analyticsEvent())
		identity := got["validatorIdentity"].(ValidatorIdentity)
		if identity.Status != StatusUnavailable || identity.Reason != "radar_request_timeout" {
			t.Fatalf("identity = %#v", identity)
		}
	}
	if resolver.calls != 2 {
		t.Fatalf("resolver calls = %d, want 2 without cache wrapper", resolver.calls)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one deduplicated warning", warnings)
	}
}

func analyticsEvent() map[string]interface{} {
	return map[string]interface{}{
		"_schema":                       InputSchema,
		"_nebu_version":                 "v0.6.8",
		"networkPassphrase":             network.PublicNetworkPassphrase,
		"validatorAddress":              testValidator,
		"validatorAttributionAvailable": true,
	}
}
