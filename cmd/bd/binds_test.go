package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// TestBinds_AttachesToSecondBead asserts a ruling filed on one bead with
// binds_id naming another renders, with its origin bracket, on the other
// bead's contract too.
func TestBinds_AttachesToSecondBead(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	filedOn := mkIssueForRuling(t, st, "filed on")
	other := mkIssueForRuling(t, st, "bound target")
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", IssueID: strPtr2(filedOn.ID), BindsID: strPtr2(other.ID), Text: "bound ruling text", FiledBy: "owner:tester"})

	out := renderContractOutput(t, st, other.ID, showOpts{})
	if !strings.Contains(out, "bound ruling text") {
		t.Fatalf("bound bead's contract should render the ruling, got:\n%s", out)
	}
	if !strings.Contains(out, "["+filedOn.ID+"]") {
		t.Fatalf("bound bead's contract should carry the origin bracket [%s], got:\n%s", filedOn.ID, out)
	}
}

func mkBindsEpicTask(t *testing.T, st *store.Store) (epic, task *beads.Issue) {
	t.Helper()
	ctx := context.Background()
	epic = &beads.Issue{Title: "epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, epic); err != nil {
		t.Fatalf("epic: %v", err)
	}
	task = &beads.Issue{Title: "task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("task: %v", err)
	}
	return epic, task
}

// TestBinds_DedupesWithEpicArm asserts a ruling that reaches a bead by two
// arms — bound explicitly and inherited through the epic chain — renders
// exactly once.
func TestBinds_DedupesWithEpicArm(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	epic, task := mkBindsEpicTask(t, st)
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", IssueID: strPtr2(epic.ID), BindsID: strPtr2(task.ID), Text: "epic ruling also bound", FiledBy: "owner:tester"})

	cv, err := st.ContractStatements(context.Background(), task.ID)
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
	_, st := newTempRulingStore(t, "bd")
	epic, task := mkBindsEpicTask(t, st)
	elsewhere := mkIssueForRuling(t, st, "elsewhere")
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", IssueID: strPtr2(epic.ID), Text: "inherited epic ruling", FiledBy: "owner:tester"})
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", IssueID: strPtr2(elsewhere.ID), BindsID: strPtr2(task.ID), Text: "bound ruling", FiledBy: "owner:tester"})

	cv, err := st.ContractStatements(context.Background(), task.ID)
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
