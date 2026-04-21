package main

import (
	contract_created "github.com/withObsrvr/nebu-processor-registry/processors/contract-created"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

func main() {
	config := cli.OriginConfig{
		Name:        "contract-created",
		Description: "Extract contract creation events from Stellar ledgers",
		Version:     version,
		SchemaID:    "nebu.contract_created.v1",
	}

	cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*contract_created.ContractCreatedEvent] {
		return contract_created.NewOrigin(networkPass)
	})
}
