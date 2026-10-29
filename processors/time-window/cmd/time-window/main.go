// Package main provides a standalone CLI for the time-window transform processor.
//
// This processor filters events based on time ranges using actual event timestamps.
// It prefers meta.closedAtUnix emitted by origin processors.
//
// Usage:
//
//	# Events from last hour
//	cat events.jsonl | time-window --last 1h
//
//	# Events from last 24 hours
//	token-transfer --start-ledger 60200000 --end-ledger 60300000 | \
//	  time-window --last 24h
//
//	# Events from last 7 days
//	token-transfer --start-ledger 60200000 --end-ledger 60300000 | \
//	  time-window --last 168h | \
//	  json-file-sink --out weekly.jsonl
package main

import (
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

var (
	lastDuration string
	startTime    int64
	endTime      int64
)

func main() {
	config := cli.TransformConfig{
		Name:        "time-window",
		Description: "Filter events by time range using ledger timestamps",
		Version:     version,
	}

	cli.RunTransformCLI(config, filterByTimeWindow, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&lastDuration, "last", "", "Filter for events from last duration (e.g., 1h, 24h, 7d)")
	cmd.Flags().Int64Var(&startTime, "start", 0, "Start timestamp (Unix seconds, 0 = no limit)")
	cmd.Flags().Int64Var(&endTime, "end", 0, "End timestamp (Unix seconds, 0 = no limit)")
}

// filterByTimeWindow filters events based on time ranges.
// Requires meta.closedAtUnix so filtering uses real ledger close time.
func filterByTimeWindow(event map[string]interface{}) map[string]interface{} {
	meta, ok := event["meta"].(map[string]interface{})
	if !ok {
		return nil
	}

	eventTimeMs, ok := eventCloseTimeMs(meta)
	if !ok {
		return nil
	}

	eventTime := eventTimeMs / 1000

	// Check --last duration
	if lastDuration != "" {
		duration, err := time.ParseDuration(lastDuration)
		if err != nil {
			return nil // Invalid duration, filter out
		}

		cutoffTime := time.Now().Unix() - int64(duration.Seconds())
		if eventTime < cutoffTime {
			return nil // Event too old
		}
	}

	// Check --start timestamp
	if startTime > 0 && eventTime < startTime {
		return nil
	}

	// Check --end timestamp
	if endTime > 0 && eventTime > endTime {
		return nil
	}

	// Passed time window filters
	return event
}

// eventCloseTimeMs returns the event's ledger close time in milliseconds
// since the Unix epoch.
//
// It prefers meta.closedAt, the RFC 3339 field carrying millisecond
// precision. From Protocol 30 (CAP-88) consecutive ledgers can close within
// the same whole second, so a seconds-resolution value cannot place them in
// the right window — it does not error, it just buckets them wrongly.
//
// It falls back to meta.closedAtUnix for events from processors that have
// not been rebuilt yet, accepting both a JSON string and a JSON number.
// That is not defensive coding: protojson renders int64 as a *string*, so
// the previous implementation's float64 type assertion never matched a
// proto-origin event and this filter silently discarded all of them.
func eventCloseTimeMs(meta map[string]interface{}) (int64, bool) {
	if raw, ok := meta["closedAt"].(string); ok && raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t.UnixMilli(), true
		}
	}
	for _, key := range []string{"closedAtUnix", "closed_at_unix"} {
		switch v := meta[key].(type) {
		case float64:
			return int64(v) * 1000, true
		case string:
			if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
				return secs * 1000, true
			}
		}
	}
	return 0, false
}
