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
	isolateExpandState(t)
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

// addLaw seeds one doctrine — a project-scoped ruling carrying an area and a
// law — through the store and returns its ruling id. extra holds flag/value
// pairs: --rationale and --verbatim.
func addLaw(t *testing.T, st *store.Store, text, concern, law, topic string, extra ...string) string {
	t.Helper()
	r := &beads.Statement{Kind: "ruling", Text: text, Concern: concern, Law: law, Topic: topic, FiledBy: "owner:tester"}
	for i := 0; i+1 < len(extra); i += 2 {
		switch extra[i] {
		case "--rationale":
			r.Rationale = extra[i+1]
		case "--verbatim":
			r.Verbatim = extra[i+1]
		default:
			t.Fatalf("addLaw: unknown flag %q", extra[i])
		}
	}
	if err := st.CreateStatement(context.Background(), r); err != nil {
		t.Fatalf("seed law %q: %v", law, err)
	}
	return r.ID
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

// --- bd doctrine list ---

func TestDoctrine_ListPrintsIDAreaAndLaw(t *testing.T) {
	st := newTempDoctrineStore(t)
	id := addLaw(t, st, "the ruling text", "authority",
		"Admin permissions are a platform business row plus a CM grant.", "admin-authority")
	addLaw(t, st, "another ruling text", "catalog",
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
	st := newTempDoctrineStore(t)
	addLaw(t, st, "text one", "catalog", "Catalog routes by MXIK code, never by name.", "catalog-routing")
	addLaw(t, st, "text two", "authority", "Admin permissions are a platform business row plus a CM grant.", "admin-authority")
	addLaw(t, st, "text three", "authority", "Grants expire.", "admin-authority",
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
	st := newTempDoctrineStore(t)
	addLaw(t, st, "text one", "authority", "Admin permissions are a platform business row plus a CM grant.", "admin-authority")

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
	id := addLaw(t, st, "a long ruling text nobody wants in a headline", "authority",
		"Admin permissions are a platform business row plus a CM grant.", "admin-authority",
		"--rationale", "One authorization path, one audit table.",
		"--verbatim", "both approved")

	out := showDoctrine(t, st, bead.ID)
	if !strings.Contains(out, "Admin permissions are a platform business row plus a CM grant.") {
		t.Fatalf("the LEGACY RULINGS line must carry the law, got\n%s", out)
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
	mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", Text: "a project ruling with no law at all", Topic: "legacy-slug", FiledBy: "owner:tester"})

	out := showDoctrine(t, st, bead.ID)
	if !strings.Contains(out, "a project ruling with no law at all") {
		t.Fatalf("a ruling with no law keeps rendering its text, got\n%s", out)
	}
}

// --- promote is refused: it would widen a ruling into project law ---

func TestDoctrine_PromoteRefusedForEveryActor(t *testing.T) {
	for _, actor := range []string{"", "coordinator", "executor"} {
		t.Run("actor="+actor, func(t *testing.T) {
			st := newTempDoctrineStore(t)
			bead := mkDoctrineIssue(t, st, "a bead")
			id := mustCreateStatementDirect(t, st, &beads.Statement{Kind: "ruling", IssueID: strPtr2(bead.ID), Text: "a ruling that outgrew its bead", Topic: "admin-authority", FiledBy: "owner:tester"}).ID
			if actor != "" {
				t.Setenv("BD_ACTOR", actor)
			}
			_, _, err := runDoctrine(t, "doctrine", "promote", id, "--concern", "authority")
			if err != errRulingsClosed {
				t.Fatalf("doctrine promote = %v, want errRulingsClosed", err)
			}
			row, err := st.GetStatement(context.Background(), id)
			if err != nil {
				t.Fatalf("get %s: %v", id, err)
			}
			if row.IssueID == nil || *row.IssueID != bead.ID || row.Concern != "" {
				t.Fatalf("a refused promote changed the ruling: issue %v concern %q", row.IssueID, row.Concern)
			}
		})
	}
}
