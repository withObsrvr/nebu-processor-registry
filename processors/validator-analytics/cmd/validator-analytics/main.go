package main

import (
	validator_analytics "github.com/withObsrvr/nebu-processor-registry/processors/validator-analytics"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

func main() {
	config := cli.OriginConfig{
		Name:        "validator-analytics",
		Description: "Extract per-ledger validator attribution and operation activity facts",
		Version:     version,
		SchemaID:    "nebu.validator_analytics.v1",
	}

	cli.RunProtoOriginCLI(config, func(networkPassphrase string) cli.ProtoOriginProcessor[*validator_analytics.ValidatorLedgerAnalytics] {
		return validator_analytics.NewOrigin(networkPassphrase)
	})
}
