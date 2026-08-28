package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// mkStatementQF creates a statement of any kind on the issue and returns its id.
func mkStatementQF(t *testing.T, st *store.Store, issueID, kind, text string) string {
	t.Helper()
	s := &beads.Statement{Kind: kind, IssueID: &issueID, Text: text, FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), s); err != nil {
		t.Fatalf("create %s %q: %v", kind, text, err)
	}
	return s.ID
}

func TestQuestionAnswerFinding_SetsLinkAndUnblocks(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "evidence settles it")
	qID := mkQuestion(t, st, issue.ID, "is production's role table correct?")
	fID := mkStatementQF(t, st, issue.ID, "finding", "three read-only queries say yes")
	_ = st.Close()

	if beadIsReady(t, dsn, issue.ID) {
		t.Fatalf("bead should be blocked while the question is active")
	}

	asOwner(t)
	out, _, err := runQuestionCmd(t, []string{"answer", qID, "--finding", fID})
	if err != nil {
		t.Fatalf("answer --finding should succeed: %v out %q", err, out)
	}
	got := getStatementQF(t, dsn, qID)
	if got.AnsweredBy == nil || *got.AnsweredBy != fID {
		t.Fatalf("answered_by should be %s, got %v", fID, got.AnsweredBy)
	}
	if got.Status != "answered" {
		t.Fatalf("status should be answered, got %s", got.Status)
	}
	// The finding itself is untouched.
	f := getStatementQF(t, dsn, fID)
	if f.Status != "active" || f.AnsweredBy != nil {
		t.Fatalf("finding should be untouched, got %+v", f)
	}
	if !beadIsReady(t, dsn, issue.ID) {
		t.Fatalf("bead should be ready once the question is answered by a finding")
	}
	// No ruling was minted.
	st2, _ := store.Open(context.Background(), dsn)
	defer st2.Close()
	rulings, err := st2.ListStatements(context.Background(), store.StatementFilter{Kinds: []string{"ruling"}})
	if err != nil {
		t.Fatalf("list rulings: %v", err)
	}
	if len(rulings) != 0 {
		t.Fatalf("answering with a finding must mint no ruling, got %d", len(rulings))
	}
}

func TestQuestionAnswerFinding_KindMismatchBothDirections(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "kind mismatch")
	q1 := mkQuestion(t, st, issue.ID, "q1")
	q2 := mkQuestion(t, st, issue.ID, "q2")
	rID := mkStatementQF(t, st, issue.ID, "ruling", "a ruling")
	fID := mkStatementQF(t, st, issue.ID, "finding", "a finding")
	_ = st.Close()
	asOwner(t)

	// --finding pointing at a ruling.
	_, _, err := runQuestionCmd(t, []string{"answer", q1, "--finding", rID})
	if err == nil || !strings.Contains(err.Error(), "not a finding") {
		t.Fatalf("--finding naming a ruling must be refused, got %v", err)
	}
	if got := getStatementQF(t, dsn, q1); got.Status != "active" || got.AnsweredBy != nil {
		t.Fatalf("question must be unchanged after the refusal, got %+v", got)
	}
	// --ruling pointing at a finding.
	_, _, err = runQuestionCmd(t, []string{"answer", q2, "--ruling", fID})
	if err == nil || !strings.Contains(err.Error(), "not a ruling") {
		t.Fatalf("--ruling naming a finding must be refused, got %v", err)
	}
	if got := getStatementQF(t, dsn, q2); got.Status != "active" || got.AnsweredBy != nil {
		t.Fatalf("question must be unchanged after the refusal, got %+v", got)
	}
}

func TestQuestionAnswerFinding_BothFlagsRefused(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "both flags")
	qID := mkQuestion(t, st, issue.ID, "q")
	rID := mkStatementQF(t, st, issue.ID, "ruling", "r")
	fID := mkStatementQF(t, st, issue.ID, "finding", "f")
	_ = st.Close()
	asOwner(t)

	_, _, err := runQuestionCmd(t, []string{"answer", qID, "--ruling", rID, "--finding", fID})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("both flags must be refused as mutually exclusive, got %v", err)
	}
	if got := getStatementQF(t, dsn, qID); got.Status != "active" || got.AnsweredBy != nil {
		t.Fatalf("question must be unchanged, got %+v", got)
	}
}

func TestQuestionAnswerFinding_NeitherFlagRefused(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "neither flag")
	qID := mkQuestion(t, st, issue.ID, "q")
	_ = st.Close()
	asOwner(t)

	_, _, err := runQuestionCmd(t, []string{"answer", qID})
	if err == nil {
		t.Fatalf("answer with neither flag must be refused")
	}
	if got := getStatementQF(t, dsn, qID); got.Status != "active" || got.AnsweredBy != nil {
		t.Fatalf("question must be unchanged, got %+v", got)
	}
}

func TestQuestionAnswer_ExecutorRefusedAndNothingWritten(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "executor answer")
	qID := mkQuestion(t, st, issue.ID, "may I settle this myself?")
	fID := mkStatementQF(t, st, issue.ID, "finding", "my own finding")
	rID := mkStatementQF(t, st, issue.ID, "ruling", "a ruling")
	_ = st.Close()

	t.Setenv("BD_ACTOR", "executor")
	want := "BD_ACTOR=executor: executors cannot file rulings; executors may file findings or questions instead"
	for _, args := range [][]string{
		{"answer", qID, "--finding", fID},
		{"answer", qID, "--ruling", rID},
	} {
		_, _, err := runQuestionCmd(t, args)
		if err == nil {
			t.Fatalf("executor must be refused for %v", args)
		}
		if err.Error() != want {
			t.Fatalf("refusal must match bd ruling add verbatim\n want %q\n got  %q", want, err.Error())
		}
	}
	got := getStatementQF(t, dsn, qID)
	if got.Status != "active" || got.AnsweredBy != nil {
		t.Fatalf("refused answer must write nothing, got %+v", got)
	}
	if beadIsReady(t, dsn, issue.ID) {
		t.Fatalf("bead must stay blocked after a refused answer")
	}
}

func TestQuestionAnswer_ShowNamesTheAnsweringKind(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		kind string
		want string
	}{
		{"finding", "finding: F-"},
		{"ruling", "ruling: R-"},
	} {
		dsn, st := newTempQFStore(t, "show "+tc.kind)
		issue := mkQFIssue(t, st, "show "+tc.kind)
		qID := mkQuestion(t, st, issue.ID, "the question")
		aID := mkStatementQF(t, st, issue.ID, tc.kind, "the answer")
		_ = st.Close()
		asOwner(t)
		if _, _, err := runQuestionCmd(t, []string{"answer", qID, "--" + tc.kind, aID}); err != nil {
			t.Fatalf("answer --%s: %v", tc.kind, err)
		}
		st2, err := store.Open(ctx, dsn)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		var buf bytes.Buffer
		err = printShowHuman(&buf, &cmdCtx{ctx: ctx, store: st2}, issue.ID, showOpts{})
		st2.Close()
		if err != nil {
			t.Fatalf("show: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "CLOSED QUESTIONS — NO LONGER BLOCKING") {
			t.Fatalf("an answered question must still render, got:\n%s", out)
		}
		if !strings.Contains(out, tc.want) {
			t.Fatalf("show must name the answering kind %q, got:\n%s", tc.want, out)
		}
	}
}

func TestQuestionAnswer_HelpNamesBothFlags(t *testing.T) {
	root := newQuestionCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"answer", "--help"})
	_ = root.Execute()
	out := buf.String()
	for _, want := range []string{"--ruling", "--finding"} {
		if !strings.Contains(out, want) {
			t.Fatalf("answer help should mention %q, got:\n%s", want, out)
		}
	}
}
