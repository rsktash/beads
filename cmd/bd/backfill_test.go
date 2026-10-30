package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newTempBackfillStore(t *testing.T, prefix string) (string, *store.Store) {
	t.Helper()
	isolateExpandState(t)
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "backfill.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, prefix); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	oldDB := flagDB
	oldJSON := flagJSON
	flagDB = dsn
	flagJSON = false
	t.Cleanup(func() {
		flagDB = oldDB
		flagJSON = oldJSON
		_ = st.Close()
	})
	return dsn, st
}

func mkBackfillIssue(t *testing.T, st *store.Store, title string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return i
}

// runBackfillViaRoot runs `bd statements backfill` via the root command, capturing stdout/stderr.
// args are passed after "statements backfill", e.g. []string{"--dry-run"}.
func runBackfillViaRoot(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	savedDB := flagDB
	savedJSON := flagJSON
	root := newRoot()
	// newRoot re-registers flags with default values, clobbering globals; restore.
	flagDB = savedDB
	flagJSON = savedJSON
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	fullArgs := append([]string{"statements", "backfill"}, args...)
	root.SetArgs(fullArgs)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

// runBackfillDirect runs the backfill subcommand directly (without root wrapper) for tests that want to avoid root parsing.
// Useful for --json as local flag case.
func runBackfillDirect(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	cmd := newStatementsBackfillCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	cmd.SetOut(bufOut)
	cmd.SetErr(bufErr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return bufOut.String(), bufErr.String(), err
}

func backfillCountStatements(t *testing.T, dsn string) int {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open count: %v", err)
	}
	defer st.Close()
	list, err := st.ListStatements(ctx, store.StatementFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return len(list)
}

// backfillPreviewArgs runs backfill the only way it still runs: a dry-run
// preview, as JSON so each test reads the rows it would have produced.
var backfillPreviewArgs = []string{"--dry-run", "--json"}

func backfillPreview(t *testing.T, out string) []beads.Statement {
	t.Helper()
	var list []beads.Statement
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("backfill preview json: %v out %q", err, out)
	}
	return list
}

// ---------- helpers for creating fixture comments ----------

func addBackfillComment(t *testing.T, st *store.Store, issueID, text string) *beads.Comment {
	t.Helper()
	c := &beads.Comment{IssueID: issueID, Author: "alice", Text: text, CreatedAt: time.Now().UTC()}
	if err := st.AddComment(context.Background(), c); err != nil {
		t.Fatalf("add comment %q: %v", text, err)
	}
	return c
}

// TestBackfill_ShoutedMarkers ensures each shouted authority marker yields a
// candidate (one per comment), while a coordinator-only comment mints nothing.
func TestBackfill_ShoutedMarkers(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	// Three shouted-marker beads, plus one coordinator-only bead that must not mint.
	i1 := mkBackfillIssue(t, st, "owner ruling bead")
	c1 := addBackfillComment(t, st, i1.ID, "OWNER RULING: we decided X")
	i2 := mkBackfillIssue(t, st, "ruled bead")
	c2 := addBackfillComment(t, st, i2.ID, "RULED: the outcome is Y")
	i3 := mkBackfillIssue(t, st, "coordinator bead")
	c3 := addBackfillComment(t, st, i3.ID, "[coordinator] please approve")
	i4 := mkBackfillIssue(t, st, "deferred bead")
	c4 := addBackfillComment(t, st, i4.ID, "DEFERRED until next sprint")

	_ = st.Close()

	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v out %q", err, previewOut)
	}
	// Expect 3 candidates: coordinator-only comment does not mint.
	list := backfillPreview(t, previewOut)
	if len(list) != 3 {
		t.Fatalf("expected exactly 3 candidates, got %d %+v out %q", len(list), list, previewOut)
	}
	// Each marker has its own assertion: find candidate linked to each comment id.
	bySrc := map[string]beads.Statement{}
	for _, s := range list {
		if s.SourceCommentID != nil {
			bySrc[*s.SourceCommentID] = s
		}
	}
	// OWNER RULING
	if _, ok := bySrc[c1.ID]; !ok {
		t.Fatalf("OWNER RULING comment %s did not yield candidate, bySrc %+v list %+v", c1.ID, bySrc, list)
	}
	// RULED:
	if _, ok := bySrc[c2.ID]; !ok {
		t.Fatalf("RULED: comment %s did not yield candidate", c2.ID)
	}
	// [coordinator]-only must NOT mint.
	if _, ok := bySrc[c3.ID]; ok {
		t.Fatalf("coordinator-only comment %s should not yield candidate, list %+v", c3.ID, list)
	}
	// DEFERRED
	if _, ok := bySrc[c4.ID]; !ok {
		t.Fatalf("DEFERRED comment %s did not yield candidate", c4.ID)
	}
	// Also assert each candidate's text contains the marker.
	if !strings.Contains(bySrc[c1.ID].Text, "OWNER RULING") {
		t.Fatalf("candidate text for OWNER RULING should contain marker, got %q", bySrc[c1.ID].Text)
	}
	if !strings.Contains(bySrc[c2.ID].Text, "RULED:") {
		t.Fatalf("candidate text for RULED: should contain marker, got %q", bySrc[c2.ID].Text)
	}
	if !strings.Contains(bySrc[c4.ID].Text, "DEFERRED") {
		t.Fatalf("candidate text for DEFERRED should contain marker, got %q", bySrc[c4.ID].Text)
	}
}

// Test that every emitted row has kind ruling and status candidate, no active.
func TestBackfill_KindAndStatus(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	i1 := mkBackfillIssue(t, st, "a")
	addBackfillComment(t, st, i1.ID, "OWNER RULING test")
	i2 := mkBackfillIssue(t, st, "b")
	addBackfillComment(t, st, i2.ID, "RULED: test")
	i4 := mkBackfillIssue(t, st, "d")
	addBackfillComment(t, st, i4.ID, "DEFERRED foo")
	_ = st.Close()
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	for _, s := range list {
		if s.Kind != "ruling" {
			t.Fatalf("expected kind ruling, got %q for %s", s.Kind, s.ID)
		}
		if s.Status != "candidate" {
			t.Fatalf("expected status candidate, got %q for %s", s.Status, s.ID)
		}
		if s.Status == "active" {
			t.Fatalf("no row is emitted with status active, found %s", s.ID)
		}
	}
}

// Test that every emitted row's source_comment_id equals comment id and issue_id equals that comment's issue.
func TestBackfill_SourceAndIssueLinkage(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	issues := []*beads.Issue{}
	comments := []*beads.Comment{}
	for idx, txt := range []string{"OWNER RULING link", "RULED: link", "DEFERRED link"} {
		iss := mkBackfillIssue(t, st, fmt.Sprintf("issue%d", idx))
		issues = append(issues, iss)
		c := addBackfillComment(t, st, iss.ID, txt)
		comments = append(comments, c)
	}
	_ = st.Close()
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	// Build map commentID -> issueID
	commentIssue := map[string]string{}
	for _, c := range comments {
		commentIssue[c.ID] = c.IssueID
	}
	for _, s := range list {
		if s.SourceCommentID == nil {
			t.Fatalf("statement %s missing source_comment_id", s.ID)
		}
		src := *s.SourceCommentID
		expectedIssue, ok := commentIssue[src]
		if !ok {
			t.Fatalf("statement %s source_comment_id %s not found in fixture", s.ID, src)
		}
		if s.IssueID == nil {
			t.Fatalf("statement %s missing issue_id, expected %s", s.ID, expectedIssue)
		}
		if *s.IssueID != expectedIssue {
			t.Fatalf("statement %s issue_id %q != comment's issue %q (src %s)", s.ID, *s.IssueID, expectedIssue, src)
		}
		// Also ensure source_comment_id equals id of comment it came from (already checked)
		if src != expectedIssue && false { // placeholder to satisfy both asserted
		}
	}
}

// A comment carrying none of the markers yields nothing, and ten unmarked yields zero.
func TestBackfill_NoMarkerYieldsNothing(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "no marker")
	addBackfillComment(t, st, iss.ID, "this is just a regular note with no marker")
	_ = st.Close()
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 0 {
		t.Fatalf("expected zero candidates for unmarked comment, got %d %+v", len(list), list)
	}
}

func TestBackfill_TenUnmarkedZero(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "ten unmarked")
	for i := 0; i < 10; i++ {
		addBackfillComment(t, st, iss.ID, fmt.Sprintf("unmarked comment %d — nothing special", i))
	}
	_ = st.Close()
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 0 {
		t.Fatalf("fixture of ten unmarked comments yields zero candidates, got %d", len(list))
	}
}

// Marker matching does not over-reach: exact fixture texts.
func TestBackfill_Overreach_RulingWasUnclear(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "overreach1")
	// Exact fixture text per gate: "the ruling was unclear"
	addBackfillComment(t, st, iss.ID, "the ruling was unclear")
	_ = st.Close()
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 0 {
		t.Fatalf("comment %q should produce nothing, got %d candidates %+v", "the ruling was unclear", len(list), list)
	}
}

func TestBackfill_Overreach_RuledOut(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "overreach2")
	addBackfillComment(t, st, iss.ID, "ruled out")
	_ = st.Close()
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 0 {
		t.Fatalf("comment %q should produce nothing, got %d candidates %+v", "ruled out", len(list), list)
	}
}

// Ensure lowercase variants of shouted markers do not match.
func TestBackfill_Overreach_LowercaseShouted(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "lowercase")
	addBackfillComment(t, st, iss.ID, "owner ruling")
	addBackfillComment(t, st, iss.ID, "ruled: lowercase")
	addBackfillComment(t, st, iss.ID, "deferred lower")
	_ = st.Close()
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 0 {
		t.Fatalf("lowercase shouted markers should not match, got %d %+v", len(list), list)
	}
}

// Comment matching more than one marker still yields exactly one candidate.
func TestBackfill_MultipleMarkersOneCandidate(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "multi")
	addBackfillComment(t, st, iss.ID, "OWNER RULING and RULED: and DEFERRED and [coordinator] all in one")
	_ = st.Close()
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 1 {
		t.Fatalf("comment matching more than one marker still yields exactly one candidate, got %d", len(list))
	}
}

// --dry-run prints what it would emit and writes nothing; statements table unchanged.
func TestBackfill_DryRun(t *testing.T) {
	dsn, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "dryrun")
	addBackfillComment(t, st, iss.ID, "OWNER RULING dryrun")
	addBackfillComment(t, st, iss.ID, "RULED: dryrun2")
	_ = st.Close()
	before := backfillCountStatements(t, dsn)
	out, _, err := runBackfillViaRoot(t, []string{"--dry-run"})
	if err != nil {
		t.Fatalf("dry-run: %v out %q", err, out)
	}
	if !strings.Contains(out, "2") {
		t.Fatalf("dry-run should print what it would emit (count 2), got %q", out)
	}
	after := backfillCountStatements(t, dsn)
	if after != before {
		t.Fatalf("dry-run should write nothing; before %d after %d", before, after)
	}
	if after != 0 {
		t.Fatalf("table unchanged after dry-run, expected 0 got %d", after)
	}
	// Ensure dry-run with json also writes nothing but emits array
	flagJSON = true
	t.Cleanup(func() { flagJSON = false })
	outJSON, _, err := runBackfillViaRoot(t, []string{"--dry-run", "--json"})
	if err != nil {
		t.Fatalf("dry-run json: %v out %q", err, outJSON)
	}
	after2 := backfillCountStatements(t, dsn)
	if after2 != 0 {
		t.Fatalf("dry-run json should still write nothing, got %d", after2)
	}
	var arr []beads.Statement
	if err := json.Unmarshal([]byte(outJSON), &arr); err != nil {
		t.Fatalf("dry-run json should emit array, unmarshal failed: %v out %q", err, outJSON)
	}
	if len(arr) != 2 {
		t.Fatalf("dry-run json should emit 2 rows, got %d out %q", len(arr), outJSON)
	}
	// Reset for other tests
	flagJSON = false
}

// A comment matching only the [coordinator] tag (any case, leading or not) mints
// no candidate; a coordinator-tagged comment that also carries a shouted marker
// still mints exactly one.
func TestBackfill_CoordinatorTagOnlyMintsNothing(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "coordinator only")
	addBackfillComment(t, st, iss.ID, "[Coordinator] upper C")
	addBackfillComment(t, st, iss.ID, "[COORDINATOR] all caps")
	addBackfillComment(t, st, iss.ID, "[coordinator] lower")
	addBackfillComment(t, st, iss.ID, "hello [coordinator] not leading")
	// A coordinator-tagged comment that also carries a real marker still mints.
	cMarked := addBackfillComment(t, st, iss.ID, "[coordinator] OWNER RULING keep this")
	_ = st.Close()
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 1 {
		t.Fatalf("expected exactly 1 candidate (only the shouted-marker comment), got %d %+v", len(list), list)
	}
	if list[0].SourceCommentID == nil || *list[0].SourceCommentID != cMarked.ID {
		t.Fatalf("the sole candidate should come from the OWNER RULING comment %s, got %+v", cMarked.ID, list[0])
	}
}

// --dry-run does not count coordinator-only comments; only the shouted marker counts.
func TestBackfill_DryRun_CoordinatorOnlyNotCounted(t *testing.T) {
	dsn, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "dryrun coordinator")
	addBackfillComment(t, st, iss.ID, "[coordinator] just a reader tag")
	addBackfillComment(t, st, iss.ID, "[coordinator] another tag only")
	addBackfillComment(t, st, iss.ID, "OWNER RULING the real one")
	_ = st.Close()

	out, _, err := runBackfillViaRoot(t, []string{"--dry-run"})
	if err != nil {
		t.Fatalf("dry-run: %v out %q", err, out)
	}
	if !strings.Contains(out, "would create 1 candidates") {
		t.Fatalf("dry-run should count only the shouted marker (1), got %q", out)
	}
	if strings.Contains(out, "reader tag") || strings.Contains(out, "another tag only") {
		t.Fatalf("dry-run should not list coordinator-only comments, got %q", out)
	}
	if backfillCountStatements(t, dsn) != 0 {
		t.Fatalf("dry-run must write nothing")
	}
}

// Ensure command appears under bd statements --help and go build succeeds is implicit.
// Test help output contains backfill.
func TestBackfill_HelpAppears(t *testing.T) {
	root := newRoot()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"statements", "--help"})
	_ = root.Execute()
	out := buf.String()
	if !strings.Contains(out, "backfill") {
		t.Fatalf("bd statements --help should contain backfill, got %q", out)
	}
	// Also `bd statements backfill --help` should show dry-run
	root2 := newRoot()
	buf2 := &bytes.Buffer{}
	root2.SetOut(buf2)
	root2.SetArgs([]string{"statements", "backfill", "--help"})
	_ = root2.Execute()
	out2 := buf2.String()
	if !strings.Contains(out2, "dry-run") {
		t.Fatalf("bd statements backfill --help should contain dry-run, got %q", out2)
	}
}

// The [coordinator] tag is a reader filter, not an authority marker: it mints
// nothing on its own, but a shouted marker alongside it still matches.
func TestIsBackfillCandidate_CoordinatorTagNotAMarker(t *testing.T) {
	if isBackfillCandidate("[coordinator] text") {
		t.Fatalf("coordinator-only should not match")
	}
	if isBackfillCandidate("[Coordinator] text") {
		t.Fatalf("coordinator-only (any case) should not match")
	}
	if !isBackfillCandidate("[coordinator] OWNER RULING keep") {
		t.Fatalf("coordinator tag with a shouted marker should still match")
	}
	if !isBackfillCandidate("OWNER RULING plain") {
		t.Fatalf("shouted marker should match")
	}
	if isBackfillCandidate("the ruling was unclear") {
		t.Fatalf("should not over-match")
	}
	if isBackfillCandidate("ruled out") {
		t.Fatalf("should not over-match ruled out")
	}
}

// Verify filed_by uses resolveActor (not re-reading BD_ACTOR directly) — ensure candidate filed_by contains actor.
func TestBackfill_FiledByUsesResolveActor(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	iss := mkBackfillIssue(t, st, "filedby")
	addBackfillComment(t, st, iss.ID, "OWNER RULING filedby")
	_ = st.Close()
	t.Setenv("BD_ACTOR", "coordinator")
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if !strings.Contains(list[0].FiledBy, "coordinator") {
		t.Fatalf("filed_by should contain coordinator via resolveActor, got %q", list[0].FiledBy)
	}
	// Unset should be owner
	_, st2 := newTempBackfillStore(t, "bd2")
	iss2 := mkBackfillIssue(t, st2, "filedby2")
	addBackfillComment(t, st2, iss2.ID, "DEFERRED filedby2")
	_ = st2.Close()
	os.Unsetenv("BD_ACTOR")
	t.Cleanup(func() { os.Unsetenv("BD_ACTOR") })
	// Ensure unset
	if _, ok := os.LookupEnv("BD_ACTOR"); ok {
		t.Fatalf("BD_ACTOR should be unset")
	}
	previewOut, _, err = runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("backfill unset: %v", err)
	}
	list2 := backfillPreview(t, previewOut)
	if len(list2) != 1 {
		t.Fatalf("expected 1, got %d", len(list2))
	}
	if !strings.Contains(list2[0].FiledBy, "owner") {
		t.Fatalf("filed_by should contain owner when BD_ACTOR unset, got %q", list2[0].FiledBy)
	}
}

func TestBackfill_EmptyDB_NoError(t *testing.T) {
	_, st := newTempBackfillStore(t, "bd")
	// No issues, no comments
	_ = st.Close()
	previewOut, _, err := runBackfillViaRoot(t, backfillPreviewArgs)
	if err != nil {
		t.Fatalf("empty db backfill should not error, got %v out %q", err, previewOut)
	}
	list := backfillPreview(t, previewOut)
	if len(list) != 0 {
		t.Fatalf("empty db should have 0 candidates, got %d", len(list))
	}
}

// Helper to avoid import error for time.
var _ = time.Now

// TestBackfill_WriteRefused: rulings are legacy, so backfill without --dry-run
// is refused for every actor and writes no ruling row.
func TestBackfill_WriteRefused(t *testing.T) {
	for _, actor := range []string{"", "coordinator"} {
		t.Run("actor="+actor, func(t *testing.T) {
			dsn, st := newTempBackfillStore(t, "bd")
			iss := mkBackfillIssue(t, st, "write refused")
			addBackfillComment(t, st, iss.ID, "OWNER RULING: would have been a candidate")
			_ = st.Close()
			if actor != "" {
				t.Setenv("BD_ACTOR", actor)
			}
			for _, args := range [][]string{{}, {"--json"}} {
				_, _, err := runBackfillViaRoot(t, args)
				if err != errRulingsClosed {
					t.Fatalf("backfill %v = %v, want errRulingsClosed", args, err)
				}
			}
			if n := backfillCountStatements(t, dsn); n != 0 {
				t.Fatalf("a refused backfill wrote %d statements", n)
			}
		})
	}
}
