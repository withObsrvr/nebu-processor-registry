package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"unicode"

	"golang.org/x/term"
)

var (
	programStatusCanceled   atomic.Bool
	programStatusWriter     io.Writer = os.Stderr
	programStatusIsTerminal           = stderrIsTerminal
)

func runWithProgramStatus(app, mode string, quiet bool, run func() error) error {
	if err := validateProgramStatusMode(mode); err != nil {
		return err
	}
	writeProgramStatus(app, mode, quiet, "working", "Running "+app)
	err := run()
	switch {
	case programStatusCanceled.Load():
		// The signal handler reports idle immediately.
	case errors.Is(err, context.Canceled):
		writeProgramStatus(app, mode, quiet, "idle", app+" canceled")
	case err != nil:
		writeProgramStatus(app, mode, quiet, "error", statusMessage(err.Error()))
	default:
		writeProgramStatus(app, mode, quiet, "done", app+" finished")
	}
	return err
}

func markProgramStatusCanceled(app, mode string, quiet bool) {
	if programStatusCanceled.CompareAndSwap(false, true) {
		writeProgramStatus(app, mode, quiet, "idle", app+" canceled")
	}
}

func programStatusDefault() string {
	if value := os.Getenv("NEBU_PROGRAM_STATUS"); value != "" {
		return value
	}
	return "auto"
}

func validateProgramStatusMode(mode string) error {
	switch mode {
	case "auto", "always", "never":
		return nil
	default:
		return fmt.Errorf("invalid --program-status value %q: expected auto, always, or never", mode)
	}
}

func writeProgramStatus(app, mode string, quiet bool, state, message string) {
	if mode == "never" || (quiet && mode != "always") {
		return
	}
	if mode == "auto" && !programStatusIsTerminal() {
		return
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(message))
	if _, err := fmt.Fprintf(programStatusWriter, "\x1b]7501;state=%s:app=%s:msg=%s\x1b\\", state, app, encoded); err != nil {
		_ = err // Status reporting must not make the processor fail.
	}
}

func statusMessage(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	if len(value) > 2048 {
		return strings.ToValidUTF8(value[:2048], "")
	}
	return value
}

func stderrIsTerminal() bool {
	return terminalFile(os.Stderr)
}

func terminalFile(file *os.File) bool {
	return os.Getenv("TERM") != "dumb" && term.IsTerminal(int(file.Fd()))
}
