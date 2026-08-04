package main

import (
	ledger_change_stats "github.com/withObsrvr/nebu-processor-registry/processors/ledger-change-stats"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.2.0"

func main() {
	config := cli.OriginConfig{
		Name:        "ledger-change-stats",
		Description: "Count ledger entry changes by change type, reason, and entry type, plus evictions",
		Version:     version,
		SchemaID:    "nebu.ledger_change_stats.v1",
	}

	cli.RunProtoOriginCLI(config, func(networkPassphrase string) cli.ProtoOriginProcessor[*ledger_change_stats.LedgerChangeStats] {
		return ledger_change_stats.NewOrigin(networkPassphrase)
	})
}
