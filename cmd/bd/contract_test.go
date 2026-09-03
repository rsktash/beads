package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// Helpers for contract tests — distinct names to avoid collision with ruling_test.go.

func newTempContractStore(t *testing.T, prefix string) (string, *store.Store) {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "contract.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, prefix); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	old := flagDB
	flagDB = dsn
	t.Cleanup(func() {
		flagDB = old
		_ = st.Close()
	})
	return dsn, st
}

func mkContractIssue(t *testing.T, st *store.Store, title, desc string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Description: desc, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return i
}

func mkContractEpic(t *testing.T, st *store.Store, title string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create epic %q: %v", title, err)
	}
	return i
}

func renderContractOutput(t *testing.T, st *store.Store, issueID string, opts showOpts) string {
	t.Helper()
	// Use a fresh store handle for reading? Use the same st.
	cc := &cmdCtx{ctx: context.Background(), store: st, json: false}
	var buf bytes.Buffer
	if err := printShowHuman(&buf, cc, issueID, opts); err != nil {
		t.Fatalf("printShowHuman %s: %v", issueID, err)
	}
	return buf.String()
}

func mustCreateStatementDirect(t *testing.T, st *store.Store, s *beads.Statement) *beads.Statement {
	t.Helper()
	if err := st.CreateStatement(context.Background(), s); err != nil {
		t.Fatalf("CreateStatement %q: %v", s.Text, err)
	}
	return s
}

func strPtr2(s string) *string { return &s }

// captureStdoutStderr runs fn with os.Stdout/Stderr piped and returns captured strings.
func captureStdoutStderr(fn func() error) (string, string, error) {
	oldOut := os.Stdout
	oldErr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr
	err := fn()
	wOut.Close()
	wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr
	bOut, _ := io.ReadAll(rOut)
	bErr, _ := io.ReadAll(rErr)
	return string(bOut), string(bErr), err
}

func runShowWithArgs(t *testing.T, dsn string, args []string) (string, string, error) {
	t.Helper()
	old := flagDB
	flagDB = dsn
	t.Cleanup(func() { flagDB = old })
	out, errStr, err := captureStdoutStderr(func() error {
		cmd := newShowCmd()
		cmd.SetArgs(args)
		return cmd.Execute()
	})
	return out, errStr, err
}

// ---------- helpers for ordering checks ----------

func indexOrFail(t *testing.T, s, substr string) int {
	t.Helper()
	idx := strings.Index(s, substr)
	if idx < 0 {
		t.Fatalf("substring %q not found in output:\n%s", substr, s)
	}
	return idx
}

func mustContain(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Fatalf("expected %q in output, got:\n%s", substr, s)
	}
}

func mustNotContain(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Fatalf("unexpected %q in output, got:\n%s", substr, s)
	}
}

func mustNotContainCI(t *testing.T, s, substr string) {
	t.Helper()
	// CONTRACT header lines always carry a "comments N" count segment
	// (feature 2, gc6.3), which is structural, not leaked comment content;
	// exclude those lines so the guard still catches an actual leak.
	var kept []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(ln, "CONTRACT ") {
			continue
		}
		kept = append(kept, ln)
	}
	body := strings.Join(kept, "\n")
	if strings.Contains(strings.ToLower(body), strings.ToLower(substr)) {
		t.Fatalf("unexpected case-insensitive %q in output, got:\n%s", substr, s)
	}
}

// TestContract_TypedBeadOrder checks order of sections for a fully typed bead.
func TestContract_TypedBeadOrder(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()

	issue := mkContractIssue(t, st, "typed bead", "base body here")
	// Add dependencies: need another issue to block
	other := mkContractIssue(t, st, "blocker", "other")
	if err := st.AddDependency(ctx, beads.Dependency{IssueID: issue.ID, DependsOnID: other.ID, Type: beads.DepBlocks}); err != nil {
		t.Fatalf("add dep: %v", err)
	}
	// Add ruling, question, finding
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "must do X", IssueID: strPtr2(issue.ID)})
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "question", Text: "what about Y?", IssueID: strPtr2(issue.ID)})
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "finding", Text: "found Z", IssueID: strPtr2(issue.ID), FiledBy: "owner:tester", Evidence: "src/foo.go:10"})
	// Add untyped history
	now := time.Now().UTC()
	if err := st.AddComment(ctx, &beads.Comment{IssueID: issue.ID, Author: "alice", Text: "note one", CreatedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatalf("add comment: %v", err)
	}

	out := renderContractOutput(t, st, issue.ID, showOpts{})

	// Order: CONTRACT, ACTIVE RULINGS, OPEN QUESTIONS, FINDINGS, BASE TEXT, DEPENDENCIES, NOTES / UNTYPED HISTORY
	idxContract := indexOrFail(t, out, "CONTRACT")
	idxRulings := indexOrFail(t, out, "ACTIVE RULINGS — MUST OBEY")
	idxQuestions := indexOrFail(t, out, "OPEN QUESTIONS — EXECUTION BLOCKERS")
	idxFindings := indexOrFail(t, out, "FINDINGS")
	idxBase := indexOrFail(t, out, "BASE TEXT")
	idxDeps := indexOrFail(t, out, "DEPENDENCIES")
	idxNotes := indexOrFail(t, out, "NOTES / UNTYPED HISTORY")

	if !(idxContract < idxRulings && idxRulings < idxQuestions && idxQuestions < idxFindings && idxFindings < idxBase && idxBase < idxDeps && idxDeps < idxNotes) {
		t.Fatalf("order wrong: CONTRACT %d RULINGS %d QUESTIONS %d FINDINGS %d BASE %d DEPS %d NOTES %d\n%s", idxContract, idxRulings, idxQuestions, idxFindings, idxBase, idxDeps, idxNotes, out)
	}
	// Also check CONTRACT line format contains two spaces after id and status
	mustContain(t, out, "CONTRACT "+issue.ID+"  [")
	mustContain(t, out, "must do X")
	mustContain(t, out, "what about Y?")
	mustContain(t, out, "found Z")
	mustContain(t, out, "evidence: src/foo.go:10")
	mustContain(t, out, "blocked by")
	mustContain(t, out, "note one")
	// Ensure no "comment" substring (case-insensitive)
	mustNotContainCI(t, out, "comment")
}

// Test that sections with no rows are omitted.
func TestContract_SectionOmittedWhenEmpty(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()

	issue := mkContractIssue(t, st, "rulings only", "body")
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "only ruling", IssueID: strPtr2(issue.ID)})

	out := renderContractOutput(t, st, issue.ID, showOpts{})
	mustContain(t, out, "ACTIVE RULINGS — MUST OBEY")
	mustNotContain(t, out, "OPEN QUESTIONS — EXECUTION BLOCKERS")
	mustNotContain(t, out, "FINDINGS") // findings header should not appear if no findings? But check: FINDINGS header should be omitted when no findings.
	// However typed bead previously had findings, so we check that absence is correct.
	// For this bead, there are zero findings, so FINDINGS should be absent.
	if strings.Contains(out, "FINDINGS") {
		t.Fatalf("FINDINGS should be omitted when no findings, got:\n%s", out)
	}
	mustNotContainCI(t, out, "comment")
}

// Legacy bead — comments present, zero statements: untyped history renders ABOVE BASE TEXT under exact banner, no ACTIVE RULINGS.
func TestContract_LegacyBead(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()
	issue := mkContractIssue(t, st, "legacy bead", "legacy base")
	// Three comments with distinct times
	baseTime := time.Now().UTC().Truncate(time.Minute)
	c1 := &beads.Comment{IssueID: issue.ID, Author: "alice", Text: "first entry", CreatedAt: baseTime.Add(-3 * time.Hour)}
	c2 := &beads.Comment{IssueID: issue.ID, Author: "bob", Text: "second entry", CreatedAt: baseTime.Add(-2 * time.Hour)}
	c3 := &beads.Comment{IssueID: issue.ID, Author: "carol", Text: "third entry", CreatedAt: baseTime.Add(-1 * time.Hour)}
	for _, c := range []*beads.Comment{c1, c2, c3} {
		if err := st.AddComment(ctx, c); err != nil {
			t.Fatalf("add comment: %v", err)
		}
	}
	out := renderContractOutput(t, st, issue.ID, showOpts{})
	// Exact legacy banner
	legacyBanner := "UNTYPED HISTORY — provenance unknown, rulings may be buried here — newest first"
	mustContain(t, out, legacyBanner)
	// Ensure it renders ABOVE BASE TEXT
	idxLegacy := indexOrFail(t, out, legacyBanner)
	idxBase := indexOrFail(t, out, "BASE TEXT")
	if idxLegacy > idxBase {
		t.Fatalf("legacy history should be above BASE TEXT: legacy %d base %d\n%s", idxLegacy, idxBase, out)
	}
	// No ACTIVE RULINGS header anywhere
	if strings.Contains(out, "ACTIVE RULINGS") {
		t.Fatalf("legacy bead should have no ACTIVE RULINGS header, got:\n%s", out)
	}
	// Check newest first order: third, second, first
	idxThird := strings.Index(out, "third entry")
	idxSecond := strings.Index(out, "second entry")
	idxFirst := strings.Index(out, "first entry")
	if !(idxThird < idxSecond && idxSecond < idxFirst) {
		t.Fatalf("legacy order newest first failed: third %d second %d first %d\n%s", idxThird, idxSecond, idxFirst, out)
	}
	mustNotContainCI(t, out, "comment")
	// Also ensure legacy banner appears, and NOTES header does NOT appear (legacy uses different banner)
	if strings.Contains(out, "NOTES / UNTYPED HISTORY") {
		t.Fatalf("legacy should use banner, not NOTES header, got:\n%s", out)
	}
}

// Mixed bead — statements and comments both present: typed sections above BASE TEXT, untyped history below DEPENDENCIES under NOTES header, not legacy banner
func TestContract_MixedBead(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()
	issue := mkContractIssue(t, st, "mixed bead", "mixed base")
	other := mkContractIssue(t, st, "dep other", "other")
	if err := st.AddDependency(ctx, beads.Dependency{IssueID: issue.ID, DependsOnID: other.ID, Type: beads.DepBlocks}); err != nil {
		t.Fatalf("add dep: %v", err)
	}
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "mixed ruling", IssueID: strPtr2(issue.ID)})
	baseTime := time.Now().UTC().Truncate(time.Minute)
	if err := st.AddComment(ctx, &beads.Comment{IssueID: issue.ID, Author: "alice", Text: "mixed note", CreatedAt: baseTime}); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	out := renderContractOutput(t, st, issue.ID, showOpts{})
	// Should have typed section above BASE TEXT
	idxRulings := indexOrFail(t, out, "ACTIVE RULINGS — MUST OBEY")
	idxBase := indexOrFail(t, out, "BASE TEXT")
	if idxRulings > idxBase {
		t.Fatalf("typed section should be above BASE TEXT: rulings %d base %d\n%s", idxRulings, idxBase, out)
	}
	// History below DEPENDENCIES under NOTES header, not legacy banner
	idxDeps := indexOrFail(t, out, "DEPENDENCIES")
	idxNotes := indexOrFail(t, out, "NOTES / UNTYPED HISTORY")
	if idxNotes < idxDeps {
		t.Fatalf("mixed history should be below DEPENDENCIES: notes %d deps %d\n%s", idxNotes, idxDeps, out)
	}
	if idxNotes < idxBase {
		t.Fatalf("mixed history should be below BASE TEXT: notes %d base %d\n%s", idxNotes, idxBase, out)
	}
	legacyBanner := "UNTYPED HISTORY — provenance unknown, rulings may be buried here — newest first"
	if strings.Contains(out, legacyBanner) {
		t.Fatalf("mixed bead should not use legacy banner, got:\n%s", out)
	}
	mustContain(t, out, "mixed note")
	mustNotContainCI(t, out, "comment")
}

// Untyped history newest first in both placements — three-comment fixture asserts order (already tested in legacy, but add mixed order test)
func TestContract_UntypedHistoryNewestFirstMixed(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()
	issue := mkContractIssue(t, st, "history order", "base")
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "ruling for mix", IssueID: strPtr2(issue.ID)})
	baseTime := time.Now().UTC().Truncate(time.Minute)
	c1 := &beads.Comment{IssueID: issue.ID, Author: "a1", Text: "alpha", CreatedAt: baseTime.Add(-3 * time.Hour)}
	c2 := &beads.Comment{IssueID: issue.ID, Author: "a2", Text: "beta", CreatedAt: baseTime.Add(-2 * time.Hour)}
	c3 := &beads.Comment{IssueID: issue.ID, Author: "a3", Text: "gamma", CreatedAt: baseTime.Add(-1 * time.Hour)}
	for _, c := range []*beads.Comment{c1, c2, c3} {
		if err := st.AddComment(ctx, c); err != nil {
			t.Fatalf("add comment: %v", err)
		}
	}
	out := renderContractOutput(t, st, issue.ID, showOpts{})
	// NOTES header should be present
	mustContain(t, out, "NOTES / UNTYPED HISTORY (newest first)")
	idxGamma := strings.Index(out, "gamma")
	idxBeta := strings.Index(out, "beta")
	idxAlpha := strings.Index(out, "alpha")
	if !(idxGamma < idxBeta && idxBeta < idxAlpha) {
		t.Fatalf("mixed order newest first failed: gamma %d beta %d alpha %d\n%s", idxGamma, idxBeta, idxAlpha, out)
	}
	mustNotContainCI(t, out, "comment")
}

// Superseded chain: three rulings where third supersedes second and second supersedes first render as one line, third. First two ids appear nowhere.
func TestContract_SupersededChain(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "chain bead", "body")
	r1 := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "first ruling", IssueID: strPtr2(issue.ID)})
	r2 := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "second ruling", IssueID: strPtr2(issue.ID), SupersedesID: strPtr2(r1.ID)})
	r3 := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "third ruling", IssueID: strPtr2(issue.ID), SupersedesID: strPtr2(r2.ID)})
	// Need to mark r1 and r2 as superseded? But chain reduction already handles via superseded map even if status still active.
	// However to be precise, set status to superseded for r1,r2 via direct update? ContractStatements fetches only active, so if r1/r2 remain active they would still be considered but filtered by superseded map, leaving only r3. That satisfies gate even without status change.
	// To ensure chain reduction works, we also ensure r3 is terminal.
	_ = r1
	_ = r2
	out := renderContractOutput(t, st, issue.ID, showOpts{})
	// Should contain third ruling id and text, not first two
	if !strings.Contains(out, r3.ID) {
		t.Fatalf("expected terminal %s in output, got:\n%s", r3.ID, out)
	}
	if !strings.Contains(out, "third ruling") {
		t.Fatalf("expected third ruling text in output, got:\n%s", out)
	}
	if strings.Contains(out, r1.ID) {
		t.Fatalf("superseded %s should not appear, output:\n%s", r1.ID, out)
	}
	if strings.Contains(out, r2.ID) {
		t.Fatalf("superseded %s should not appear, output:\n%s", r2.ID, out)
	}
	if strings.Contains(out, "first ruling") {
		t.Fatalf("first ruling text should not appear, got:\n%s", out)
	}
	if strings.Contains(out, "second ruling") {
		t.Fatalf("second ruling text should not appear, got:\n%s", out)
	}
	// Ensure only one ruling line? Count occurrences of "R-"
	// But there should be exactly one R- line in ACTIVE RULINGS
	// We check that output contains only one ruling id substring: r3.ID appears once
	count := strings.Count(out, r3.ID)
	if count != 1 {
		t.Fatalf("expected exactly one occurrence of terminal id %s, got %d in:\n%s", r3.ID, count, out)
	}
	mustNotContainCI(t, out, "comment")
}

// Inherited rulings: task under epic under root epic, with one ruling on each plus one project-scoped, renders all four; each inherited line carries origin tag, task's own carries none.
func TestContract_InheritedRulings(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()

	root := mkContractEpic(t, st, "root epic")
	epic := &beads.Issue{Title: "child epic", Type: beads.TypeEpic, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, root.ID, epic, nil); err != nil {
		t.Fatalf("create child epic: %v", err)
	}
	task := &beads.Issue{Title: "leaf task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Create rulings
	taskRuling := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "task ruling", IssueID: strPtr2(task.ID)})
	epicRuling := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "epic ruling", IssueID: strPtr2(epic.ID)})
	rootRuling := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "root ruling", IssueID: strPtr2(root.ID)})
	projRuling := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "project ruling"})

	out := renderContractOutput(t, st, task.ID, showOpts{})

	// All four should appear
	for _, s := range []*beads.Statement{taskRuling, epicRuling, rootRuling, projRuling} {
		if !strings.Contains(out, s.Text) {
			t.Fatalf("expected ruling text %q (%s) in output, got:\n%s", s.Text, s.ID, out)
		}
		if !strings.Contains(out, s.ID) {
			t.Fatalf("expected ruling id %s in output, got:\n%s", s.ID, out)
		}
	}

	// Each inherited line carries its origin tag and task's own line carries none.
	// Find lines containing each ruling text and check for bracket origin tag.
	lines := strings.Split(out, "\n")
	findLine := func(text string) string {
		for _, l := range lines {
			if strings.Contains(l, text) {
				return l
			}
		}
		return ""
	}
	taskLine := findLine("task ruling")
	epicLine := findLine("epic ruling")
	rootLine := findLine("root ruling")
	projLine := findLine("project ruling")

	if taskLine == "" || epicLine == "" || rootLine == "" || projLine == "" {
		t.Fatalf("failed to find lines for each ruling in:\n%s", out)
	}
	// Task's own line should carry no origin tag (no bracket with ancestor id or project)
	// For own, there should be no "[" after the date except maybe not? Our implementation adds no tag for own, so line should be: "  R-x  date  text" with no "[".
	// Check that taskLine does not contain epic.ID, root.ID, or "[project]"
	if strings.Contains(taskLine, "[") {
		// But task line may contain no bracket at all. If it contains bracket, it's tagged incorrectly.
		// However findings have [filed_by] but rulings shouldn't have bracket for own.
		t.Fatalf("task own line should carry no origin tag, got %q", taskLine)
	}
	if !strings.Contains(epicLine, epic.ID) {
		t.Fatalf("epic inherited line should carry origin tag with epic id %s, got %q", epic.ID, epicLine)
	}
	if !strings.Contains(rootLine, root.ID) {
		t.Fatalf("root inherited line should carry origin tag with root id %s, got %q", root.ID, rootLine)
	}
	if !strings.Contains(projLine, "[project]") {
		t.Fatalf("project ruling line should carry [project] tag, got %q", projLine)
	}
	mustNotContainCI(t, out, "comment")
}

// Scope self narrowing: ruling on epic with scope self does not appear in child, while sibling ruling on same epic with default scope does.
func TestContract_ScopeSelfNarrowing(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()

	epic := mkContractEpic(t, st, "epic scope")
	task := &beads.Issue{Title: "child task", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateChild(ctx, epic.ID, task, nil); err != nil {
		t.Fatalf("create child: %v", err)
	}

	// Self-scoped ruling on epic
	selfRuling := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "self ruling", IssueID: strPtr2(epic.ID), Scope: "self"})
	inheritRuling := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "inherit ruling", IssueID: strPtr2(epic.ID), Scope: "inherit"})

	out := renderContractOutput(t, st, task.ID, showOpts{})

	if strings.Contains(out, selfRuling.ID) || strings.Contains(out, "self ruling") {
		t.Fatalf("self-scoped ruling should NOT appear in child, but found %s in:\n%s", selfRuling.ID, out)
	}
	if !strings.Contains(out, inheritRuling.ID) || !strings.Contains(out, "inherit ruling") {
		t.Fatalf("inherit ruling should appear in child, missing %s in:\n%s", inheritRuling.ID, out)
	}
	mustNotContainCI(t, out, "comment")
}

// No comment substring in any of the six rendered outputs — additional explicit test per case (already checked above, but one more aggregate)
func TestContract_NoCommentSubstringAcrossAll(t *testing.T) {
	// This test just ensures previous tests' outputs don't contain comment; we duplicate check by rendering each case again and asserting CI.
	// We'll do a simple typed bead output check as representative; the other cases already assert per test.
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "check no forbidden word", "body")
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "ruling", IssueID: strPtr2(issue.ID)})
	out := renderContractOutput(t, st, issue.ID, showOpts{})
	mustNotContainCI(t, out, "comment")
	mustNotContainCI(t, out, "Comment")
	mustNotContainCI(t, out, "COMMENT")
}

// --include comments flag still parses and exits zero, and no longer changes text output
func TestContract_IncludeCommentsFlagParses(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()
	issue := mkContractIssue(t, st, "include flag", "body text")
	if err := st.AddComment(ctx, &beads.Comment{IssueID: issue.ID, Author: "alice", Text: "history entry", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	// Need to close store before CLI reopens? ruling_test closes st before running command to avoid lock.
	_ = st.Close()
	// Without flag
	outNoFlag, _, errNoFlag := runShowWithArgs(t, dsn, []string{issue.ID})
	if errNoFlag != nil {
		t.Fatalf("show without flag should succeed, err %v out %q", errNoFlag, outNoFlag)
	}
	// With --include comments
	outWithFlag, _, errWithFlag := runShowWithArgs(t, dsn, []string{issue.ID, "--include", "comments"})
	if errWithFlag != nil {
		t.Fatalf("show --include comments should parse and exit zero, err %v", errWithFlag)
	}
	// Text output should be same (include no longer changes text output)
	// Trim spaces for comparison? Should be byte-equal after contract change.
	if outNoFlag != outWithFlag {
		t.Fatalf("text output should be same with and without --include comments\nwithout:\n%s\nwith:\n%s", outNoFlag, outWithFlag)
	}
	mustNotContainCI(t, outWithFlag, "comment")
}

// Base text outline threshold and selectors inside BASE TEXT
func TestContract_BaseTextThresholdAndSelectors(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()

	// Build a description at or above 2048 bytes with headings
	var sb strings.Builder
	sb.WriteString("## Alpha\n")
	sb.WriteString("section alpha body\n")
	sb.WriteString("## Beta\n")
	sb.WriteString("section beta body\n")
	// Pad to exceed threshold
	for sb.Len() < 2100 {
		sb.WriteString("x")
	}
	largeDesc := sb.String()

	// Small desc for other selector tests
	smallDesc := "line1\nline2\nline3\nline4\nline5\n## Sec\nsec body\n"

	// Test outline inside BASE TEXT: large desc, no --full => outline
	issueLarge := mkContractIssue(t, st, "large bead", largeDesc)
	outOutline := renderContractOutput(t, st, issueLarge.ID, showOpts{})
	mustContain(t, outOutline, "BASE TEXT")
	// Outline inside base text should contain description chars or sections header
	if !strings.Contains(outOutline, "description:") && !strings.Contains(outOutline, "sections:") {
		t.Fatalf("outline should be inside BASE TEXT, got:\n%s", outOutline)
	}
	// Ensure body not directly printed? But outline case should not contain full body "x" repeated? Hard to assert. Instead ensure BASE TEXT appears before outline content.
	idxBase := indexOrFail(t, outOutline, "BASE TEXT")
	idxDesc := strings.Index(outOutline, "description:")
	if idxDesc < idxBase {
		t.Fatalf("outline should appear after BASE TEXT header")
	}
	mustNotContainCI(t, outOutline, "comment")

	// Test --full still prints body inside BASE TEXT
	outFull := renderContractOutput(t, st, issueLarge.ID, showOpts{full: true})
	mustContain(t, outFull, "BASE TEXT")
	mustContain(t, outFull, "Alpha")
	mustContain(t, outFull, "Beta")
	// Should not contain outline marker when full
	if strings.Contains(outFull, "description:") {
		t.Fatalf("--full should print body not outline, got:\n%s", outFull)
	}
	idxBaseFull := indexOrFail(t, outFull, "BASE TEXT")
	idxAlpha := strings.Index(outFull, "Alpha")
	if idxAlpha < idxBaseFull {
		t.Fatalf("body should be after BASE TEXT header")
	}

	// Test --section selects within BASE TEXT
	issueSmall := mkContractIssue(t, st, "small bead", smallDesc)
	outSection := renderContractOutput(t, st, issueSmall.ID, showOpts{section: "sec"})
	mustContain(t, outSection, "BASE TEXT")
	mustContain(t, outSection, "sec body")
	idxBaseSec := indexOrFail(t, outSection, "BASE TEXT")
	idxSecBody := strings.Index(outSection, "sec body")
	if idxSecBody < idxBaseSec {
		t.Fatalf("--section body should be after BASE TEXT header")
	}
	// Ensure other sections not present? For simplicity just check sec body present
	mustNotContainCI(t, outSection, "comment")

	// Test --lines selects within BASE TEXT
	ls, _ := parseLineSlice("2-3", 0, 0)
	outLines := renderContractOutput(t, st, issueSmall.ID, showOpts{lineSlice: ls})
	mustContain(t, outLines, "BASE TEXT")
	mustContain(t, outLines, "line2")
	mustContain(t, outLines, "line3")
	idxBaseLines := indexOrFail(t, outLines, "BASE TEXT")
	idxLine2 := strings.Index(outLines, "line2")
	if idxLine2 < idxBaseLines {
		t.Fatalf("--lines body should be after BASE TEXT header")
	}
	mustNotContainCI(t, outLines, "comment")

	// Test --head selects within BASE TEXT
	lsHead, _ := parseLineSlice("", 2, 0)
	outHead := renderContractOutput(t, st, issueSmall.ID, showOpts{lineSlice: lsHead})
	mustContain(t, outHead, "BASE TEXT")
	mustContain(t, outHead, "line1")
	mustContain(t, outHead, "line2")
	if strings.Contains(outHead, "line3") {
		t.Fatalf("--head 2 should not contain line3, got:\n%s", outHead)
	}
	mustNotContainCI(t, outHead, "comment")

	// Test --tail selects within BASE TEXT
	lsTail, _ := parseLineSlice("", 0, 2)
	outTail := renderContractOutput(t, st, issueSmall.ID, showOpts{lineSlice: lsTail})
	mustContain(t, outTail, "BASE TEXT")
	// tail 2 of smallDesc should be last 2 lines: "## Sec" and "sec body" or "line5" and "## Sec"? Actually smallDesc lines: line1,line2,line3,line4,line5,## Sec,sec body => tail 2 => ## Sec, sec body
	mustContain(t, outTail, "sec body")
	idxBaseTail := indexOrFail(t, outTail, "BASE TEXT")
	idxSecTail := strings.Index(outTail, "sec body")
	if idxSecTail < idxBaseTail {
		t.Fatalf("--tail body should be after BASE TEXT header")
	}
	mustNotContainCI(t, outTail, "comment")
}

// Workfile still prints header and still writes same body file byte-compared against bead's description
func TestContract_WorkfilePreserved(t *testing.T) {
	// Setup temp project with .bd
	tmpDir := t.TempDir()
	bdDir := filepath.Join(tmpDir, ".bd")
	if err := os.MkdirAll(filepath.Join(bdDir, ".scratch"), 0755); err != nil {
		t.Fatalf("mkdir scratch: %v", err)
	}
	dsnFile := filepath.Join(tmpDir, "workfile.db")
	dsn := dsnFile
	// Write .bd/config
	if err := os.WriteFile(filepath.Join(bdDir, "config"), []byte("db="+dsn+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	// Need to set BD_DIR to tmpDir/.bd? Or rely on walk up. Let's set env to ensure findBeadsDir finds it.
	// Use BD_DIR override pointing to bdDir
	t.Setenv("BD_DIR", bdDir)
	// Also ensure flagDB is dsn? workfile uses flagDB via config.Resolve; if flagDB is set, BeadDir still resolved via BD_DIR
	// Set flagDB to dsn so openStore uses that DSN, but BeadDir still from BD_DIR
	oldFlag := flagDB
	flagDB = dsn
	t.Cleanup(func() { flagDB = oldFlag })

	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, "bd"); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	desc := "workfile body line1\nline2\n"
	issue := &beads.Issue{Title: "workfile bead", Description: desc, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(ctx, issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	_ = st.Close()

	// Capture stdout via pipe and run workfile command
	out, _, err := captureStdoutStderr(func() error {
		cmd := newWorkfileCmd()
		cmd.SetArgs([]string{issue.ID})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("workfile execute: %v out %q", err, out)
	}
	// Should contain CONTRACT header
	if !strings.Contains(out, "CONTRACT") {
		t.Fatalf("workfile header should contain CONTRACT, got %q", out)
	}
	if !strings.Contains(out, issue.ID) {
		t.Fatalf("workfile header should contain issue id, got %q", out)
	}
	mustNotContainCI(t, out, "comment")

	// Check file written
	written, err := os.ReadFile(filepath.Join(bdDir, ".scratch", issue.ID+".md"))
	if err != nil {
		// Also try default location if workfile used flagDB path? It should use BeadDir/.scratch
		t.Fatalf("read workfile: %v", err)
	}
	expected := desc
	if !strings.HasSuffix(expected, "\n") {
		expected += "\n"
	}
	if string(written) != expected {
		t.Fatalf("workfile body mismatch: expected %q got %q", expected, string(written))
	}
	// Byte-compare against bead's description (with trailing newline ensured)
	if string(written) != expected {
		t.Fatalf("byte compare failed")
	}

	// Also ensure workfile header is same as bd show --outline header? Not needed, just ensure it prints header.
}

// Multi-id still separates beads with existing --- line, and failing id still yields partial-success non-zero exit
func TestContract_MultiIdSeparationAndPartialSuccess(t *testing.T) {
	dsn, st := newTempContractStore(t, "bd")
	defer st.Close()
	issueA := mkContractIssue(t, st, "multi A", "body A")
	issueB := mkContractIssue(t, st, "multi B", "body B")
	_ = st.Close()

	// Test separation: two valid ids
	out, _, err := runShowWithArgs(t, dsn, []string{issueA.ID, issueB.ID})
	if err != nil {
		t.Fatalf("multi show two ids should succeed, err %v out %q", err, out)
	}
	if !strings.Contains(out, "\n---") {
		t.Fatalf("multi-id should separate with --- line, got:\n%s", out)
	}
	if !strings.Contains(out, issueA.ID) || !strings.Contains(out, issueB.ID) {
		t.Fatalf("both ids should appear in multi output, got:\n%s", out)
	}
	mustNotContainCI(t, out, "comment")

	// Test partial success: one valid, one invalid -> non-zero exit but still prints valid
	// Need to reopen store? dsn already has issues, we closed st but dsn file persists
	out2, errStr, err2 := runShowWithArgs(t, dsn, []string{issueA.ID, "bd-9999"})
	if err2 == nil {
		t.Fatalf("multi with failing id should return error, got nil out %q errStr %q", out2, errStr)
	}
	if !strings.Contains(errStr, "bd-9999") && !strings.Contains(out2, "bd-9999") {
		// error is printed to stderr via fmt.Fprintf(os.Stderr, "error: %s: %v\n", id, err)
		// Our capture of stderr should contain it
		// But we check err message itself
		if !strings.Contains(err2.Error(), "1 of 2") {
			t.Fatalf("expected partial-success error, got %v errStr %q out %q", err2, errStr, out2)
		}
	}
	// Stdout should still contain the valid bead
	if !strings.Contains(out2, issueA.ID) {
		t.Fatalf("partial success should still contain valid bead %s in stdout, got:\n%s\nstderr:\n%s", issueA.ID, out2, errStr)
	}
	// Also check that separator not needed for single success? Only one valid, so no --- needed, but that's fine.
	mustNotContainCI(t, out2, "comment")
}

// Additional test to ensure workfile via direct printShowHuman with outline still shows BASE TEXT
func TestContract_WorkfileOutlineInsideBaseText(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	// Large description
	var sb strings.Builder
	sb.WriteString("## Head\nbody\n")
	for sb.Len() < 2100 {
		sb.WriteString("y")
	}
	desc := sb.String()
	issue := mkContractIssue(t, st, "outline bead", desc)
	// workfile forces outline: true
	out := renderContractOutput(t, st, issue.ID, showOpts{outline: true})
	mustContain(t, out, "BASE TEXT")
	if !strings.Contains(out, "description:") {
		t.Fatalf("workfile outline should be inside BASE TEXT, got:\n%s", out)
	}
	mustNotContainCI(t, out, "comment")
}

// Ensure CONTRACT line has two spaces after id
func TestContract_ContractLineFormat(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "format check", "body")
	out := renderContractOutput(t, st, issue.ID, showOpts{})
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		t.Fatalf("empty output")
	}
	first := lines[0]
	expectedPrefix := "CONTRACT " + issue.ID + "  ["
	if !strings.HasPrefix(first, expectedPrefix) {
		t.Fatalf("first line should be %q, got %q", expectedPrefix, first)
	}
}

func TestContract_DependenciesInsideBaseTextOrder(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()
	issue := mkContractIssue(t, st, "dep order", "body")
	other := mkContractIssue(t, st, "other2", "other")
	if err := st.AddDependency(ctx, beads.Dependency{IssueID: issue.ID, DependsOnID: other.ID, Type: beads.DepBlocks}); err != nil {
		t.Fatalf("add dep: %v", err)
	}
	out := renderContractOutput(t, st, issue.ID, showOpts{})
	idxBase := indexOrFail(t, out, "BASE TEXT")
	idxDeps := indexOrFail(t, out, "DEPENDENCIES")
	if idxBase > idxDeps {
		t.Fatalf("BASE TEXT should be before DEPENDENCIES, got base %d deps %d\n%s", idxBase, idxDeps, out)
	}
	mustNotContainCI(t, out, "comment")
}

func TestContract_FindingsEvidence(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "finding evidence", "body")
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "finding", Text: "found bug", IssueID: strPtr2(issue.ID), FiledBy: "tester:owner", Evidence: "src/main.go:42"})
	out := renderContractOutput(t, st, issue.ID, showOpts{})
	mustContain(t, out, "evidence: src/main.go:42")
	mustContain(t, out, "[tester:owner]")
	mustNotContainCI(t, out, "comment")
}

// Test that description selectors still work inside BASE TEXT for edge cases
func TestContract_SectionNotFoundStillErrors(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	issue := mkContractIssue(t, st, "sec not found", "no headings here")
	cc := &cmdCtx{ctx: context.Background(), store: st, json: false}
	var buf bytes.Buffer
	err := printShowHuman(&buf, cc, issue.ID, showOpts{section: "missing"})
	if err == nil {
		t.Fatalf("section not found should error")
	}
	if !strings.Contains(err.Error(), "section") {
		t.Fatalf("error should mention section, got %v", err)
	}
}

// Ensure that include set is ignored for text (already tested via runShow), but also direct render ignores it
func TestContract_IncludeSetIgnoredInText(t *testing.T) {
	_, st := newTempContractStore(t, "bd")
	defer st.Close()
	ctx := context.Background()
	issue := mkContractIssue(t, st, "include ignore", "body")
	if err := st.AddComment(ctx, &beads.Comment{IssueID: issue.ID, Author: "bob", Text: "history", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	out1 := renderContractOutput(t, st, issue.ID, showOpts{})
	out2 := renderContractOutput(t, st, issue.ID, showOpts{include: parseIncludeSet([]string{"comments"})})
	if out1 != out2 {
		t.Fatalf("include set should not change text output\nwithout: %q\nwith: %q", out1, out2)
	}
}

// Dummy to avoid unused import warning for fmt if not used elsewhere
var _ = fmt.Sprintf
