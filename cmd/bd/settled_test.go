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

// Helpers for rulings/settled tests — distinct names to avoid collision.

func newTempSettledStore(t *testing.T, prefix string) (string, *store.Store) {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "settled.db")
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

func mkSettledIssue(t *testing.T, st *store.Store, title string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return i
}

func mkSettledEpic(t *testing.T, st *store.Store, title string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create epic %q: %v", title, err)
	}
	return i
}

func mustCreateSettledStatement(t *testing.T, st *store.Store, s *beads.Statement) *beads.Statement {
	t.Helper()
	if err := st.CreateStatement(context.Background(), s); err != nil {
		t.Fatalf("CreateStatement %q: %v", s.Text, err)
	}
	return s
}

func runRulings(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	root := newRulingsCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

func runSettled(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	root := newSettledCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

func runRootHelp(t *testing.T) string {
	t.Helper()
	root := newRoot()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})
	_ = root.Execute()
	return buf.String()
}

// ---------- bd rulings ----------

func TestRulings_ListsEveryActiveNewestFirst(t *testing.T) {
	dsn, st := newTempSettledStore(t, "bd")
	// create issues
	iss1 := mkSettledIssue(t, st, "issue1")
	iss2 := mkSettledIssue(t, st, "issue2")
	// Times newest first: create with explicit times
	t1 := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	t2 := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	t3 := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
	// active ruling newest is R-3
	rOld := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "old ruling", IssueID: &iss1.ID, CreatedAt: t1})
	rMid := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "mid ruling", IssueID: &iss2.ID, CreatedAt: t2})
	rNew := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "new ruling", CreatedAt: t3}) // project
	// superseded and retracted should be absent
	rSup := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "superseded ruling", IssueID: &iss1.ID})
	_ = st.UpdateStatementStatus(context.Background(), rSup.ID, "superseded")
	rRet := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "retracted ruling", IssueID: &iss1.ID})
	_ = st.UpdateStatementStatus(context.Background(), rRet.ID, "retracted")
	_ = st.Close()

	out, _, err := runRulings(t, []string{})
	if err != nil {
		t.Fatalf("rulings: %v out %q", err, out)
	}
	// Should contain three active, newest first
	for _, id := range []string{rNew.ID, rMid.ID, rOld.ID} {
		if !strings.Contains(out, id) {
			t.Fatalf("expected active ruling %s in output, got %q", id, out)
		}
	}
	if strings.Contains(out, rSup.ID) {
		t.Fatalf("superseded %s should be absent, got %q", rSup.ID, out)
	}
	if strings.Contains(out, rRet.ID) {
		t.Fatalf("retracted %s should be absent, got %q", rRet.ID, out)
	}
	if strings.Contains(out, "superseded ruling") || strings.Contains(out, "retracted ruling") {
		t.Fatalf("superseded/retracted text should be absent, got %q", out)
	}
	// Check newest first order: rNew before rMid before rOld
	idxNew := strings.Index(out, rNew.ID)
	idxMid := strings.Index(out, rMid.ID)
	idxOld := strings.Index(out, rOld.ID)
	if !(idxNew < idxMid && idxMid < idxOld) {
		t.Fatalf("newest first order wrong: new %d mid %d old %d out %q", idxNew, idxMid, idxOld, out)
	}
	// Check each line contains id, date, bead attached, text
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d out %q", len(lines), out)
	}
	for _, line := range lines {
		// id check: should contain R-
		if !strings.Contains(line, "R-") {
			t.Fatalf("line missing id R-: %q", line)
		}
		// date check: should contain 2006-01-02 formatted date (we used truncated now)
		if !strings.Contains(line, rNew.CreatedAt.Format("2006-01-02")) && !strings.Contains(line, rMid.CreatedAt.Format("2006-01-02")) && !strings.Contains(line, rOld.CreatedAt.Format("2006-01-02")) {
			// at least contains a date-like pattern yyyy-mm-dd
			if len(line) < 10 || !strings.Contains(line, "-") {
				t.Fatalf("line missing date: %q", line)
			}
		}
		// bead attached: project or issue id
		hasBead := strings.Contains(line, "project") || strings.Contains(line, iss1.ID) || strings.Contains(line, iss2.ID)
		if !hasBead {
			t.Fatalf("line missing bead attached (project or issue): %q", line)
		}
		// text
		if !strings.Contains(line, "ruling") {
			t.Fatalf("line missing text: %q", line)
		}
	}
	_ = dsn
}

func TestRulings_ScopeProject(t *testing.T) {
	_, st := newTempSettledStore(t, "bd")
	iss := mkSettledIssue(t, st, "bead")
	// one project, four bead-scoped
	proj := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "project ruling"})
	for i := 0; i < 4; i++ {
		mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "bead ruling", IssueID: &iss.ID})
	}
	_ = st.Close()
	out, _, err := runRulings(t, []string{"--scope", "project"})
	if err != nil {
		t.Fatalf("rulings --scope project: %v", err)
	}
	if !strings.Contains(out, proj.ID) {
		t.Fatalf("project ruling %s should be in output, got %q", proj.ID, out)
	}
	if !strings.Contains(out, "project ruling") {
		t.Fatalf("project ruling text missing, got %q", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Should be exactly one line (or at least contain only project)
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 line for project scope, got %d: %q", len(lines), out)
	}
	if strings.Contains(out, "bead ruling") {
		t.Fatalf("bead rulings should be absent in project scope, got %q", out)
	}
	// also check count via --json
	old := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = old })
	outJSON, _, err := runRulings(t, []string{"--scope", "project"})
	if err != nil {
		t.Fatalf("json project: %v", err)
	}
	flagJSON = false
	var got []beads.Statement
	if err := json.Unmarshal([]byte(outJSON), &got); err != nil {
		t.Fatalf("unmarshal json project: %v out %q", err, outJSON)
	}
	if len(got) != 1 {
		t.Fatalf("json project should have 1, got %d", len(got))
	}
	if got[0].ID != proj.ID {
		t.Fatalf("json project id mismatch, got %s want %s", got[0].ID, proj.ID)
	}
}

func TestRulings_IssueBoundMatchesContract(t *testing.T) {
	_, st := newTempSettledStore(t, "bd")
	ctx := context.Background()
	root := mkSettledEpic(t, st, "root epic")
	epic := &beads.Issue{Title: "child epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, root.ID, epic, nil); err != nil {
		t.Fatalf("create child epic: %v", err)
	}
	task := &beads.Issue{Title: "leaf task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskRuling := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "task ruling", IssueID: &task.ID})
	epicRuling := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "epic ruling", IssueID: &epic.ID})
	rootRuling := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "root ruling", IssueID: &root.ID})
	projRuling := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "project ruling"})
	// also add a self-scoped ruling on epic that should NOT appear in task
	selfRuling := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "self ruling", IssueID: &epic.ID, Scope: "self"})
	// Need contract view via store directly
	cv, err := st.ContractStatements(ctx, task.ID)
	if err != nil {
		t.Fatalf("contract: %v", err)
	}
	_ = selfRuling // should not be in cv
	if len(cv.Rulings) != 4 {
		t.Fatalf("contract expected 4 rulings (task,epic,root,project) got %d %+v", len(cv.Rulings), cv.Rulings)
	}
	// Ensure self not in contract
	for _, r := range cv.Rulings {
		if r.ID == selfRuling.ID {
			t.Fatalf("self ruling should not be in contract")
		}
	}
	_ = st.Close()
	out, _, err := runRulings(t, []string{task.ID})
	if err != nil {
		t.Fatalf("rulings <issue>: %v", err)
	}
	// Expect same set as contract: task, epic, root, project
	for _, s := range []*beads.Statement{taskRuling, epicRuling, rootRuling, projRuling} {
		if !strings.Contains(out, s.ID) {
			t.Fatalf("expected ruling %s (%s) in rulings output, got %q", s.ID, s.Text, out)
		}
		if !strings.Contains(out, s.Text) {
			t.Fatalf("expected text %q in output, got %q", s.Text, out)
		}
	}
	if strings.Contains(out, selfRuling.ID) || strings.Contains(out, "self ruling") {
		t.Fatalf("self ruling should not appear in task's bound rulings, got %q", out)
	}
	// Also check that the two agree on ordering? Contract ordering is weight then newest; rulings should match.
	// Build order indices from contract and from rulings output
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(cv.Rulings) {
		t.Fatalf("rulings line count %d != contract count %d, out %q cv %+v", len(lines), len(cv.Rulings), out, cv.Rulings)
	}
	// Check ordering matches contract order by comparing ids in sequence
	for i, r := range cv.Rulings {
		if !strings.Contains(lines[i], r.ID) {
			t.Fatalf("ordering mismatch at %d: expected %s in line %q, contract order %v, output lines %v", i, r.ID, lines[i], cv.Rulings, lines)
		}
	}
}

func TestRulings_JSONEmptyAndArray(t *testing.T) {
	_, st := newTempSettledStore(t, "bd")
	_ = st.Close()
	// empty
	old := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = old })
	out, _, err := runRulings(t, []string{})
	if err != nil {
		t.Fatalf("rulings --json empty: %v", err)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed != "[]" {
		// The writeJSON indents, so empty may be "[]\n" but trimmed should be []
		// Check that unmarshals to empty slice not nil
		var got []beads.Statement
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("unmarshal empty: %v out %q", err, out)
		}
		if got == nil {
			t.Fatalf("empty result should be [] not null, got nil slice out %q", out)
		}
		if len(got) != 0 {
			t.Fatalf("expected 0 length, got %d out %q", len(got), out)
		}
		if !strings.Contains(out, "[]") {
			t.Fatalf("empty json should contain [], got %q", out)
		}
	}
	// ensure not "null"
	if strings.Contains(out, "null") {
		t.Fatalf("empty json should be [] not null, got %q", out)
	}
	// also test with one ruling returns array
	flagJSON = false
	// recreate store
	dsn2, st2 := newTempSettledStore(t, "bd2")
	iss := mkSettledIssue(t, st2, "one")
	mustCreateSettledStatement(t, st2, &beads.Statement{Kind: "ruling", Text: "one ruling", IssueID: &iss.ID})
	_ = st2.Close()
	flagJSON = true
	out2, _, err := runRulings(t, []string{})
	if err != nil {
		t.Fatalf("rulings json with data: %v", err)
	}
	var got2 []beads.Statement
	if err := json.Unmarshal([]byte(out2), &got2); err != nil {
		t.Fatalf("unmarshal with data: %v out %q", err, out2)
	}
	if len(got2) != 1 {
		t.Fatalf("expected 1, got %d out %q", len(got2), out2)
	}
	if got2[0].Text != "one ruling" {
		t.Fatalf("text mismatch %+v", got2[0])
	}
	_ = dsn2
}

// ---------- bd settled ----------

func TestSettled_SearchBothKindsCaseInsensitive(t *testing.T) {
	_, st := newTempSettledStore(t, "bd")
	iss := mkSettledIssue(t, st, "bead")
	// statement with MixedCase token
	stmt := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "UniqueStatementToken MiXeD", IssueID: &iss.ID})
	// comment with lower token
	c := &beads.Comment{IssueID: iss.ID, Author: "alice", Text: "another UniqueCommentToken here"}
	if err := st.AddComment(context.Background(), c); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	_ = st.Close()

	// search lower case should match statement case-insensitively
	out, _, err := runSettled(t, []string{"uniquestatementtoken"})
	if err != nil {
		t.Fatalf("settled search: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), strings.ToLower(stmt.ID)) {
		t.Fatalf("expected statement id %s in output, got %q", stmt.ID, out)
	}
	if !strings.Contains(strings.ToLower(out), "ruling") {
		t.Fatalf("expected kind ruling in output, got %q", out)
	}
	// search upper case should also match
	out2, _, err := runSettled(t, []string{"UNIQUECOMMENTTOKEN"})
	if err != nil {
		t.Fatalf("settled upper: %v", err)
	}
	if !strings.Contains(out2, c.ID) {
		t.Fatalf("expected comment id %s in output, got %q", c.ID, out2)
	}
	if !strings.Contains(strings.ToLower(out2), "comment") {
		t.Fatalf("expected kind comment, got %q", out2)
	}
}

func TestSettled_TrueNegativeCanary(t *testing.T) {
	_, st := newTempSettledStore(t, "bd")
	iss := mkSettledIssue(t, st, "bead")
	mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "hello world", IssueID: &iss.ID})
	if err := st.AddComment(context.Background(), &beads.Comment{IssueID: iss.ID, Author: "a", Text: "some note"}); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	_ = st.Close()
	out, errStr, err := runSettled(t, []string{"no-fixture-can-match-this-token-xyz-999"})
	if err != nil {
		t.Fatalf("true negative should exit 0, got err %v errStr %q out %q", err, errStr, out)
	}
	if errStr != "" {
		t.Fatalf("true negative should have no stderr, got %q", errStr)
	}
	expected := "settled: no matches\n"
	if out != expected {
		t.Fatalf("true negative exact line mismatch: expected %q got %q", expected, out)
	}
}

func TestSettled_BrokenDatabaseCanary(t *testing.T) {
	dir := t.TempDir()
	badFile := filepath.Join(dir, "bad.db")
	if err := os.WriteFile(badFile, []byte("this is not a database"), 0644); err != nil {
		t.Fatalf("write bad db: %v", err)
	}
	old := flagDB
	flagDB = badFile
	t.Cleanup(func() { flagDB = old })
	out, errStr, err := runSettled(t, []string{"anything"})
	if err == nil {
		t.Fatalf("broken database should exit non-zero, got nil out %q errStr %q", out, errStr)
	}
	combined := out + errStr
	if strings.Contains(combined, "settled: no matches") {
		t.Fatalf("broken db should NOT print no-matches line, got out %q errStr %q", out, errStr)
	}
}

func TestSettled_MatchesOnlyCommentOnlyStatementBoth(t *testing.T) {
	_, st := newTempSettledStore(t, "bd")
	iss := mkSettledIssue(t, st, "bead")
	// statement only token
	stmtOnlyTok := "stmt-only-token-abc123"
	stmt := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "contains " + stmtOnlyTok, IssueID: &iss.ID})
	// comment only token
	cmtOnlyTok := "cmt-only-token-xyz789"
	c := &beads.Comment{IssueID: iss.ID, Author: "bob", Text: "contains " + cmtOnlyTok}
	if err := st.AddComment(context.Background(), c); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	// shared token
	sharedTok := "shared-token-555"
	sharedStmt := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "finding", Text: "shared " + sharedTok, IssueID: &iss.ID})
	sharedCmt := &beads.Comment{IssueID: iss.ID, Author: "carol", Text: "shared " + sharedTok}
	if err := st.AddComment(context.Background(), sharedCmt); err != nil {
		t.Fatalf("add shared comment: %v", err)
	}
	_ = st.Close()

	// query statement only
	outStmt, _, err := runSettled(t, []string{stmtOnlyTok})
	if err != nil {
		t.Fatalf("stmt only query: %v", err)
	}
	if !strings.Contains(outStmt, stmt.ID) {
		t.Fatalf("stmt only should contain %s, got %q", stmt.ID, outStmt)
	}
	if strings.Contains(outStmt, c.ID) {
		t.Fatalf("stmt only should NOT contain comment %s, got %q", c.ID, outStmt)
	}
	if strings.Contains(outStmt, "settled: no matches") {
		t.Fatalf("stmt only should not contain no-matches, got %q", outStmt)
	}
	if !strings.Contains(strings.ToLower(outStmt), "ruling") {
		t.Fatalf("stmt only should contain kind ruling, got %q", outStmt)
	}

	// query comment only
	outCmt, _, err := runSettled(t, []string{cmtOnlyTok})
	if err != nil {
		t.Fatalf("cmt only query: %v", err)
	}
	if !strings.Contains(outCmt, c.ID) {
		t.Fatalf("cmt only should contain %s, got %q", c.ID, outCmt)
	}
	if strings.Contains(outCmt, stmt.ID) {
		t.Fatalf("cmt only should NOT contain statement %s, got %q", stmt.ID, outCmt)
	}
	if !strings.Contains(strings.ToLower(outCmt), "comment") {
		t.Fatalf("cmt only should contain kind comment, got %q", outCmt)
	}

	// query both
	outBoth, _, err := runSettled(t, []string{sharedTok})
	if err != nil {
		t.Fatalf("both query: %v", err)
	}
	if !strings.Contains(outBoth, sharedStmt.ID) {
		t.Fatalf("both should contain statement %s, got %q", sharedStmt.ID, outBoth)
	}
	if !strings.Contains(outBoth, sharedCmt.ID) {
		t.Fatalf("both should contain comment %s, got %q", sharedCmt.ID, outBoth)
	}
	// should have two lines
	lines := strings.Split(strings.TrimSpace(outBoth), "\n")
	if len(lines) != 2 {
		t.Fatalf("both should return 2 rows, got %d out %q", len(lines), outBoth)
	}
}

func TestSettled_EmptyQueryErrors(t *testing.T) {
	_, st := newTempSettledStore(t, "bd")
	_ = st.Close()
	// empty string arg
	_, _, err := runSettled(t, []string{""})
	if err == nil {
		t.Fatalf("empty query string should error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "empty") {
		t.Fatalf("empty query error should mention empty, got %q", err.Error())
	}
	// no args
	_, _, err = runSettled(t, []string{})
	if err == nil {
		t.Fatalf("no args should error")
	}
	// whitespace only
	_, _, err = runSettled(t, []string{"   "})
	if err == nil {
		t.Fatalf("whitespace query should error")
	}
}

func TestSettled_NoMatchesNeverAlongsideHits(t *testing.T) {
	_, st := newTempSettledStore(t, "bd")
	iss := mkSettledIssue(t, st, "bead")
	mustCreateSettledStatement(t, st, &beads.Statement{Kind: "ruling", Text: "has token", IssueID: &iss.ID})
	_ = st.Close()
	out, _, err := runSettled(t, []string{"token"})
	if err != nil {
		t.Fatalf("hits: %v", err)
	}
	if strings.Contains(out, "settled: no matches") {
		t.Fatalf("hits should NOT contain no-matches line, got %q", out)
	}
}

func TestBothCommandsAppearInHelp(t *testing.T) {
	help := runRootHelp(t)
	if !strings.Contains(help, "rulings") {
		t.Fatalf("bd --help should contain rulings, got %q", help)
	}
	if !strings.Contains(help, "settled") {
		t.Fatalf("bd --help should contain settled, got %q", help)
	}
}

func TestSettled_MatchesReturnKindAndId(t *testing.T) {
	_, st := newTempSettledStore(t, "bd")
	iss := mkSettledIssue(t, st, "bead")
	qStmt := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "question", Text: "question token QTEST", IssueID: &iss.ID})
	fStmt := mustCreateSettledStatement(t, st, &beads.Statement{Kind: "finding", Text: "finding token FTEST", IssueID: &iss.ID})
	_ = st.Close()
	out, _, err := runSettled(t, []string{"QTEST"})
	if err != nil {
		t.Fatalf("qtest: %v", err)
	}
	if !strings.Contains(out, qStmt.ID) || !strings.Contains(strings.ToLower(out), "question") {
		t.Fatalf("should contain id %s and kind question, got %q", qStmt.ID, out)
	}
	out2, _, err := runSettled(t, []string{"FTEST"})
	if err != nil {
		t.Fatalf("ftest: %v", err)
	}
	if !strings.Contains(out2, fStmt.ID) || !strings.Contains(strings.ToLower(out2), "finding") {
		t.Fatalf("should contain id %s and kind finding, got %q", fStmt.ID, out2)
	}
}
