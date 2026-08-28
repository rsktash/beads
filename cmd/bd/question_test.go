package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// Helpers for question/finding tests — distinct names to avoid collision with ruling_test.go.

func newTempQFStore(t *testing.T, prefix string) (string, *store.Store) {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "qf.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, prefix); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	oldDB := flagDB
	oldJSON := flagJSON
	flagDB = dsn
	flagJSON = false
	t.Cleanup(func() {
		flagDB = oldDB
		flagJSON = oldJSON
		_ = st.Close()
	})
	return dsn, st
}

func mkQFIssue(t *testing.T, st *store.Store, title string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return i
}

func runQuestionCmd(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	root := newQuestionCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

func runFindingCmd(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	root := newFindingCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

func countStatementsQF(t *testing.T, dsn string) int {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open count: %v", err)
	}
	defer st.Close()
	list, err := st.ListStatements(ctx, store.StatementFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return len(list)
}

func getStatementQF(t *testing.T, dsn, id string) *beads.Statement {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open get: %v", err)
	}
	defer st.Close()
	s, err := st.GetStatement(ctx, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return s
}

func TestQuestion_ExecutorSucceeds(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "q executor")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "executor")
	out, _, err := runQuestionCmd(t, []string{"add", issue.ID, "question from executor"})
	if err != nil {
		t.Fatalf("executor question add should succeed: %v out %q", err, out)
	}
	if !strings.Contains(out, "Q-") {
		t.Fatalf("output should contain Q- id, got %q", out)
	}
	id := strings.TrimSpace(out)
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	got, _ := st2.GetStatement(ctx, id)
	if got.Kind != "question" {
		t.Fatalf("kind should be question, got %s", got.Kind)
	}
	if got.Status != "active" {
		t.Fatalf("status should be active, got %s", got.Status)
	}
	if !strings.Contains(got.FiledBy, "executor") {
		t.Fatalf("filed_by should contain executor, got %q", got.FiledBy)
	}
	if got.IssueID == nil || *got.IssueID != issue.ID {
		t.Fatalf("issue_id mismatch, got %v want %s", got.IssueID, issue.ID)
	}
}

func TestQuestion_CoordinatorSucceeds(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "q coordinator")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runQuestionCmd(t, []string{"add", issue.ID, "question from coordinator"})
	if err != nil {
		t.Fatalf("coordinator should succeed: %v", err)
	}
	if !strings.Contains(out, "Q-") {
		t.Fatalf("missing Q- id %q", out)
	}
	id := strings.TrimSpace(out)
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	got, _ := st2.GetStatement(ctx, id)
	if !strings.Contains(got.FiledBy, "coordinator") {
		t.Fatalf("filed_by should contain coordinator, got %q", got.FiledBy)
	}
}

func TestQuestion_OwnerSucceeds(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "q owner")
	_ = st.Close()
	orig, had := os.LookupEnv("BD_ACTOR")
	if had {
		os.Unsetenv("BD_ACTOR")
		t.Cleanup(func() { os.Setenv("BD_ACTOR", orig) })
	}
	out, _, err := runQuestionCmd(t, []string{"add", issue.ID, "question from owner"})
	if err != nil {
		t.Fatalf("owner should succeed: %v", err)
	}
	if !strings.Contains(out, "Q-") {
		t.Fatalf("missing Q- %q", out)
	}
	id := strings.TrimSpace(out)
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	got, _ := st2.GetStatement(ctx, id)
	if !strings.Contains(strings.ToLower(got.FiledBy), "owner") {
		t.Fatalf("filed_by should contain owner, got %q", got.FiledBy)
	}
}

func TestQuestion_MissingIssueFails(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "executor")
	before := countStatementsQF(t, dsn)
	_, _, err := runQuestionCmd(t, []string{"add", "does-not-exist", "question text"})
	if err == nil {
		t.Fatalf("missing issue should fail")
	}
	after := countStatementsQF(t, dsn)
	if after != before {
		t.Fatalf("missing issue should write nothing, before %d after %d", before, after)
	}
}

func TestQuestion_NoIssueArgErrors(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "executor")
	before := countStatementsQF(t, dsn)
	// Only one arg (text) without issue id -> should error due to ExactArgs(2)
	_, _, err := runQuestionCmd(t, []string{"add", "only text"})
	if err == nil {
		t.Fatalf("question add with single arg should error (requires issue)")
	}
	after := countStatementsQF(t, dsn)
	if after != before {
		t.Fatalf("should write nothing on arg error, before %d after %d", before, after)
	}
	_ = dsn
}

func TestQuestion_BeadStatusUnchanged(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "status unchanged")
	_ = st.Close()
	ctx := context.Background()
	t.Setenv("BD_ACTOR", "executor")
	// Get before status
	st2, _ := store.Open(ctx, dsn)
	beforeIssue, _ := st2.GetIssue(ctx, issue.ID)
	beforeStatus := beforeIssue.Status
	_ = st2.Close()
	_, _, err := runQuestionCmd(t, []string{"add", issue.ID, "does not change status"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	st3, _ := store.Open(ctx, dsn)
	defer st3.Close()
	afterIssue, _ := st3.GetIssue(ctx, issue.ID)
	if afterIssue.Status != beforeStatus {
		t.Fatalf("bead status should not change after question, before %s after %s", beforeStatus, afterIssue.Status)
	}
}

func TestFinding_EvidenceStored(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "finding evidence")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "executor")
	out, _, err := runFindingCmd(t, []string{"add", issue.ID, "found something", "--evidence", "src/api.ts:84"})
	if err != nil {
		t.Fatalf("finding add with evidence: %v", err)
	}
	id := strings.TrimSpace(out)
	if !strings.Contains(id, "F-") {
		t.Fatalf("missing F- id %q", id)
	}
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	got, _ := st2.GetStatement(ctx, id)
	if got.Evidence != "src/api.ts:84" {
		t.Fatalf("evidence should be stored verbatim, got %q", got.Evidence)
	}
	if got.Kind != "finding" {
		t.Fatalf("kind should be finding, got %s", got.Kind)
	}
	if !strings.Contains(got.FiledBy, "executor") {
		t.Fatalf("filed_by should contain executor, got %q", got.FiledBy)
	}
}

func TestFinding_WithoutEvidenceSucceeds(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "finding no evidence")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "executor")
	out, _, err := runFindingCmd(t, []string{"add", issue.ID, "found without evidence"})
	if err != nil {
		t.Fatalf("finding without evidence should succeed: %v", err)
	}
	id := strings.TrimSpace(out)
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	got, _ := st2.GetStatement(ctx, id)
	if got.Evidence != "" {
		t.Fatalf("evidence should be empty, got %q", got.Evidence)
	}
}

func TestFinding_CoordinatorSucceeds(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "finding coordinator")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runFindingCmd(t, []string{"add", issue.ID, "coordinator finding"})
	if err != nil {
		t.Fatalf("coordinator finding should succeed: %v", err)
	}
	id := strings.TrimSpace(out)
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	got, _ := st2.GetStatement(ctx, id)
	if !strings.Contains(got.FiledBy, "coordinator") {
		t.Fatalf("filed_by should contain coordinator, got %q", got.FiledBy)
	}
}

func TestFinding_BeadStatusUnchanged(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "finding status")
	_ = st.Close()
	ctx := context.Background()
	t.Setenv("BD_ACTOR", "executor")
	st2, _ := store.Open(ctx, dsn)
	before, _ := st2.GetIssue(ctx, issue.ID)
	_ = st2.Close()
	_, _, err := runFindingCmd(t, []string{"add", issue.ID, "finding status test"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	st3, _ := store.Open(ctx, dsn)
	defer st3.Close()
	after, _ := st3.GetIssue(ctx, issue.ID)
	if after.Status != before.Status {
		t.Fatalf("finding should not change bead status, before %s after %s", before.Status, after.Status)
	}
}

func TestQuestionAnswer_Success(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "answer success")
	ctx := context.Background()
	q := &beads.Statement{Kind: "question", IssueID: &issue.ID, Text: "open question", FiledBy: "executor:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create q: %v", err)
	}
	qID := q.ID
	r := &beads.Statement{Kind: "ruling", IssueID: &issue.ID, Text: "ruling answer", FiledBy: "coordinator:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(ctx, r); err != nil {
		t.Fatalf("create r: %v", err)
	}
	rID := r.ID
	// Capture ruling before
	rBefore := getStatementQF(t, dsn, rID)
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runQuestionCmd(t, []string{"answer", qID, "--ruling", rID})
	if err != nil {
		t.Fatalf("answer should succeed: %v out %q", err, out)
	}
	if !strings.Contains(out, qID) && !strings.Contains(out, "Q-") {
		t.Fatalf("answer output should contain question id, got %q", out)
	}
	// Check question updated
	qAfter := getStatementQF(t, dsn, qID)
	if qAfter.AnsweredBy == nil || *qAfter.AnsweredBy != rID {
		t.Fatalf("answered_by should be %s, got %v", rID, qAfter.AnsweredBy)
	}
	if qAfter.Status != "answered" {
		t.Fatalf("status should be answered, got %s", qAfter.Status)
	}
	// Ruling untouched
	rAfter := getStatementQF(t, dsn, rID)
	if rAfter.Status != rBefore.Status {
		t.Fatalf("ruling status should be untouched, before %s after %s", rBefore.Status, rAfter.Status)
	}
	if rAfter.Text != rBefore.Text {
		t.Fatalf("ruling text should be untouched")
	}
	if rAfter.AnsweredBy != nil {
		t.Fatalf("ruling answered_by should remain nil, got %v", rAfter.AnsweredBy)
	}
}

func TestQuestionAnswer_NonExistentQuestionFails(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "answer fail q")
	r := &beads.Statement{Kind: "ruling", IssueID: &issue.ID, Text: "ruling", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), r); err != nil {
		t.Fatalf("create ruling: %v", err)
	}
	_ = st.Close()
	before := countStatementsQF(t, dsn)
	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runQuestionCmd(t, []string{"answer", "Q-9999", "--ruling", r.ID})
	if err == nil {
		t.Fatalf("answering non-existent question should fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "question") {
		t.Fatalf("error should mention question, got %q", err.Error())
	}
	after := countStatementsQF(t, dsn)
	if after != before {
		t.Fatalf("should write nothing, before %d after %d", before, after)
	}
	// Also check question not created as side effect
}

func TestQuestionAnswer_NonExistentRulingFails(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "answer fail r")
	q := &beads.Statement{Kind: "question", IssueID: &issue.ID, Text: "q", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), q); err != nil {
		t.Fatalf("create q: %v", err)
	}
	_ = st.Close()
	before := countStatementsQF(t, dsn)
	// Capture question before
	qBefore := getStatementQF(t, dsn, q.ID)
	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runQuestionCmd(t, []string{"answer", q.ID, "--ruling", "R-9999"})
	if err == nil {
		t.Fatalf("answering with non-existent ruling should fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ruling") {
		t.Fatalf("error should mention ruling, got %q", err.Error())
	}
	after := countStatementsQF(t, dsn)
	if after != before {
		t.Fatalf("should write nothing, before %d after %d", before, after)
	}
	// Ensure question unchanged
	qAfter := getStatementQF(t, dsn, q.ID)
	if qAfter.Status != qBefore.Status || (qAfter.AnsweredBy != nil && qBefore.AnsweredBy == nil) {
		t.Fatalf("question should be unchanged after failed answer, before %+v after %+v", qBefore, qAfter)
	}
}

func TestQuestionAnswer_NotAQuestionFails(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "answer not question")
	q := &beads.Statement{Kind: "finding", IssueID: &issue.ID, Text: "not a question", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), q); err != nil {
		t.Fatalf("create finding: %v", err)
	}
	r := &beads.Statement{Kind: "ruling", IssueID: &issue.ID, Text: "ruling", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), r); err != nil {
		t.Fatalf("create ruling: %v", err)
	}
	_ = st.Close()
	before := countStatementsQF(t, dsn)
	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runQuestionCmd(t, []string{"answer", q.ID, "--ruling", r.ID})
	if err == nil {
		t.Fatalf("answering a finding (not a question) should fail")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "not a question") && !strings.Contains(low, "question") {
		t.Fatalf("error should mention not a question, got %q", err.Error())
	}
	after := countStatementsQF(t, dsn)
	if after != before {
		t.Fatalf("should write nothing, before %d after %d", before, after)
	}
	// Ensure ruling untouched and finding unchanged
	qAfter := getStatementQF(t, dsn, q.ID)
	if qAfter.Status != "active" || qAfter.AnsweredBy != nil {
		t.Fatalf("finding should remain unchanged, got %+v", qAfter)
	}
}

func TestQuestionAnswer_AlreadyAnsweredFails(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "already answered")
	ctx := context.Background()
	q := &beads.Statement{Kind: "question", IssueID: &issue.ID, Text: "q", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create q: %v", err)
	}
	r1 := &beads.Statement{Kind: "ruling", IssueID: &issue.ID, Text: "r1", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(ctx, r1); err != nil {
		t.Fatalf("create r1: %v", err)
	}
	r2 := &beads.Statement{Kind: "ruling", IssueID: &issue.ID, Text: "r2", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(ctx, r2); err != nil {
		t.Fatalf("create r2: %v", err)
	}
	// First answer succeeds via store
	if err := st.SetAnsweredBy(ctx, q.ID, r1.ID); err != nil {
		t.Fatalf("first answer: %v", err)
	}
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runQuestionCmd(t, []string{"answer", q.ID, "--ruling", r2.ID})
	if err == nil {
		t.Fatalf("answering already answered question should fail")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "already answered") {
		t.Fatalf("error should mention already answered, got %q", err.Error())
	}
	// Ensure still answered by r1, not r2
	qAfter := getStatementQF(t, dsn, q.ID)
	if qAfter.AnsweredBy == nil || *qAfter.AnsweredBy != r1.ID {
		t.Fatalf("should remain answered by %s, got %v", r1.ID, qAfter.AnsweredBy)
	}
}

func TestQuestionAnswer_RulingNotRulingFails(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "ruling not ruling")
	q := &beads.Statement{Kind: "question", IssueID: &issue.ID, Text: "q", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), q); err != nil {
		t.Fatalf("create q: %v", err)
	}
	f := &beads.Statement{Kind: "finding", IssueID: &issue.ID, Text: "finding not ruling", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(context.Background(), f); err != nil {
		t.Fatalf("create finding: %v", err)
	}
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runQuestionCmd(t, []string{"answer", q.ID, "--ruling", f.ID})
	if err == nil {
		t.Fatalf("answering with a finding as ruling should fail")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "not a ruling") && !strings.Contains(low, "ruling") {
		t.Fatalf("error should mention not a ruling, got %q", err.Error())
	}
	_ = dsn
}

func TestQuestion_JsonPrintsStatement(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "json question")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "executor")
	// Use flagJSON via global
	old := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = old })
	out, _, err := runQuestionCmd(t, []string{"add", issue.ID, "json question text"})
	if err != nil {
		t.Fatalf("json add: %v", err)
	}
	var got beads.Statement
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json unmarshal failed, out %q: %v", out, err)
	}
	if got.Kind != "question" || got.Text != "json question text" {
		t.Fatalf("json statement mismatch, got %+v", got)
	}
	if !strings.Contains(got.FiledBy, "executor") {
		t.Fatalf("filed_by should contain executor, got %q", got.FiledBy)
	}
	_ = dsn
}

func TestFinding_JsonPrintsStatement(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "json finding")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	old := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = old })
	out, _, err := runFindingCmd(t, []string{"add", issue.ID, "json finding text", "--evidence", "src/foo.go:10"})
	if err != nil {
		t.Fatalf("json finding add: %v", err)
	}
	var got beads.Statement
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json unmarshal failed %q: %v", out, err)
	}
	if got.Kind != "finding" || got.Evidence != "src/foo.go:10" {
		t.Fatalf("json finding mismatch, got %+v", got)
	}
	_ = dsn
}

func TestQuestionAnswer_JsonPrintsUpdated(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "json answer")
	ctx := context.Background()
	q := &beads.Statement{Kind: "question", IssueID: &issue.ID, Text: "q", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create q: %v", err)
	}
	r := &beads.Statement{Kind: "ruling", IssueID: &issue.ID, Text: "r", FiledBy: "owner:tester", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(ctx, r); err != nil {
		t.Fatalf("create r: %v", err)
	}
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	old := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = old })
	out, _, err := runQuestionCmd(t, []string{"answer", q.ID, "--ruling", r.ID})
	if err != nil {
		t.Fatalf("json answer: %v", err)
	}
	var got beads.Statement
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json unmarshal answer %q: %v", out, err)
	}
	if got.ID != q.ID {
		t.Fatalf("json answer should return question, got %s want %s", got.ID, q.ID)
	}
	if got.AnsweredBy == nil || *got.AnsweredBy != r.ID {
		t.Fatalf("answered_by mismatch %v", got.AnsweredBy)
	}
	if got.Status != "answered" {
		t.Fatalf("status should be answered, got %s", got.Status)
	}
	_ = dsn
}

func TestQuestion_HelpListsSubcommands(t *testing.T) {
	root := newQuestionCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})
	_ = root.Execute()
	out := buf.String()
	if !strings.Contains(out, "add") {
		t.Fatalf("question help should list add, got %q", out)
	}
	if !strings.Contains(out, "answer") {
		t.Fatalf("question help should list answer, got %q", out)
	}
}

func TestFinding_HelpListsSubcommands(t *testing.T) {
	root := newFindingCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})
	_ = root.Execute()
	out := buf.String()
	if !strings.Contains(out, "add") {
		t.Fatalf("finding help should list add, got %q", out)
	}
}

func TestQuestionAnswer_Help(t *testing.T) {
	// Ensure answer subcommand exists and has --ruling flag
	root := newQuestionCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"answer", "--help"})
	_ = root.Execute()
	out := buf.String()
	if !strings.Contains(out, "ruling") {
		t.Fatalf("answer help should mention ruling flag, got %q", out)
	}
}

func TestQuestion_NoEvidenceField(t *testing.T) {
	// Ensure question does not store evidence unless provided (should be empty)
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "no evidence")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "executor")
	out, _, err := runQuestionCmd(t, []string{"add", issue.ID, "question without evidence"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	id := strings.TrimSpace(out)
	got := getStatementQF(t, dsn, id)
	if got.Evidence != "" {
		t.Fatalf("question evidence should be empty, got %q", got.Evidence)
	}
}

// Ensure time import used
var _ = time.Now
var _ = os.Getenv
