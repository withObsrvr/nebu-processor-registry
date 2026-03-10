// Package main provides a standalone CLI for the aggregator transform processor.
//
// This processor buckets events into time windows and emits summary events
// with count, sum, min, and max statistics. Uses ledger timestamps for
// deterministic replay.
//
// Usage:
//
//	# 5-minute volume summaries
//	token-transfer --start-ledger 60200000 | aggregator --window 5m --sum-field transfer.amount
//
//	# Hourly summaries grouped by asset
//	token-transfer --start-ledger 60200000 | aggregator --window 1h --sum-field transfer.amount --group-by transfer.asset.issuedAsset.assetCode
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

var (
	windowDuration time.Duration
	sumField       string
	groupByField   string
)

// bucket holds aggregation state for a time window + group key.
type bucket struct {
	windowStart time.Time
	windowEnd   time.Time
	groupKey    string
	count       int64
	sum         float64
	min         float64
	max         float64
	hasValues   bool
}

// Global state: active buckets keyed by "windowStart|groupKey"
var buckets = make(map[string]*bucket)

func main() {
	config := cli.TransformConfig{
		Name:        "aggregator",
		Description: "Aggregate events into time-bucketed summaries",
		Version:     version,
	}

	cli.RunTransformCLI(config, aggregate, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&windowDuration, "window", 5*time.Minute, "Window duration (e.g., 1m, 5m, 1h)")
	cmd.Flags().StringVar(&sumField, "sum-field", "", "Field path to sum (e.g., transfer.amount)")
	cmd.Flags().StringVar(&groupByField, "group-by", "", "Optional field path for grouping (e.g., transfer.asset.issuedAsset.assetCode)")
}

// aggregate processes each event and emits summary events when windows close.
func aggregate(event map[string]interface{}) map[string]interface{} {
	// Extract timestamp from event
	ts := extractTimestamp(event)
	if ts.IsZero() {
		return nil
	}

	// Determine which window this event belongs to
	windowStart := ts.Truncate(windowDuration)
	windowEnd := windowStart.Add(windowDuration)

	// Extract group key
	groupKey := ""
	if groupByField != "" {
		groupKey = resolveStringField(event, groupByField)
	}

	bucketKey := fmt.Sprintf("%d|%s", windowStart.Unix(), groupKey)

	// Check if we need to flush any closed windows
	var result map[string]interface{}
	for key, b := range buckets {
		if ts.After(b.windowEnd) || ts.Equal(b.windowEnd) {
			// Window has closed — emit summary
			result = emitSummary(b)
			delete(buckets, key)
			// Only emit one summary per event (others will flush on subsequent events)
			break
		}
	}

	// Get or create bucket
	b, exists := buckets[bucketKey]
	if !exists {
		b = &bucket{
			windowStart: windowStart,
			windowEnd:   windowEnd,
			groupKey:    groupKey,
			min:         math.MaxFloat64,
			max:         -math.MaxFloat64,
		}
		buckets[bucketKey] = b
	}

	// Update bucket stats
	b.count++

	if sumField != "" {
		if val := resolveNumericField(event, sumField); val != 0 || resolveStringField(event, sumField) == "0" {
			b.sum += val
			if val < b.min {
				b.min = val
			}
			if val > b.max {
				b.max = val
			}
			b.hasValues = true
		}
	}

	return result
}

func emitSummary(b *bucket) map[string]interface{} {
	summary := map[string]interface{}{
		"_schema": "nebu.aggregator.v1",
		"window": map[string]interface{}{
			"start":    b.windowStart.UTC().Format(time.RFC3339),
			"end":      b.windowEnd.UTC().Format(time.RFC3339),
			"duration": windowDuration.String(),
		},
		"stats": map[string]interface{}{
			"count": b.count,
		},
	}

	if b.hasValues {
		stats := summary["stats"].(map[string]interface{})
		stats["sum"] = formatFloat(b.sum)
		stats["min"] = formatFloat(b.min)
		stats["max"] = formatFloat(b.max)
	}

	if b.groupKey != "" {
		summary["groupKey"] = b.groupKey
	}

	return summary
}

func formatFloat(f float64) string {
	if f == math.Trunc(f) {
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%g", f)
}

func extractTimestamp(event map[string]interface{}) time.Time {
	// Try meta.closedAtUnix (origin processors with proto)
	if meta, ok := event["meta"].(map[string]interface{}); ok {
		if v := toFloat64(meta["closedAtUnix"]); v != 0 {
			return time.Unix(int64(v), 0)
		}
		if v := toFloat64(meta["closed_at_unix"]); v != 0 {
			return time.Unix(int64(v), 0)
		}
		// Try ISO 8601 timestamp
		if ts, ok := meta["timestamp"].(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				return t
			}
		}
	}

	// Try top-level timestamp (contract-events)
	if v := toFloat64(event["timestamp"]); v != 0 {
		return time.Unix(int64(v), 0)
	}

	return time.Time{}
}

func resolveStringField(event map[string]interface{}, path string) string {
	parts := strings.Split(path, ".")
	current := event
	for i, part := range parts {
		val, ok := current[part]
		if !ok {
			return ""
		}
		if i == len(parts)-1 {
			return fmt.Sprintf("%v", val)
		}
		current, ok = val.(map[string]interface{})
		if !ok {
			return ""
		}
	}
	return ""
}

func resolveNumericField(event map[string]interface{}, path string) float64 {
	str := resolveStringField(event, path)
	if str == "" {
		return 0
	}
	f, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return 0
	}
	return f
}

func toFloat64(val interface{}) float64 {
	switch v := val.(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	case int64:
		return float64(v)
	case int:
		return float64(v)
	}
	return 0
}

