// Package main provides a standalone CLI for the {NAME} processor.
//
// {DESCRIPTION}
//
// Origin processors emit typed protobuf events. Define your event
// shape in proto/{NAME_UNDERSCORED}.proto and generate Go code before
// building this binary. See BUILDING_PROTO_PROCESSORS.md for the full
// walkthrough.
package main

import (
	"context"
	"fmt"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
	"github.com/withObsrvr/nebu/pkg/processor/cli"

	// TODO: replace with your generated proto package path.
	pb "github.com/your-username/nebu-processor-{NAME}/proto"
)

var version = "0.1.0"

func main() {
	config := cli.OriginConfig{
		Name:        "{NAME}",
		Description: "{DESCRIPTION}",
		Version:     version,
		// SchemaID is the canonical identifier for the events this
		// processor emits. Bump the version suffix on breaking changes.
		SchemaID: "nebu.{NAME_UNDERSCORED}.v1",
	}

	// RunProtoOriginCLI handles:
	// - Connecting to RPC (--start-ledger, --end-ledger, --rpc-url)
	// - Running the origin through the nebu runtime
	// - Converting protobuf events to JSON via protojson
	// - The --describe-json introspection protocol (auto-generated
	//   JSON Schema from the *pb.YourEvent type)
	cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*pb.YourEvent] {
		return NewOrigin(networkPass)
	})
}

// Origin extracts {NAME} events from Stellar ledgers.
type Origin struct {
	passphrase string
	emitter    *processor.Emitter[*pb.YourEvent]
}

// NewOrigin creates a new {NAME} origin processor.
func NewOrigin(passphrase string) *Origin {
	return &Origin{
		passphrase: passphrase,
		// Buffer size of 1024 gives the emitter headroom so extraction
		// doesn't block on JSON serialization. Tune for your workload.
		emitter: processor.NewEmitter[*pb.YourEvent](1024),
	}
}

// Name implements processor.Processor.
func (o *Origin) Name() string { return "{NAME}" }

// Type implements processor.Processor.
func (o *Origin) Type() processor.Type { return processor.TypeOrigin }

// Out returns the output channel consumed by the runtime.
func (o *Origin) Out() <-chan *pb.YourEvent { return o.emitter.Out() }

// Close closes the emitter. Called by the runtime at shutdown.
func (o *Origin) Close() { o.emitter.Close() }

// ProcessLedger implements processor.Origin. It does not return an
// error — per-ledger failures are reported via processor.ReportWarning
// (see https://github.com/withObsrvr/nebu/blob/main/docs/STABILITY.md
// for the streams-never-throw contract).
func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) {
	reader, err := ingest.NewLedgerTransactionReaderFromLedgerCloseMeta(o.passphrase, ledger)
	if err != nil {
		processor.ReportWarning(ctx, o.Name(),
			fmt.Errorf("ledger %d: create tx reader: %w", ledger.LedgerSequence(), err))
		return
	}
	defer reader.Close()

	for {
		tx, err := reader.Read()
		if err != nil {
			break // End of transactions.
		}

		// TODO: extract your domain events from tx and populate pb.YourEvent.
		event := &pb.YourEvent{
			// TODO: fill in your fields.
		}

		select {
		case <-ctx.Done():
			return
		default:
			o.emitter.Emit(event)
		}
	}
}
