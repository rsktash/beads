package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// newTempAuthorityStore opens a fresh store and points the CLI's --db at it.
// HOME is redirected at an empty directory so no test can reach the machine's
// real transcripts.
func newTempAuthorityStore(t *testing.T) *store.Store {
	t.Helper()
	isolateExpandState(t)
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "authority.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, "bd"); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	oldDB, oldJSON := flagDB, flagJSON
	flagDB, flagJSON = filepath.Join(dir, "authority.db"), false
	t.Cleanup(func() {
		flagDB, flagJSON = oldDB, oldJSON
		_ = st.Close()
	})
	t.Setenv("HOME", t.TempDir())
	return st
}

func runAuthority(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	savedDB, savedJSON := flagDB, flagJSON
	root := newRoot()
	flagDB, flagJSON = savedDB, savedJSON
	outBuf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(outBuf)
	root.SetErr(errBuf)
	root.SetArgs(args)
	err := root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func mkAuthIssue(t *testing.T, st *store.Store, title, desc string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1, Description: desc}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	return i
}

// authStatement writes one statement straight through the store so a fixture
// can set an exact (kind, issue, topic, areas, author) tuple.
type authStatement struct {
	kind      string
	issueID   string
	text      string
	filedBy   string
	topic     string
	workspace string
	concern   string
	law       string
	status    string
	createdAt time.Time
}

func mkAuthStatement(t *testing.T, st *store.Store, s authStatement) *beads.Statement {
	t.Helper()
	if s.filedBy == "" {
		s.filedBy = "owner:tester"
	}
	if s.status == "" {
		s.status = "active"
	}
	row := &beads.Statement{
		Kind: s.kind, Text: s.text, FiledBy: s.filedBy, Status: s.status, Scope: "inherit",
		Topic: s.topic, Workspace: s.workspace, Concern: s.concern, Law: s.law,
		CreatedAt: s.createdAt,
	}
	if s.issueID != "" {
		id := s.issueID
		row.IssueID = &id
	}
	if err := st.CreateStatement(context.Background(), row); err != nil {
		t.Fatalf("create %s: %v", s.kind, err)
	}
	return row
}

// briefSection returns the lines of one labelled section, the label line
// first, with the label column stripped.
func briefSection(out, label string) []string {
	var lines []string
	inSection := false
	for _, l := range strings.Split(out, "\n") {
		if l == "" {
			continue
		}
		switch {
		case strings.HasPrefix(l, label+" "):
			inSection = true
			lines = append(lines, strings.TrimSpace(l[len(label):]))
		case strings.HasPrefix(l, " "):
			if inSection {
				lines = append(lines, strings.TrimSpace(l))
			}
		default:
			inSection = false
		}
	}
	return lines
}

func requireNoErr(t *testing.T, err error, errOut string) {
	t.Helper()
	if err != nil {
		t.Fatalf("authority failed: %v (stderr %q)", err, errOut)
	}
}

// --- section order ---

func TestAuthority_SectionOrder(t *testing.T) {
	st := newTempAuthorityStore(t)
	epic := mkAuthIssue(t, st, "the epic", "")
	bead := mkAuthIssue(t, st, "serialize seq draws", "## Files\n- server/src/auth.ts\n")
	if err := st.AddDependency(context.Background(), beads.Dependency{
		IssueID: bead.ID, DependsOnID: epic.ID, Type: beads.DepParentChild,
	}); err != nil {
		t.Fatalf("dep: %v", err)
	}
	mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, text: "the ruling", topic: "seq-draws", workspace: "server", concern: "authority"})
	mkAuthStatement(t, st, authStatement{kind: "question", issueID: bead.ID, text: "the question", topic: "seq-draws", workspace: "server", concern: "authority"})
	mkAuthStatement(t, st, authStatement{kind: "finding", issueID: bead.ID, text: "the finding", filedBy: "executor:agent", topic: "seq-draws", workspace: "server", concern: "authority"})
	mkAuthStatement(t, st, authStatement{kind: "ruling", text: "a project law", topic: "doctrine-slug", concern: "authority", law: "Admin permissions are a platform business row."})

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)

	want := []string{"BRIEF", "RULINGS", "TOPIC", "QUESTIONS", "FINDINGS", "DOCTRINE", "RELATED"}
	at := -1
	for _, label := range want {
		idx := strings.Index(out, "\n"+label+" ")
		if strings.HasPrefix(out, label+" ") {
			idx = 0
		}
		if idx < 0 {
			t.Fatalf("section %s missing from brief:\n%s", label, out)
		}
		if idx <= at {
			t.Fatalf("section %s out of order at %d (previous %d):\n%s", label, idx, at, out)
		}
		at = idx
	}
}

// --- the byte cap ---

func TestAuthority_CapsAt6000Bytes(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "capped bead", "## Files\n- server/src/auth.ts\n")
	for i := 0; i < 200; i++ {
		mkAuthStatement(t, st, authStatement{
			kind: "ruling", issueID: bead.ID, topic: "capped-topic",
			text: fmt.Sprintf("ruling %03d ", i) + strings.Repeat("x", 400),
		})
	}

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)
	marker := "\n[truncated, +" // the capWriter's marker prefix
	if len(out) > 6000+len(marker)+32 {
		t.Fatalf("brief is %d bytes, over the 6000-byte cap plus its marker", len(out))
	}

	// The same fixture with a cap the sections cannot stay under: the marker
	// must be the last line, and nothing may follow it.
	out, errOut, err = runAuthority(t, "authority", bead.ID, "--max-bytes", "800")
	requireNoErr(t, err, errOut)
	if !strings.Contains(out, marker) {
		t.Fatalf("no truncation marker under --max-bytes 800:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "[truncated, +") || !strings.HasSuffix(last, " more bytes]") {
		t.Fatalf("last line is not the truncation marker: %q", last)
	}
	if len(out) > 800+len(last)+2 {
		t.Fatalf("brief is %d bytes, over --max-bytes 800 plus the marker line", len(out))
	}
}

// --- section caps ---

func TestAuthority_RulingsCappedAtTwelveWithMore(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "many rulings", "## Files\n- server/src/auth.ts\n")
	for i := 0; i < 30; i++ {
		mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "many-rulings", text: fmt.Sprintf("ruling number %d", i)})
	}

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)

	lines := briefSection(out, "RULINGS")
	if len(lines) != 13 {
		t.Fatalf("want 12 ruling lines plus the more line, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	want := "(18 more — bd rulings " + bead.ID + ")"
	if lines[12] != want {
		t.Fatalf("last RULINGS line = %q, want %q", lines[12], want)
	}
}

func TestAuthority_TopicCappedAtFive(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "many topics", "## Files\n- server/src/auth.ts\n")
	for i := 0; i < 9; i++ {
		mkAuthStatement(t, st, authStatement{
			kind: "question", issueID: bead.ID, text: fmt.Sprintf("question %d", i),
			topic: fmt.Sprintf("slug-%d", i), workspace: "server", concern: "authority",
		})
	}

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)

	lines := briefSection(out, "TOPIC")
	if len(lines) != 6 {
		t.Fatalf("want 5 topic lines plus the more line, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[5], "(4 more — bd topics --concern authority)") {
		t.Fatalf("last TOPIC line = %q", lines[5])
	}
}

// --- structure, not wording ---

func TestAuthority_TopicFilledByStructureNotWords(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "serialize seq draws", "## Files\n- server/src/auth.ts\n")
	other := mkAuthIssue(t, st, "unrelated bead", "")
	// The slug shares no word with the bead's title, and its workspace is not
	// the bead's either: the concern is the only thing that can match it.
	mkAuthStatement(t, st, authStatement{
		kind: "question", issueID: other.ID, text: "does a candidate keep its business row?",
		topic: "candidate-account", workspace: "web-app", concern: "authority",
	})

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)

	lines := strings.Join(briefSection(out, "TOPIC"), "\n")
	if !strings.Contains(lines, "candidate-account") {
		t.Fatalf("the concern-matched topic is missing from TOPIC:\n%s", out)
	}
	for _, word := range strings.Fields("serialize seq draws") {
		if strings.Contains("candidate-account", word) {
			t.Fatalf("fixture is not word-independent: %q appears in the slug", word)
		}
	}
}

// --- the words form ---

func TestAuthority_WordsWithoutConcernRefuses(t *testing.T) {
	newTempAuthorityStore(t)

	out, errOut, err := runAuthority(t, "authority", "some words")
	if err == nil {
		t.Fatalf("a words query without --concern must refuse; got:\n%s", out)
	}
	for _, name := range []string{"authority", "sync", "tokens", "schema", "catalog", "frontend"} {
		if !strings.Contains(errOut, name) && !strings.Contains(err.Error(), name) {
			t.Fatalf("concern %q missing from the refusal: stderr %q err %v", name, errOut, err)
		}
	}
	if strings.Contains(errOut, "\n  all\n") {
		t.Fatalf("the all concern narrows nothing and must not be offered:\n%s", errOut)
	}
}

func TestAuthority_WordsWithConcernRuns(t *testing.T) {
	st := newTempAuthorityStore(t)
	mkAuthStatement(t, st, authStatement{kind: "ruling", text: "a law over authority", topic: "auth-topic", concern: "authority"})

	out, errOut, err := runAuthority(t, "authority", "candidate account", "--concern", "authority")
	requireNoErr(t, err, errOut)
	if !strings.Contains(out, "concern authority") {
		t.Fatalf("BRIEF line does not name the concern:\n%s", out)
	}
	if !strings.Contains(out, "a law over authority") {
		t.Fatalf("the concern's ruling is missing:\n%s", out)
	}
}

// --- inheritance ---

func TestAuthority_InheritedRulingsPresent(t *testing.T) {
	st := newTempAuthorityStore(t)
	epic := mkAuthIssue(t, st, "the epic", "")
	bead := mkAuthIssue(t, st, "the child", "## Files\n- server/src/auth.ts\n")
	if err := st.AddDependency(context.Background(), beads.Dependency{
		IssueID: bead.ID, DependsOnID: epic.ID, Type: beads.DepParentChild,
	}); err != nil {
		t.Fatalf("dep: %v", err)
	}
	mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: epic.ID, text: "the epic's law", topic: "epic-law"})

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)

	lines := strings.Join(briefSection(out, "RULINGS"), "\n")
	if !strings.Contains(lines, "the epic's law") {
		t.Fatalf("the inherited ruling is missing:\n%s", out)
	}
	if !strings.Contains(lines, "["+epic.ID+"]") {
		t.Fatalf("the inherited ruling carries no origin bracket:\n%s", lines)
	}
}

// --- doctrine ---

func TestAuthority_DoctrineOutsideAreasIsACount(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "an authority bead", "## Files\n- server/src/auth.ts\n")
	mkAuthStatement(t, st, authStatement{kind: "ruling", text: "the law that binds", topic: "in-area", concern: "authority", law: "Admin permissions are a platform business row."})
	for i := 0; i < 3; i++ {
		mkAuthStatement(t, st, authStatement{kind: "ruling", text: fmt.Sprintf("outside law %d", i), topic: fmt.Sprintf("out-%d", i), concern: "catalog"})
	}

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)

	lines := briefSection(out, "DOCTRINE")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Admin permissions are a platform business row.") {
		t.Fatalf("the in-area law is missing or is not rendered from its law column:\n%s", joined)
	}
	if !strings.Contains(lines[0], "authority") {
		t.Fatalf("the DOCTRINE line does not carry the concern: %q", lines[0])
	}
	if !strings.Contains(joined, "(3 more doctrine laws outside this bead's areas)") {
		t.Fatalf("the outside-areas count is missing:\n%s", joined)
	}
	for i := 0; i < 3; i++ {
		if strings.Contains(joined, fmt.Sprintf("outside law %d", i)) {
			t.Fatalf("an outside law was printed as a line, not counted:\n%s", joined)
		}
	}
}

func TestAuthority_DoctrineFallsBackToTheRulingHeadline(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "an authority bead", "## Files\n- server/src/auth.ts\n")
	// Nothing writes the law column yet; the headline must stand in for it.
	mkAuthStatement(t, st, authStatement{kind: "ruling", text: "a law with no law column", topic: "no-law", concern: "authority"})

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)
	if !strings.Contains(strings.Join(briefSection(out, "DOCTRINE"), "\n"), "a law with no law column") {
		t.Fatalf("an empty law column did not fall back to the ruling headline:\n%s", out)
	}
}

// --- related ---

func TestAuthority_RelatedNeverReadsTranscript(t *testing.T) {
	st := newTempAuthorityStore(t) // HOME is already an empty directory
	bead := mkAuthIssue(t, st, "serialize seq draws", "## Files\n- server/src/auth.ts\n")
	linked := mkAuthIssue(t, st, "floor lock", "")
	if err := st.AddDependency(context.Background(), beads.Dependency{
		IssueID: bead.ID, DependsOnID: linked.ID, Type: beads.DepBlocks,
	}); err != nil {
		t.Fatalf("dep: %v", err)
	}

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)

	lines := strings.Join(briefSection(out, "RELATED"), "\n")
	if !strings.Contains(lines, linked.ID) {
		t.Fatalf("the linked bead is missing from RELATED:\n%s", lines)
	}
	if !strings.Contains(lines, "transcript: ") || !strings.Contains(lines, "rg -n ") {
		t.Fatalf("RELATED prints no transcript command:\n%s", lines)
	}
	if !strings.Contains(lines, "transcript: 0 ") {
		t.Fatalf("an empty HOME must report no sessions:\n%s", lines)
	}
}

func TestAuthority_RelatedDepthCapsHops(t *testing.T) {
	st := newTempAuthorityStore(t)
	a := mkAuthIssue(t, st, "hop zero", "## Files\n- server/src/auth.ts\n")
	b := mkAuthIssue(t, st, "hop one", "")
	c := mkAuthIssue(t, st, "hop two", "")
	for _, edge := range [][2]string{{a.ID, b.ID}, {b.ID, c.ID}} {
		if err := st.AddDependency(context.Background(), beads.Dependency{
			IssueID: edge[0], DependsOnID: edge[1], Type: beads.DepBlocks,
		}); err != nil {
			t.Fatalf("dep: %v", err)
		}
	}

	out, errOut, err := runAuthority(t, "authority", a.ID)
	requireNoErr(t, err, errOut)
	lines := strings.Join(briefSection(out, "RELATED"), "\n")
	if !strings.Contains(lines, b.ID) || strings.Contains(lines, c.ID) {
		t.Fatalf("default depth 1 must reach %s and stop before %s:\n%s", b.ID, c.ID, lines)
	}

	out, errOut, err = runAuthority(t, "authority", a.ID, "--depth", "2")
	requireNoErr(t, err, errOut)
	if !strings.Contains(strings.Join(briefSection(out, "RELATED"), "\n"), c.ID) {
		t.Fatalf("--depth 2 must reach %s:\n%s", c.ID, out)
	}

	if _, _, err := runAuthority(t, "authority", a.ID, "--depth", "4"); err == nil {
		t.Fatal("--depth 4 must be refused: the maximum is 3")
	}
}

// --- expand ---

func TestAuthority_ExpandOpensRecord(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "expandable", "## Files\n- server/src/auth.ts\n")
	// The first line alone overruns the 120-rune headline, so the second one
	// can only reach the reader through --expand.
	long := "the first line of the ruling " + strings.Repeat("y", 150) + "\nand a second line only --expand shows"
	r := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "expand-me", text: long})

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)
	if strings.Contains(out, "and a second line only --expand shows") {
		t.Fatalf("the headline already leaked the full text:\n%s", out)
	}

	out, errOut, err = runAuthority(t, "authority", bead.ID, "--expand", r.ID)
	requireNoErr(t, err, errOut)
	if !strings.Contains(out, "and a second line only --expand shows") {
		t.Fatalf("--expand did not open the record:\n%s", out)
	}
	if !strings.Contains(out, "source: ") {
		t.Fatalf("--expand printed no source line:\n%s", out)
	}
}

// --- since handoff ---

func TestAuthority_SinceHandoffWithoutPlanErrors(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "no plan", "## Files\n- server/src/auth.ts\n")

	_, _, err := runAuthority(t, "authority", bead.ID, "--since", "handoff")
	if err == nil {
		t.Fatal("--since handoff with no plan must error, not fall back to full history")
	}
	if !strings.Contains(err.Error(), "--since handoff") {
		t.Fatalf("the error does not name the flag: %v", err)
	}
}

func TestAuthority_SinceHandoffResolvesTheLanesNewestHandoff(t *testing.T) {
	st := newTempAuthorityStore(t)
	ctx := context.Background()
	bead := mkAuthIssue(t, st, "in a lane", "## Files\n- server/src/auth.ts\n")

	old := time.Now().UTC().Add(-72 * time.Hour)
	oldRow := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "before", text: "filed before the handoff", createdAt: old})
	setStatementChangedAt(t, st, oldRow.ID, &old)

	if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: "plan-1", Title: "a plan"}); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if err := st.AddLane(ctx, &store.PlanLane{PlanID: "plan-1", Lane: "a", Queue: []string{bead.ID}}); err != nil {
		t.Fatalf("add lane: %v", err)
	}
	if err := st.ClaimLane(ctx, "plan-1", "a", "sess-1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	handoffAt := time.Now().UTC().Add(-24 * time.Hour)
	if err := st.Handoff(ctx, &store.PlanHandoff{
		ID: "h-1", PlanID: "plan-1", Lane: "a", SessionID: "sess-1", CreatedAt: handoffAt,
	}, 0); err != nil {
		t.Fatalf("handoff: %v", err)
	}
	mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "after", text: "filed after the handoff"})

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--since", "handoff")
	requireNoErr(t, err, errOut)
	if !strings.Contains(out, "filed after the handoff") {
		t.Fatalf("the ruling filed after the handoff is missing:\n%s", out)
	}
	if strings.Contains(out, "filed before the handoff") {
		t.Fatalf("--since handoff did not drop the older ruling:\n%s", out)
	}
}

// --- json ---

func TestAuthority_JSONIsUncapped(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "capped bead", "## Files\n- server/src/auth.ts\n")
	for i := 0; i < 200; i++ {
		mkAuthStatement(t, st, authStatement{
			kind: "ruling", issueID: bead.ID, topic: "capped-topic",
			text: fmt.Sprintf("ruling %03d ", i) + strings.Repeat("x", 400),
		})
	}

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--json")
	requireNoErr(t, err, errOut)

	var res struct {
		Rulings []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"rulings"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("brief json: %v", err)
	}
	if len(res.Rulings) != 200 {
		t.Fatalf("want 200 rulings in --json, got %d", len(res.Rulings))
	}
	if !strings.Contains(res.Rulings[0].Text, strings.Repeat("x", 400)) {
		t.Fatal("--json truncated a ruling's text")
	}
}

// --- filters ---

func TestAuthority_GrepFiltersBuiltSections(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "grep me", "## Files\n- server/src/auth.ts\n")
	mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "keep-me", text: "the keyword ruling"})
	mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "drop-me", text: "an unrelated ruling"})

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--grep", "keyword")
	requireNoErr(t, err, errOut)
	lines := strings.Join(briefSection(out, "RULINGS"), "\n")
	if !strings.Contains(lines, "the keyword ruling") || strings.Contains(lines, "an unrelated ruling") {
		t.Fatalf("--grep did not filter the built section:\n%s", lines)
	}
}

func TestAuthority_KindNarrowsToOneSection(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "narrow me", "## Files\n- server/src/auth.ts\n")
	mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "narrow", text: "a ruling"})
	mkAuthStatement(t, st, authStatement{kind: "question", issueID: bead.ID, topic: "narrow", text: "a question"})

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings")
	requireNoErr(t, err, errOut)
	if !strings.Contains(out, "RULINGS") {
		t.Fatalf("--kind rulings dropped the RULINGS section:\n%s", out)
	}
	for _, gone := range []string{"QUESTIONS", "TOPIC", "RELATED"} {
		if strings.Contains(out, gone+" ") {
			t.Fatalf("--kind rulings still printed %s:\n%s", gone, out)
		}
	}

	if _, _, err := runAuthority(t, "authority", bead.ID, "--kind", "nonsense"); err == nil {
		t.Fatal("an unknown --kind must be refused")
	}
}

func TestAuthority_AuthorFilterSplitsOwnerFromAgent(t *testing.T) {
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "authored", "## Files\n- server/src/auth.ts\n")
	mkAuthStatement(t, st, authStatement{kind: "finding", issueID: bead.ID, topic: "authored", text: "the agent's finding", filedBy: "executor:agent"})
	mkAuthStatement(t, st, authStatement{kind: "finding", issueID: bead.ID, topic: "authored", text: "the owner's finding", filedBy: "owner:rustam"})

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--author", "agent")
	requireNoErr(t, err, errOut)
	lines := strings.Join(briefSection(out, "FINDINGS"), "\n")
	if !strings.Contains(lines, "the agent's finding") || strings.Contains(lines, "the owner's finding") {
		t.Fatalf("--author agent did not split the section:\n%s", lines)
	}
}
