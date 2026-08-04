package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/withObsrvr/nebu/pkg/runtime"
)

// progressHook is a self-contained nebu runtime.Hooks bundle that
// shows live ledger-processing progress on stderr. It's a reference
// example of how to wire observability into a pipeline via the
// OriginConfig.Hooks field, without modifying the processor or the runtime.
//
// The hook prints to stderr at most once per second (or every 100
// ledgers, whichever comes first) so high-throughput pipelines don't
// drown the output. stdout (the JSON event stream) is untouched.
//
// Output is suppressed when stderr is not a terminal, so the hook is a
// no-op when stderr is redirected.
func progressHook() runtime.Hooks {
	if !isStderrTerminal() {
		return runtime.Hooks{}
	}

	var (
		startedAt time.Time
		startSeq  uint32
		total     uint32
		lastPrint time.Time
	)

	return runtime.Hooks{
		OnStart: func(_ context.Context, info runtime.PipelineInfo) {
			startedAt = time.Now()
			startSeq = info.StartLedger
			if info.EndLedger > 0 {
				total = info.EndLedger - info.StartLedger + 1
			}
		},

		AfterLedger: func(_ context.Context, ledger xdr.LedgerCloseMeta, _ runtime.LedgerStats) {
			seq := ledger.LedgerSequence()
			done := seq - startSeq + 1

			if done%100 != 0 && time.Since(lastPrint) < time.Second {
				return
			}
			lastPrint = time.Now()

			elapsed := time.Since(startedAt)
			rate := float64(done) / elapsed.Seconds()
			if total > 0 {
				pct := float64(done) / float64(total) * 100
				var eta time.Duration
				if rate > 0 {
					eta = time.Duration(float64(total-done)/rate) * time.Second
				}
				fmt.Fprintf(os.Stderr, "\r[%6.2f%%] %d/%d ledgers, %.0f/sec, eta %s     ",
					pct, done, total, rate, eta.Round(time.Second))
			} else {
				fmt.Fprintf(os.Stderr, "\r[stream] %d ledgers, %.0f/sec, %s elapsed     ",
					done, rate, elapsed.Round(time.Second))
			}
		},

		OnEnd: func(_ context.Context, summary runtime.PipelineSummary) {
			fmt.Fprintf(os.Stderr, "\n[done] %d ledgers in %s",
				summary.LedgersProcessed, summary.Duration.Round(time.Millisecond))
			if summary.Warnings > 0 {
				fmt.Fprintf(os.Stderr, " (%d warnings)", summary.Warnings)
			}
			if summary.FatalErr != nil {
				fmt.Fprintf(os.Stderr, " — fatal: %v", summary.FatalErr)
			}
			fmt.Fprintln(os.Stderr)
		},
	}
}

func isStderrTerminal() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
