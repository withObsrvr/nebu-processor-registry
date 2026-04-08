// --describe-json protocol implementation.
//
// This is a local copy of the describe protocol logic. It is
// duplicated across processors that use a custom cobra setup
// instead of nebu's pkg/processor/cli helpers (RunProtoOriginCLI,
// RunTransformCLI, RunSinkCLI), which wire --describe-json
// automatically. When nebu exports BuildOriginEnvelope /
// EmitDescribeIfRequested from pkg/processor/cli (likely v0.5.1+),
// this file can be collapsed to a one-line call.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/withObsrvr/nebu/pkg/processor"
)

const describeFlagName = "describe-json"

// isDescribeJSONRequested scans os.Args for --describe-json without
// going through cobra. Must be called before cobra.Execute() —
// cobra's unknown-flag validation would otherwise reject the flag.
// Both bare (--describe-json) and value-bearing (--describe-json=true)
// forms are accepted, matching how pflag parses the flag.
func isDescribeJSONRequested() bool {
	flag := "--" + describeFlagName
	flagEq := flag + "="
	for _, arg := range os.Args[1:] {
		if arg == flag {
			return true
		}
		if val, ok := strings.CutPrefix(arg, flagEq); ok {
			b, err := strconv.ParseBool(val)
			return err == nil && b
		}
	}
	return false
}

// emitDescribeIfRequested checks for --describe-json and, if set,
// builds the envelope via buildEnvelope, writes it to stdout, and
// exits 0. Otherwise returns so the caller proceeds to cobra.Execute.
func emitDescribeIfRequested(cmd *cobra.Command, buildEnvelope func() processor.DescribeEnvelope) {
	if !isDescribeJSONRequested() {
		return
	}
	env := buildEnvelope()
	env.Flags = collectFlagInfo(cmd)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// collectFlagInfo walks cobra's registered flags into DescribeFlag.
// Mirrors pkg/processor/cli's private collectFlagInfo helper so the
// describe output is byte-consistent with helper-based processors.
func collectFlagInfo(cmd *cobra.Command) []processor.DescribeFlag {
	reserved := map[string]bool{
		describeFlagName: true,
		"help":           true,
		"version":        true,
	}
	var flags []processor.DescribeFlag
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || reserved[f.Name] {
			return
		}
		required := false
		if f.Annotations != nil {
			if v, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; ok && len(v) > 0 {
				required = v[0] == "true"
			}
		}
		flags = append(flags, processor.DescribeFlag{
			Name:        f.Name,
			Type:        f.Value.Type(),
			Required:    required,
			Description: f.Usage,
			Default:     f.DefValue,
		})
	})
	return flags
}
