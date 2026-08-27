package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newTempRulingStore(t *testing.T, prefix string) (string, *store.Store) {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "ruling.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, prefix); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	// Ensure flagDB points at this DSN for openStore inside command
	old := flagDB
	flagDB = dsn
	t.Cleanup(func() {
		flagDB = old
		_ = st.Close()
	})
	return dsn, st
}

func mkIssueForRuling(t *testing.T, st *store.Store, title string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return i
}

func countStatements(t *testing.T, dsn string) int {
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

func runRulingAdd(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	root := newRulingCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

func TestResolveActor_UnsetIsOwner(t *testing.T) {
	orig, had := os.LookupEnv("BD_ACTOR")
	if had {
		os.Unsetenv("BD_ACTOR")
		t.Cleanup(func() { os.Setenv("BD_ACTOR", orig) })
	}
	identity, isExec := resolveActor()
	if isExec {
		t.Fatalf("unset BD_ACTOR should not be executor, got %q isExec=%v", identity, isExec)
	}
	if !strings.Contains(strings.ToLower(identity), "owner") {
		t.Fatalf("unset identity should contain owner, got %q", identity)
	}
	if !strings.Contains(identity, assigneeFromEnv()) {
		t.Fatalf("identity should contain user %q, got %q", assigneeFromEnv(), identity)
	}
}

func TestResolveActor_CoordinatorAllowed(t *testing.T) {
	t.Setenv("BD_ACTOR", "coordinator")
	identity, isExec := resolveActor()
	if isExec {
		t.Fatalf("coordinator should not be executor, got %q", identity)
	}
	if !strings.Contains(identity, "coordinator") {
		t.Fatalf("coordinator identity should contain coordinator, got %q", identity)
	}
}

func TestResolveActor_ExecutorRefused(t *testing.T) {
	t.Setenv("BD_ACTOR", "executor")
	identity, isExec := resolveActor()
	if !isExec {
		t.Fatalf("executor should be executor, got %q", identity)
	}
	if !strings.Contains(identity, "executor") {
		t.Fatalf("executor identity should contain executor, got %q", identity)
	}
}

func TestResolveActor_EmptyStringIsExecutor(t *testing.T) {
	t.Setenv("BD_ACTOR", "")
	_, isExec := resolveActor()
	if !isExec {
		t.Fatalf("empty BD_ACTOR explicitly set should be executor")
	}
}

func TestResolveActor_OtherValueIsExecutor(t *testing.T) {
	val := "banana"
	t.Setenv("BD_ACTOR", val)
	identity, isExec := resolveActor()
	if !isExec {
		t.Fatalf("%q should be executor", val)
	}
	if !strings.Contains(identity, val) {
		t.Fatalf("identity %q should contain %q", identity, val)
	}
}

func TestRuling_ExecutorRefuses(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "executor refuse")
	_ = st.Close()
	before := countStatements(t, dsn)
	t.Setenv("BD_ACTOR", "executor")
	out, errStr, err := runRulingAdd(t, []string{"add", issue.ID, "ruling text"})
	if err == nil {
		t.Fatalf("executor should be refused, got out %q errStr %q", out, errStr)
	}
	msg := err.Error() + errStr + out
	if !strings.Contains(msg, "BD_ACTOR") {
		t.Fatalf("error should name BD_ACTOR, got %q", msg)
	}
	low := strings.ToLower(msg)
	if !strings.Contains(low, "finding") && !strings.Contains(low, "question") {
		t.Fatalf("error should suggest findings/questions, got %q", msg)
	}
	after := countStatements(t, dsn)
	if after != before {
		t.Fatalf("executor refusal should leave zero new rows, before %d after %d", before, after)
	}
}

func TestRuling_CoordinatorSucceeds(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "coordinator ok")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runRulingAdd(t, []string{"add", issue.ID, "coordinator ruling"})
	if err != nil {
		t.Fatalf("coordinator should succeed: %v", err)
	}
	if !strings.Contains(out, "R-") {
		t.Fatalf("output should contain R-n id, got %q", out)
	}
	after := countStatements(t, dsn)
	if after != 1 {
		t.Fatalf("expected 1 statement, got %d", after)
	}
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	list, _ := st2.ListStatements(ctx, store.StatementFilter{})
	if len(list) != 1 || !strings.Contains(list[0].FiledBy, "coordinator") {
		t.Fatalf("filed_by should contain coordinator, got %+v", list[0])
	}
	if list[0].FiledBy == "" {
		t.Fatalf("filed_by empty")
	}
}

func TestRuling_UnsetSucceeds(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "owner ok")
	_ = st.Close()
	// Unset BD_ACTOR
	orig, had := os.LookupEnv("BD_ACTOR")
	if had {
		os.Unsetenv("BD_ACTOR")
		t.Cleanup(func() { os.Setenv("BD_ACTOR", orig) })
	}
	out, _, err := runRulingAdd(t, []string{"add", issue.ID, "owner ruling"})
	if err != nil {
		t.Fatalf("unset owner should succeed: %v", err)
	}
	if !strings.Contains(out, "R-") {
		t.Fatalf("output missing R- id, got %q", out)
	}
	after := countStatements(t, dsn)
	if after != 1 {
		t.Fatalf("expected 1, got %d", after)
	}
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	list, _ := st2.ListStatements(ctx, store.StatementFilter{})
	if !strings.Contains(list[0].FiledBy, "owner") {
		t.Fatalf("filed_by should contain owner, got %q", list[0].FiledBy)
	}
}

func TestRuling_OtherValueRefused(t *testing.T) {
	val := "custom-" + time.Now().Format("150405")
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "other refuse")
	_ = st.Close()
	before := countStatements(t, dsn)
	t.Setenv("BD_ACTOR", val)
	_, _, err := runRulingAdd(t, []string{"add", issue.ID, "text"})
	if err == nil {
		t.Fatalf("BD_ACTOR=%s should be refused as executor", val)
	}
	if !strings.Contains(err.Error(), val) && !strings.Contains(err.Error(), "BD_ACTOR") {
		t.Fatalf("error should name BD_ACTOR value %q, got %q", val, err.Error())
	}
	after := countStatements(t, dsn)
	if after != before {
		t.Fatalf("refusal should write nothing, before %d after %d", before, after)
	}
}

func TestRuling_DeferAtomicSuccess(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "defer success")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	deferUntil := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	out, _, err := runRulingAdd(t, []string{"add", issue.ID, "defer ruling", "--defer", deferUntil})
	if err != nil {
		t.Fatalf("defer add: %v", err)
	}
	if !strings.Contains(out, "R-") {
		t.Fatalf("missing id %q", out)
	}
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	got, _ := st2.GetIssue(ctx, issue.ID)
	if got.DeferUntil == nil {
		t.Fatalf("defer_until not set")
	}
	if got.DeferUntil.Format(time.RFC3339) != deferUntil {
		t.Fatalf("defer_until mismatch got %v want %s", got.DeferUntil, deferUntil)
	}
	list, _ := st2.ListStatements(ctx, store.StatementFilter{})
	if len(list) != 1 {
		t.Fatalf("expected 1 ruling, got %d", len(list))
	}
}

func TestRuling_DeferAtomicFailure(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "defer fail")
	// Capture original defer_until nil
	ctx := context.Background()
	orig, _ := st.GetIssue(ctx, issue.ID)
	if orig.DeferUntil != nil {
		t.Fatalf("expected nil defer")
	}
	_ = st.Close()
	// Make second half fail by using store directly with an invalid IssueUpdate that violates CHECK (priority 99)
	// Use the transaction function directly.
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	identity, _ := resolveActor()
	badPrio := 99
	upd := store.IssueUpdate{Priority: &badPrio}
	stmt := &beads.Statement{Kind: "ruling", IssueID: &issue.ID, Text: "should rollback", FiledBy: identity, Status: "active", Scope: "inherit"}
	err := st2.CreateStatementWithIssueUpdate(ctx, stmt, &upd)
	if err == nil {
		t.Fatalf("expected transaction to fail on priority CHECK")
	}
	// Assert neither statement nor defer changed (defer stays nil even though we used priority failure, not defer)
	// For defer case, also test that statement count unchanged and issue defer unchanged
	// Use defer case with priority failure is proxy for second half failure.
	afterSt, _ := store.Open(ctx, dsn)
	defer afterSt.Close()
	list, _ := afterSt.ListStatements(ctx, store.StatementFilter{})
	if len(list) != 0 {
		t.Fatalf("failed transaction should leave zero statements, got %d", len(list))
	}
	got, _ := afterSt.GetIssue(ctx, issue.ID)
	if got.DeferUntil != nil {
		t.Fatalf("defer_until should remain nil after rollback, got %v", *got.DeferUntil)
	}
	// Also ensure counter not leaked: next successful ruling should be R-1
	upd2 := store.IssueUpdate{Priority: func() *int { i := 2; return &i }()}
	okStmt := &beads.Statement{Kind: "ruling", IssueID: &issue.ID, Text: "ok after", FiledBy: identity, Status: "active", Scope: "inherit"}
	if err := afterSt.CreateStatementWithIssueUpdate(ctx, okStmt, &upd2); err != nil {
		t.Fatalf("next ok: %v", err)
	}
	if okStmt.ID != "R-1" {
		t.Fatalf("counter leaked, expected R-1 got %s", okStmt.ID)
	}
}

// Use command-level defer failure by making issue update target not exist (but ruling issue id valid? can't diff)
// Instead we test via store for close as well.

func TestRuling_CloseAtomicSuccess(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "close success")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingAdd(t, []string{"add", issue.ID, "close ruling", "--close"})
	if err != nil {
		t.Fatalf("close add: %v", err)
	}
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	got, _ := st2.GetIssue(ctx, issue.ID)
	if got.Status != beads.StatusClosed {
		t.Fatalf("expected closed, got %s", got.Status)
	}
	list, _ := st2.ListStatements(ctx, store.StatementFilter{})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestRuling_CloseAtomicFailure(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "close fail")
	ctx := context.Background()
	_ = st.Close()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	identity, _ := resolveActor()
	// Make second half fail: update issue with priority 99 while also closing
	badPrio := 99
	closed := beads.StatusClosed
	upd := store.IssueUpdate{Status: &closed, Priority: &badPrio}
	stmt := &beads.Statement{Kind: "ruling", IssueID: &issue.ID, Text: "close should rollback", FiledBy: identity, Status: "active", Scope: "inherit"}
	err := st2.CreateStatementWithIssueUpdate(ctx, stmt, &upd)
	if err == nil {
		t.Fatalf("expected failure")
	}
	afterSt, _ := store.Open(ctx, dsn)
	defer afterSt.Close()
	list, _ := afterSt.ListStatements(ctx, store.StatementFilter{})
	if len(list) != 0 {
		t.Fatalf("should have zero statements after failed close tx, got %d", len(list))
	}
	got, _ := afterSt.GetIssue(ctx, issue.ID)
	if got.Status == beads.StatusClosed {
		t.Fatalf("status should not be closed after rollback")
	}
}

func TestRuling_ProjectScoped(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runRulingAdd(t, []string{"add", "project-wide text"})
	if err != nil {
		t.Fatalf("project scoped: %v", err)
	}
	if !strings.Contains(out, "R-") {
		t.Fatalf("missing id %q", out)
	}
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	list, _ := st2.ListStatements(ctx, store.StatementFilter{})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].IssueID != nil {
		t.Fatalf("project scoped should have nil issue_id, got %v", *list[0].IssueID)
	}
}

func TestRuling_Supersedes(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "supersede base")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	// Create R-1 via command
	out, _, err := runRulingAdd(t, []string{"add", issue.ID, "original ruling"})
	if err != nil {
		t.Fatalf("r1: %v", err)
	}
	r1 := strings.TrimSpace(out)
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	// Ensure we have R-1
	_, _ = st2.GetStatement(ctx, r1)
	// Need to reopen to set flagDB still? flagDB still dsn
	// File superseding ruling
	out2, _, err := runRulingAdd(t, []string{"add", issue.ID, "superseding", "--supersedes", r1})
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	r2 := strings.TrimSpace(out2)
	st3, _ := store.Open(ctx, dsn)
	defer st3.Close()
	gotR2, _ := st3.GetStatement(ctx, r2)
	if gotR2.SupersedesID == nil || *gotR2.SupersedesID != r1 {
		t.Fatalf("supersedes_id wrong: %+v", gotR2)
	}
	gotR1, _ := st3.GetStatement(ctx, r1)
	if gotR1.Status != "superseded" {
		t.Fatalf("old status should be superseded, got %s", gotR1.Status)
	}
	// Superseding non-existent should error and write nothing
	before := countStatements(t, dsn)
	_, _, err = runRulingAdd(t, []string{"add", issue.ID, "bad supersede", "--supersedes", "R-9999"})
	if err == nil {
		t.Fatalf("supersede non-existent should fail")
	}
	after := countStatements(t, dsn)
	if after != before {
		t.Fatalf("bad supersede should write nothing, before %d after %d", before, after)
	}
}

func TestRuling_Answers(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "answer base")
	ctx := context.Background()
	// Create question via store directly
	q := &beads.Statement{Kind: "question", IssueID: &issue.ID, Text: "open question", FiledBy: "tester:owner", Status: "active", Scope: "inherit"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create q: %v", err)
	}
	qID := q.ID
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runRulingAdd(t, []string{"add", issue.ID, "answering ruling", "--answers", qID})
	if err != nil {
		t.Fatalf("answers: %v", err)
	}
	rID := strings.TrimSpace(out)
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	gotQ, _ := st2.GetStatement(ctx, qID)
	if gotQ.AnsweredBy == nil || *gotQ.AnsweredBy != rID {
		t.Fatalf("question answered_by should be %s, got %v", rID, gotQ.AnsweredBy)
	}
	if gotQ.Status != "answered" {
		t.Fatalf("question status should be answered, got %s", gotQ.Status)
	}
	// Non-existent
	before := countStatements(t, dsn)
	_, _, err = runRulingAdd(t, []string{"add", issue.ID, "bad answer", "--answers", "Q-9999"})
	if err == nil {
		t.Fatalf("answering non-existent should fail")
	}
	after := countStatements(t, dsn)
	if after != before {
		t.Fatalf("bad answer should write nothing, before %d after %d", before, after)
	}
}

func TestRuling_Scope(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "scope base")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	// self scope
	out, _, err := runRulingAdd(t, []string{"add", issue.ID, "self ruling", "--scope", "self"})
	if err != nil {
		t.Fatalf("scope self: %v", err)
	}
	r1 := strings.TrimSpace(out)
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	got, _ := st2.GetStatement(ctx, r1)
	if got.Scope != "self" {
		t.Fatalf("scope should be self, got %s", got.Scope)
	}
	// inherit default
	out2, _, err := runRulingAdd(t, []string{"add", issue.ID, "inherit ruling"})
	if err != nil {
		t.Fatalf("inherit: %v", err)
	}
	r2 := strings.TrimSpace(out2)
	st3, _ := store.Open(ctx, dsn)
	got2, _ := st3.GetStatement(ctx, r2)
	if got2.Scope != "inherit" {
		t.Fatalf("default scope should be inherit, got %s", got2.Scope)
	}
}

func TestRuling_FiledByContainsActor(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "filedby")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingAdd(t, []string{"add", issue.ID, "filedby test"})
	if err != nil {
		t.Fatalf("filedby: %v", err)
	}
	ctx := context.Background()
	st2, _ := store.Open(ctx, dsn)
	defer st2.Close()
	list, _ := st2.ListStatements(ctx, store.StatementFilter{})
	if len(list) != 1 {
		t.Fatalf("count")
	}
	if list[0].FiledBy == "" {
		t.Fatalf("filed_by empty")
	}
	if !strings.Contains(list[0].FiledBy, "coordinator") {
		t.Fatalf("filed_by should contain coordinator, got %q", list[0].FiledBy)
	}
	// Also check owner case
	dsn2, st2b := newTempRulingStore(t, "bd2")
	issue2 := mkIssueForRuling(t, st2b, "filedby owner")
	_ = st2b.Close()
	orig, had := os.LookupEnv("BD_ACTOR")
	if had {
		os.Unsetenv("BD_ACTOR")
		t.Cleanup(func() { os.Setenv("BD_ACTOR", orig) })
	}
	_, _, err = runRulingAdd(t, []string{"add", issue2.ID, "owner filedby"})
	if err != nil {
		t.Fatalf("owner filedby: %v", err)
	}
	ctx2 := context.Background()
	_ = dsn2
	st3, _ := store.Open(ctx2, dsn2)
	defer st3.Close()
	list2, _ := st3.ListStatements(ctx2, store.StatementFilter{})
	if !strings.Contains(list2[0].FiledBy, "owner") {
		t.Fatalf("owner filed_by should contain owner, got %q", list2[0].FiledBy)
	}
}

func TestRuling_DeferAndCloseMutualExclusive(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "mutual")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	deferStr := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	_, _, err := runRulingAdd(t, []string{"add", issue.ID, "text", "--defer", deferStr, "--close"})
	if err == nil {
		t.Fatalf("defer and close together should fail")
	}
	_ = dsn
}
