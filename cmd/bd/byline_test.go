package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
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
// untouched through `bd ruling add --verbatim` and GetStatement.
func TestVerbatim_RoundTrips(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "verbatim roundtrip", "")
	st.Close()

	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runRulingAdd(t, []string{"add", issue.ID, "ruling text", "--verbatim", "go with A"})
	if err != nil {
		t.Fatalf("ruling add: %v", err)
	}
	rID := strings.TrimSpace(out)

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	got, err := st2.GetStatement(ctx, rID)
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
	dsn, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "verbatim absent from headline", "")
	st.Close()

	t.Setenv("BD_ACTOR", "coordinator")
	if _, _, err := runRulingAdd(t, []string{"add", issue.ID, "headline text", "--verbatim", "the secret verbatim sentence"}); err != nil {
		t.Fatalf("ruling add: %v", err)
	}

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	out := renderContractOutput(t, st2, issue.ID, showOpts{})
	mustNotContain(t, out, "the secret verbatim sentence")
}

// TestVerbatim_ShownOnExpand checks the same verbatim sentence appears once
// the ruling's id is passed to --expand.
func TestVerbatim_ShownOnExpand(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "verbatim shown on expand", "")
	st.Close()

	t.Setenv("BD_ACTOR", "coordinator")
	out, _, err := runRulingAdd(t, []string{"add", issue.ID, "headline text", "--verbatim", "the expand verbatim sentence"})
	if err != nil {
		t.Fatalf("ruling add: %v", err)
	}
	rID := strings.TrimSpace(out)

	ctx := context.Background()
	st2, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st2.Close()
	rendered := renderContractOutput(t, st2, issue.ID, showOpts{expand: map[string]bool{rID: true}})
	mustContain(t, rendered, "the expand verbatim sentence")
}
