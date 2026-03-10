package main

import (
	ttl_tracker "github.com/withObsrvr/nebu-processor-registry/processors/ttl-tracker"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

func main() {
	config := cli.OriginConfig{
		Name:        "ttl-tracker",
		Description: "Extract TTL changes from Stellar ledgers",
		Version:     version,
	}

	cli.RunProtoOriginCLI(config, func(networkPass string) cli.ProtoOriginProcessor[*ttl_tracker.TtlEvent] {
		return ttl_tracker.NewOrigin(networkPass)
	})
}
