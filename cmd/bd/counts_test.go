package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
)

// Feature 2: the CONTRACT header line carries the record counts for the
// bead — rulings, open and answered questions, findings, comments.

func TestCounts_AllZeroesRender(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "bare bead", "body")
	st.Close()

	out, _, err := runShowWithArgs(t, dsn, []string{issue.ID})
	if err != nil {
		t.Fatalf("show: %v, out %q", err, out)
	}
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.HasSuffix(first, "| rulings 0  questions 0 open / 0 answered  findings 0  comments 0") {
		t.Fatalf("unexpected CONTRACT line: %q", first)
	}
}

func TestCounts_MixedBead(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()

	issue := mkContractIssue(t, st, "mixed bead", "body")

	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "ruling one", IssueID: strPtr2(issue.ID)})
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "ruling two", IssueID: strPtr2(issue.ID)})
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "question", Text: "open question", IssueID: strPtr2(issue.ID)})
	answered := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "question", Text: "answered question", IssueID: strPtr2(issue.ID)})
	if err := st.CloseQuestion(ctx, answered.ID, "answered", "", ""); err != nil {
		t.Fatalf("close question: %v", err)
	}
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "finding", Text: "found it", IssueID: strPtr2(issue.ID), FiledBy: "owner:tester", Evidence: "src/foo.go:1"})
	for i := 0; i < 3; i++ {
		if err := st.AddComment(ctx, &beads.Comment{IssueID: issue.ID, Author: "alice", Text: "note", CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("add comment: %v", err)
		}
	}

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.HasSuffix(first, "| rulings 2  questions 1 open / 1 answered  findings 1  comments 3") {
		t.Fatalf("unexpected CONTRACT line: %q", first)
	}
}

func TestCounts_InheritedRulingsCounted(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()

	epic := mkContractEpic(t, st, "parent epic")
	child := &beads.Issue{Title: "child task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, child, nil); err != nil {
		t.Fatalf("create child: %v", err)
	}
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "epic ruling", IssueID: strPtr2(epic.ID)})

	out := renderContractOutput(t, st, child.ID, showOpts{})
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(first, "rulings 1") {
		t.Fatalf("expected inherited ruling counted, got CONTRACT line: %q", first)
	}
}

func TestCounts_PrefixUnchanged(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()

	issue := mkContractIssue(t, st, "prefix check", "body")

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	mustContain(t, out, "CONTRACT "+issue.ID+"  [")
}
