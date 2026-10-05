package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// mkFindingStatement creates an active finding on the issue and returns its id.
func mkFindingStatement(t *testing.T, st *store.Store, issueID, text string) string {
	t.Helper()
	f := &beads.Statement{Kind: "finding", IssueID: &issueID, Text: text, FiledBy: "executor:tester", Status: "active", Scope: "inherit", Evidence: "file.go:1"}
	if err := st.CreateStatement(context.Background(), f); err != nil {
		t.Fatalf("create finding %q: %v", text, err)
	}
	return f.ID
}

// contractFindingIDs is the bead's FINDINGS block as bd show renders it.
func contractFindingIDs(t *testing.T, dsn, issueID string) []string {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	view, err := st.ContractStatements(ctx, issueID)
	if err != nil {
		t.Fatalf("contract: %v", err)
	}
	var ids []string
	for _, f := range view.Findings {
		ids = append(ids, f.ID)
	}
	return ids
}

func TestFindingClose_CitesRulingAndDropsOutOfActive(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "finding close")
	fID := mkFindingStatement(t, st, issue.ID, "the API returns 500 on empty input")
	rID := mkRulingStatement(t, st, issue.ID, "empty input is a 400")
	_ = st.Close()

	if ids := contractFindingIDs(t, dsn, issue.ID); len(ids) != 1 || ids[0] != fID {
		t.Fatalf("finding should be active before close, got %v", ids)
	}

	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runFindingCmd(t, []string{"close", fID, "--ruling", rID, "--note", "settled by the 400 ruling"})
	if err != nil {
		t.Fatalf("close should succeed: %v", err)
	}
	if strings.TrimSpace(out) != fID {
		t.Fatalf("close should print the finding id, got %q", out)
	}
	got := getStatementQF(t, dsn, fID)
	if got.Status != "answered" {
		t.Fatalf("closed finding status: want answered, got %s", got.Status)
	}
	if got.AnsweredBy == nil || *got.AnsweredBy != rID {
		t.Fatalf("closed finding must cite %s in answered_by, got %v", rID, got.AnsweredBy)
	}
	if !strings.HasPrefix(got.ClosedBy, "coordinator:") {
		t.Fatalf("closed_by must record the closing actor, got %q", got.ClosedBy)
	}
	if got.RetireNote != "settled by the 400 ruling" {
		t.Fatalf("note not recorded, got %q", got.RetireNote)
	}
	if got.Evidence != "file.go:1" || got.FiledBy != "executor:tester" {
		t.Fatalf("close must not touch provenance, got evidence %q filed_by %q", got.Evidence, got.FiledBy)
	}
	if got.ChangedAt == nil {
		t.Fatalf("close must stamp changed_at")
	}
	if ids := contractFindingIDs(t, dsn, issue.ID); len(ids) != 0 {
		t.Fatalf("closed finding must drop out of the FINDINGS block, got %v", ids)
	}
	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	active, err := st2.ListStatements(ctx, store.StatementFilter{Kinds: []string{"finding"}, Statuses: []string{"active"}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("closed finding must not list as active, got %d", len(active))
	}
}

func TestFindingClose_NoteIsOptional(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "no note")
	fID := mkFindingStatement(t, st, issue.ID, "a finding")
	rID := mkRulingStatement(t, st, issue.ID, "a ruling")
	_ = st.Close()

	asOwner(t)
	if _, _, err := runFindingCmd(t, []string{"close", fID, "--ruling", rID}); err != nil {
		t.Fatalf("close without --note should succeed: %v", err)
	}
	got := getStatementQF(t, dsn, fID)
	if got.Status != "answered" || !strings.HasPrefix(got.ClosedBy, "owner:") {
		t.Fatalf("owner close: status %s closed_by %q", got.Status, got.ClosedBy)
	}
}

func TestFindingClose_Refusals(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "refusals")
	fID := mkFindingStatement(t, st, issue.ID, "a finding")
	other := mkFindingStatement(t, st, issue.ID, "another finding")
	rID := mkRulingStatement(t, st, issue.ID, "a ruling")
	qID := mkQuestion(t, st, issue.ID, "a question")
	_ = st.Close()
	asOwner(t)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing ruling flag", []string{"close", fID}, "--ruling is required"},
		{"nonexistent ruling", []string{"close", fID, "--ruling", "R-9999"}, "ruling R-9999 not found"},
		{"ruling names a finding", []string{"close", fID, "--ruling", other}, "is not a ruling"},
		{"nonexistent finding", []string{"close", "F-9999", "--ruling", rID}, "finding F-9999 not found"},
		{"target is a question", []string{"close", qID, "--ruling", rID}, "is not a finding"},
	}
	for _, c := range cases {
		_, _, err := runFindingCmd(t, c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
	}
	if got := getStatementQF(t, dsn, fID); got.Status != "active" || got.AnsweredBy != nil {
		t.Fatalf("refused closes must write nothing, got %+v", got)
	}

	if _, _, err := runFindingCmd(t, []string{"close", fID, "--ruling", rID}); err != nil {
		t.Fatalf("first close should succeed: %v", err)
	}
	_, _, err := runFindingCmd(t, []string{"close", fID, "--ruling", rID})
	if err == nil || !strings.Contains(err.Error(), "already closed") {
		t.Fatalf("second close must be refused as already closed, got %v", err)
	}
}

func TestFindingClose_ExecutorRefusedAndNothingWritten(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "executor refused")
	fID := mkFindingStatement(t, st, issue.ID, "a finding")
	rID := mkRulingStatement(t, st, issue.ID, "a ruling")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "executor")
	_, _, err := runFindingCmd(t, []string{"close", fID, "--ruling", rID})
	if err == nil {
		t.Fatalf("executor must be refused")
	}
	if err.Error() != executorRefusal("executor:x").Error() {
		t.Fatalf("refusal must be the shared executor refusal, got %q", err.Error())
	}
	got := getStatementQF(t, dsn, fID)
	if got.Status != "active" || got.AnsweredBy != nil || got.ClosedBy != "" {
		t.Fatalf("refused close must write nothing, got %+v", got)
	}
}
