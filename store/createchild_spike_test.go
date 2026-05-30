package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
)

// TestOldPath_LeaksCounter demonstrates the bug the fix removes: the OLD
// non-transactional sequence (NextChildID, THEN a separate CreateIssue) leaves
// the child counter advanced when the insert fails — a permanent gap.
func TestOldPath_LeaksCounter(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "old")
	parent := &beads.Issue{Title: "Parent", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, parent); err != nil {
		t.Fatalf("parent: %v", err)
	}

	// child .1 the old way: alloc id (counter->1), insert ok.
	id1, _ := st.NextChildID(ctx, parent.ID)
	if err := st.CreateIssue(ctx, &beads.Issue{ID: id1, Title: "one", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}); err != nil {
		t.Fatalf("c1: %v", err)
	}

	// alloc id .2 (counter committed at 2, separate round-trip), then the
	// insert FAILS: priority 99 violates CHECK(priority BETWEEN 0 AND 4).
	id2, _ := st.NextChildID(ctx, parent.ID)
	if err := st.CreateIssue(ctx, &beads.Issue{ID: id2, Title: "bad", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 99}); err == nil {
		t.Fatalf("expected insert to fail on priority 99")
	}

	// next child: counter already at 2 -> .3. GAP at .2 (the bug).
	id3, _ := st.NextChildID(ctx, parent.ID)
	if !strings.HasSuffix(id3, ".3") {
		t.Fatalf("expected old-path gap to land at .3, got %s", id3)
	}
	t.Logf("OLD PATH BUG: failed insert left the counter advanced; next id = %s (GAP at .2)", id3)
}

// TestCreateChild_AtomicOnFailure proves the fix: a child create that fails
// AFTER the counter is allocated must NOT leak the counter (no gap) and must
// NOT leave an orphan bead — the whole unit rolls back.
func TestCreateChild_AtomicOnFailure(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "atom")
	parent := &beads.Issue{Title: "Parent", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, parent); err != nil {
		t.Fatalf("parent: %v", err)
	}

	// 1) a normal child -> .1
	c1 := &beads.Issue{Title: "child one", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, parent.ID, c1, nil); err != nil {
		t.Fatalf("c1: %v", err)
	}
	if want := parent.ID + ".1"; c1.ID != want {
		t.Fatalf("c1 id = %s, want %s", c1.ID, want)
	}

	// 2) a FAILING child: priority 99 fails the insert AFTER NextChildIndex
	//    bumped the (in-tx) counter to 2.
	bad := &beads.Issue{Title: "bad child", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 99}
	if err := st.CreateChild(ctx, parent.ID, bad, nil); err == nil {
		t.Fatalf("expected CreateChild to fail on priority 99")
	} else {
		t.Logf("expected failure surfaced: %v", err)
	}

	// 3) no orphan: parent.2 must not exist.
	if got, _ := st.GetIssue(ctx, parent.ID+".2"); got != nil {
		t.Fatalf("orphan bead leaked at %s.2: %+v", parent.ID, got)
	}

	// 4) no gap: the next successful child must be .2, not .3.
	c2 := &beads.Issue{Title: "child two", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, parent.ID, c2, nil); err != nil {
		t.Fatalf("c2: %v", err)
	}
	if want := parent.ID + ".2"; c2.ID != want {
		t.Fatalf("GAP: c2 id = %s, want %s (counter leaked from the failed create)", c2.ID, want)
	}
	t.Logf("FIX OK: failed create rolled back; next child is %s (no gap, no orphan)", c2.ID)
}
