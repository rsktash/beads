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
	return newSqliteStoreWithPrefix(t, "bd")
}

func newSqliteStoreWithPrefix(t *testing.T, prefix string) *store.Store {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, prefix); err != nil {
		t.Fatalf("set prefix: %v", err)
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

func TestPrefixAndIDFormat(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "yuklar")

	i := &beads.Issue{Title: "first", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, i); err != nil {
		t.Fatal(err)
	}
	if !startsWith(i.ID, "yuklar-") {
		t.Fatalf("id should start with 'yuklar-', got %q", i.ID)
	}
	hash := i.ID[len("yuklar-"):]
	if len(hash) < 3 || len(hash) > 8 {
		t.Fatalf("hash length out of range: %q", hash)
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) {
			t.Fatalf("hash should be base36, got char %q in %q", c, hash)
		}
	}

	// Two issues with identical content but different timestamps must get
	// different ids (timestamp is part of the seed).
	time.Sleep(2 * time.Millisecond)
	j := &beads.Issue{Title: "first", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, j); err != nil {
		t.Fatal(err)
	}
	if i.ID == j.ID {
		t.Fatalf("expected distinct ids for distinct timestamps: both got %s", i.ID)
	}
}

func TestNoPrefixError(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	i := &beads.Issue{Title: "x", Type: beads.TypeTask, Status: beads.StatusOpen}
	err = st.CreateIssue(ctx, i)
	if err == nil {
		t.Fatal("expected ErrNoPrefix when issue_prefix unset")
	}
}

func TestCounterMode(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "yk")
	if err := st.SetConfig(ctx, store.CfgIssueIDMode, store.IDModeCounter); err != nil {
		t.Fatal(err)
	}
	a := &beads.Issue{Title: "one", Type: beads.TypeTask, Status: beads.StatusOpen}
	b := &beads.Issue{Title: "two", Type: beads.TypeTask, Status: beads.StatusOpen}
	if err := st.CreateIssue(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateIssue(ctx, b); err != nil {
		t.Fatal(err)
	}
	if a.ID != "yk-1" || b.ID != "yk-2" {
		t.Fatalf("counter ids wrong: a=%s b=%s", a.ID, b.ID)
	}
}

func TestNextChildIDAtomic(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStore(t)
	parent := mkIssue(t, st, "epic", 0)

	c1, err := st.NextChildID(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := st.NextChildID(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c1 != parent.ID+".1" {
		t.Fatalf("first child should be %s.1, got %s", parent.ID, c1)
	}
	if c2 != parent.ID+".2" {
		t.Fatalf("second child should be %s.2, got %s", parent.ID, c2)
	}
}

func startsWith(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func TestPostgresSearchPathExtraction(t *testing.T) {
	// pure unit test of the DSN parser; no postgres connection involved.
	cases := []struct {
		dsn   string
		want  string
		hasIt bool
	}{
		{"postgres://u@h/db?search_path=yuklar", "yuklar", true},
		{"postgres://u@h/db?search_path=auth&sslmode=disable", "auth", true},
		{"postgres://u@h/db", "", false},
		{"postgres://u@h/db?sslmode=disable", "", false},
	}
	for _, c := range cases {
		got, ok := store.PostgresSearchPathForTest(c.dsn)
		if got != c.want || ok != c.hasIt {
			t.Errorf("postgresSearchPath(%q) = (%q,%v); want (%q,%v)", c.dsn, got, ok, c.want, c.hasIt)
		}
	}
}

func TestPriorityCheckConstraint(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	// Custom statuses/types are user-configurable so we no longer CHECK them
	// at the DB level. Priority is still range-checked (0..4).
	bad := &beads.Issue{Title: "x", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 99}
	if err := st.CreateIssue(ctx, bad); err == nil {
		t.Fatal("expected priority CHECK to reject 99")
	}
}
