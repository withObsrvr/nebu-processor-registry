package main

import (
	account_effects "github.com/withObsrvr/nebu-processor-registry/processors/account-effects"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

func main() {
	config := cli.OriginConfig{
		Name:        "account-effects",
		Description: "Extract account-level effects from Stellar ledgers",
		Version:     version,
		SchemaID:    "nebu.account_effects.v1",
	}

	cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*account_effects.AccountEffect] {
		return account_effects.NewOrigin(networkPass)
	})
}
