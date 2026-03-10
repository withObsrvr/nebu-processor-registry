// Package main provides a standalone CLI for the rate-limiter transform processor.
//
// This processor limits event throughput to a target rate using a token bucket
// algorithm. Events are buffered and emitted at a controlled pace.
//
// Usage:
//
//	# Limit to 100 events/sec
//	token-transfer --start-ledger 60200000 | rate-limiter --rate 100
//
//	# Limit to 10 events/sec with burst of 50
//	token-transfer --start-ledger 60200000 | rate-limiter --rate 10 --burst 50
package main

import (
	"time"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

var (
	rate  float64
	burst int

	// Token bucket state
	tokens    float64
	maxTokens float64
	lastTime  time.Time
	started   bool
)

func main() {
	config := cli.TransformConfig{
		Name:        "rate-limiter",
		Description: "Limit event throughput to a target rate",
		Version:     version,
	}

	cli.RunTransformCLI(config, rateLimit, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().Float64Var(&rate, "rate", 100, "Target events per second")
	cmd.Flags().IntVar(&burst, "burst", 0, "Max burst size (default: same as rate)")
}

// rateLimit passes events through at a controlled rate using a token bucket.
func rateLimit(event map[string]interface{}) map[string]interface{} {
	if rate <= 0 {
		return event
	}

	if !started {
		started = true
		lastTime = time.Now()
		if burst <= 0 {
			maxTokens = rate
		} else {
			maxTokens = float64(burst)
		}
		tokens = maxTokens
	}

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(lastTime).Seconds()
	lastTime = now
	tokens += elapsed * rate
	if tokens > maxTokens {
		tokens = maxTokens
	}

	// Wait if no tokens available
	if tokens < 1.0 {
		waitTime := time.Duration((1.0 - tokens) / rate * float64(time.Second))
		time.Sleep(waitTime)
		tokens = 0
		lastTime = time.Now()
	} else {
		tokens -= 1.0
	}

	return event
}
