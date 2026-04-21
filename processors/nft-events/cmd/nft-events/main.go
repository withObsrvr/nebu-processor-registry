package main

import (
	nft_events "github.com/withObsrvr/nebu-processor-registry/processors/nft-events"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

func main() {
	config := cli.OriginConfig{
		Name:        "nft-events",
		Description: "Extract NFT-like contract events and calls from Stellar ledgers",
		Version:     version,
		SchemaID:    "nebu.nft_events.v1",
	}

	cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*nft_events.NftEvent] {
		return nft_events.NewOrigin(networkPass)
	})
}
