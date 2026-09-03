package store_test

// Feature 9: filing or changing a ruling, question, finding or comment bumps
// the bound bead's updated_at and stamps the record's changed_at. Every test
// here reads GetIssue before and after the write and asserts the timestamp
// moved (or, for the negative cases, that it did not).

import (
	"context"
	"testing"
	"time"

	"github.com/rsktash/beads"
)

func TestUpdated_RulingBumpsIssue(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "upd1")
	issue := mkIssue(t, st, "ruling bump target", 1)
	before, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r", IssueID: strPtr(issue.ID)})

	after, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("expected updated_at to advance, before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestUpdated_QuestionBumpsIssue(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "upd2")
	issue := mkIssue(t, st, "question bump target", 1)
	before, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	mustCreateStatement(t, st, &beads.Statement{Kind: "question", Text: "q", IssueID: strPtr(issue.ID)})

	after, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("expected updated_at to advance, before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestUpdated_FindingBumpsIssue(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "upd3")
	issue := mkIssue(t, st, "finding bump target", 1)
	before, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	mustCreateStatement(t, st, &beads.Statement{Kind: "finding", Text: "f", IssueID: strPtr(issue.ID)})

	after, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("expected updated_at to advance, before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestUpdated_CommentBumpsIssue(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "upd4")
	issue := mkIssue(t, st, "comment bump target", 1)
	before, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	if err := st.AddComment(ctx, &beads.Comment{IssueID: issue.ID, Author: "alice", Text: "note", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("add comment: %v", err)
	}

	after, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("expected updated_at to advance, before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestUpdated_SupersedeBumpsBothBeads(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "upd5")
	issueA := mkIssue(t, st, "bead A", 1)
	issueB := mkIssue(t, st, "bead B", 1)
	original := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "original", IssueID: strPtr(issueA.ID)})

	beforeA, err := st.GetIssue(ctx, issueA.ID)
	if err != nil {
		t.Fatalf("get beforeA: %v", err)
	}
	beforeB, err := st.GetIssue(ctx, issueB.ID)
	if err != nil {
		t.Fatalf("get beforeB: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	newRuling := &beads.Statement{Kind: "ruling", Text: "superseding", IssueID: strPtr(issueB.ID), SupersedesID: strPtr(original.ID)}
	if err := st.CreateStatementWithIssueUpdate(ctx, newRuling, nil); err != nil {
		t.Fatalf("create superseding ruling: %v", err)
	}

	afterA, err := st.GetIssue(ctx, issueA.ID)
	if err != nil {
		t.Fatalf("get afterA: %v", err)
	}
	afterB, err := st.GetIssue(ctx, issueB.ID)
	if err != nil {
		t.Fatalf("get afterB: %v", err)
	}
	if !afterA.UpdatedAt.After(beforeA.UpdatedAt) {
		t.Fatalf("expected the superseded ruling's bead A to bump, before=%v after=%v", beforeA.UpdatedAt, afterA.UpdatedAt)
	}
	if !afterB.UpdatedAt.After(beforeB.UpdatedAt) {
		t.Fatalf("expected the new ruling's bead B to bump, before=%v after=%v", beforeB.UpdatedAt, afterB.UpdatedAt)
	}
}

func TestUpdated_AnswerBumpsBoth(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "upd6")
	issueQ := mkIssue(t, st, "question bead", 1)
	issueR := mkIssue(t, st, "ruling bead", 1)
	q := mustCreateStatement(t, st, &beads.Statement{Kind: "question", Text: "open q", IssueID: strPtr(issueQ.ID)})
	r := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "answer", IssueID: strPtr(issueR.ID)})

	beforeQ, err := st.GetIssue(ctx, issueQ.ID)
	if err != nil {
		t.Fatalf("get beforeQ: %v", err)
	}
	beforeR, err := st.GetIssue(ctx, issueR.ID)
	if err != nil {
		t.Fatalf("get beforeR: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	if err := st.SetAnsweredBy(ctx, q.ID, r.ID); err != nil {
		t.Fatalf("set answered by: %v", err)
	}

	afterQ, err := st.GetIssue(ctx, issueQ.ID)
	if err != nil {
		t.Fatalf("get afterQ: %v", err)
	}
	afterR, err := st.GetIssue(ctx, issueR.ID)
	if err != nil {
		t.Fatalf("get afterR: %v", err)
	}
	if !afterQ.UpdatedAt.After(beforeQ.UpdatedAt) {
		t.Fatalf("expected the question's bead to bump, before=%v after=%v", beforeQ.UpdatedAt, afterQ.UpdatedAt)
	}
	if !afterR.UpdatedAt.After(beforeR.UpdatedAt) {
		t.Fatalf("expected the ruling's bead to bump, before=%v after=%v", beforeR.UpdatedAt, afterR.UpdatedAt)
	}
}

func TestUpdated_ProjectScopedBumpsNothing(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "upd7")
	issue := mkIssue(t, st, "unrelated bead", 1)
	before, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "project-wide"})

	after, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("a project-scoped statement should bump no issue, before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestUpdated_ChangedAtMatchesIssue(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "upd8")
	issue := mkIssue(t, st, "changed at target", 1)
	r := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r", IssueID: strPtr(issue.ID)})

	got, err := st.GetStatement(ctx, r.ID)
	if err != nil {
		t.Fatalf("get statement: %v", err)
	}
	if got.ChangedAt == nil {
		t.Fatalf("expected changed_at to be set")
	}
	updatedIssue, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if !got.ChangedAt.Equal(updatedIssue.UpdatedAt) {
		t.Fatalf("expected changed_at to match the bead's updated_at, changed_at=%v updated_at=%v", got.ChangedAt, updatedIssue.UpdatedAt)
	}
}

func TestUpdated_AnnotateDoesNotBump(t *testing.T) {
	ctx := context.Background()
	st := newSqliteStoreWithPrefix(t, "upd9")
	issue := mkIssue(t, st, "annotate target", 1)
	r := mustCreateStatement(t, st, &beads.Statement{Kind: "ruling", Text: "r", IssueID: strPtr(issue.ID)})

	before, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	beforeStatement, err := st.GetStatement(ctx, r.ID)
	if err != nil {
		t.Fatalf("get statement before: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	if err := st.AnnotateStatement(ctx, r.ID, "sess-1", "msg-1", "tool-1"); err != nil {
		t.Fatalf("annotate: %v", err)
	}

	after, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	afterStatement, err := st.GetStatement(ctx, r.ID)
	if err != nil {
		t.Fatalf("get statement after: %v", err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("bd annotate should bump nothing, before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
	if beforeStatement.ChangedAt == nil || afterStatement.ChangedAt == nil {
		t.Fatalf("expected changed_at to already be set on the ruling")
	}
	if !beforeStatement.ChangedAt.Equal(*afterStatement.ChangedAt) {
		t.Fatalf("bd annotate should not touch changed_at, before=%v after=%v", beforeStatement.ChangedAt, afterStatement.ChangedAt)
	}
}
