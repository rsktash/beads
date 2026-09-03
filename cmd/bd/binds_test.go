package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// TestBinds_AttachesToSecondBead asserts `bd ruling add <issue> "<text>"
// --binds <other>` files the ruling on <issue> but renders it, with its
// origin bracket, on <other>'s contract too.
func TestBinds_AttachesToSecondBead(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	filedOn := mkIssueForRuling(t, st, "filed on")
	other := mkIssueForRuling(t, st, "bound target")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runRulingAdd(t, []string{"add", filedOn.ID, "bound ruling text", "--binds", other.ID})
	if err != nil {
		t.Fatalf("binds add: %v", err)
	}
	rID := strings.TrimSpace(out)

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	got, err := st2.GetStatement(ctx, rID)
	if err != nil {
		t.Fatalf("get ruling: %v", err)
	}
	if got.IssueID == nil || *got.IssueID != filedOn.ID {
		t.Fatalf("ruling should still be filed on %s, got %v", filedOn.ID, got.IssueID)
	}
	if got.BindsID == nil || *got.BindsID != other.ID {
		t.Fatalf("ruling should carry binds_id %s, got %v", other.ID, got.BindsID)
	}

	out2 := renderContractOutput(t, st2, other.ID, showOpts{})
	if !strings.Contains(out2, "bound ruling text") {
		t.Fatalf("bound bead's contract should render the ruling, got:\n%s", out2)
	}
	if !strings.Contains(out2, "["+filedOn.ID+"]") {
		t.Fatalf("bound bead's contract should carry the origin bracket [%s], got:\n%s", filedOn.ID, out2)
	}
}

// TestBinds_RefusesProjectScoped asserts --binds on a project-scoped ruling
// (single-arg `bd ruling add`) is refused rather than silently ignored.
func TestBinds_RefusesProjectScoped(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	other := mkIssueForRuling(t, st, "would-be target")
	_ = st.Close()
	before := countStatements(t, dsn)
	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingAdd(t, []string{"add", "project-wide text", "--binds", other.ID})
	if err == nil {
		t.Fatalf("--binds on a project-scoped ruling should be refused")
	}
	if !strings.Contains(err.Error(), "--binds is redundant on a project-scoped ruling") {
		t.Fatalf("error should name the redundant-binds refusal, got %v", err)
	}
	after := countStatements(t, dsn)
	if after != before {
		t.Fatalf("refused write should leave zero new rows, before %d after %d", before, after)
	}
}

// TestBinds_RefusesSelfReference asserts --binds naming the ruling's own
// issue id is refused.
func TestBinds_RefusesSelfReference(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "self bind")
	_ = st.Close()
	before := countStatements(t, dsn)
	t.Setenv("BD_ACTOR", "coordinator")
	_, _, err := runRulingAdd(t, []string{"add", issue.ID, "text", "--binds", issue.ID})
	if err == nil {
		t.Fatalf("--binds naming the ruling's own bead should be refused")
	}
	if !strings.Contains(err.Error(), "--binds names the bead the ruling is already on") {
		t.Fatalf("error should name the self-reference refusal, got %v", err)
	}
	after := countStatements(t, dsn)
	if after != before {
		t.Fatalf("refused write should leave zero new rows, before %d after %d", before, after)
	}
}

// TestBinds_DedupesWithEpicArm asserts a ruling that reaches a bead by two
// arms — bound explicitly and inherited through the epic chain — renders
// exactly once.
func TestBinds_DedupesWithEpicArm(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	ctx := context.Background()
	epic := &beads.Issue{Title: "epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, epic); err != nil {
		t.Fatalf("epic: %v", err)
	}
	task := &beads.Issue{Title: "task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("task: %v", err)
	}
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	// Filed on the epic (inherited by task via the epic arm) and bound to
	// task explicitly in the same statement.
	_, _, err := runRulingAdd(t, []string{"add", epic.ID, "epic ruling also bound", "--binds", task.ID})
	if err != nil {
		t.Fatalf("binds add: %v", err)
	}

	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	cv, err := st2.ContractStatements(ctx, task.ID)
	if err != nil {
		t.Fatalf("contract task: %v", err)
	}
	count := 0
	for _, r := range cv.Rulings {
		if strings.Contains(r.Text, "epic ruling also bound") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one rendered ruling reaching task by both arms, got %d: %+v", count, cv.Rulings)
	}
}

// TestBinds_PrecedesEpicChain asserts a bound ruling renders before an
// inherited epic-chain ruling on the same bead.
func TestBinds_PrecedesEpicChain(t *testing.T) {
	dsn, st := newTempRulingStore(t, "bd")
	ctx := context.Background()
	epic := &beads.Issue{Title: "epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, epic); err != nil {
		t.Fatalf("epic: %v", err)
	}
	task := &beads.Issue{Title: "task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("task: %v", err)
	}
	elsewhere := mkIssueForRuling(t, st, "elsewhere")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")

	// Inherited ruling: filed on the epic, reaches task through the epic arm.
	if _, _, err := runRulingAdd(t, []string{"add", epic.ID, "inherited epic ruling"}); err != nil {
		t.Fatalf("epic ruling add: %v", err)
	}
	// Bound ruling: filed elsewhere, attached to task explicitly.
	if _, _, err := runRulingAdd(t, []string{"add", elsewhere.ID, "bound ruling", "--binds", task.ID}); err != nil {
		t.Fatalf("bound ruling add: %v", err)
	}

	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	cv, err := st2.ContractStatements(ctx, task.ID)
	if err != nil {
		t.Fatalf("contract task: %v", err)
	}
	if len(cv.Rulings) < 2 {
		t.Fatalf("expected at least 2 rulings on task's contract, got %d: %+v", len(cv.Rulings), cv.Rulings)
	}
	if !strings.Contains(cv.Rulings[0].Text, "bound ruling") {
		t.Fatalf("bound ruling should render first, got order %+v", cv.Rulings)
	}
}
