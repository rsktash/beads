package main

import (
	"context"
	"testing"

	"github.com/rsktash/beads"
)

// TestByline_QuestionCarriesAuthor files a question with BD_ACTOR unset (the
// owner) through the real CLI path and checks the rendered line carries the
// author between the date and the text.
func TestByline_QuestionCarriesAuthor(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "byline question", "")

	asOwner(t)
	if _, _, err := runQuestionCmd(t, []string{"add", issue.ID, "a question needing an author"}); err != nil {
		t.Fatalf("question add: %v", err)
	}

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	mustContain(t, out, "  owner  ")
}

// TestByline_FindingCarriesAuthor files a finding as BD_ACTOR=executor and
// checks the bare actor word renders with no surrounding bracket.
func TestByline_FindingCarriesAuthor(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "byline finding", "")

	t.Setenv("BD_ACTOR", "executor")
	if _, _, err := runFindingCmd(t, []string{"add", issue.ID, "a finding needing an author"}); err != nil {
		t.Fatalf("finding add: %v", err)
	}

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	mustContain(t, out, "  executor  ")
	mustNotContain(t, out, "[executor:")
}

// TestByline_ClosedQuestionCarriesAuthor closes a question and checks the
// author renders between the date and the status.
func TestByline_ClosedQuestionCarriesAuthor(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "byline closed question", "")
	qID := mkQuestion(t, st, issue.ID, "a question about to close")

	if err := st.CloseQuestion(context.Background(), qID, "answered", "", ""); err != nil {
		t.Fatalf("close question: %v", err)
	}

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	mustContain(t, out, "  owner  answered")
}

// TestByline_EmptyFiledByRendersUnknown writes a statement directly with an
// empty FiledBy and checks the byline falls back to "unknown".
func TestByline_EmptyFiledByRendersUnknown(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "byline unknown", "")
	mustCreateStatementDirect(t, st, &beads.Statement{
		Kind:    "finding",
		Text:    "an orphaned finding",
		IssueID: strPtr2(issue.ID),
		FiledBy: "",
	})

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	mustContain(t, out, "  unknown  ")
}

// TestVerbatim_RoundTrips checks the owner's verbatim sentence round-trips
// untouched through the store and GetStatement.
func TestVerbatim_RoundTrips(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "verbatim roundtrip", "")
	r := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", IssueID: strPtr2(issue.ID), Text: "ruling text", Verbatim: "go with A", FiledBy: "owner:tester"})

	got, err := st.GetStatement(context.Background(), r.ID)
	if err != nil {
		t.Fatalf("get statement: %v", err)
	}
	if got.Verbatim != "go with A" {
		t.Fatalf("verbatim should round-trip untouched, got %q", got.Verbatim)
	}
}

// TestVerbatim_AbsentFromHeadline checks the verbatim sentence never appears
// in the default (unexpanded) rendering.
func TestVerbatim_AbsentFromHeadline(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "verbatim absent from headline", "")
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", IssueID: strPtr2(issue.ID), Text: "headline text", Verbatim: "the secret verbatim sentence", FiledBy: "owner:tester"})

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	mustNotContain(t, out, "the secret verbatim sentence")
}

// TestVerbatim_ShownOnExpand checks the same verbatim sentence appears once
// the ruling's id is passed to --expand.
func TestVerbatim_ShownOnExpand(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "verbatim shown on expand", "")
	r := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", IssueID: strPtr2(issue.ID), Text: "headline text", Verbatim: "the expand verbatim sentence", FiledBy: "owner:tester"})

	rendered := renderContractOutput(t, st, issue.ID, showOpts{expand: map[string]bool{r.ID: true}})
	mustContain(t, rendered, "the expand verbatim sentence")
}
