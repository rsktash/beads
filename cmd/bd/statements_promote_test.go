package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// runStatementsViaRoot runs `bd statements <args...>` via the root command.
func runStatementsViaRoot(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	savedDB := flagDB
	savedJSON := flagJSON
	root := newRoot()
	flagDB = savedDB
	flagJSON = savedJSON
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	fullArgs := append([]string{"statements"}, args...)
	root.SetArgs(fullArgs)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

// mkCandidate creates a candidate ruling linked to an issue and a real source
// comment, then returns it.
func mkCandidate(t *testing.T, st *store.Store, issueID, text string) *beads.Statement {
	t.Helper()
	c := addBackfillComment(t, st, issueID, text)
	iid := issueID
	src := c.ID
	stmt := &beads.Statement{
		Kind:            "ruling",
		IssueID:         &iid,
		Text:            text,
		FiledBy:         "owner:tester",
		Status:          "candidate",
		Scope:           "inherit",
		SourceCommentID: &src,
	}
	if err := st.CreateStatement(context.Background(), stmt); err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	return stmt
}

func getStatement(t *testing.T, dsn, id string) *beads.Statement {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	got, err := st.GetStatement(ctx, id)
	if err != nil {
		t.Fatalf("get statement %s: %v", id, err)
	}
	return got
}

// Owner (BD_ACTOR unset) may promote a candidate; it flips to active.
func TestPromote_OwnerAllowed(t *testing.T) {
	dsn, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "promote owner")
	cand := mkCandidate(t, st, iss.ID, "OWNER RULING promote me")
	_ = st.Close()

	out, _, err := runStatementsViaRoot(t, []string{"promote", cand.ID})
	if err != nil {
		t.Fatalf("promote: %v out %q", err, out)
	}
	if !strings.Contains(out, cand.ID) {
		t.Fatalf("output should name the promoted id, got %q", out)
	}
	got := getStatement(t, dsn, cand.ID)
	if got.Status != "active" {
		t.Fatalf("status should be active after promote, got %q", got.Status)
	}
}

// BD_ACTOR=coordinator may promote (not an executor).
func TestPromote_CoordinatorAllowed(t *testing.T) {
	dsn, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "promote coordinator")
	cand := mkCandidate(t, st, iss.ID, "RULED: coordinator promote")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runStatementsViaRoot(t, []string{"promote", cand.ID})
	if err != nil {
		t.Fatalf("promote: %v out %q", err, out)
	}
	got := getStatement(t, dsn, cand.ID)
	if got.Status != "active" {
		t.Fatalf("status should be active after coordinator promote, got %q", got.Status)
	}
}

// BD_ACTOR=executor is refused; nothing mutates.
func TestPromote_ExecutorRefused(t *testing.T) {
	dsn, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "promote executor")
	cand := mkCandidate(t, st, iss.ID, "DEFERRED executor promote")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "executor")
	_, _, err := runStatementsViaRoot(t, []string{"promote", cand.ID})
	if err == nil {
		t.Fatalf("executor promote should error")
	}
	if !strings.Contains(err.Error(), "executor") {
		t.Fatalf("error should name executor gating, got %q", err.Error())
	}
	got := getStatement(t, dsn, cand.ID)
	if got.Status != "candidate" {
		t.Fatalf("status must stay candidate after refused promote, got %q", got.Status)
	}
}

// Promoting a non-candidate errors and does not mutate.
func TestPromote_NonCandidateErrors(t *testing.T) {
	dsn, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "promote active")
	cand := mkCandidate(t, st, iss.ID, "OWNER RULING already active")
	// Flip to active out-of-band.
	if err := st.UpdateStatementStatus(context.Background(), cand.ID, "active"); err != nil {
		t.Fatalf("pre-activate: %v", err)
	}
	_ = st.Close()

	_, _, err := runStatementsViaRoot(t, []string{"promote", cand.ID})
	if err == nil {
		t.Fatalf("promoting an already-active statement should error")
	}
	if !strings.Contains(err.Error(), "candidate") {
		t.Fatalf("error should mention candidate, got %q", err.Error())
	}
	got := getStatement(t, dsn, cand.ID)
	if got.Status != "active" {
		t.Fatalf("status unchanged expected active, got %q", got.Status)
	}
}

// Promoting a missing statement errors clearly.
func TestPromote_NotFound(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	_ = st.Close()
	_, _, err := runStatementsViaRoot(t, []string{"promote", "R-999"})
	if err == nil {
		t.Fatalf("promoting a missing statement should error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error should say not found, got %q", err.Error())
	}
}

// --json emits the promoted statement with active status.
func TestPromote_JSON(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "promote json")
	cand := mkCandidate(t, st, iss.ID, "RULED: json promote")
	_ = st.Close()

	out, _, err := runStatementsViaRoot(t, []string{"promote", cand.ID, "--json"})
	if err != nil {
		t.Fatalf("promote json: %v out %q", err, out)
	}
	var got beads.Statement
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json unmarshal: %v out %q", err, out)
	}
	if got.ID != cand.ID {
		t.Fatalf("json id mismatch, got %q want %q", got.ID, cand.ID)
	}
	if got.Status != "active" {
		t.Fatalf("json status should be active, got %q", got.Status)
	}
}

// After promotion the ruling appears in bd rulings (active list).
func TestPromote_AppearsInRulings(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "promote rulings")
	cand := mkCandidate(t, st, iss.ID, "OWNER RULING becomes active")
	_ = st.Close()

	if _, _, err := runStatementsViaRoot(t, []string{"promote", cand.ID}); err != nil {
		t.Fatalf("promote: %v", err)
	}

	// List active rulings via the rulings command (project-wide).
	savedDB := flagDB
	root := newRoot()
	flagDB = savedDB
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"rulings"})
	if err := root.Execute(); err != nil {
		t.Fatalf("rulings: %v", err)
	}
	if !strings.Contains(buf.String(), cand.ID) {
		t.Fatalf("promoted ruling %s should appear in bd rulings, got %q", cand.ID, buf.String())
	}
}
