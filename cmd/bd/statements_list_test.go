package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rsktash/beads"
)

// Default list shows candidate statements with their source comment id.
func TestStatementsList_DefaultCandidates(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "list candidates")
	c1 := mkCandidate(t, st, iss.ID, "OWNER RULING first candidate")
	c2 := mkCandidate(t, st, iss.ID, "RULED: second candidate")
	_ = st.Close()

	out, _, err := runStatementsViaRoot(t, []string{"list"})
	if err != nil {
		t.Fatalf("list: %v out %q", err, out)
	}
	if !strings.Contains(out, c1.ID) || !strings.Contains(out, c2.ID) {
		t.Fatalf("list should show both candidate ids, got %q", out)
	}
	// Source comment id must appear.
	if c1.SourceCommentID == nil || !strings.Contains(out, *c1.SourceCommentID) {
		t.Fatalf("list should show source comment id, got %q", out)
	}
}

// --candidates explicitly is the same default view.
func TestStatementsList_CandidatesFlag(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "list flag")
	cand := mkCandidate(t, st, iss.ID, "DEFERRED via flag")
	_ = st.Close()

	out, _, err := runStatementsViaRoot(t, []string{"list", "--candidates"})
	if err != nil {
		t.Fatalf("list --candidates: %v out %q", err, out)
	}
	if !strings.Contains(out, cand.ID) {
		t.Fatalf("--candidates should show candidate, got %q", out)
	}
}

// An issue id scopes the list to that bead.
func TestStatementsList_ScopedToIssue(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss1 := mkBackfillIssue(t, st, "scoped one")
	iss2 := mkBackfillIssue(t, st, "scoped two")
	c1 := mkCandidate(t, st, iss1.ID, "OWNER RULING in one")
	c2 := mkCandidate(t, st, iss2.ID, "OWNER RULING in two")
	_ = st.Close()

	out, _, err := runStatementsViaRoot(t, []string{"list", iss1.ID})
	if err != nil {
		t.Fatalf("scoped list: %v out %q", err, out)
	}
	if !strings.Contains(out, c1.ID) {
		t.Fatalf("scoped list should show candidate in target issue, got %q", out)
	}
	if strings.Contains(out, c2.ID) {
		t.Fatalf("scoped list should not show candidate from other issue, got %q", out)
	}
}

// --json emits the full candidate rows.
func TestStatementsList_JSON(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "list json")
	mkCandidate(t, st, iss.ID, "RULED: json row")
	_ = st.Close()

	out, _, err := runStatementsViaRoot(t, []string{"list", "--json"})
	if err != nil {
		t.Fatalf("list json: %v out %q", err, out)
	}
	var arr []beads.Statement
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("json unmarshal: %v out %q", err, out)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 row, got %d out %q", len(arr), out)
	}
	if arr[0].Status != "candidate" {
		t.Fatalf("row status should be candidate, got %q", arr[0].Status)
	}
	if arr[0].SourceCommentID == nil {
		t.Fatalf("row should carry source_comment_id")
	}
}

// --status active shows promoted/active rows, not candidates.
func TestStatementsList_StatusFilter(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "list status")
	cand := mkCandidate(t, st, iss.ID, "OWNER RULING candidate row")
	active := mkCandidate(t, st, iss.ID, "OWNER RULING active row")
	if err := st.UpdateStatementStatus(context.Background(), active.ID, "active"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	_ = st.Close()

	out, _, err := runStatementsViaRoot(t, []string{"list", "--status", "active"})
	if err != nil {
		t.Fatalf("list --status active: %v out %q", err, out)
	}
	if !strings.Contains(out, active.ID) {
		t.Fatalf("--status active should show active row, got %q", out)
	}
	if strings.Contains(out, cand.ID) {
		t.Fatalf("--status active should not show candidate row, got %q", out)
	}
}

// list appears under bd statements --help.
func TestStatementsList_HelpAppears(t *testing.T) {
	out, _, _ := runStatementsViaRoot(t, []string{"--help"})
	if !strings.Contains(out, "list") {
		t.Fatalf("bd statements --help should list the list command, got %q", out)
	}
	if !strings.Contains(out, "promote") {
		t.Fatalf("bd statements --help should list the promote command, got %q", out)
	}
}
