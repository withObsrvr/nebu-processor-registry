package token_transfer

import (
	"context"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/processors/token_transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/withObsrvr/nebu/pkg/runtime"
	"github.com/withObsrvr/nebu/pkg/source/rpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestOrigin_ProcessLedger(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Integration test - processes real ledgers from RPC
	src, err := rpc.NewLedgerSource("https://archive-rpc.lightsail.network")
	require.NoError(t, err)
	defer src.Close()

	origin := NewOrigin(network.PublicNetworkPassphrase)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Collect events in background
	var events []string
	done := make(chan struct{})

	go func() {
		for ev := range origin.Out() {
			// Categorize by event type
			switch {
			case ev.GetTransfer() != nil:
				events = append(events, "transfer")
			case ev.GetMint() != nil:
				events = append(events, "mint")
			case ev.GetBurn() != nil:
				events = append(events, "burn")
			case ev.GetClawback() != nil:
				events = append(events, "clawback")
			case ev.GetFee() != nil:
				events = append(events, "fee")
			}
		}
		close(done)
	}()

	// Run origin processor on a small range
	rt := runtime.NewRuntime()
	err = rt.RunOrigin(ctx, src, origin, 60200000, 60200002)
	require.NoError(t, err)

	// Close emitter and wait for collection to finish
	origin.Close()
	<-done

	// Should have collected some events (exact count varies)
	assert.Greater(t, len(events), 0, "should have processed some token transfer events")

	// Log summary
	t.Logf("Processed %d events from ledgers 60200000-60200002", len(events))
}

func TestOrigin_Name(t *testing.T) {
	origin := NewOrigin(network.PublicNetworkPassphrase)
	assert.Equal(t, "stellar/token-transfer", origin.Name())
}

func TestOrigin_Type(t *testing.T) {
	origin := NewOrigin(network.PublicNetworkPassphrase)
	assert.Equal(t, "origin", origin.Type().String())
}

// TestConvertEventCloseTime pins where the close time comes from.
//
// Until 2026-11-14 both meta fields were derived from sdkEvent.Meta.ClosedAt,
// which the SDK builds with timestamppb.New(tx.Ledger.ClosedAt()) — whole
// seconds. The protobuf Timestamp can carry sub-second precision, so reading
// it looked safe, but the value had already lost it: on testnet ledger
// 5727700, true close time 1794662144916, this emitted "...44.000Z". Three
// decimal places of false precision are worse than none, because the proto
// comment tells consumers to prefer closed_at for exactly the ordering CAP-88
// breaks.
//
// The event meta passed in deliberately carries a *different*, truncated
// timestamp, so a regression that goes back to reading it fails here.
func TestConvertEventCloseTime(t *testing.T) {
	tests := []struct {
		name          string
		closeTimeMs   int64
		wantUnix      int64
		wantFormatted string
	}{
		{
			// The real Protocol 30 ledger this was found on.
			name:          "a Protocol 30 ledger keeps its milliseconds",
			closeTimeMs:   1794662144916,
			wantUnix:      1794662144,
			wantFormatted: "2026-11-14T13:15:44.916Z",
		},
		{
			// Pre-Protocol-30: LedgerCloseTimeMs promotes the whole second,
			// so output for historical ledgers is unchanged.
			name:          "a pre-Protocol-30 ledger is unchanged",
			closeTimeMs:   1765158311000,
			wantUnix:      1765158311,
			wantFormatted: "2025-12-08T01:45:11.000Z",
		},
		{
			// The case the field exists for: same whole second, different
			// millisecond. closed_at_unix cannot tell these apart and
			// closed_at must.
			name:          "the second half of a same-second pair",
			closeTimeMs:   1794662144999,
			wantUnix:      1794662144,
			wantFormatted: "2026-11-14T13:15:44.999Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdkEvent := &token_transfer.TokenTransferEvent{
				Meta: &token_transfer.EventMeta{
					LedgerSequence: 5727700,
					// Truncated, as the SDK supplies it. Nothing should read
					// this field for the close time any more.
					ClosedAt: timestamppb.New(time.UnixMilli(tt.closeTimeMs).Truncate(time.Second)),
					TxHash:   "abc123",
				},
			}

			event := convertEvent(sdkEvent, true, tt.closeTimeMs)
			require.NotNil(t, event)

			assert.Equal(t, tt.wantUnix, event.Meta.ClosedAtUnix)
			assert.Equal(t, tt.wantFormatted, event.Meta.ClosedAt)
		})
	}
}

// TestConvertEventCloseTimeRendersThreeDigits guards the layout. RFC3339Nano
// would render a whole second as "...44Z", so the field's shape would vary
// with its value and a consumer could read the short form as second
// resolution.
func TestConvertEventCloseTimeRendersThreeDigits(t *testing.T) {
	for _, ms := range []int64{1794662144000, 1794662144900, 1794662144916} {
		sdkEvent := &token_transfer.TokenTransferEvent{
			Meta: &token_transfer.EventMeta{ClosedAt: timestamppb.New(time.UnixMilli(ms))},
		}
		event := convertEvent(sdkEvent, true, ms)
		require.NotNil(t, event)
		assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`, event.Meta.ClosedAt)
	}
}
