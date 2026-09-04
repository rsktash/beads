package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// newTempDoctrineStore opens a fresh store, points the CLI's --db at it and
// clears BD_ACTOR so `bd ruling add` runs as the owner unless a test says
// otherwise.
func newTempDoctrineStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "doctrine.db")
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(ctx, store.CfgIssuePrefix, "bd"); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	oldDB, oldJSON := flagDB, flagJSON
	flagDB, flagJSON = dsn, false
	t.Cleanup(func() {
		flagDB, flagJSON = oldDB, oldJSON
		_ = st.Close()
	})
	t.Setenv("HOME", t.TempDir())
	unsetBDActor(t)
	return st
}

// unsetBDActor makes the test run as the owner, whatever BD_ACTOR the shell
// running `go test` carries. The t.Setenv call is what registers the restore:
// it snapshots the original value and marks the test non-parallel, and the
// Unsetenv after it is the state the test actually wants.
func unsetBDActor(t *testing.T) {
	t.Helper()
	t.Setenv("BD_ACTOR", "coordinator")
	if err := os.Unsetenv("BD_ACTOR"); err != nil {
		t.Fatalf("unset BD_ACTOR: %v", err)
	}
}

func runDoctrine(t *testing.T, args ...string) (string, string, error) {
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

func mkDoctrineIssue(t *testing.T, st *store.Store, title string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return i
}

// addLaw files one doctrine through the CLI and returns its ruling id.
func addLaw(t *testing.T, text, concern, law, topic string, extra ...string) string {
	t.Helper()
	args := []string{"ruling", "add", text, "--doctrine", "--concern", concern, "--law", law, "--topic", topic}
	args = append(args, extra...)
	out, errOut, err := runDoctrine(t, args...)
	if err != nil {
		t.Fatalf("ruling add %q failed: %v (stderr %q)", law, err, errOut)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		t.Fatalf("ruling add printed no id (stderr %q)", errOut)
	}
	return id
}

// showDoctrine renders one bead's contract straight through the renderer.
// `bd show` writes to os.Stdout rather than the command's writer, so the
// renderer is the honest seam for asserting on what a reader sees.
func showDoctrine(t *testing.T, st *store.Store, issueID string, expand ...string) string {
	t.Helper()
	opts := showOpts{expand: map[string]bool{}}
	for _, id := range expand {
		opts.expand[id] = true
	}
	cc := &cmdCtx{ctx: context.Background(), store: st}
	var buf bytes.Buffer
	if err := printShowHuman(&buf, cc, issueID, opts); err != nil {
		t.Fatalf("printShowHuman %s: %v", issueID, err)
	}
	return buf.String()
}

func countAllStatements(t *testing.T, st *store.Store) int {
	t.Helper()
	list, err := st.ListStatements(context.Background(), store.StatementFilter{})
	if err != nil {
		t.Fatalf("list statements: %v", err)
	}
	return len(list)
}

// --- Behaviour 2: --doctrine requires --law and a project scope ---

func TestDoctrine_RequiresLawAndProjectScope(t *testing.T) {
	st := newTempDoctrineStore(t)
	bead := mkDoctrineIssue(t, st, "a bead")

	_, _, err := runDoctrine(t, "ruling", "add", bead.ID, "the text", "--doctrine",
		"--concern", "authority", "--law", "Admin rides the grant path.", "--topic", "admin-authority")
	if err == nil {
		t.Fatalf("--doctrine with an issue id must be refused")
	}
	if !strings.Contains(err.Error(), "takes no issue id") {
		t.Fatalf("the refusal must name the issue id as the problem, got %q", err)
	}

	_, _, err = runDoctrine(t, "ruling", "add", "the text", "--doctrine",
		"--concern", "authority", "--topic", "admin-authority")
	if err == nil {
		t.Fatalf("--doctrine with no --law must be refused")
	}
	if !strings.Contains(err.Error(), "--law") {
		t.Fatalf("the refusal must name --law, got %q", err)
	}

	if n := countAllStatements(t, st); n != 0 {
		t.Fatalf("a refused doctrine must write nothing, got %d statements", n)
	}
}

func TestDoctrine_LawCappedAt200(t *testing.T) {
	newTempDoctrineStore(t)
	long := strings.Repeat("a", 200) + "."
	_, _, err := runDoctrine(t, "ruling", "add", "the text", "--doctrine",
		"--concern", "authority", "--law", long, "--topic", "admin-authority")
	if err == nil {
		t.Fatalf("a 201-character law must be refused")
	}
	if !strings.Contains(err.Error(), "201") || !strings.Contains(err.Error(), "200") {
		t.Fatalf("the refusal must name the length and the limit, got %q", err)
	}

	ok := strings.Repeat("a", 199) + "."
	if _, _, err := runDoctrine(t, "ruling", "add", "the text", "--doctrine",
		"--concern", "authority", "--law", ok, "--topic", "admin-authority"); err != nil {
		t.Fatalf("a 200-character law is at the cap, not over it: %v", err)
	}
}

func TestDoctrine_LawNeedsFullStop(t *testing.T) {
	newTempDoctrineStore(t)
	_, _, err := runDoctrine(t, "ruling", "add", "the text", "--doctrine",
		"--concern", "authority", "--law", "Admin rides the grant path", "--topic", "admin-authority")
	if err == nil {
		t.Fatalf("an unpunctuated law must be refused")
	}
	if !strings.Contains(err.Error(), "full stop") {
		t.Fatalf("the refusal must name the full stop, got %q", err)
	}
}

func TestDoctrine_RationaleCappedAt600(t *testing.T) {
	newTempDoctrineStore(t)
	_, _, err := runDoctrine(t, "ruling", "add", "the text", "--doctrine",
		"--concern", "authority", "--law", "Admin rides the grant path.",
		"--rationale", strings.Repeat("b", 601), "--topic", "admin-authority")
	if err == nil {
		t.Fatalf("a 601-character rationale must be refused")
	}
	if !strings.Contains(err.Error(), "601") || !strings.Contains(err.Error(), "600") {
		t.Fatalf("the refusal must name the length and the limit, got %q", err)
	}

	if _, _, err := runDoctrine(t, "ruling", "add", "the text", "--doctrine",
		"--concern", "authority", "--law", "Admin rides the grant path.",
		"--rationale", strings.Repeat("b", 600), "--topic", "admin-authority"); err != nil {
		t.Fatalf("a 600-character rationale is at the cap, not over it: %v", err)
	}
}

// --- Behaviour 4: the area vocabulary is closed ---

func TestDoctrine_UnknownConcernRefused(t *testing.T) {
	newTempDoctrineStore(t)
	_, _, err := runDoctrine(t, "ruling", "add", "the text", "--doctrine",
		"--concern", "authorty", "--law", "Admin rides the grant path.", "--topic", "admin-authority")
	if err == nil {
		t.Fatalf("an unknown concern must be refused")
	}
	msg := err.Error()
	for _, name := range []string{"authority", "sync", "tokens", "schema", "catalog", "frontend", "all"} {
		if !strings.Contains(msg, name) {
			t.Fatalf("the refusal must list the vocabulary; %q is missing from %q", name, msg)
		}
	}

	if _, _, err := runDoctrine(t, "doctrine", "list", "--concern", "authorty"); err == nil {
		t.Fatalf("bd doctrine list must refuse an unknown concern too")
	}
	if _, _, err := runDoctrine(t, "doctrine", "list", "--workspace", "servr"); err == nil {
		t.Fatalf("bd doctrine list must refuse an unknown workspace too")
	}
}

// --- Behaviour 5: the same-area laws print before the write ---

func TestDoctrine_PrintsSameAreaDoctrinesBeforeWrite(t *testing.T) {
	newTempDoctrineStore(t)
	existing := addLaw(t, "the first ruling text", "authority",
		"Admin permissions are a platform business row plus a CM grant.", "admin-authority")
	addLaw(t, "an unrelated ruling text", "catalog",
		"Catalog routes by MXIK code, never by name.", "catalog-routing")

	out, errOut, err := runDoctrine(t, "ruling", "add", "a second authority text", "--doctrine",
		"--concern", "authority", "--law", "Grants expire.", "--topic", "admin-authority")
	if err != nil {
		t.Fatalf("the write must succeed: %v (stderr %q)", err, errOut)
	}
	if !strings.Contains(errOut, "Admin permissions are a platform business row plus a CM grant.") {
		t.Fatalf("stderr must hold the existing law, got %q", errOut)
	}
	if !strings.Contains(errOut, existing) {
		t.Fatalf("stderr must cite the existing ruling id %s, got %q", existing, errOut)
	}
	if strings.Contains(errOut, "Catalog routes by MXIK code") {
		t.Fatalf("stderr must hold same-area laws only, got %q", errOut)
	}
	if strings.Contains(out, "Admin permissions") || strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("stdout must hold only the new id, got %q", out)
	}
}

// --- bd doctrine list ---

func TestDoctrine_ListPrintsIDAreaAndLaw(t *testing.T) {
	newTempDoctrineStore(t)
	id := addLaw(t, "the ruling text", "authority",
		"Admin permissions are a platform business row plus a CM grant.", "admin-authority")
	addLaw(t, "another ruling text", "catalog",
		"Catalog routes by MXIK code, never by name.", "catalog-routing")

	out, errOut, err := runDoctrine(t, "doctrine", "list")
	if err != nil {
		t.Fatalf("doctrine list failed: %v (stderr %q)", err, errOut)
	}
	want := id + "  authority  Admin permissions are a platform business row plus a CM grant."
	if !strings.Contains(out, want) {
		t.Fatalf("the line must be id, area, law:\nwant %q\ngot\n%s", want, out)
	}

	out, _, err = runDoctrine(t, "doctrine", "list", "--concern", "catalog")
	if err != nil {
		t.Fatalf("filtered list failed: %v", err)
	}
	if strings.Contains(out, "Admin permissions") || !strings.Contains(out, "Catalog routes by MXIK code") {
		t.Fatalf("--concern must narrow the listing, got\n%s", out)
	}
}

// --- Behaviour 7: render groups by area in the vocabulary's order ---

func TestDoctrine_RenderGroupsByArea(t *testing.T) {
	newTempDoctrineStore(t)
	addLaw(t, "text one", "catalog", "Catalog routes by MXIK code, never by name.", "catalog-routing")
	addLaw(t, "text two", "authority", "Admin permissions are a platform business row plus a CM grant.", "admin-authority")
	addLaw(t, "text three", "authority", "Grants expire.", "admin-authority",
		"--rationale", "Because a grant without an end date is a permanent one.")

	out, errOut, err := runDoctrine(t, "doctrine", "render")
	if err != nil {
		t.Fatalf("doctrine render failed: %v (stderr %q)", err, errOut)
	}

	var headings []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "## ") {
			headings = append(headings, strings.TrimPrefix(l, "## "))
		}
	}
	if len(headings) != 2 {
		t.Fatalf("three laws in two concerns must produce two headings, got %v", headings)
	}
	// authority is seeded before catalog, so it heads the page.
	if headings[0] != "authority" || headings[1] != "catalog" {
		t.Fatalf("the headings must follow the vocabulary order, got %v", headings)
	}
	if !strings.Contains(out, "**Admin permissions are a platform business row plus a CM grant.**") {
		t.Fatalf("the law must render in bold, got\n%s", out)
	}
	if !strings.Contains(out, "\nBecause a grant without an end date is a permanent one.\n") {
		t.Fatalf("the rationale must render as its own paragraph, got\n%s", out)
	}
	if !strings.Contains(out, "(admin-authority)") {
		t.Fatalf("the attribution must cite the topic, got\n%s", out)
	}

	authorityAt := strings.Index(out, "## authority")
	catalogAt := strings.Index(out, "## catalog")
	grantsAt := strings.Index(out, "**Grants expire.**")
	if !(authorityAt < grantsAt && grantsAt < catalogAt) {
		t.Fatalf("every law must sit under its own heading, got\n%s", out)
	}
}

func TestDoctrine_RenderHasGeneratedBanner(t *testing.T) {
	newTempDoctrineStore(t)
	addLaw(t, "text one", "authority", "Admin permissions are a platform business row plus a CM grant.", "admin-authority")

	out, errOut, err := runDoctrine(t, "doctrine", "render")
	if err != nil {
		t.Fatalf("doctrine render failed: %v (stderr %q)", err, errOut)
	}
	first := strings.SplitN(out, "\n", 2)[0]
	if first != "<!-- generated by `bd doctrine render` — do not hand-edit -->" {
		t.Fatalf("the file must open with the generated banner, got %q", first)
	}
}

// --- Behaviour 9: the law is the headline ---

func TestDoctrine_ShowRendersLawNotText(t *testing.T) {
	st := newTempDoctrineStore(t)
	bead := mkDoctrineIssue(t, st, "a bead")
	id := addLaw(t, "a long ruling text nobody wants in a headline", "authority",
		"Admin permissions are a platform business row plus a CM grant.", "admin-authority",
		"--rationale", "One authorization path, one audit table.",
		"--verbatim", "both approved")

	out := showDoctrine(t, st, bead.ID)
	if !strings.Contains(out, "Admin permissions are a platform business row plus a CM grant.") {
		t.Fatalf("the ACTIVE RULINGS line must carry the law, got\n%s", out)
	}
	if strings.Contains(out, "a long ruling text nobody wants in a headline") {
		t.Fatalf("the ruling text must not render without --expand, got\n%s", out)
	}
	if strings.Contains(out, "One authorization path") || strings.Contains(out, "both approved") {
		t.Fatalf("rationale and verbatim must wait for --expand, got\n%s", out)
	}

	out = showDoctrine(t, st, bead.ID, id)
	for _, want := range []string{
		"a long ruling text nobody wants in a headline",
		"rationale: One authorization path, one audit table.",
		"verbatim: both approved",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("--expand must open %q, got\n%s", want, out)
		}
	}
}

func TestDoctrine_LawlessProjectRulingUnchanged(t *testing.T) {
	st := newTempDoctrineStore(t)
	bead := mkDoctrineIssue(t, st, "a bead")
	if _, _, err := runDoctrine(t, "ruling", "add", "a project ruling with no law at all",
		"--topic", "legacy-slug"); err != nil {
		t.Fatalf("a lawless project ruling must still file: %v", err)
	}

	out := showDoctrine(t, st, bead.ID)
	if !strings.Contains(out, "a project ruling with no law at all") {
		t.Fatalf("a ruling with no law keeps rendering its text, got\n%s", out)
	}
}

// --- Behaviour 10: promote re-scopes in place ---

func TestDoctrine_PromoteRescopesInPlace(t *testing.T) {
	st := newTempDoctrineStore(t)
	bead := mkDoctrineIssue(t, st, "a bead")
	out, errOut, err := runDoctrine(t, "ruling", "add", bead.ID, "a ruling that outgrew its bead", "--topic", "admin-authority")
	if err != nil {
		t.Fatalf("ruling add failed: %v (stderr %q)", err, errOut)
	}
	id := strings.TrimSpace(out)
	before := countAllStatements(t, st)

	if _, errOut, err := runDoctrine(t, "doctrine", "promote", id, "--concern", "authority"); err != nil {
		t.Fatalf("promote failed: %v (stderr %q)", err, errOut)
	}

	if after := countAllStatements(t, st); after != before {
		t.Fatalf("promote must not mint a ruling: %d rows before, %d after", before, after)
	}
	row, err := st.GetStatement(context.Background(), id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	if row.IssueID != nil {
		t.Fatalf("promote must set issue_id NULL, got %v", *row.IssueID)
	}
	if row.Concern != "authority" {
		t.Fatalf("promote must stamp the concern, got %q", row.Concern)
	}
	if row.Text != "a ruling that outgrew its bead" {
		t.Fatalf("promote must not rewrite the ruling, got %q", row.Text)
	}

	listOut, _, err := runDoctrine(t, "doctrine", "list")
	if err != nil {
		t.Fatalf("doctrine list failed: %v", err)
	}
	if !strings.Contains(listOut, id) {
		t.Fatalf("the promoted ruling must be doctrine now, got\n%s", listOut)
	}
}

func TestDoctrine_PromoteRefusesExecutor(t *testing.T) {
	st := newTempDoctrineStore(t)
	bead := mkDoctrineIssue(t, st, "a bead")
	out, _, err := runDoctrine(t, "ruling", "add", bead.ID, "a ruling that outgrew its bead", "--topic", "admin-authority")
	if err != nil {
		t.Fatalf("ruling add failed: %v", err)
	}
	id := strings.TrimSpace(out)

	t.Setenv("BD_ACTOR", "executor")
	_, _, err = runDoctrine(t, "doctrine", "promote", id, "--concern", "authority")
	if err == nil {
		t.Fatalf("an executor must not promote a ruling to doctrine")
	}
	if !strings.Contains(err.Error(), "BD_ACTOR=executor") {
		t.Fatalf("the refusal must name the actor, got %q", err)
	}

	row, err := st.GetStatement(context.Background(), id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	if row.IssueID == nil || *row.IssueID != bead.ID {
		t.Fatalf("a refused promote must not move the ruling")
	}
}
