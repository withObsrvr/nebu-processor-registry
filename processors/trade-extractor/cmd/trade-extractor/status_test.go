package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRunWithProgramStatus(t *testing.T) {
	originalWriter := programStatusWriter
	originalTerminal := programStatusIsTerminal
	t.Cleanup(func() {
		programStatusWriter = originalWriter
		programStatusIsTerminal = originalTerminal
		programStatusCanceled.Store(false)
	})

	tests := []struct {
		name      string
		mode      string
		quiet     bool
		runErr    error
		canceled  bool
		wantState string
		wantEmpty bool
	}{
		{name: "success", mode: "always", wantState: "state=done"},
		{name: "error", mode: "always", runErr: errors.New("failed"), wantState: "state=error"},
		{name: "canceled", mode: "always", canceled: true, wantState: "state=idle"},
		{name: "quiet auto", mode: "auto", quiet: true, wantEmpty: true},
		{name: "never", mode: "never", wantEmpty: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			programStatusCanceled.Store(false)
			programStatusIsTerminal = func() bool { return true }
			var output bytes.Buffer
			programStatusWriter = &output

			err := runWithProgramStatus("test-app", tt.mode, tt.quiet, func() error {
				if tt.canceled {
					markProgramStatusCanceled("test-app", tt.mode, tt.quiet)
				}
				return tt.runErr
			})
			if !errors.Is(err, tt.runErr) {
				t.Fatalf("runWithProgramStatus() error = %v, want %v", err, tt.runErr)
			}
			if tt.wantEmpty {
				if output.Len() != 0 {
					t.Fatalf("status output = %q, want empty", output.String())
				}
				return
			}
			if !strings.Contains(output.String(), "state=working") || !strings.Contains(output.String(), tt.wantState) {
				t.Fatalf("status output = %q, want working and %s", output.String(), tt.wantState)
			}
		})
	}
}

func TestTerminalFileRejectsNonTerminalCharacterDevice(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devNull.Close() })
	if terminalFile(devNull) {
		t.Fatal("os.DevNull reported as a terminal")
	}
}

func TestRunWithProgramStatusRejectsInvalidMode(t *testing.T) {
	called := false
	err := runWithProgramStatus("test-app", "sometimes", false, func() error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "expected auto, always, or never") {
		t.Fatalf("error = %v, want invalid mode", err)
	}
	if called {
		t.Fatal("run function called for invalid mode")
	}
}
