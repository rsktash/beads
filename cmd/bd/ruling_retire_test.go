package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func runRulingRetire(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	return runRulingAdd(t, args)
}

func TestRetire_FlipsStatusNoNewRow(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "retire flips status")
	rID := mkRulingStatement(t, st, issue.ID, "active ruling")
	_ = st.Close()

	before := countStatements(t, dsn)
	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingRetire(t, []string{"retire", rID, "--note", "no longer needed"})
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	after := countStatements(t, dsn)
	if after != before {
		t.Fatalf("retire should mint no new row, before %d after %d", before, after)
	}

	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	got, err := st2.GetStatement(ctx, rID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "retired" {
		t.Fatalf("expected status retired, got %s", got.Status)
	}
}

func TestRetire_StoresNote(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "retire stores note")
	rID := mkRulingStatement(t, st, issue.ID, "active ruling")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingRetire(t, []string{"retire", rID, "--note", "spent approval"})
	if err != nil {
		t.Fatalf("retire: %v", err)
	}

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	var note string
	if err := st2.DB().QueryRow("SELECT retire_note FROM statements WHERE id = ?", rID).Scan(&note); err != nil {
		t.Fatalf("select retire_note: %v", err)
	}
	if note != "spent approval" {
		t.Fatalf("expected retire_note %q, got %q", "spent approval", note)
	}
}

func TestRetire_RefusesExecutor(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "retire refuses executor")
	rID := mkRulingStatement(t, st, issue.ID, "active ruling")
	_ = st.Close()

	before := countStatements(t, dsn)
	t.Setenv("BD_ACTOR", "executor")
	_, _, err := runRulingRetire(t, []string{"retire", rID, "--note", "should be refused"})
	if err == nil {
		t.Fatalf("expected executor refusal")
	}
	if !strings.Contains(err.Error(), "executors cannot file rulings") {
		t.Fatalf("error should name executor refusal, got %q", err.Error())
	}
	after := countStatements(t, dsn)
	if after != before {
		t.Fatalf("refusal should write nothing, before %d after %d", before, after)
	}
}

func TestRetire_RequiresNote(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "retire requires note")
	rID := mkRulingStatement(t, st, issue.ID, "active ruling")
	_ = st.Close()
	_ = dsn

	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingRetire(t, []string{"retire", rID})
	if err == nil {
		t.Fatalf("expected error for missing --note")
	}
	if err.Error() != "--note is required" {
		t.Fatalf("expected %q, got %q", "--note is required", err.Error())
	}
}

func TestRetire_RefusesQuestion(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "retire refuses question")
	ctx := context.Background()
	q := &beads.Statement{Kind: "question", IssueID: &issue.ID, Text: "not a ruling", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create question: %v", err)
	}
	_ = st.Close()
	_ = dsn

	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingRetire(t, []string{"retire", q.ID, "--note", "not a ruling"})
	if err == nil {
		t.Fatalf("expected error retiring a question")
	}
	if !strings.Contains(err.Error(), "not a ruling") {
		t.Fatalf("error should name 'not a ruling', got %q", err.Error())
	}
}

func TestRetire_RefusesTwice(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "retire refuses twice")
	rID := mkRulingStatement(t, st, issue.ID, "active ruling")
	_ = st.Close()
	_ = dsn

	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingRetire(t, []string{"retire", rID, "--note", "first retire"})
	if err != nil {
		t.Fatalf("first retire: %v", err)
	}
	_, _, err = runRulingRetire(t, []string{"retire", rID, "--note", "second retire"})
	if err == nil {
		t.Fatalf("expected error retiring twice")
	}
	if !strings.Contains(err.Error(), "already retired") {
		t.Fatalf("error should name 'already retired', got %q", err.Error())
	}
}

func TestRetire_NeverRendersInShow(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "retire never renders in show")
	keepID := mkRulingStatement(t, st, issue.ID, "kept ruling")
	retireID := mkRulingStatement(t, st, issue.ID, "retired ruling")
	_ = dsn

	t.Setenv("BD_ACTOR", "coordinator")
	if err := st.RetireRuling(context.Background(), retireID, "gone"); err != nil {
		t.Fatalf("retire: %v", err)
	}

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	if strings.Contains(out, retireID) {
		t.Fatalf("retired ruling %s should not render in show, got:\n%s", retireID, out)
	}
	if !strings.Contains(out, keepID) {
		t.Fatalf("active ruling %s should render in show, got:\n%s", keepID, out)
	}
	st.Close()
}

func TestRetire_NeverRendersInRulings(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "retire never renders in rulings")
	keepID := mkRulingStatement(t, st, issue.ID, "kept ruling for rulings list")
	retireID := mkRulingStatement(t, st, issue.ID, "retired ruling for rulings list")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingRetire(t, []string{"retire", retireID, "--note", "gone"})
	if err != nil {
		t.Fatalf("retire: %v", err)
	}

	out, _, err := runRulingsCmd(t, []string{})
	if err != nil {
		t.Fatalf("rulings: %v", err)
	}
	if strings.Contains(out, retireID) {
		t.Fatalf("retired ruling %s should not appear in bd rulings, got:\n%s", retireID, out)
	}
	if !strings.Contains(out, keepID) {
		t.Fatalf("active ruling %s should appear in bd rulings, got:\n%s", keepID, out)
	}
	_ = dsn
}

func TestRetire_VisibleViaStatementsList(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "retire visible via statements list")
	rID := mkRulingStatement(t, st, issue.ID, "ruling to retire")
	_ = st.Close()
	_ = dsn

	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingRetire(t, []string{"retire", rID, "--note", "gone but readable"})
	if err != nil {
		t.Fatalf("retire: %v", err)
	}

	root := newStatementsListCmd()
	bufOut := &strings.Builder{}
	root.SetOut(bufOut)
	root.SetArgs([]string{"--status", "retired"})
	if err := root.Execute(); err != nil {
		t.Fatalf("statements list: %v", err)
	}
	if !strings.Contains(bufOut.String(), rID) {
		t.Fatalf("retired ruling %s should be listed by bd statements list --status retired, got:\n%s", rID, bufOut.String())
	}
}
