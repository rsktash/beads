package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func strPtr(s string) *string { return &s }

func mustCreateStatement(t *testing.T, st *store.Store, s *beads.Statement) *beads.Statement {
	t.Helper()
	if err := st.CreateStatement(context.Background(), s); err != nil {
		t.Fatalf("CreateStatement %q: %v", s.Text, err)
	}
	return s
}

func TestMigrationVersion4Applied(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	_ = st.SetConfig(ctx, store.CfgIssuePrefix, "bd")
	status, err := st.MigrationStatus(ctx)
	if err != nil {
		t.Fatalf("MigrationStatus: %v", err)
	}
	if len(status) != 7 {
		t.Fatalf("expected 7 migrations, got %d: %+v", len(status), status)
	}
	for _, m := range status {
		if !m.Applied {
			t.Fatalf("migration %d %s not applied", m.Version, m.Name)
		}
	}
	found4 := false
	for _, m := range status {
		if m.Version == 4 {
			found4 = true
		}
	}
	if !found4 {
		t.Fatalf("migration version 4 not found in status")
	}
}

func TestMigrationIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db")
	st1, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = st1.SetConfig(ctx, store.CfgIssuePrefix, "bd")
	_ = st1.Close()

	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("second open should be no-op, got: %v", err)
	}
	defer st2.Close()
	status, err := st2.MigrationStatus(ctx)
	if err != nil {
		t.Fatalf("MigrationStatus second: %v", err)
	}
	if len(status) != 6 {
		t.Fatalf("expected 6 after second open, got %d", len(status))
	}
}

// TestMigration0005_PreservesRowsAndChain asserts the SQLite 0005 rebuild
// carries both rows and the supersedes_id chain through the rename, and
// that resolved_statements is queryable again once the DROP VIEW / recreate
// finding lands (F-17).
func TestMigration0005_PreservesRowsAndChain(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db")
	st1, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = st1.SetConfig(ctx, store.CfgIssuePrefix, "bd")
	r1 := mustCreateStatement(t, st1, &beads.Statement{Kind: "ruling", Text: "original ruling"})
	r2 := mustCreateStatement(t, st1, &beads.Statement{Kind: "ruling", Text: "superseding ruling", SupersedesID: strPtr(r1.ID)})
	if err := st1.UpdateStatementStatus(ctx, r1.ID, "superseded"); err != nil {
		t.Fatalf("supersede r1: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen: migrations (including 0005) re-run against the same DSN.
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	list, err := st2.ListStatements(ctx, store.StatementFilter{})
	if err != nil {
		t.Fatalf("list after reopen: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 rows to survive the rebuild, got %d: %+v", len(list), list)
	}

	got1, err := st2.GetStatement(ctx, r1.ID)
	if err != nil {
		t.Fatalf("get r1 after reopen: %v", err)
	}
	if got1.Status != "superseded" {
		t.Fatalf("r1 status should survive as superseded, got %s", got1.Status)
	}

	got2, err := st2.GetStatement(ctx, r2.ID)
	if err != nil {
		t.Fatalf("get r2 after reopen: %v", err)
	}
	if got2.SupersedesID == nil || *got2.SupersedesID != r1.ID {
		t.Fatalf("r2.supersedes_id should still point at %s, got %v", r1.ID, got2.SupersedesID)
	}

	// resolved_statements must still be queryable post-rebuild.
	var cnt int
	if err := st2.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resolved_statements`).Scan(&cnt); err != nil {
		t.Fatalf("resolved_statements not queryable after 0005: %v", err)
	}
}

func TestStatementID_GaplessPerKind(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "gap")
	s1 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r1"})
	if s1.ID != "R-1" {
		t.Fatalf("expected R-1, got %s", s1.ID)
	}
	s2 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r2"})
	if s2.ID != "R-2" {
		t.Fatalf("expected R-2, got %s", s2.ID)
	}
	s3 := mustCreateStatement(t, st, &beads.Statement{Kind: "question", Text: "q1"})
	if s3.ID != "Q-1" {
		t.Fatalf("expected Q-1, got %s", s3.ID)
	}
	s4 := mustCreateStatement(t, st, &beads.Statement{Kind: "finding", Text: "f1"})
	if s4.ID != "F-1" {
		t.Fatalf("expected F-1, got %s", s4.ID)
	}
	s5 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r3"})
	if s5.ID != "R-3" {
		t.Fatalf("expected R-3, got %s", s5.ID)
	}
	_ = ctx
}

func TestCreateStatement_AtomicCounterOnFailure(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "atomic")
	bad := &beads.Statement{Kind: "ruling", Text: "bad", IssueID: strPtr("does-not-exist")}
	err := st.CreateStatement(ctx, bad)
	if err == nil {
		t.Fatalf("expected failure for missing issue_id")
	}
	ok := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "ok"})
	if ok.ID != "R-1" {
		t.Fatalf("counter leaked: expected R-1 after failed insert, got %s", ok.ID)
	}
	_, err = st.GetStatement(ctx, "R-1")
	if err != nil {
		t.Fatalf("R-1 should exist: %v", err)
	}
	_ = ok
}

func TestStatement_ForeignKeyEnforcement(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "fk")
	bad := &beads.Statement{Kind: "ruling", Text: "bad fk", IssueID: strPtr("missing-id")}
	if err := st.CreateStatement(ctx, bad); err == nil {
		t.Fatalf("expected FK violation for missing issue_id")
	}
	proj := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "project"})
	if proj.IssueID != nil {
		t.Fatalf("project-scoped should have nil IssueID, got %v", *proj.IssueID)
	}
	got, err := st.GetStatement(ctx, proj.ID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if got.IssueID != nil {
		t.Fatalf("retrieved project IssueID should be nil, got %v", *got.IssueID)
	}
}

func TestStatement_CascadeOnIssueDelete(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "cascade")
	issue := mkIssue(t, st, "to delete", 1)
	s := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "will cascade", IssueID: strPtr(issue.ID)})
	if s.IssueID == nil || *s.IssueID != issue.ID {
		t.Fatalf("issue link wrong")
	}
	if err := st.DeleteIssue(ctx, issue.ID); err != nil {
		t.Fatalf("delete issue: %v", err)
	}
	_, err := st.GetStatement(ctx, s.ID)
	if err == nil {
		t.Fatalf("expected statement gone after issue delete")
	}
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	list, _ := st.ListStatements(ctx, store.StatementFilter{IssueIDs: []string{issue.ID}})
	if len(list) != 0 {
		t.Fatalf("expected 0 statements after cascade, got %d", len(list))
	}
}

func TestAncestorsAndContractInheritance(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "inherit")
	root := &beads.Issue{Title: "root epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, root); err != nil {
		t.Fatalf("root: %v", err)
	}
	epic := &beads.Issue{Title: "child epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, root.ID, epic, nil); err != nil {
		t.Fatalf("epic child: %v", err)
	}
	task := &beads.Issue{Title: "leaf task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("task child: %v", err)
	}

	anc, err := st.Ancestors(ctx, task.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(anc) != 2 || anc[0] != epic.ID || anc[1] != root.ID {
		t.Fatalf("Ancestors wrong: got %v want [%s %s]", anc, epic.ID, root.ID)
	}

	own := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "own ruling", IssueID: strPtr(task.ID), Scope: "inherit"})
	epicInherit := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "epic inherit", IssueID: strPtr(epic.ID), Scope: "inherit"})
	rootInherit := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "root inherit", IssueID: strPtr(root.ID), Scope: "inherit"})
	proj := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "project ruling"})
	epicSelf := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "epic self", IssueID: strPtr(epic.ID), Scope: "self"})

	cv, err := st.ContractStatements(ctx, task.ID)
	if err != nil {
		t.Fatalf("ContractStatements: %v", err)
	}
	got := map[string]bool{}
	for _, r := range cv.Rulings {
		got[r.ID] = true
		if r.IssueID == nil && r.ID != proj.ID {
			t.Fatalf("non-project ruling %s has nil IssueID", r.ID)
		}
	}
	if !got[own.ID] {
		t.Fatalf("task own ruling missing: %s", own.ID)
	}
	if !got[epicInherit.ID] {
		t.Fatalf("epic inherit ruling missing: %s", epicInherit.ID)
	}
	if !got[rootInherit.ID] {
		t.Fatalf("root inherit missing: %s", rootInherit.ID)
	}
	if !got[proj.ID] {
		t.Fatalf("project ruling missing: %s", proj.ID)
	}
	if got[epicSelf.ID] {
		t.Fatalf("epic self ruling should be excluded but was present: %s", epicSelf.ID)
	}

	idx := func(id string) int {
		for i, r := range cv.Rulings {
			if r.ID == id {
				return i
			}
		}
		return -1
	}
	if !(idx(own.ID) < idx(epicInherit.ID) && idx(epicInherit.ID) < idx(rootInherit.ID) && idx(rootInherit.ID) < idx(proj.ID)) {
		t.Fatalf("ordering wrong: own %d epic %d root %d proj %d in %+v", idx(own.ID), idx(epicInherit.ID), idx(rootInherit.ID), idx(proj.ID), cv.Rulings)
	}

	ownSelf := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "own self", IssueID: strPtr(task.ID), Scope: "self"})
	cv2, _ := st.ContractStatements(ctx, task.ID)
	foundOwnSelf := false
	for _, r := range cv2.Rulings {
		if r.ID == ownSelf.ID {
			foundOwnSelf = true
		}
	}
	if !foundOwnSelf {
		t.Fatalf("own self-scoped ruling should be included but was excluded")
	}
}

func TestContract_SupersessionChainThree(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "chain3")
	issue := mkIssue(t, st, "chain task", 1)

	r1 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r1", IssueID: strPtr(issue.ID)})
	r2 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r2", IssueID: strPtr(issue.ID), SupersedesID: strPtr(r1.ID)})
	r3 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r3", IssueID: strPtr(issue.ID), SupersedesID: strPtr(r2.ID)})

	cv, err := st.ContractStatements(ctx, issue.ID)
	if err != nil {
		t.Fatalf("contract: %v", err)
	}
	if len(cv.Rulings) != 1 {
		t.Fatalf("expected 1 terminal ruling, got %d: %+v", len(cv.Rulings), cv.Rulings)
	}
	if cv.Rulings[0].ID != r3.ID {
		t.Fatalf("expected terminal %s, got %s", r3.ID, cv.Rulings[0].ID)
	}
	_ = r1
	_ = r2
}

func TestContract_TwoBranches(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "branch2")
	issue := mkIssue(t, st, "branch task", 1)

	r1 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "a1", IssueID: strPtr(issue.ID)})
	r2 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "a2", IssueID: strPtr(issue.ID), SupersedesID: strPtr(r1.ID)})
	r3 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "b1", IssueID: strPtr(issue.ID)})
	r4 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "b2", IssueID: strPtr(issue.ID), SupersedesID: strPtr(r3.ID)})

	cv, err := st.ContractStatements(ctx, issue.ID)
	if err != nil {
		t.Fatalf("contract: %v", err)
	}
	if len(cv.Rulings) != 2 {
		t.Fatalf("expected 2 terminals, got %d: %+v", len(cv.Rulings), cv.Rulings)
	}
	got := map[string]bool{}
	for _, r := range cv.Rulings {
		got[r.ID] = true
	}
	if !got[r2.ID] || !got[r4.ID] {
		t.Fatalf("expected terminals %s and %s, got %v", r2.ID, r4.ID, got)
	}
	if got[r1.ID] || got[r3.ID] {
		t.Fatalf("superseded rulings should not be in terminals")
	}
}

func TestContract_40Statements_SinglePass(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "perf40")
	root := &beads.Issue{Title: "root", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, root); err != nil {
		t.Fatalf("root: %v", err)
	}
	epic := &beads.Issue{Title: "epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, root.ID, epic, nil); err != nil {
		t.Fatalf("epic: %v", err)
	}
	task := &beads.Issue{Title: "task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("task: %v", err)
	}
	for i := 0; i < 10; i++ {
		mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: fmt.Sprintf("task ruling %d", i), IssueID: strPtr(task.ID)})
		mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: fmt.Sprintf("epic ruling %d", i), IssueID: strPtr(epic.ID)})
		mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: fmt.Sprintf("root ruling %d", i), IssueID: strPtr(root.ID)})
		mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: fmt.Sprintf("proj %d", i)})
	}
	start := time.Now()
	cv, err := st.ContractStatements(ctx, task.ID)
	if err != nil {
		t.Fatalf("contract: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("contract took too long: %v", elapsed)
	}
	total := len(cv.Rulings) + len(cv.Questions) + len(cv.Findings)
	if total != 40 {
		t.Fatalf("expected 40 statements in contract view, got %d (R=%d Q=%d F=%d)", total, len(cv.Rulings), len(cv.Questions), len(cv.Findings))
	}
	var cnt int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resolved_statements WHERE issue_id = ?`, task.ID).Scan(&cnt); err != nil {
		t.Fatalf("count view: %v", err)
	}
	if cnt != 40 {
		t.Fatalf("view count expected 40, got %d", cnt)
	}
}

func TestGetStatement_NotFound(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "notfound")
	_, err := st.GetStatement(ctx, "R-999")
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateStatementStatusAndSetAnsweredBy_NotFound(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "mutnotfound")
	if err := st.UpdateStatementStatus(ctx, "R-999", "retracted"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound for UpdateStatementStatus, got %v", err)
	}
	if err := st.SetAnsweredBy(ctx, "Q-999", "R-1"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound for SetAnsweredBy, got %v", err)
	}
}

func TestUpdateStatementStatus(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "upd")
	s := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "to update"})
	if err := st.UpdateStatementStatus(ctx, s.ID, "retracted"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := st.GetStatement(ctx, s.ID)
	if got.Status != "retracted" {
		t.Fatalf("expected retracted, got %s", got.Status)
	}
}

func TestSetAnsweredBy(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "ans")
	q := mustCreateStatement(t, st, &beads.Statement{Kind: "question", Text: "open q"})
	r := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "answer"})
	if err := st.SetAnsweredBy(ctx, q.ID, r.ID); err != nil {
		t.Fatalf("setAnswered: %v", err)
	}
	got, _ := st.GetStatement(ctx, q.ID)
	if got.AnsweredBy == nil || *got.AnsweredBy != r.ID {
		t.Fatalf("answered_by not set, got %v", got.AnsweredBy)
	}
}

func TestListStatementsOrderingAndFilter(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "list")
	t1 := time.Now().Add(-3 * time.Hour).UTC()
	t2 := time.Now().Add(-2 * time.Hour).UTC()
	t3 := time.Now().Add(-1 * time.Hour).UTC()
	s1 := &beads.Statement{Kind: "ruling", Text: "old", CreatedAt: t1}
	s2 := &beads.Statement{Kind: "ruling", Text: "mid", CreatedAt: t2}
	s3 := &beads.Statement{Kind: "question", Text: "new", CreatedAt: t3}
	mustCreateStatement(t, st, s1)
	mustCreateStatement(t, st, s2)
	mustCreateStatement(t, st, s3)

	list, err := st.ListStatements(ctx, store.StatementFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	if list[0].Text != "new" || list[1].Text != "mid" || list[2].Text != "old" {
		t.Fatalf("ordering wrong newest first: %+v", list)
	}
	rl, _ := st.ListStatements(ctx, store.StatementFilter{Kinds: []string{"ruling"}})
	if len(rl) != 2 {
		t.Fatalf("expected 2 rulings, got %d", len(rl))
	}
	active, _ := st.ListStatements(ctx, store.StatementFilter{Statuses: []string{"active"}})
	if len(active) != 3 {
		t.Fatalf("expected 3 active, got %d", len(active))
	}
}

func TestPostgresStatements(t *testing.T) {
	dsn := os.Getenv("BD_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("BD_TEST_POSTGRES_DSN not set — skipping postgres test")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	sep := "?"
	if contains(dsn, "?") {
		sep = "&"
	}
	pgDSN := fmt.Sprintf("%s%ssearch_path=%s", dsn, sep, schema)
	st, err := store.Open(ctx, pgDSN)
	if err != nil {
		t.Fatalf("postgres open: %v", err)
	}
	defer func() {
		db := st.DB()
		_, _ = db.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
		_ = st.Close()
	}()
	_ = st.SetConfig(ctx, store.CfgIssuePrefix, "pg")

	r := &beads.Statement{Kind: "ruling", Text: "pg ruling"}
	if err := st.CreateStatement(ctx, r); err != nil {
		t.Fatalf("create ruling pg: %v", err)
	}
	if r.ID != "R-1" {
		t.Fatalf("expected R-1 pg, got %s", r.ID)
	}
	q := &beads.Statement{Kind: "question", Text: "pg question"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create question pg: %v", err)
	}
	f := &beads.Statement{Kind: "finding", Text: "pg finding"}
	if err := st.CreateStatement(ctx, f); err != nil {
		t.Fatalf("create finding pg: %v", err)
	}
	if _, err := st.GetStatement(ctx, r.ID); err != nil {
		t.Fatalf("get ruling pg: %v", err)
	}
	if _, err := st.GetStatement(ctx, q.ID); err != nil {
		t.Fatalf("get question pg: %v", err)
	}
	if _, err := st.GetStatement(ctx, f.ID); err != nil {
		t.Fatalf("get finding pg: %v", err)
	}
	list, err := st.ListStatements(ctx, store.StatementFilter{})
	if err != nil {
		t.Fatalf("list pg: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 pg statements, got %d", len(list))
	}
	status, err := st.MigrationStatus(ctx)
	if err != nil {
		t.Fatalf("migration status pg: %v", err)
	}
	if len(status) != 7 {
		t.Fatalf("expected 7 migrations pg, got %d", len(status))
	}
	var cnt int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resolved_statements`).Scan(&cnt); err != nil {
		t.Fatalf("view not queryable pg: %v", err)
	}
	var viewCnt int
	qry := fmt.Sprintf(`SELECT COUNT(*) FROM resolved_statements WHERE statement_id = '%s'`, r.ID)
	if err := st.DB().QueryRowContext(ctx, qry).Scan(&viewCnt); err != nil {
		t.Fatalf("view query pg: %v", err)
	}
	_ = cnt
	_ = viewCnt
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

func TestStatementEvidenceAndSourceComment(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "evidence")
	issue := mkIssue(t, st, "with comment", 1)
	c := &beads.Comment{IssueID: issue.ID, Author: "alice", Text: "legacy"}
	if err := st.AddComment(ctx, c); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	s := &beads.Statement{Kind: "ruling", Text: "with evidence", IssueID: strPtr(issue.ID), Evidence: "src/foo.go:10", SourceCommentID: strPtr(c.ID)}
	mustCreateStatement(t, st, s)
	got, err := st.GetStatement(ctx, s.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Evidence != "src/foo.go:10" {
		t.Fatalf("evidence mismatch: %s", got.Evidence)
	}
	if got.SourceCommentID == nil || *got.SourceCommentID != c.ID {
		t.Fatalf("source_comment_id mismatch: %v", got.SourceCommentID)
	}
	db := st.DB()
	_, err = db.ExecContext(ctx, "DELETE FROM comments WHERE id = ?", c.ID)
	if err != nil {
		t.Fatalf("delete comment raw: %v", err)
	}
	got2, _ := st.GetStatement(ctx, s.ID)
	if got2.SourceCommentID != nil {
		t.Fatalf("expected SourceCommentID nil after comment delete, got %v", *got2.SourceCommentID)
	}
}

func TestResolvedStatements_Origin(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "origin")
	root := &beads.Issue{Title: "root epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, root); err != nil {
		t.Fatalf("root: %v", err)
	}
	epic := &beads.Issue{Title: "child epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, root.ID, epic, nil); err != nil {
		t.Fatalf("epic: %v", err)
	}
	task := &beads.Issue{Title: "leaf task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("task: %v", err)
	}
	taskRuling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "task ruling", IssueID: strPtr(task.ID)})
	epicRuling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "epic ruling", IssueID: strPtr(epic.ID)})
	rootRuling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "root ruling", IssueID: strPtr(root.ID)})
	projRuling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "project ruling"})

	q := `SELECT statement_id, origin_kind, origin_issue_id, depth FROM resolved_statements WHERE issue_id = ?`
	rows, err := st.DB().QueryContext(ctx, q, task.ID)
	if err != nil {
		t.Fatalf("query view: %v", err)
	}
	defer rows.Close()
	type row struct {
		stmtID string
		kind   string
		origin sql.NullString
		depth  sql.NullInt64
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.stmtID, &r.kind, &r.origin, &r.depth); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 rows for task, got %d: %+v", len(got), got)
	}
	m := map[string]row{}
	for _, r := range got {
		m[r.stmtID] = r
	}
	if r, ok := m[taskRuling.ID]; !ok {
		t.Fatalf("task own row missing")
	} else {
		if r.kind != "self" {
			t.Fatalf("task own origin_kind expected self, got %s", r.kind)
		}
		if r.origin.Valid {
			t.Fatalf("task own origin_issue_id should be NULL, got %s", r.origin.String)
		}
		if !r.depth.Valid || r.depth.Int64 != 0 {
			t.Fatalf("task own depth expected 0, got %+v", r.depth)
		}
	}
	if r, ok := m[epicRuling.ID]; !ok {
		t.Fatalf("epic row missing")
	} else {
		if r.kind != "epic" {
			t.Fatalf("epic origin_kind expected epic, got %s", r.kind)
		}
		if !r.origin.Valid || r.origin.String != epic.ID {
			t.Fatalf("epic origin_issue_id expected %s, got %+v", epic.ID, r.origin)
		}
	}
	if r, ok := m[rootRuling.ID]; !ok {
		t.Fatalf("root row missing")
	} else {
		if r.kind != "epic" {
			t.Fatalf("root origin_kind expected epic, got %s", r.kind)
		}
		if !r.origin.Valid || r.origin.String != root.ID {
			t.Fatalf("root origin_issue_id expected %s, got %+v", root.ID, r.origin)
		}
	}
	if r, ok := m[projRuling.ID]; !ok {
		t.Fatalf("project row missing")
	} else {
		if r.kind != "project" {
			t.Fatalf("project origin_kind expected project, got %s", r.kind)
		}
		if r.origin.Valid {
			t.Fatalf("project origin_issue_id should be NULL, got %s", r.origin.String)
		}
		if r.depth.Valid {
			t.Fatalf("project depth should be NULL, got %d", r.depth.Int64)
		}
	}
}

func TestResolvedStatements_AncestorDistance(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "distance")
	root := &beads.Issue{Title: "root", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, root); err != nil {
		t.Fatalf("root: %v", err)
	}
	epic := &beads.Issue{Title: "epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, root.ID, epic, nil); err != nil {
		t.Fatalf("epic: %v", err)
	}
	task := &beads.Issue{Title: "task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("task: %v", err)
	}
	epicRuling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "epic ruling", IssueID: strPtr(epic.ID)})
	rootRuling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "root ruling", IssueID: strPtr(root.ID)})

	q := `SELECT statement_id, depth FROM resolved_statements WHERE issue_id = ? AND origin_kind = 'epic' ORDER BY depth`
	rows, err := st.DB().QueryContext(ctx, q, task.ID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	depths := map[string]int64{}
	for rows.Next() {
		var sid string
		var d sql.NullInt64
		if err := rows.Scan(&sid, &d); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if d.Valid {
			depths[sid] = d.Int64
		}
	}
	epicDepth, ok1 := depths[epicRuling.ID]
	rootDepth, ok2 := depths[rootRuling.ID]
	if !ok1 || !ok2 {
		t.Fatalf("missing epic or root row: %+v", depths)
	}
	if epicDepth == rootDepth {
		t.Fatalf("epic and root depths should be distinct, got %d both", epicDepth)
	}
	if epicDepth > rootDepth {
		t.Fatalf("nearer epic depth %d should be smaller than root depth %d", epicDepth, rootDepth)
	}
}

func TestResolvedStatements_Supersession(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "supersede")
	issue := mkIssue(t, st, "chain", 1)
	r1 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r1", IssueID: strPtr(issue.ID)})
	r2 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r2", IssueID: strPtr(issue.ID), SupersedesID: strPtr(r1.ID)})
	r3 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r3", IssueID: strPtr(issue.ID), SupersedesID: strPtr(r2.ID)})

	q := `SELECT statement_id FROM resolved_statements WHERE issue_id = ?`
	rows, err := st.DB().QueryContext(ctx, q, issue.ID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var ids []string
	for rows.Next() {
		var sid string
		rows.Scan(&sid)
		ids = append(ids, sid)
	}
	rows.Close()
	if len(ids) != 1 || ids[0] != r3.ID {
		t.Fatalf("expected exactly terminal %s, got %v (r1=%s r2=%s r3=%s)", r3.ID, ids, r1.ID, r2.ID, r3.ID)
	}
	issue2 := mkIssue(t, st, "branch", 1)
	a1 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "a1", IssueID: strPtr(issue2.ID)})
	a2 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "a2", IssueID: strPtr(issue2.ID), SupersedesID: strPtr(a1.ID)})
	b1 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "b1", IssueID: strPtr(issue2.ID)})
	b2 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "b2", IssueID: strPtr(issue2.ID), SupersedesID: strPtr(b1.ID)})
	rows2, err := st.DB().QueryContext(ctx, q, issue2.ID)
	if err != nil {
		t.Fatalf("query2: %v", err)
	}
	var ids2 []string
	for rows2.Next() {
		var sid string
		rows2.Scan(&sid)
		ids2 = append(ids2, sid)
	}
	rows2.Close()
	if len(ids2) != 2 {
		t.Fatalf("expected 2 terminals, got %d: %v", len(ids2), ids2)
	}
	m := map[string]bool{}
	for _, id := range ids2 {
		m[id] = true
	}
	if !m[a2.ID] || !m[b2.ID] {
		t.Fatalf("expected terminals %s and %s, got %v", a2.ID, b2.ID, ids2)
	}
	if m[a1.ID] || m[b1.ID] {
		t.Fatalf("superseded should not appear: %v", ids2)
	}
}

func TestResolvedStatements_NonActiveStatus(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "nonactive")
	statuses := []string{"superseded", "answered", "retracted", "candidate"}
	for _, s := range statuses {
		issue := mkIssue(t, st, "nonactive "+s, 1)
		r := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "ruling " + s, IssueID: strPtr(issue.ID), Status: s})
		q := `SELECT COUNT(*) FROM resolved_statements WHERE statement_id = ? AND issue_id = ?`
		var cnt int
		if err := st.DB().QueryRowContext(ctx, q, r.ID, issue.ID).Scan(&cnt); err != nil {
			t.Fatalf("count %s: %v", s, err)
		}
		if cnt != 0 {
			t.Fatalf("status %s should not appear in view, got %d rows for %s", s, cnt, r.ID)
		}
		cv, _ := st.ContractStatements(ctx, issue.ID)
		for _, ru := range cv.Rulings {
			if ru.ID == r.ID {
				t.Fatalf("non-active %s ruling %s should not appear in ContractStatements", s, r.ID)
			}
		}
	}
}

func TestResolvedStatements_Narrowing(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "narrow")
	epic := &beads.Issue{Title: "epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, epic); err != nil {
		t.Fatalf("epic: %v", err)
	}
	task := &beads.Issue{Title: "task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("task: %v", err)
	}
	selfRuling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "self scope", IssueID: strPtr(epic.ID), Scope: "self"})
	inheritRuling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "inherit scope", IssueID: strPtr(epic.ID), Scope: "inherit"})

	q := `SELECT statement_id FROM resolved_statements WHERE issue_id = ?`
	rows, err := st.DB().QueryContext(ctx, q, task.ID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	foundSelf, foundInherit := false, false
	for rows.Next() {
		var sid string
		rows.Scan(&sid)
		if sid == selfRuling.ID {
			foundSelf = true
		}
		if sid == inheritRuling.ID {
			foundInherit = true
		}
	}
	rows.Close()
	if foundSelf {
		t.Fatalf("ancestor ruling with scope self should not appear for descendant, found %s", selfRuling.ID)
	}
	if !foundInherit {
		t.Fatalf("ancestor ruling with default scope should appear for descendant, missing %s", inheritRuling.ID)
	}
	ownSelf := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "own self", IssueID: strPtr(task.ID), Scope: "self"})
	qq := `SELECT COUNT(*) FROM resolved_statements WHERE issue_id = ? AND statement_id = ?`
	var cnt2 int
	if err := st.DB().QueryRowContext(ctx, qq, task.ID, ownSelf.ID).Scan(&cnt2); err != nil {
		t.Fatalf("count ownSelf: %v", err)
	}
	if cnt2 != 1 {
		t.Fatalf("bead's own ruling with scope self should still produce its own row, got %d", cnt2)
	}
}

func TestResolvedStatements_QuestionFindingNonInheritance(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "qfind")
	epic := &beads.Issue{Title: "epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, epic); err != nil {
		t.Fatalf("epic: %v", err)
	}
	task := &beads.Issue{Title: "task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("task: %v", err)
	}
	qOnEpic := mustCreateStatement(t, st, &beads.Statement{Kind: "question", Text: "epic question", IssueID: strPtr(epic.ID)})
	qq := `SELECT COUNT(*) FROM resolved_statements WHERE issue_id = ? AND statement_id = ?`
	var cnt int
	if err := st.DB().QueryRowContext(ctx, qq, task.ID, qOnEpic.ID).Scan(&cnt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("question on ancestor epic should produce no row for child task, got %d", cnt)
	}
	projQ := mustCreateStatement(t, st, &beads.Statement{Kind: "question", Text: "project question"})
	var cnt2 int
	if err := st.DB().QueryRowContext(ctx, qq, task.ID, projQ.ID).Scan(&cnt2); err != nil {
		t.Fatalf("proj query: %v", err)
	}
	if cnt2 != 0 {
		t.Fatalf("project-scoped question should produce no row for any bead, got %d for task", cnt2)
	}
	var cnt3 int
	if err := st.DB().QueryRowContext(ctx, qq, epic.ID, projQ.ID).Scan(&cnt3); err != nil {
		t.Fatalf("proj query epic: %v", err)
	}
	if cnt3 != 0 {
		t.Fatalf("project-scoped question should produce no row for any bead, got %d for epic", cnt3)
	}
	fOnEpic := mustCreateStatement(t, st, &beads.Statement{Kind: "finding", Text: "epic finding", IssueID: strPtr(epic.ID)})
	var cnt4 int
	if err := st.DB().QueryRowContext(ctx, qq, task.ID, fOnEpic.ID).Scan(&cnt4); err != nil {
		t.Fatalf("finding query: %v", err)
	}
	if cnt4 != 0 {
		t.Fatalf("finding on ancestor should not inherit, got %d", cnt4)
	}
}

var _ = sql.ErrNoRows

// TestResolved_BlocksInheritance asserts a ruling on a bead that blocks a
// second bead reaches that second bead's contract, carrying its blocker's id
// as origin (never the target bead's own id).
func TestResolved_BlocksInheritance(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "blocksinherit")
	a := mkIssue(t, st, "blocker A", 1)
	b := mkIssue(t, st, "blocked B", 1)
	if err := st.AddDependency(ctx, beads.Dependency{IssueID: b.ID, DependsOnID: a.ID, Type: beads.DepBlocks}); err != nil {
		t.Fatalf("A blocks B: %v", err)
	}
	ruling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "ruling on A", IssueID: strPtr(a.ID)})

	cv, err := st.ContractStatements(ctx, b.ID)
	if err != nil {
		t.Fatalf("contract B: %v", err)
	}
	var found *beads.Statement
	for i := range cv.Rulings {
		if cv.Rulings[i].ID == ruling.ID {
			found = &cv.Rulings[i]
		}
	}
	if found == nil {
		t.Fatalf("expected B's contract to hold ruling from blocker A, got %+v", cv.Rulings)
	}
	if found.IssueID == nil || *found.IssueID != a.ID {
		t.Fatalf("origin should be blocker A (%s), got %v", a.ID, found.IssueID)
	}
	if found.IssueID != nil && *found.IssueID == b.ID {
		t.Fatalf("origin must not be the target bead B itself")
	}
}

// TestResolved_BlocksOneHopOnly asserts the blocks arm is not transitive: A
// blocks B blocks C, a ruling on A does not reach C.
func TestResolved_BlocksOneHopOnly(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "blocksonehop")
	a := mkIssue(t, st, "A", 1)
	b := mkIssue(t, st, "B", 1)
	c := mkIssue(t, st, "C", 1)
	if err := st.AddDependency(ctx, beads.Dependency{IssueID: b.ID, DependsOnID: a.ID, Type: beads.DepBlocks}); err != nil {
		t.Fatalf("A blocks B: %v", err)
	}
	if err := st.AddDependency(ctx, beads.Dependency{IssueID: c.ID, DependsOnID: b.ID, Type: beads.DepBlocks}); err != nil {
		t.Fatalf("B blocks C: %v", err)
	}
	ruling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "ruling on A one hop", IssueID: strPtr(a.ID)})

	cvB, err := st.ContractStatements(ctx, b.ID)
	if err != nil {
		t.Fatalf("contract B: %v", err)
	}
	foundB := false
	for _, r := range cvB.Rulings {
		if r.ID == ruling.ID {
			foundB = true
		}
	}
	if !foundB {
		t.Fatalf("B (one hop from A) should hold the ruling")
	}

	cvC, err := st.ContractStatements(ctx, c.ID)
	if err != nil {
		t.Fatalf("contract C: %v", err)
	}
	for _, r := range cvC.Rulings {
		if r.ID == ruling.ID {
			t.Fatalf("C (two hops from A) should not hold the ruling, blocks is one hop only")
		}
	}
}

// TestResolved_BlocksSkipsQuestions asserts only rulings cross a blocks edge —
// a question filed on the blocker never reaches the blocked bead.
func TestResolved_BlocksSkipsQuestions(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "blocksskipsq")
	a := mkIssue(t, st, "A", 1)
	b := mkIssue(t, st, "B", 1)
	if err := st.AddDependency(ctx, beads.Dependency{IssueID: b.ID, DependsOnID: a.ID, Type: beads.DepBlocks}); err != nil {
		t.Fatalf("A blocks B: %v", err)
	}
	q := mustCreateStatement(t, st, &beads.Statement{Kind: "question", Text: "question on A", IssueID: strPtr(a.ID)})

	cv, err := st.ContractStatements(ctx, b.ID)
	if err != nil {
		t.Fatalf("contract B: %v", err)
	}
	for _, qq := range cv.Questions {
		if qq.ID == q.ID {
			t.Fatalf("question on blocker A should not reach blocked B")
		}
	}
}

// TestResolved_BlocksRespectsScopeSelf asserts scope=self on the blocks arm
// mirrors the epic arm: a self-scoped ruling on the blocker stops there.
func TestResolved_BlocksRespectsScopeSelf(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "blocksself")
	a := mkIssue(t, st, "A", 1)
	b := mkIssue(t, st, "B", 1)
	if err := st.AddDependency(ctx, beads.Dependency{IssueID: b.ID, DependsOnID: a.ID, Type: beads.DepBlocks}); err != nil {
		t.Fatalf("A blocks B: %v", err)
	}
	ruling := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "self scoped ruling on A", IssueID: strPtr(a.ID), Scope: "self"})

	cv, err := st.ContractStatements(ctx, b.ID)
	if err != nil {
		t.Fatalf("contract B: %v", err)
	}
	for _, r := range cv.Rulings {
		if r.ID == ruling.ID {
			t.Fatalf("scope=self ruling on blocker A should not reach blocked B")
		}
	}
}
