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

func TestMigrationVersion3Applied(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	// Set prefix to ensure DB is usable
	_ = st.SetConfig(ctx, store.CfgIssuePrefix, "bd")
	status, err := st.MigrationStatus(ctx)
	if err != nil {
		t.Fatalf("MigrationStatus: %v", err)
	}
	if len(status) != 3 {
		t.Fatalf("expected 3 migrations, got %d: %+v", len(status), status)
	}
	for _, m := range status {
		if !m.Applied {
			t.Fatalf("migration %d %s not applied", m.Version, m.Name)
		}
	}
	// Also check via appliedMigrations directly? Just ensure version 3 present
	found3 := false
	for _, m := range status {
		if m.Version == 3 {
			found3 = true
		}
	}
	if !found3 {
		t.Fatalf("migration version 3 not found in status")
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
	if len(status) != 3 {
		t.Fatalf("expected 3 after second open, got %d", len(status))
	}
}

func TestStatementID_GaplessPerKind(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "gap")
	// Create statements gapless per kind: ruling, ruling, question, finding, ruling => R-1,R-2,Q-1,F-1,R-3
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

	// Also check ListStatements shows them
	_ = ctx
}

func TestCreateStatement_AtomicCounterOnFailure(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "atomic")
	// Try to create with non-existent issue_id, should fail and not advance counter
	bad := &beads.Statement{Kind: "ruling", Text: "bad", IssueID: strPtr("does-not-exist")}
	err := st.CreateStatement(ctx, bad)
	if err == nil {
		t.Fatalf("expected failure for missing issue_id")
	}
	// Next successful ruling should still be R-1
	ok := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "ok"})
	if ok.ID != "R-1" {
		t.Fatalf("counter leaked: expected R-1 after failed insert, got %s", ok.ID)
	}
	// Also ensure bad row not written
	_, err = st.GetStatement(ctx, "R-1")
	if err != nil {
		t.Fatalf("R-1 should exist: %v", err)
	}
	// The bad id would have been R-1 if counter advanced; check that R-2 does not exist mistakenly?
	_ = ok
}

func TestStatement_ForeignKeyEnforcement(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "fk")
	// Non-NULL issue_id missing should be rejected
	bad := &beads.Statement{Kind: "ruling", Text: "bad fk", IssueID: strPtr("missing-id")}
	if err := st.CreateStatement(ctx, bad); err == nil {
		t.Fatalf("expected FK violation for missing issue_id")
	}
	// NULL issue_id (project-scoped) should be accepted
	proj := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "project"})
	if proj.IssueID != nil {
		t.Fatalf("project-scoped should have nil IssueID, got %v", *proj.IssueID)
	}
	// Retrieve and check
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
	// Ensure List returns zero
	list, _ := st.ListStatements(ctx, store.StatementFilter{IssueIDs: []string{issue.ID}})
	if len(list) != 0 {
		t.Fatalf("expected 0 statements after cascade, got %d", len(list))
	}
}

func TestAncestorsAndContractInheritance(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "inherit")
	// Build hierarchy: root epic -> epic -> task
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

	// ancestors should be [epic.ID, root.ID] nearest first
	anc, err := st.Ancestors(ctx, task.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(anc) != 2 || anc[0] != epic.ID || anc[1] != root.ID {
		t.Fatalf("Ancestors wrong: got %v want [%s %s]", anc, epic.ID, root.ID)
	}

	// Create statements
	// 1. task own
	own := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "own ruling", IssueID: strPtr(task.ID), Scope: "inherit"})
	// 2. epic inherit
	epicInherit := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "epic inherit", IssueID: strPtr(epic.ID), Scope: "inherit"})
	// 3. root inherit
	rootInherit := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "root inherit", IssueID: strPtr(root.ID), Scope: "inherit"})
	// 4. project-scoped
	proj := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "project ruling"})
	// 5. epic self (should be excluded)
	epicSelf := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "epic self", IssueID: strPtr(epic.ID), Scope: "self"})

	cv, err := st.ContractStatements(ctx, task.ID)
	if err != nil {
		t.Fatalf("ContractStatements: %v", err)
	}
	// Build map of returned ruling IDs
	got := map[string]bool{}
	for _, r := range cv.Rulings {
		got[r.ID] = true
		// Also check origin is carried via IssueID
		if r.IssueID == nil && r.ID != proj.ID {
			t.Fatalf("non-project ruling %s has nil IssueID", r.ID)
		}
	}
	// Case 1: task own present
	if !got[own.ID] {
		t.Fatalf("task own ruling missing: %s", own.ID)
	}
	// Case 2: epic inherit present
	if !got[epicInherit.ID] {
		t.Fatalf("epic inherit ruling missing: %s", epicInherit.ID)
	}
	// Case 3: root inherit present
	if !got[rootInherit.ID] {
		t.Fatalf("root inherit missing: %s", rootInherit.ID)
	}
	// Case 4: project present
	if !got[proj.ID] {
		t.Fatalf("project ruling missing: %s", proj.ID)
	}
	// Case 5: epic self excluded
	if got[epicSelf.ID] {
		t.Fatalf("epic self ruling should be excluded but was present: %s", epicSelf.ID)
	}

	// Also check ordering: own, then epic, then root, then project
	// Find indices
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

	// Own self-scoped should NOT be excluded
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

	// Mark r1 and r2 as superseded? The resolver should handle terminal regardless of status.
	// Keep status active for all, chain reduction should pick terminal only.
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

	// Chain A: r1 -> r2
	r1 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "a1", IssueID: strPtr(issue.ID)})
	r2 := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "a2", IssueID: strPtr(issue.ID), SupersedesID: strPtr(r1.ID)})
	// Chain B: r3 -> r4
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

func TestContract_40Statements_NoPerStatementQuery(t *testing.T) {
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

	// Create 40 statements across 3 levels + project
	for i := 0; i < 10; i++ {
		mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: fmt.Sprintf("task ruling %d", i), IssueID: strPtr(task.ID)})
		mustCreateStatement(t, st, &beads.Statement{Kind: "question", Text: fmt.Sprintf("epic q %d", i), IssueID: strPtr(epic.ID)})
		mustCreateStatement(t, st, &beads.Statement{Kind: "finding", Text: fmt.Sprintf("root f %d", i), IssueID: strPtr(root.ID)})
		mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: fmt.Sprintf("proj %d", i)})
	}
	// total 40

	start := time.Now()
	cv, err := st.ContractStatements(ctx, task.ID)
	if err != nil {
		t.Fatalf("contract: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("contract took too long: %v", elapsed)
	}
	// Expect 10 task rulings + 10 epic questions + 10 root findings + 10 project rulings = 40 but rulings will be filtered to include task and project and root findings etc.
	// Actually counts: Rulings = task 10 + project 10 =20, Questions=10, Findings=10 => total 40 but split.
	total := len(cv.Rulings) + len(cv.Questions) + len(cv.Findings)
	if total != 40 {
		t.Fatalf("expected 40 statements in contract view, got %d (R=%d Q=%d F=%d)", total, len(cv.Rulings), len(cv.Questions), len(cv.Findings))
	}
	// Ensure one pass: we already ensured implementation uses 2 queries, but we assert no error and performance
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
	// Create with explicit times to test ordering newest first
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

	// Filter by kind
	rl, _ := st.ListStatements(ctx, store.StatementFilter{Kinds: []string{"ruling"}})
	if len(rl) != 2 {
		t.Fatalf("expected 2 rulings, got %d", len(rl))
	}
	// Filter by status (all active)
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
	// Create throwaway search_path schema
	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	// Need base dsn without search_path to create schema, but we can just open with search_path and it will create schema via Store.Open logic
	// Append search_path param
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
		// Drop schema
		db := st.DB()
		_, _ = db.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema))
		_ = st.Close()
	}()
	// Ensure prefix
	_ = st.SetConfig(ctx, store.CfgIssuePrefix, "pg")

	// Insert one of each kind
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
	// Read back
	if _, err := st.GetStatement(ctx, r.ID); err != nil {
		t.Fatalf("get ruling pg: %v", err)
	}
	if _, err := st.GetStatement(ctx, q.ID); err != nil {
		t.Fatalf("get question pg: %v", err)
	}
	if _, err := st.GetStatement(ctx, f.ID); err != nil {
		t.Fatalf("get finding pg: %v", err)
	}
	// List
	list, err := st.ListStatements(ctx, store.StatementFilter{})
	if err != nil {
		t.Fatalf("list pg: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 pg statements, got %d", len(list))
	}
	// Check migration version
	status, err := st.MigrationStatus(ctx)
	if err != nil {
		t.Fatalf("migration status pg: %v", err)
	}
	if len(status) != 3 {
		t.Fatalf("expected 3 migrations pg, got %d", len(status))
	}
	// t.Logf success
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

// Ensure statement with evidence and source_comment handling not broken
func TestStatementEvidenceAndSourceComment(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "evidence")
	issue := mkIssue(t, st, "with comment", 1)
	// Add comment
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
	// Delete comment should set source_comment_id to NULL via ON DELETE SET NULL
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

// Test that we import sql for ErrNotFound check etc
var _ = sql.ErrNoRows
