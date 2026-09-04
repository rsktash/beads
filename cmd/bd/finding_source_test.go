package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
)

// fileSourcedFinding drives `bd finding add` with --source through the real
// command and returns the printed statement id.
func fileSourcedFinding(t *testing.T, issueID, sourceID, text string) string {
	t.Helper()
	out, _, err := runFindingCmd(t, []string{"add", issueID, text, "--source", sourceID})
	if err != nil {
		t.Fatalf("finding add --source %s: %v (out %q)", sourceID, err, out)
	}
	return strings.TrimSpace(out)
}

func TestFindingSource_RoundTrips(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "carries a source")
	source := mkQFIssue(t, st, "the source bead")
	_ = st.Close()

	fID := fileSourcedFinding(t, issue.ID, source.ID, "hit while working the source")
	got := getStatementQF(t, dsn, fID)
	if got.SourceIssueID == nil || *got.SourceIssueID != source.ID {
		t.Fatalf("SourceIssueID should round-trip %s, got %v", source.ID, got.SourceIssueID)
	}
}

func TestFindingSource_RefusesUnknownId(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "unknown source")
	_ = st.Close()
	before := countStatementsQF(t, dsn)

	_, _, err := runFindingCmd(t, []string{"add", issue.ID, "text", "--source", "ghost-9999"})
	if err == nil || !strings.Contains(err.Error(), "no such issue") || !strings.Contains(err.Error(), "ghost-9999") {
		t.Fatalf("unknown --source must be refused naming the id, got %v", err)
	}
	if after := countStatementsQF(t, dsn); after != before {
		t.Fatalf("refusal must write nothing, before %d after %d", before, after)
	}
}

func TestFindingSource_RefusesSelf(t *testing.T) {
	dsn, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "self source")
	_ = st.Close()
	before := countStatementsQF(t, dsn)

	_, _, err := runFindingCmd(t, []string{"add", issue.ID, "text", "--source", issue.ID})
	if err == nil || !strings.Contains(err.Error(), "--source names the bead the finding is already on") {
		t.Fatalf("self --source must be refused with the contract wording, got %v", err)
	}
	if after := countStatementsQF(t, dsn); after != before {
		t.Fatalf("refusal must write nothing, before %d after %d", before, after)
	}
}

func TestFindingSource_RendersFromSegment(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "from renders")
	source := mkQFIssue(t, st, "named source")

	out, _, err := runFindingCmd(t, []string{"add", issue.ID, "headline here", "--source", source.ID, "--evidence", "src/x.go:12"})
	if err != nil {
		t.Fatalf("finding add: %v", err)
	}
	_ = strings.TrimSpace(out)

	shown := renderContractOutput(t, st, issue.ID, showOpts{})
	mustContain(t, shown, "from: "+source.ID)
	mustContain(t, shown, "from: "+source.ID+"  evidence: src/x.go:12")
}

func TestFindingSource_OmittedWhenNull(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "no source")

	if _, _, err := runFindingCmd(t, []string{"add", issue.ID, "plain finding"}); err != nil {
		t.Fatalf("finding add: %v", err)
	}
	shown := renderContractOutput(t, st, issue.ID, showOpts{})
	mustContain(t, shown, "\nFINDINGS\n")
	if strings.Contains(shown, "from:") {
		t.Fatalf("from: must appear nowhere when source_issue_id is NULL, got:\n%s", shown)
	}
}

func TestFindingSource_BackSectionOnSourceBead(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "where it was filed")
	source := mkQFIssue(t, st, "where it came from")
	fileSourcedFinding(t, issue.ID, source.ID, "the headline")

	shown := renderContractOutput(t, st, source.ID, showOpts{})
	mustContain(t, shown, "FINDINGS FILED FROM HERE")
	mustContain(t, shown, "["+issue.ID+"]")
	mustContain(t, shown, "the headline")
}

func TestFindingSource_BackSectionAbsentWhenNone(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	issue := mkQFIssue(t, st, "not a source")
	if _, _, err := runFindingCmd(t, []string{"add", issue.ID, "own finding"}); err != nil {
		t.Fatalf("finding add: %v", err)
	}

	shown := renderContractOutput(t, st, issue.ID, showOpts{})
	mustContain(t, shown, "\nFINDINGS\n")
	if strings.Contains(shown, "FINDINGS FILED FROM HERE") {
		t.Fatalf("back section must render only when a finding names this bead as source, got:\n%s", shown)
	}
}

func TestFindingSource_DoesNotInherit(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	parent := mkQFIssue(t, st, "the epic")
	child := mkQFIssue(t, st, "the task")
	if err := st.AddDependency(context.Background(), beads.Dependency{IssueID: child.ID, DependsOnID: parent.ID, Type: beads.DepParentChild}); err != nil {
		t.Fatalf("add parent-child dep: %v", err)
	}
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "parent ruling", IssueID: strPtr2(parent.ID), FiledBy: "owner:tester"})
	fileSourcedFinding(t, child.ID, parent.ID, "found while executing the child")

	shown := renderContractOutput(t, st, parent.ID, showOpts{})
	mustContain(t, shown, "FINDINGS FILED FROM HERE")
	mustContain(t, shown, "["+child.ID+"]")
	if strings.Contains(shown, "\nFINDINGS\n") {
		t.Fatalf("the child's sourced finding must not reach the parent's FINDINGS block, got:\n%s", shown)
	}
}

func TestFindingSource_SectionOrder(t *testing.T) {
	_, st := newTempQFStore(t, "bd")
	source := mkQFIssue(t, st, "has both blocks")
	other := mkQFIssue(t, st, "files from it")
	if _, _, err := runFindingCmd(t, []string{"add", source.ID, "own finding"}); err != nil {
		t.Fatalf("finding add: %v", err)
	}
	fileSourcedFinding(t, other.ID, source.ID, "filed from elsewhere")

	shown := renderContractOutput(t, st, source.ID, showOpts{})
	idxFindings := indexOrFail(t, shown, "\nFINDINGS\n")
	idxBack := indexOrFail(t, shown, "\nFINDINGS FILED FROM HERE\n")
	idxBase := indexOrFail(t, shown, "BASE TEXT")
	if !(idxFindings < idxBack && idxBack < idxBase) {
		t.Fatalf("section order wrong: FINDINGS %d, FINDINGS FILED FROM HERE %d, BASE TEXT %d\n%s", idxFindings, idxBack, idxBase, shown)
	}
}
