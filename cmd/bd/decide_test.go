package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func runCreateCmd(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	root := newCreateCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

// decideIssueCount counts every bead row, for before/after comparisons.
func decideIssueCount(t *testing.T, dsn string) int {
	t.Helper()
	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open count: %v", err)
	}
	defer st.Close()
	list, err := st.ListIssues(context.Background(), store.ListFilter{})
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	return len(list)
}

// decideBeadByTitle finds the one bead carrying title, or nil.
func decideBeadByTitle(t *testing.T, dsn, title string) *beads.Issue {
	t.Helper()
	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open find: %v", err)
	}
	defer st.Close()
	list, err := st.ListIssues(context.Background(), store.ListFilter{})
	if err != nil {
		t.Fatalf("list issues: %v", err)
	}
	for _, i := range list {
		if i.Title == title {
			return &i
		}
	}
	return nil
}

// decideQuestionsOn returns the questions filed on one bead.
func decideQuestionsOn(t *testing.T, dsn, issueID string) []beads.Statement {
	t.Helper()
	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open questions: %v", err)
	}
	defer st.Close()
	return listQuestionsOn(t, st, issueID)
}

func listQuestionsOn(t *testing.T, st *store.Store, issueID string) []beads.Statement {
	t.Helper()
	list, err := st.ListStatements(context.Background(), store.StatementFilter{
		IssueIDs: []string{issueID}, Kinds: []string{"question"},
	})
	if err != nil {
		t.Fatalf("list questions on %s: %v", issueID, err)
	}
	return list
}

// decideRefusal is the pinned Decide-gate message (Behaviour 6).
const decideRefusal = `a "Decide:" bead needs the question attached: bd create "<title>" --question "<the fork>" --topic <slug>`

func TestDecide_RefusesWithoutQuestion(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	_ = st.Close()
	asOwner(t)

	_, _, err := runCreateCmd(t, []string{"Decide: which cursor shape"})
	if err == nil {
		t.Fatalf("a Decide: title without --question must be refused")
	}
	if !strings.Contains(err.Error(), decideRefusal) {
		t.Fatalf("the refusal must carry the pinned message %q, got:\n%s", decideRefusal, err.Error())
	}
}

func TestDecide_RefusalWritesNoBead(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	_ = st.Close()
	asOwner(t)

	before := decideIssueCount(t, dsn)
	_, _, err := runCreateCmd(t, []string{"Decide: which cursor shape", "--desc", "body"})
	if err == nil {
		t.Fatalf("the refused create must fail")
	}
	if after := decideIssueCount(t, dsn); after != before {
		t.Fatalf("the refused create must write no bead, before %d after %d", before, after)
	}
}

func TestDecide_CaseInsensitive(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	_ = st.Close()
	asOwner(t)

	before := decideIssueCount(t, dsn)
	for _, title := range []string{"decide: x", "DECIDE  which cursor"} {
		_, _, err := runCreateCmd(t, []string{title})
		if err == nil {
			t.Fatalf("%q must be refused as a Decide title", title)
		}
		if !strings.Contains(err.Error(), decideRefusal) {
			t.Fatalf("the refusal for %q must carry the pinned message, got:\n%s", title, err.Error())
		}
	}
	if after := decideIssueCount(t, dsn); after != before {
		t.Fatalf("the refused creates must write no bead, before %d after %d", before, after)
	}
}

func TestDecide_CreatesBeadAndQuestion(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	_ = st.Close()
	asOwner(t)

	_, _, err := runCreateCmd(t, []string{
		"Decide: which cursor shape", "--question", "Keyset or offset?", "--topic", "exports-paging",
	})
	if err != nil {
		t.Fatalf("create with --question must succeed: %v", err)
	}
	bead := decideBeadByTitle(t, dsn, "Decide: which cursor shape")
	if bead == nil {
		t.Fatalf("the bead was not created")
	}
	qs := decideQuestionsOn(t, dsn, bead.ID)
	if len(qs) != 1 {
		t.Fatalf("want exactly one question on the new bead, got %d", len(qs))
	}
	q := qs[0]
	if q.Text != "Keyset or offset?" {
		t.Fatalf("question text mismatch, got %q", q.Text)
	}
	if q.Status != "active" || q.Scope != "inherit" || q.Topic != "exports-paging" {
		t.Fatalf("question shape mismatch, got %+v", q)
	}
	if q.IssueID == nil || *q.IssueID != bead.ID {
		t.Fatalf("question must sit on %s, got %v", bead.ID, q.IssueID)
	}
}

func TestDecide_RollsBackOnQuestionFailure(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	defer st.Close()

	before := decideIssueCount(t, dsn)
	bad := &beads.Statement{Kind: "question", Text: ""} // text is required
	i := &beads.Issue{Title: "Decide: rollback probe", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssueWithQuestion(context.Background(), "", i, nil, bad); err == nil {
		t.Fatalf("a failure inside the store call must fail the create")
	}
	if after := decideIssueCount(t, dsn); after != before {
		t.Fatalf("the bead must roll back with the question, before %d after %d", before, after)
	}
	if decideBeadByTitle(t, dsn, "Decide: rollback probe") != nil {
		t.Fatalf("no bead may survive the failed question")
	}
}

func TestDecide_QuestionAllowedOnPlainTitle(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	_ = st.Close()
	asOwner(t)

	_, _, err := runCreateCmd(t, []string{
		"plain title carries a question", "--question", "what about it?", "--topic", "plain-fork",
	})
	if err != nil {
		t.Fatalf("--question on a plain title must succeed: %v", err)
	}
	bead := decideBeadByTitle(t, dsn, "plain title carries a question")
	if bead == nil {
		t.Fatalf("the bead was not created")
	}
	qs := decideQuestionsOn(t, dsn, bead.ID)
	if len(qs) != 1 {
		t.Fatalf("want exactly one question on the new bead, got %d", len(qs))
	}
	if qs[0].Topic != "plain-fork" {
		t.Fatalf("question topic mismatch, got %q", qs[0].Topic)
	}
}
