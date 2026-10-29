package main

import (
	"testing"
)

// TestEventCloseTimeMsPrefersMillisecondField is the CAP-88 case: two events
// from consecutive ledgers closing inside one whole second must be
// distinguishable. closedAtUnix cannot tell them apart; closedAt can.
func TestEventCloseTimeMsPrefersMillisecondField(t *testing.T) {
	first := map[string]interface{}{
		"closedAt":     "2026-11-04T10:00:00.200Z",
		"closedAtUnix": "1793872800",
	}
	second := map[string]interface{}{
		"closedAt":     "2026-11-04T10:00:00.800Z",
		"closedAtUnix": "1793872800", // same whole second
	}

	a, ok := eventCloseTimeMs(first)
	if !ok {
		t.Fatal("first event: expected a close time")
	}
	b, ok := eventCloseTimeMs(second)
	if !ok {
		t.Fatal("second event: expected a close time")
	}
	if a == b {
		t.Fatalf("same-second ledgers must be distinguishable, both resolved to %d", a)
	}
	if got := b - a; got != 600 {
		t.Fatalf("expected 600ms apart, got %dms", got)
	}
}

// TestEventCloseTimeMsAcceptsProtojsonString pins the bug this replaced:
// protojson renders int64 as a JSON *string*, so a float64 type assertion
// never matched a proto-origin event and the filter discarded all of them.
func TestEventCloseTimeMsAcceptsProtojsonString(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]interface{}
		want int64
	}{
		{"protojson string", map[string]interface{}{"closedAtUnix": "1765158311"}, 1765158311000},
		{"plain number", map[string]interface{}{"closedAtUnix": float64(1765158311)}, 1765158311000},
		{"snake_case fallback", map[string]interface{}{"closed_at_unix": "1765158311"}, 1765158311000},
		{"rfc3339 preferred", map[string]interface{}{
			"closedAt": "2025-12-08T01:45:11.123Z", "closedAtUnix": "1765158311",
		}, 1765158311123},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := eventCloseTimeMs(tt.meta)
			if !ok {
				t.Fatal("expected a close time")
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEventCloseTimeMsMissing(t *testing.T) {
	if _, ok := eventCloseTimeMs(map[string]interface{}{}); ok {
		t.Fatal("expected no close time from empty meta")
	}
	if _, ok := eventCloseTimeMs(map[string]interface{}{"closedAt": "not-a-time"}); ok {
		t.Fatal("expected unparseable closedAt to be rejected")
	}
}
