package validatoridentityenricher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRadarResolverResolvesAtFixedTimeAndRetriesTransientFailure(t *testing.T) {
	var calls atomic.Int32
	at := time.Date(2026, 7, 1, 12, 30, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.URL.Path != "/v1/node/"+testValidator {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("at") != "2026-07-01T12:30:00Z" {
			t.Errorf("at = %q", r.URL.Query().Get("at"))
		}
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") != "test-enricher" {
			t.Errorf("headers = %#v", r.Header)
		}
		if call == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"publicKey":%q,"name":"SDF 1","displayName":"SDF Validator 1","alias":"sdf1","homeDomain":"www.stellar.org","organizationId":"org-1","dateUpdated":"2026-07-15T21:06:29.862Z","isValidator":true}`, testValidator)
	}))
	defer server.Close()

	resolver, err := NewRadarResolver(RadarOptions{
		Client:     server.Client(),
		BaseURL:    server.URL,
		At:         &at,
		Retries:    1,
		RetryDelay: time.Millisecond,
		UserAgent:  "test-enricher",
	})
	if err != nil {
		t.Fatalf("NewRadarResolver: %v", err)
	}
	got := resolver.Resolve(context.Background(), "mainnet", testValidator)
	if got.Status != StatusResolved || got.Node.Name != "SDF 1" || got.Node.OrganizationID != "org-1" {
		t.Fatalf("result = %#v", got)
	}
	if resolver.Source() != "radar" || resolver.TemporalBasis() != "at" {
		t.Fatalf("resolver provenance = %s/%s", resolver.Source(), resolver.TemporalBasis())
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestRadarResolverStatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantStatus LookupStatus
		wantReason string
	}{
		{name: "not found", statusCode: http.StatusNotFound, wantStatus: StatusNotFound, wantReason: "radar_not_found"},
		{name: "rate limited", statusCode: http.StatusTooManyRequests, wantStatus: StatusUnavailable, wantReason: "radar_http_429"},
		{name: "bad JSON", statusCode: http.StatusOK, body: `{`, wantStatus: StatusUnavailable, wantReason: "radar_invalid_response"},
		{name: "key mismatch", statusCode: http.StatusOK, body: `{"publicKey":"GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF"}`, wantStatus: StatusUnavailable, wantReason: "radar_public_key_mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			resolver, err := NewRadarResolver(RadarOptions{Client: server.Client(), BaseURL: server.URL})
			if err != nil {
				t.Fatalf("NewRadarResolver: %v", err)
			}
			got := resolver.Resolve(context.Background(), "mainnet", testValidator)
			if got.Status != tt.wantStatus || got.Reason != tt.wantReason {
				t.Fatalf("result = %#v, want %s/%s", got, tt.wantStatus, tt.wantReason)
			}
		})
	}
}

func TestRadarResolverHonorsContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	resolver, err := NewRadarResolver(RadarOptions{Client: server.Client(), BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewRadarResolver: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	got := resolver.Resolve(ctx, "mainnet", testValidator)
	if got.Status != StatusUnavailable || got.Reason != "radar_request_timeout" {
		t.Fatalf("result = %#v", got)
	}
}

func TestRadarResolverUsesNetworkSpecificBaseURL(t *testing.T) {
	var mainnetCalls atomic.Int32
	var testnetCalls atomic.Int32
	mainnet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mainnetCalls.Add(1)
		fmt.Fprintf(w, `{"publicKey":%q}`, testValidator)
	}))
	defer mainnet.Close()
	testnet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		testnetCalls.Add(1)
		fmt.Fprintf(w, `{"publicKey":%q}`, testValidator)
	}))
	defer testnet.Close()
	resolver, err := NewRadarResolver(RadarOptions{
		Client:         mainnet.Client(),
		MainnetBaseURL: mainnet.URL,
		TestnetBaseURL: testnet.URL,
	})
	if err != nil {
		t.Fatalf("NewRadarResolver: %v", err)
	}
	resolver.Resolve(context.Background(), "mainnet", testValidator)
	resolver.Resolve(context.Background(), "testnet", testValidator)
	if mainnetCalls.Load() != 1 || testnetCalls.Load() != 1 {
		t.Fatalf("mainnet calls=%d testnet calls=%d", mainnetCalls.Load(), testnetCalls.Load())
	}
}
