package validatoridentityenricher

import (
	"context"
	"testing"
)

func TestParseSnapshotEnvelopeAndRawRadarArray(t *testing.T) {
	tests := []struct {
		name              string
		data              string
		network           string
		wantUpdated       string
		wantTemporalBasis string
	}{
		{
			name: "canonical envelope",
			data: `{
				"network":"mainnet",
				"generatedAt":"2026-07-20T12:00:00Z",
				"nodes":[{"publicKey":"` + testValidator + `","name":"SDF 1","alias":"sdf1"}]
			}`,
			network:           "mainnet",
			wantUpdated:       "2026-07-20T12:00:00Z",
			wantTemporalBasis: "snapshot",
		},
		{
			name:              "raw Radar array",
			data:              `[{"publicKey":"` + testValidator + `","name":"SDF 1","dateUpdated":"2026-07-15T21:06:29.862Z","isValidator":true}]`,
			network:           "testnet",
			wantUpdated:       "2026-07-15T21:06:29.862Z",
			wantTemporalBasis: "snapshot",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, err := ParseSnapshot([]byte(tt.data))
			if err != nil {
				t.Fatalf("ParseSnapshot: %v", err)
			}
			got := resolver.Resolve(context.Background(), tt.network, testValidator)
			if got.Status != StatusResolved || got.Node.Name != "SDF 1" || got.Node.DateUpdated != tt.wantUpdated {
				t.Fatalf("result = %#v", got)
			}
			if resolver.Source() != "snapshot" || resolver.TemporalBasis() != tt.wantTemporalBasis {
				t.Fatalf("provenance = %s/%s", resolver.Source(), resolver.TemporalBasis())
			}
		})
	}
}

func TestSnapshotResolverStatusMapping(t *testing.T) {
	resolver, err := ParseSnapshot([]byte(`{"network":"mainnet","nodes":[{"publicKey":"` + testValidator + `"}]}`))
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	mismatch := resolver.Resolve(context.Background(), "testnet", testValidator)
	if mismatch.Status != StatusUnavailable || mismatch.Reason != "snapshot_network_mismatch" {
		t.Fatalf("mismatch = %#v", mismatch)
	}
	notFound := resolver.Resolve(context.Background(), "mainnet", "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF")
	if notFound.Status != StatusNotFound || notFound.Reason != "snapshot_not_found" {
		t.Fatalf("not found = %#v", notFound)
	}
}

func TestParseSnapshotRejectsInvalidInputs(t *testing.T) {
	tests := []string{
		``,
		`{`,
		`{"network":"private","nodes":[]}`,
		`{"nodes":[{}]}`,
		`{"nodes":[{"publicKey":"` + testValidator + `"},{"publicKey":"` + testValidator + `"}]}`,
	}
	for _, data := range tests {
		if _, err := ParseSnapshot([]byte(data)); err == nil {
			t.Fatalf("ParseSnapshot(%q) unexpectedly succeeded", data)
		}
	}
}
