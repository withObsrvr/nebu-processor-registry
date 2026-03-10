package main

import (
	liquidity_pool_events "github.com/withObsrvr/nebu-processor-registry/processors/liquidity-pool-events"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

func main() {
	config := cli.OriginConfig{
		Name:        "liquidity-pool-events",
		Description: "Extract liquidity pool operations from Stellar ledgers",
		Version:     version,
	}

	cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*liquidity_pool_events.LiquidityPoolEvent] {
		return liquidity_pool_events.NewOrigin(networkPass)
	})
}
