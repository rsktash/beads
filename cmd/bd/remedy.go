package main

import (
	"fmt"
	"strings"
)

// lastResolvedDSN is the display-safe DSN (no password) from the most
// recent successful openStore call. It is package state, not a cmdCtx
// field, because the one place that needs it — remedyForError, at the
// single point every command's error funnels through before printing — runs
// after the command has already returned and its cmdCtx is gone.
var lastResolvedDSN string

// remedyForError appends the next command a dispatched agent should run to
// error classes it can hit in normal operation but can't resolve from the
// bare message alone. It is the single choke point every command error
// passes through (main.go, immediately before printing) so no individual
// command composes this text itself.
//
// Two classes handled here:
//   - schema-mismatch: a raw driver "column ... not found/does not exist"
//     error, which means the binary's schema is ahead of the DB it opened.
//   - not-found on a resolved DB: names the DSN that was actually resolved
//     so a dispatched agent can tell a genuinely-missing id apart from the
//     "ran from a subdirectory, resolved an unintended DB" trap.
//
// DB-resolution failure itself (no DSN could be resolved at all) is not
// handled here — internal/config.NoBeadDirError composes that message at
// the source, since remedyForError never sees a DSN to report in that case.
func remedyForError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case looksLikeSchemaMismatch(low):
		return fmt.Errorf("%s — run `bd schema list` to check for schema drift, then `bd schema apply`", msg)
	case looksLikeNotFound(low) && lastResolvedDSN != "":
		return fmt.Errorf("%s (resolved DSN: %s — if this looks wrong, run bd from the repo root, or pass --db)", msg, lastResolvedDSN)
	}
	return err
}

func looksLikeSchemaMismatch(low string) bool {
	if !strings.Contains(low, "column") {
		return false
	}
	return strings.Contains(low, "could not be found") ||
		strings.Contains(low, "no such column") ||
		strings.Contains(low, "does not exist")
}

func looksLikeNotFound(low string) bool {
	return strings.Contains(low, "not found")
}
