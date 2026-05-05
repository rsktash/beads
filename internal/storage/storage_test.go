package storage_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rustamsmax/beads/internal/storage"
	"github.com/rustamsmax/beads/internal/types"
)

func newSqliteStore(t *testing.T) *storage.Store {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	st, err := storage.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func mkIssue(t *testing.T, st *storage.Store, title string, p int) *types.Issue {
	t.Helper()
	i := &types.Issue{Title: title, Type: types.TypeTask, Status: types.StatusOpen, Priority: p}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create %s: %v", title, err)
	}
	return i
}

func TestReadyExcludesBlocked(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()

	a := mkIssue(t, st, "A", 0)
	b := mkIssue(t, st, "B", 1)

	if err := st.AddDependency(ctx, a.ID, b.ID, types.DepBlocks); err != nil {
		t.Fatalf("dep add: %v", err)
	}

	ready, err := st.Ready(ctx)
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != a.ID {
		t.Fatalf("expected only %s ready, got %+v", a.ID, ready)
	}

	closed := types.StatusClosed
	if _, err := st.UpdateIssue(ctx, a.ID, storage.IssueUpdate{Status: &closed}); err != nil {
		t.Fatalf("close A: %v", err)
	}
	ready, _ = st.Ready(ctx)
	if len(ready) != 1 || ready[0].ID != b.ID {
		t.Fatalf("after closing A, expected only %s ready, got %+v", b.ID, ready)
	}
}

func TestCycleDetection(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	a := mkIssue(t, st, "A", 0)
	b := mkIssue(t, st, "B", 0)
	c := mkIssue(t, st, "C", 0)

	if err := st.AddDependency(ctx, a.ID, b.ID, types.DepBlocks); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDependency(ctx, b.ID, c.ID, types.DepBlocks); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDependency(ctx, c.ID, a.ID, types.DepBlocks); err != storage.ErrCycle {
		t.Fatalf("expected ErrCycle, got %v", err)
	}
	if err := st.AddDependency(ctx, b.ID, a.ID, types.DepBlocks); err != storage.ErrCycle {
		t.Fatalf("expected ErrCycle, got %v", err)
	}
	// non-blocks edge can be added freely
	if err := st.AddDependency(ctx, c.ID, a.ID, types.DepRelatesTo); err != nil {
		t.Fatalf("relates_to should not cycle-check: %v", err)
	}
}

func TestUpdateAndFilters(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	a := mkIssue(t, st, "Alpha", 1)
	mkIssue(t, st, "Beta", 0)

	prio := 3
	out, err := st.UpdateIssue(ctx, a.ID, storage.IssueUpdate{Priority: &prio})
	if err != nil {
		t.Fatal(err)
	}
	if out.Priority != 3 {
		t.Fatalf("priority not updated: %d", out.Priority)
	}
	open := types.StatusOpen
	got, err := st.ListIssues(ctx, storage.ListFilter{Status: &open})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 open, got %d", len(got))
	}
	// list ordering: by priority asc — Beta(p0) before Alpha(p3)
	if got[0].Title != "Beta" {
		t.Fatalf("expected Beta first, got %s", got[0].Title)
	}
}
