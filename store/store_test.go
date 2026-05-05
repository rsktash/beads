package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newSqliteStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func mkIssue(t *testing.T, st *store.Store, title string, p int) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: p}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create %s: %v", title, err)
	}
	return i
}

func dep(from, to string, t beads.DependencyType) beads.Dependency {
	return beads.Dependency{IssueID: from, DependsOnID: to, Type: t}
}

func TestReadyExcludesBlocked(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()

	a := mkIssue(t, st, "A", 0)
	b := mkIssue(t, st, "B", 1)

	// b "depends on" a (blocks): meaning a blocks b. We add (issue=b, depends_on=a).
	if err := st.AddDependency(ctx, dep(b.ID, a.ID, beads.DepBlocks)); err != nil {
		t.Fatalf("dep add: %v", err)
	}

	ready, err := st.Ready(ctx)
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != a.ID {
		t.Fatalf("expected only %s ready, got %+v", a.ID, ready)
	}

	closed := beads.StatusClosed
	if _, err := st.UpdateIssue(ctx, a.ID, store.IssueUpdate{Status: &closed}); err != nil {
		t.Fatalf("close A: %v", err)
	}
	ready, _ = st.Ready(ctx)
	if len(ready) != 1 || ready[0].ID != b.ID {
		t.Fatalf("after closing A, expected only %s ready, got %+v", b.ID, ready)
	}
}

func TestPinnedBlockerIsTransparent(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	a := mkIssue(t, st, "A", 0)
	b := mkIssue(t, st, "B", 0)
	if err := st.AddDependency(ctx, dep(b.ID, a.ID, beads.DepBlocks)); err != nil {
		t.Fatal(err)
	}
	pinned := beads.StatusPinned
	if _, err := st.UpdateIssue(ctx, a.ID, store.IssueUpdate{Status: &pinned}); err != nil {
		t.Fatal(err)
	}
	ready, _ := st.Ready(ctx)
	// b is no longer blocked because A is pinned. A itself is not 'open' so not ready.
	ids := map[string]bool{}
	for _, r := range ready {
		ids[r.ID] = true
	}
	if !ids[b.ID] {
		t.Fatalf("expected B ready when blocker is pinned, got %+v", ready)
	}
}

func TestEphemeralAndDeferExcludedFromReady(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	mkIssue(t, st, "normal", 0)
	eph := &beads.Issue{Title: "eph", Type: beads.TypeMessage, Status: beads.StatusOpen, Priority: 0, Ephemeral: true}
	if err := st.CreateIssue(ctx, eph); err != nil {
		t.Fatal(err)
	}
	tomorrow := time.Now().Add(24 * time.Hour)
	deferred := &beads.Issue{Title: "deferred", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 0, DeferUntil: &tomorrow}
	if err := st.CreateIssue(ctx, deferred); err != nil {
		t.Fatal(err)
	}
	ready, _ := st.Ready(ctx)
	for _, r := range ready {
		if r.ID == eph.ID {
			t.Fatal("ephemeral should not be ready")
		}
		if r.ID == deferred.ID {
			t.Fatal("deferred should not be ready")
		}
	}
}

func TestCycleDetection(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	a := mkIssue(t, st, "A", 0)
	b := mkIssue(t, st, "B", 0)
	c := mkIssue(t, st, "C", 0)

	if err := st.AddDependency(ctx, dep(a.ID, b.ID, beads.DepBlocks)); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDependency(ctx, dep(b.ID, c.ID, beads.DepBlocks)); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDependency(ctx, dep(c.ID, a.ID, beads.DepBlocks)); err != store.ErrCycle {
		t.Fatalf("expected ErrCycle, got %v", err)
	}
	if err := st.AddDependency(ctx, dep(b.ID, a.ID, beads.DepBlocks)); err != store.ErrCycle {
		t.Fatalf("expected ErrCycle, got %v", err)
	}
	if err := st.AddDependency(ctx, dep(c.ID, a.ID, beads.DepRelatesTo)); err != nil {
		t.Fatalf("relates_to should not cycle-check: %v", err)
	}
}

func TestLabelsAndComments(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	a := mkIssue(t, st, "A", 0)
	if err := st.AddLabel(ctx, a.ID, "infra"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddLabel(ctx, a.ID, "p0"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddLabel(ctx, a.ID, "infra"); err != nil { // duplicate is no-op
		t.Fatalf("duplicate label should be silently OK: %v", err)
	}
	ls, err := st.ListLabels(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 2 {
		t.Fatalf("expected 2 labels, got %d (%v)", len(ls), ls)
	}

	c := &beads.Comment{IssueID: a.ID, Author: "alice", Text: "hello"}
	if err := st.AddComment(ctx, c); err != nil {
		t.Fatal(err)
	}
	if c.ID == "" {
		t.Fatal("comment ID should be auto-generated")
	}
	cs, _ := st.ListComments(ctx, a.ID)
	if len(cs) != 1 || cs[0].Text != "hello" {
		t.Fatalf("comment readback wrong: %+v", cs)
	}
}

func TestUpdateAndFilters(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	a := mkIssue(t, st, "Alpha", 1)
	mkIssue(t, st, "Beta", 0)

	prio := 3
	out, err := st.UpdateIssue(ctx, a.ID, store.IssueUpdate{Priority: &prio})
	if err != nil {
		t.Fatal(err)
	}
	if out.Priority != 3 {
		t.Fatalf("priority not updated: %d", out.Priority)
	}
	open := beads.StatusOpen
	got, err := st.ListIssues(ctx, store.ListFilter{Status: &open})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 open, got %d", len(got))
	}
	if got[0].Title != "Beta" {
		t.Fatalf("expected Beta first, got %s", got[0].Title)
	}
}

func TestCheckConstraints(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	bad := &beads.Issue{Title: "x", Type: "not-a-real-type", Status: beads.StatusOpen}
	if err := st.CreateIssue(ctx, bad); err == nil {
		t.Fatal("expected DB CHECK to reject invalid issue_type")
	}
}
