package main

import (
	contract_state "github.com/withObsrvr/nebu-processor-registry/processors/contract-state"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

func main() {
	config := cli.OriginConfig{
		Name:        "contract-state",
		Description: "Extract contract data state changes from Stellar ledgers",
		Version:     version,
		SchemaID:    "nebu.contract_state.v1",
	}

	cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*contract_state.ContractStateEvent] {
		return contract_state.NewOrigin(networkPass)
	})
}
