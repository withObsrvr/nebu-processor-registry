package main

import (
	soroban_tx_resources "github.com/withObsrvr/nebu-processor-registry/processors/soroban-tx-resources"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

func main() {
	config := cli.OriginConfig{
		Name:        "soroban-tx-resources",
		Description: "Extract per-transaction Soroban resource footprints, result codes, fees, and envelope sizes",
		Version:     version,
		SchemaID:    "nebu.soroban_tx_resources.v1",
	}

	cli.RunProtoOriginCLI(config, func(networkPassphrase string) cli.ProtoOriginProcessor[*soroban_tx_resources.SorobanTxResources] {
		return soroban_tx_resources.NewOrigin(networkPassphrase)
	})
}
