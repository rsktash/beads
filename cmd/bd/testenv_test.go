package main

import "testing"

// isolateExpandState gives one test its own expand-dedupe state directory.
//
// The dedupe deliberately keys its state file on ${TMPDIR:-/tmp}/bd-expand
// plus the session id resolved from the environment (expand_state.go), which
// is right in a real session and wrong in a test binary two ways: a test reads
// the marks left by the developer's own bd runs, and every test in one binary
// shares the session, so one test's --expand marks a record for the next. A
// test that hits either renders the record as its id alone and fails on the
// missing text. Setting TMPDIR per test settles both.
//
// Every store fixture below calls this, so a new test inherits the isolation
// rather than having to remember it.
func isolateExpandState(t *testing.T) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
}
