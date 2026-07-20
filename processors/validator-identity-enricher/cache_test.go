package validatoridentityenricher

import (
	"context"
	"testing"
	"time"
)

type sequenceResolver struct {
	results []LookupResult
	calls   int
}

func (s *sequenceResolver) Resolve(_ context.Context, _, _ string) LookupResult {
	index := s.calls
	s.calls++
	if index >= len(s.results) {
		index = len(s.results) - 1
	}
	return s.results[index]
}

func (s *sequenceResolver) Source() string        { return "radar" }
func (s *sequenceResolver) TemporalBasis() string { return "current" }

func TestCachedResolverCachesResolvedAndNotFound(t *testing.T) {
	for _, status := range []LookupStatus{StatusResolved, StatusNotFound} {
		t.Run(string(status), func(t *testing.T) {
			delegate := &sequenceResolver{results: []LookupResult{{Status: status}}}
			cached, err := NewCachedResolver(delegate, CacheOptions{MaxEntries: 2, UnavailableTTL: time.Second})
			if err != nil {
				t.Fatalf("NewCachedResolver: %v", err)
			}
			for i := 0; i < 2; i++ {
				got := cached.Resolve(context.Background(), "mainnet", testValidator)
				if got.Status != status {
					t.Fatalf("status = %s, want %s", got.Status, status)
				}
			}
			if delegate.calls != 1 {
				t.Fatalf("delegate calls = %d, want 1", delegate.calls)
			}
		})
	}
}

func TestCachedResolverRetriesUnavailableAfterTTL(t *testing.T) {
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	delegate := &sequenceResolver{results: []LookupResult{
		{Status: StatusUnavailable, Reason: "radar_http_503"},
		{Status: StatusResolved, Node: NodeIdentity{PublicKey: testValidator, Name: "recovered"}},
	}}
	cached, err := NewCachedResolver(delegate, CacheOptions{
		MaxEntries:     2,
		UnavailableTTL: time.Minute,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewCachedResolver: %v", err)
	}

	first := cached.Resolve(context.Background(), "mainnet", testValidator)
	now = now.Add(30 * time.Second)
	second := cached.Resolve(context.Background(), "mainnet", testValidator)
	if first.Status != StatusUnavailable || second.Status != StatusUnavailable || delegate.calls != 1 {
		t.Fatalf("failure cache did not suppress retry: first=%#v second=%#v calls=%d", first, second, delegate.calls)
	}

	now = now.Add(31 * time.Second)
	third := cached.Resolve(context.Background(), "mainnet", testValidator)
	if third.Status != StatusResolved || delegate.calls != 2 {
		t.Fatalf("expired failure was not retried: third=%#v calls=%d", third, delegate.calls)
	}
}

func TestCachedResolverSeparatesNetworksAndEvictsLRU(t *testing.T) {
	delegate := &sequenceResolver{results: []LookupResult{{Status: StatusNotFound}}}
	cached, err := NewCachedResolver(delegate, CacheOptions{MaxEntries: 1, UnavailableTTL: time.Second})
	if err != nil {
		t.Fatalf("NewCachedResolver: %v", err)
	}
	cached.Resolve(context.Background(), "mainnet", testValidator)
	cached.Resolve(context.Background(), "testnet", testValidator)
	cached.Resolve(context.Background(), "mainnet", testValidator)
	if delegate.calls != 3 {
		t.Fatalf("delegate calls = %d, want 3 after network separation and eviction", delegate.calls)
	}
}
