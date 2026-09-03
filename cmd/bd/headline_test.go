package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rsktash/beads"
)

func TestHeadline_TruncatesAt120Runes(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "trunc fixture", "")
	longText := strings.Repeat("a", 300)
	r := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: longText, IssueID: strPtr2(issue.ID), FiledBy: "owner:tester"})

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	want := "  " + rulingHeadline(*r, issue.ID)
	if !strings.Contains(out, want) {
		t.Fatalf("expected headline line %q in:\n%s", want, out)
	}
	if !strings.HasSuffix(want, "…") {
		t.Fatalf("expected the headline to end in an ellipsis, got %q", want)
	}
	truncated := headlineText(longText)
	if n := len([]rune(truncated)); n != 121 {
		t.Fatalf("expected a truncated text field of 121 runes, got %d (%q)", n, truncated)
	}
}

func TestHeadline_CollapsesNewlines(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "collapse fixture", "")
	raw := "first\nsecond\tthird"
	r := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: raw, IssueID: strPtr2(issue.ID), FiledBy: "owner:tester"})

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	want := "  " + rulingHeadline(*r, issue.ID)
	if !strings.Contains(out, want) {
		t.Fatalf("expected collapsed headline %q in:\n%s", want, out)
	}
	// The raw text's embedded newline and tab must not have produced a
	// second line for this ruling.
	count := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, r.ID) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one line for %s, got %d in:\n%s", r.ID, count, out)
	}
}

func TestHeadline_ShowFieldOrder(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "field order fixture", "")
	r := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "short ruling text", IssueID: strPtr2(issue.ID), FiledBy: "owner:tester"})

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	want := "  " + r.ID + "  " + r.CreatedAt.Format("2006-01-02") + "  owner  " + r.Text
	if !strings.Contains(out, want) {
		t.Fatalf("expected line %q in:\n%s", want, out)
	}
}

func TestHeadline_ProjectMarkerAfterAuthor(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "project marker fixture", "")
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "project-wide ruling", FiledBy: "owner:tester"})

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	if !strings.Contains(out, "  owner  [project]  ") {
		t.Fatalf("expected the marker right after the author field in:\n%s", out)
	}
}

func TestHeadline_RulingsFullPrintsWholeText(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "full fixture", "")
	longText := strings.Repeat("b", 300)
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: longText, IssueID: strPtr2(issue.ID), FiledBy: "owner:tester"})

	out := renderContractOutput(t, st, issue.ID, showOpts{rulingsMode: rulingsModeFull})
	if !strings.Contains(out, longText) {
		t.Fatalf("expected the full untruncated text in:\n%s", out)
	}
	if strings.Contains(out, "…") {
		t.Fatalf("--rulings full must not truncate, got an ellipsis in:\n%s", out)
	}
}

func TestHeadline_ExpandOpensNamedRuling(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "expand fixture", "")
	text1 := strings.Repeat("c", 300)
	text2 := strings.Repeat("d", 300)
	r1 := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: text1, IssueID: strPtr2(issue.ID), FiledBy: "owner:tester"})
	r2 := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: text2, IssueID: strPtr2(issue.ID), FiledBy: "owner:tester"})

	out := renderContractOutput(t, st, issue.ID, showOpts{expand: map[string]bool{r1.ID: true}})
	if !strings.Contains(out, "      "+text1) {
		t.Fatalf("expected %s's full text on a continuation line in:\n%s", r1.ID, out)
	}
	if strings.Contains(out, "      "+text2) {
		t.Fatalf("%s was not named by --expand, but its continuation line appeared in:\n%s", r2.ID, out)
	}
}

func TestHeadline_WorkfileHeaderMatchesShow(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "workfile match fixture", "")
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "workfile ruling", IssueID: strPtr2(issue.ID), FiledBy: "owner:tester"})

	cc := &cmdCtx{ctx: context.Background(), store: st, json: false}
	var bufOutline, bufDefault bytes.Buffer
	if err := printShowHuman(&bufOutline, cc, issue.ID, showOpts{outline: true}); err != nil {
		t.Fatalf("printShowHuman outline: %v", err)
	}
	if err := printShowHuman(&bufDefault, cc, issue.ID, showOpts{}); err != nil {
		t.Fatalf("printShowHuman default: %v", err)
	}

	extractRulingsBlock := func(s string) string {
		t.Helper()
		start := strings.Index(s, "\nACTIVE RULINGS")
		end := strings.Index(s, "\nBASE TEXT")
		if start < 0 || end < 0 {
			t.Fatalf("missing ACTIVE RULINGS or BASE TEXT marker in:\n%s", s)
		}
		return s[start:end]
	}
	outlineBlock := extractRulingsBlock(bufOutline.String())
	defaultBlock := extractRulingsBlock(bufDefault.String())
	if outlineBlock != defaultBlock {
		t.Fatalf("ACTIVE RULINGS block differs between workfile (outline) and default show:\nworkfile: %q\ndefault:  %q", outlineBlock, defaultBlock)
	}
}

func TestHeadline_TerseTruncatesToo(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	issue := mkContractIssue(t, st, "terse trunc fixture", "")
	longText := strings.Repeat("e", 300)
	r := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: longText, IssueID: strPtr2(issue.ID), FiledBy: "owner:tester"})
	// runRulingsCmd opens its own store handle through flagDB; close this
	// one first so the sqlite file isn't held open twice (same pattern as
	// the pre-existing tests in rulings_test.go).
	_ = st.Close()

	t.Setenv(EnvTerse, "1")
	out, _, err := runRulingsCmd(t, []string{issue.ID})
	if err != nil {
		t.Fatalf("rulings %s: %v", issue.ID, err)
	}
	want := r.ID + "  [" + issue.ID + "]  " + headlineText(longText)
	line := strings.TrimRight(out, "\n")
	if line != want {
		t.Fatalf("want %q\ngot  %q", want, line)
	}
}

// TestHeadline_ShowStaysUnder4KB is the byte-cap check: an epic bound by 20
// inherited rulings of 400 characters each stays under the 4 KB agent-context
// budget once headlines truncate. Pre-flight arithmetic (beads-gc6.2 F-11):
// a headline line is ~43 bytes of fixed fields (indent, id, date, author,
// origin marker, separators) plus up to 120 runes of text plus a newline —
// roughly 165 bytes/line, so 20 lines is ~3.3 KB and still exercises the cap
// (40 lines would be ~6.6 KB and could never fit).
func TestHeadline_ShowStaysUnder4KB(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()
	root := mkContractEpic(t, st, "cap root")
	epic := &beads.Issue{Title: "cap epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, root.ID, epic, nil); err != nil {
		t.Fatalf("create child epic: %v", err)
	}
	for i := 0; i < 20; i++ {
		mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: strings.Repeat("z", 400), IssueID: strPtr2(root.ID), FiledBy: "owner:tester"})
	}

	out := renderContractOutput(t, st, epic.ID, showOpts{})
	if len(out) >= 4096 {
		t.Fatalf("expected the rendered contract under 4096 bytes, got %d:\n%s", len(out), out)
	}
}
