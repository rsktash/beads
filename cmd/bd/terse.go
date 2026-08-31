package main

import (
	"io"
	"os"
	"strings"
)

// EnvTerse forces terse render mode on ("1"/"true") or off ("0"/"false"),
// overriding the auto-detect. Unset falls through to auto: terse when
// stdout is not a TTY (a dispatched agent's stdout is always piped/
// captured), unchanged (verbose) for a human terminal.
const EnvTerse = "BD_TERSE"

// wantTerse reports whether the current command should render its terse,
// agent-facing form: one line per row, no decorative headers or banners.
// w is the command's configured output writer (cmd.OutOrStdout()), not a
// hard-coded os.Stdout, so tests that redirect output through a buffer get
// a deterministic, non-TTY answer without needing the env override.
func wantTerse(w io.Writer) bool {
	switch strings.TrimSpace(os.Getenv(EnvTerse)) {
	case "1", "true":
		return true
	case "0", "false":
		return false
	}
	return !isTerminalWriter(w)
}

// isTerminalWriter reports whether w is a character-device file (a real
// terminal), the no-dependency equivalent of isatty. Any writer that isn't
// an *os.File — a bytes.Buffer in tests, a pipe, a redirected file — is
// treated as non-terminal.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
