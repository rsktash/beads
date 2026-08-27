package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// TestReadyExcludesActiveQuestion verifies that a bead carrying an active
// question is absent from Store.Ready.
func TestReadyExcludesActiveQuestion(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()

	a := mkIssue(t, st, "A question-blocked", 1)
	b := mkIssue(t, st, "B ready", 1)

	// Attach active question to A.
	q := &beads.Statement{Kind: "question", Text: "open question on A", IssueID: &a.ID, Status: "active"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create question: %v", err)
	}

	ready, err := st.Ready(ctx)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	ids := map[string]bool{}
	for _, r := range ready {
		ids[r.ID] = true
	}
	if ids[a.ID] {
		t.Fatalf("active question should exclude %s from ready, got %+v", a.ID, ready)
	}
	if !ids[b.ID] {
		t.Fatalf("B should still be ready, got %+v", ready)
	}
}

// TestReadyReturnsAfterAnswered verifies that setting the question's status to
// answered returns bead to Ready.
func TestReadyReturnsAfterAnswered(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()

	a := mkIssue(t, st, "blocked then answered", 1)
	q := &beads.Statement{Kind: "question", Text: "q to answer", IssueID: &a.ID, Status: "active"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Ensure excluded first.
	ready, _ := st.Ready(ctx)
	for _, r := range ready {
		if r.ID == a.ID {
			t.Fatalf("should be excluded before answered")
		}
	}
	if err := st.UpdateStatementStatus(ctx, q.ID, "answered"); err != nil {
		t.Fatalf("update to answered: %v", err)
	}
	ready, _ = st.Ready(ctx)
	found := false
	for _, r := range ready {
		if r.ID == a.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("after answered, %s should be ready, got %+v", a.ID, ready)
	}
}

// TestReadyReturnsAfterRetracted verifies that setting status to retracted also returns.
func TestReadyReturnsAfterRetracted(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()

	a := mkIssue(t, st, "blocked then retracted", 1)
	q := &beads.Statement{Kind: "question", Text: "q to retract", IssueID: &a.ID, Status: "active"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.UpdateStatementStatus(ctx, q.ID, "retracted"); err != nil {
		t.Fatalf("update to retracted: %v", err)
	}
	ready, _ := st.Ready(ctx)
	found := false
	for _, r := range ready {
		if r.ID == a.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("after retracted, %s should be ready, got %+v", a.ID, ready)
	}
}

// TestReadyFindingsStayReady verifies bead with only findings stays ready.
func TestReadyFindingsStayReady(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	a := mkIssue(t, st, "finding bead", 1)
	f := &beads.Statement{Kind: "finding", Text: "a finding", IssueID: &a.ID, Status: "active"}
	if err := st.CreateStatement(ctx, f); err != nil {
		t.Fatalf("create finding: %v", err)
	}
	ready, _ := st.Ready(ctx)
	found := false
	for _, r := range ready {
		if r.ID == a.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("bead with only finding should stay ready")
	}
}

// TestReadyRulingsStayReady verifies bead with only rulings stays ready.
func TestReadyRulingsStayReady(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	a := mkIssue(t, st, "ruling bead", 1)
	f := &beads.Statement{Kind: "ruling", Text: "a ruling", IssueID: &a.ID, Status: "active"}
	if err := st.CreateStatement(ctx, f); err != nil {
		t.Fatalf("create ruling: %v", err)
	}
	ready, _ := st.Ready(ctx)
	found := false
	for _, r := range ready {
		if r.ID == a.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("bead with only ruling should stay ready")
	}
}

// TestReadyProjectScopedQuestionExcludesNothing verifies that a question with
// NULL issue_id does not exclude anything.
func TestReadyProjectScopedQuestionExcludesNothing(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	a := mkIssue(t, st, "alpha", 1)
	b := mkIssue(t, st, "beta", 1)
	// Capture baseline ready before project-scoped question.
	baseline, _ := st.Ready(ctx)
	// Project-scoped.
	q := &beads.Statement{Kind: "question", Text: "project question", Status: "active"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create project question: %v", err)
	}
	after, _ := st.Ready(ctx)
	if len(baseline) != len(after) {
		t.Fatalf("project-scoped question should not change ready count: before %d after %d", len(baseline), len(after))
	}
	// Both still present.
	ids := map[string]bool{}
	for _, r := range after {
		ids[r.ID] = true
	}
	if !ids[a.ID] || !ids[b.ID] {
		t.Fatalf("project-scoped question excluded something: got %+v", after)
	}
}

// TestReadyAncestorQuestionDoesNotExcludeChild verifies per-bead blocking only.
func TestReadyAncestorQuestionDoesNotExcludeChild(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	epic := &beads.Issue{Title: "epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, epic); err != nil {
		t.Fatalf("epic: %v", err)
	}
	child := &beads.Issue{Title: "child task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, child, nil); err != nil {
		t.Fatalf("child: %v", err)
	}
	// Question on ancestor epic.
	q := &beads.Statement{Kind: "question", Text: "question on epic", IssueID: &epic.ID, Status: "active"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create q: %v", err)
	}
	ready, _ := st.Ready(ctx)
	ids := map[string]bool{}
	for _, r := range ready {
		ids[r.ID] = true
	}
	if ids[epic.ID] {
		t.Fatalf("epic with active question should be excluded, but was ready")
	}
	if !ids[child.ID] {
		t.Fatalf("child should NOT be excluded by ancestor question; ready=%+v", ready)
	}
}

// TestReadyFiftyBeadsTenQuestions verifies exclusion on larger list and that
// only one additional query is used (implementation does single DISTINCT query,
// not per-bead). The count assertion proves correctness; the one-query property
// is verified by code review: activeQuestionIssueIDs does exactly one query.
func TestReadyFiftyBeadsTenQuestions(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	var all []*beads.Issue
	for i := 0; i < 50; i++ {
		iss := mkIssue(t, st, fmt.Sprintf("issue-%02d", i), 1)
		all = append(all, iss)
	}
	// Attach active question to first 10.
	for i := 0; i < 10; i++ {
		q := &beads.Statement{Kind: "question", Text: fmt.Sprintf("q %d", i), IssueID: &all[i].ID, Status: "active"}
		if err := st.CreateStatement(ctx, q); err != nil {
			t.Fatalf("create q %d: %v", i, err)
		}
	}
	ready, err := st.Ready(ctx)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(ready) != 40 {
		t.Fatalf("expected 40 ready (50-10 blocked), got %d", len(ready))
	}
	blockedSet := map[string]bool{}
	for i := 0; i < 10; i++ {
		blockedSet[all[i].ID] = true
	}
	for _, r := range ready {
		if blockedSet[r.ID] {
			t.Fatalf("blocked bead %s should not be ready", r.ID)
		}
	}
	// Ensure the other 40 are present.
	if len(ready) != 40 {
		t.Fatalf("wrong count")
	}
	present := map[string]bool{}
	for _, r := range ready {
		present[r.ID] = true
	}
	for i := 10; i < 50; i++ {
		if !present[all[i].ID] {
			t.Fatalf("expected issue %s to be ready", all[i].ID)
		}
	}
}

// TestReadyExcludesOnlyActiveStatus ensures non-active statuses do not block.
func TestReadyExcludesOnlyActiveStatus(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	a := mkIssue(t, st, "candidate status", 1)
	q := &beads.Statement{Kind: "question", Text: "candidate", IssueID: &a.ID, Status: "candidate"}
	if err := st.CreateStatement(ctx, q); err != nil {
		t.Fatalf("create: %v", err)
	}
	ready, _ := st.Ready(ctx)
	found := false
	for _, r := range ready {
		if r.ID == a.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("candidate question should not block")
	}
	// superseded should also not block
	if err := st.UpdateStatementStatus(ctx, q.ID, "superseded"); err != nil {
		t.Fatalf("update superseded: %v", err)
	}
	ready, _ = st.Ready(ctx)
	found = false
	for _, r := range ready {
		if r.ID == a.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("superseded question should not block")
	}
}

// TestReadyWithParentAndLimit verifies exclusion composes with parent scoping
// and limit capping when applied after Ready (as cmd/bd/ready.go does).
func TestReadyWithParentAndLimit(t *testing.T) {
	st := newSqliteStore(t)
	ctx := context.Background()
	epic := &beads.Issue{Title: "epic parent", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, epic); err != nil {
		t.Fatalf("epic: %v", err)
	}
	// 5 children under epic
	var children []*beads.Issue
	for i := 0; i < 5; i++ {
		ch := &beads.Issue{Title: fmt.Sprintf("child %d", i), Type: beads.TypeTask, Status: beads.StatusOpen, Priority: i}
		if err := st.CreateChild(ctx, epic.ID, ch, nil); err != nil {
			t.Fatalf("child %d: %v", i, err)
		}
		children = append(children, ch)
	}
	// Block 2 of them with active questions.
	for i := 0; i < 2; i++ {
		q := &beads.Statement{Kind: "question", Text: fmt.Sprintf("block %d", i), IssueID: &children[i].ID, Status: "active"}
		if err := st.CreateStatement(ctx, q); err != nil {
			t.Fatalf("q: %v", err)
		}
	}
	// Store.Ready should exclude those 2.
	ready, _ := st.Ready(ctx)
	readyIDs := map[string]bool{}
	for _, r := range ready {
		readyIDs[r.ID] = true
	}
	if readyIDs[children[0].ID] || readyIDs[children[1].ID] {
		t.Fatalf("blocked children should not be ready")
	}
	if !readyIDs[children[2].ID] || !readyIDs[children[3].ID] || !readyIDs[children[4].ID] {
		t.Fatalf("unblocked children should be ready")
	}

	// Simulate --parent <epic> scoping (as ready.go does WalkChildren intersect).
	descendants, err := st.Descendants(ctx, epic.ID, true, 8)
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	inTree := map[string]bool{}
	for _, d := range descendants {
		inTree[d.Issue.ID] = true
	}
	var parentScoped []beads.Issue
	for _, r := range ready {
		if inTree[r.ID] {
			parentScoped = append(parentScoped, r)
		}
	}
	if len(parentScoped) != 3 {
		t.Fatalf("parent scoped expected 3 (5-2 blocked), got %d: %+v", len(parentScoped), parentScoped)
	}
	// Limit caps after exclusion+parent scoping.
	limit := 2
	if len(parentScoped) > limit {
		parentScoped = parentScoped[:limit]
	}
	if len(parentScoped) != 2 {
		t.Fatalf("limit 2 expected 2, got %d", len(parentScoped))
	}
}

// Ensure imported store is used.
var _ = store.ErrNotFound
