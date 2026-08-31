package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsktash/beads/internal/config"
)

// TestRemedy_SchemaMismatchNamesNextStep covers R2's schema-mismatch class:
// a raw driver "column ... could not be found" style error must name the
// next command. `bd migrate` in this codebase is upstream-Dolt import only
// (no --inspect flag exists), so the remedy points at the diagnostic that
// actually exists: `bd schema list` (applied + pending migrations), then
// `bd schema apply`.
func TestRemedy_SchemaMismatchNamesNextStep(t *testing.T) {
	cases := []error{
		errors.New(`sql: column "foo" could not be found`),
		errors.New(`no such column: bar`),
		errors.New(`pq: column "baz" does not exist`),
	}
	for _, in := range cases {
		got := remedyForError(in)
		if got == nil {
			t.Fatalf("remedyForError(%v) returned nil", in)
		}
		if !strings.Contains(got.Error(), "bd schema list") {
			t.Fatalf("schema-mismatch error should name `bd schema list`, got %q", got.Error())
		}
	}
}

// TestRemedy_NotFoundNamesResolvedDSNAndRemedy covers R2's DB-resolution
// class for the case where resolution succeeded but the id genuinely (or
// due to an unintended DB) isn't there: the wrapped error must say which
// DSN was resolved and name the remedy.
func TestRemedy_NotFoundNamesResolvedDSNAndRemedy(t *testing.T) {
	old := lastResolvedDSN
	lastResolvedDSN = "/tmp/somewhere/.bd/bd.db"
	t.Cleanup(func() { lastResolvedDSN = old })

	got := remedyForError(errors.New("not found"))
	if got == nil {
		t.Fatalf("remedyForError returned nil")
	}
	msg := got.Error()
	if !strings.Contains(msg, lastResolvedDSN) {
		t.Fatalf("not-found remedy should name the resolved DSN %q, got %q", lastResolvedDSN, msg)
	}
	if !strings.Contains(msg, "repo root") || !strings.Contains(msg, "--db") {
		t.Fatalf("not-found remedy should name the repo-root/--db remedy, got %q", msg)
	}
}

// TestRemedy_NotFoundEndToEnd exercises the real plumbing: openStore sets
// lastResolvedDSN, a genuine GetIssue "not found" bubbles up through a
// command, and remedyForError (the same call main.go makes) enriches it.
func TestRemedy_NotFoundEndToEnd(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "not found fixture")
	_ = issue
	_ = st.Close()

	_, _, err := runRulingsCmd(t, []string{"bd-does-not-exist"})
	if err == nil {
		t.Fatalf("unknown issue id should fail")
	}
	got := remedyForError(err)
	if !strings.Contains(got.Error(), dsn) {
		t.Fatalf("wrapped not-found error should name the resolved DSN %q, got %q", dsn, got.Error())
	}
	if !strings.Contains(got.Error(), "--db") {
		t.Fatalf("wrapped not-found error should name --db as a remedy, got %q", got.Error())
	}
}

// TestRemedy_NilPassesThrough guards the no-op case: nothing to wrap.
func TestRemedy_NilPassesThrough(t *testing.T) {
	if remedyForError(nil) != nil {
		t.Fatalf("remedyForError(nil) should return nil")
	}
}

// TestRemedy_UnmatchedErrorUnchanged guards against over-matching: an error
// unrelated to schema drift or not-found must pass through verbatim.
func TestRemedy_UnmatchedErrorUnchanged(t *testing.T) {
	in := errors.New("--defer and --close are mutually exclusive")
	got := remedyForError(in)
	if got.Error() != in.Error() {
		t.Fatalf("unrelated error should pass through unchanged, got %q", got.Error())
	}
}

// TestConfig_NoBeadDirNamesRemedies covers R2's DB-resolution class for the
// case where resolution finds nothing at all: the error must name the repo
// root and --db remedies (bd init alone isn't enough — it also covers the
// "ran from a subdirectory" trap).
func TestConfig_NoBeadDirNamesRemedies(t *testing.T) {
	err := config.NoBeadDirError()
	if err == nil {
		t.Fatalf("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"bd init", "repo root", "--db"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("no-.bd-dir error should name %q, got %q", want, msg)
		}
	}
}

// TestConfig_ResolveFailureUsesNoBeadDirError exercises Resolve itself (not
// just the exported helper) against a directory with no .bd anywhere in its
// parent chain and outside any git repo.
func TestConfig_ResolveFailureUsesNoBeadDirError(t *testing.T) {
	dir := t.TempDir()
	// A bare subdirectory of a TempDir, guaranteed to have no .bd upward and
	// (t.TempDir() is not inside a git worktree in CI/sandboxed test runs)
	// no git repo to fall back to.
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(sub)
	_, err := config.Resolve("")
	if err == nil {
		t.Skip("environment has a discoverable .bd or git root above the temp dir; resolution succeeded")
	}
	msg := err.Error()
	for _, want := range []string{"bd init", "repo root", "--db"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Resolve failure should name %q, got %q", want, msg)
		}
	}
}
