package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// newTempAreasStore opens a fresh store and points the CLI's --db flag at it.
func newTempAreasStore(t *testing.T) *store.Store {
	t.Helper()
	isolateExpandState(t)
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "areas.db")
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
	return st
}

// runAreasViaRoot runs `bd areas <args...>` through the root command.
func runAreasViaRoot(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	savedDB, savedJSON := flagDB, flagJSON
	root := newRoot()
	// newRoot re-registers the persistent flags with their defaults; restore.
	flagDB, flagJSON = savedDB, savedJSON
	bufOut, bufErr := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(append([]string{"areas"}, args...))
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

func mkAreasIssue(t *testing.T, st *store.Store, title, desc string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1, Description: desc}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return i
}

// mkAreasStatement files a statement carrying a workspace, a concern and a topic.
func mkAreasStatement(t *testing.T, st *store.Store, issueID, topic, workspace, concern string) *beads.Statement {
	t.Helper()
	iid := issueID
	s := &beads.Statement{
		Kind:      "ruling",
		IssueID:   &iid,
		Text:      "ruling for " + topic,
		FiledBy:   "owner:tester",
		Status:    "active",
		Scope:     "inherit",
		Topic:     topic,
		Workspace: workspace,
		Concern:   concern,
	}
	if err := st.CreateStatement(context.Background(), s); err != nil {
		t.Fatalf("create statement: %v", err)
	}
	return s
}

func statementByID(t *testing.T, st *store.Store, id string) *beads.Statement {
	t.Helper()
	s, err := st.GetStatement(context.Background(), id)
	if err != nil {
		t.Fatalf("get statement %s: %v", id, err)
	}
	return s
}

// The seeded vocabulary prints as 14 lines: 7 workspaces, 6 concerns and all.
func TestAreas_ListsThirteenPlusAll(t *testing.T) {
	newTempAreasStore(t)

	out, _, err := runAreasViaRoot(t, nil)
	if err != nil {
		t.Fatalf("areas: %v out %q", err, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 14 {
		t.Fatalf("expected 14 lines, got %d:\n%s", len(lines), out)
	}
	for i, l := range lines {
		wantKind := "workspace"
		if i >= 7 {
			wantKind = "concern"
		}
		if !strings.HasPrefix(l, wantKind) {
			t.Fatalf("line %d should start with %s, got %q", i, wantKind, l)
		}
		if !strings.Contains(l, "open ") || !strings.Contains(l, "topics ") {
			t.Fatalf("line %d should carry both counts, got %q", i, l)
		}
	}
	if !strings.Contains(out, "mobile/owner-app/") {
		t.Fatalf("listing should print workspace roots, got %q", out)
	}
	if !strings.Contains(out, "9 patterns") || !strings.Contains(out, "1 pattern ") {
		t.Fatalf("listing should print pattern counts, singular for one, got %q", out)
	}
}

// A bead resolves through the paths in its ## Files section.
func TestAreas_ResolveBeadFromFilesSection(t *testing.T) {
	st := newTempAreasStore(t)
	i := mkAreasIssue(t, st, "with files", "## Files\n\n- `server/src/db.ts`\n")

	out, _, err := runAreasViaRoot(t, []string{"resolve", i.ID})
	if err != nil {
		t.Fatalf("resolve: %v out %q", err, out)
	}
	if !strings.Contains(out, "workspace: server") {
		t.Fatalf("resolve should name the workspace, got %q", out)
	}
	if !strings.Contains(out, "concerns: schema") {
		t.Fatalf("resolve should name the concern, got %q", out)
	}
	if strings.Contains(out, "all") {
		t.Fatalf("the all concern must not come back on a resolve, got %q", out)
	}
}

// A path argument resolves the same way, without touching the issue table.
func TestAreas_ResolvePath(t *testing.T) {
	newTempAreasStore(t)

	out, _, err := runAreasViaRoot(t, []string{"resolve", "web-app/src/App.tsx"})
	if err != nil {
		t.Fatalf("resolve path: %v out %q", err, out)
	}
	if !strings.Contains(out, "workspace: web-app") || !strings.Contains(out, "concerns: frontend") {
		t.Fatalf("path resolve should give web-app + frontend, got %q", out)
	}
}

// A bead with no ## Files section exits 0 and explains on stderr.
func TestAreas_ResolveBeadWithoutFilesSection(t *testing.T) {
	st := newTempAreasStore(t)
	i := mkAreasIssue(t, st, "no files", "## Scope\n\nnothing here\n")

	out, errOut, err := runAreasViaRoot(t, []string{"resolve", i.ID})
	if err != nil {
		t.Fatalf("resolve should exit 0, got %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("stdout should stay empty, got %q", out)
	}
	if !strings.Contains(errOut, "## Files") || !strings.Contains(errOut, i.ID) {
		t.Fatalf("stderr should name the bead and the missing section, got %q", errOut)
	}
}

// Adding a concern is a decision: an executor is refused.
func TestAreas_AddConcernRefusesExecutor(t *testing.T) {
	st := newTempAreasStore(t)
	t.Setenv("BD_ACTOR", "executor")

	out, _, err := runAreasViaRoot(t, []string{"add", "concern", "billing", "--paths", "server/src/billing/**"})
	if err == nil {
		t.Fatalf("executor should be refused, got out %q", out)
	}
	if !strings.Contains(err.Error(), "BD_ACTOR=executor") {
		t.Fatalf("refusal should name the actor, got %v", err)
	}

	areas, lerr := st.ListAreas(context.Background())
	if lerr != nil {
		t.Fatalf("list areas: %v", lerr)
	}
	for _, a := range areas {
		if a.Name == "billing" {
			t.Fatalf("the refused concern must not be written")
		}
	}
}

// A new workspace directory is a fact about the repo: every actor may add one.
func TestAreas_AddWorkspaceOpenToExecutor(t *testing.T) {
	st := newTempAreasStore(t)
	t.Setenv("BD_ACTOR", "executor")

	out, _, err := runAreasViaRoot(t, []string{"add", "workspace", "ops", "--root", "ops"})
	if err != nil {
		t.Fatalf("executor should be allowed to add a workspace, got %v out %q", err, out)
	}

	areas, err := st.ListAreas(context.Background())
	if err != nil {
		t.Fatalf("list areas: %v", err)
	}
	var found bool
	for _, a := range areas {
		if a.Kind == store.AreaWorkspace && a.Name == "ops" {
			found = true
			if a.Root != "ops/" {
				t.Fatalf("root should be prefix-safe, got %q", a.Root)
			}
		}
	}
	if !found {
		t.Fatalf("the workspace should be in the vocabulary, got %v", areas)
	}
	if w, _ := areas.ResolvePath("ops/deploy.sh"); w != "ops" {
		t.Fatalf("the new workspace should resolve its own paths, got %q", w)
	}
}

// rename rewrites the statements naming the entry, in the same transaction.
func TestAreas_RenameRewritesStatements(t *testing.T) {
	st := newTempAreasStore(t)
	i := mkAreasIssue(t, st, "carrier", "## Files\n\n- server/src/db.ts\n")
	stmt := mkAreasStatement(t, st, i.ID, "sessions", "server", "sync")
	multi := mkAreasStatement(t, st, i.ID, "catalog-sync", "server", "sync,catalog")

	out, _, err := runAreasViaRoot(t, []string{"rename", "concern", "sync", "sync2"})
	if err != nil {
		t.Fatalf("rename: %v out %q", err, out)
	}
	if got := statementByID(t, st, stmt.ID).Concern; got != "sync2" {
		t.Fatalf("statement concern should follow the rename, got %q", got)
	}
	gotConcerns := strings.Split(statementByID(t, st, multi.ID).Concern, ",")
	wantConcerns := map[string]bool{"sync2": true, "catalog": true}
	for _, concern := range gotConcerns {
		if concern == "sync" {
			t.Fatalf("multi-concern statement should not retain sync, got %v", gotConcerns)
		}
		delete(wantConcerns, concern)
	}
	if len(wantConcerns) != 0 {
		t.Fatalf("multi-concern statement should contain sync2 and catalog, got %v", gotConcerns)
	}

	areas, err := st.ListAreas(context.Background())
	if err != nil {
		t.Fatalf("list areas: %v", err)
	}
	var names []string
	for _, a := range areas {
		names = append(names, a.Name)
	}
	hasSync, hasSync2 := false, false
	for _, name := range names {
		hasSync = hasSync || name == "sync"
		hasSync2 = hasSync2 || name == "sync2"
	}
	if !hasSync2 || hasSync {
		t.Fatalf("vocabulary should carry the new name only, got %v", names)
	}
}

// merge folds one entry into another and rewrites the statements naming it.
func TestAreas_MergeRewritesStatements(t *testing.T) {
	st := newTempAreasStore(t)
	i := mkAreasIssue(t, st, "carrier", "## Files\n\n- server/src/db.ts\n")
	stmt := mkAreasStatement(t, st, i.ID, "sync-window", "server", "sync")
	keep := mkAreasStatement(t, st, i.ID, "catalog-drift", "server", "catalog")
	multi := mkAreasStatement(t, st, i.ID, "sync-window", "server", "sync,catalog")

	out, _, err := runAreasViaRoot(t, []string{"merge", "concern", "sync", "catalog"})
	if err != nil {
		t.Fatalf("merge: %v out %q", err, out)
	}
	if got := statementByID(t, st, stmt.ID).Concern; got != "catalog" {
		t.Fatalf("merged statement should name the target concern, got %q", got)
	}
	if got := statementByID(t, st, keep.ID).Concern; got != "catalog" {
		t.Fatalf("an untouched statement should keep its concern, got %q", got)
	}
	if got := statementByID(t, st, multi.ID).Concern; got != "catalog" {
		t.Fatalf("merged multi-concern statement should deduplicate the target, got %q", got)
	}

	areas, err := st.ListAreas(context.Background())
	if err != nil {
		t.Fatalf("list areas: %v", err)
	}
	for _, a := range areas {
		if a.Kind == store.AreaConcern && a.Name == "sync" {
			t.Fatalf("the merged-away concern should be gone")
		}
	}
	// The counts still read: the merged concern's topics are on the target now.
	for _, a := range areas {
		if a.Kind == store.AreaConcern && a.Name == "catalog" && a.Topics != 2 {
			t.Fatalf("catalog should carry both topics after the merge, got %d", a.Topics)
		}
	}
}

// Topic counts treat each member of a comma-separated concern as an area.
func TestAreas_TopicCountsConcernMembership(t *testing.T) {
	st := newTempAreasStore(t)
	i := mkAreasIssue(t, st, "carrier", "## Files\n\n- server/src/db.ts\n")
	mkAreasStatement(t, st, i.ID, "shared-topic", "server", "sync,catalog")

	areas, err := st.ListAreas(context.Background())
	if err != nil {
		t.Fatalf("list areas: %v", err)
	}
	want := map[string]int{"sync": 1, "catalog": 1}
	for _, a := range areas {
		if a.Kind != store.AreaConcern {
			continue
		}
		if n, ok := want[a.Name]; ok {
			if a.Topics != n {
				t.Fatalf("concern %s topics = %d, want %d", a.Name, a.Topics, n)
			}
			delete(want, a.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing seeded concerns from area list: %v", want)
	}
}

// A duplicate (kind, name) is refused, and the message names it.
func TestAreas_AddDuplicateRefused(t *testing.T) {
	newTempAreasStore(t)

	out, _, err := runAreasViaRoot(t, []string{"add", "workspace", "server", "--root", "server/"})
	if err == nil {
		t.Fatalf("a duplicate workspace should be refused, got out %q", out)
	}
	if !strings.Contains(err.Error(), `workspace "server" already exists`) {
		t.Fatalf("refusal should read `%%s %%q already exists`, got %v", err)
	}

	out, _, err = runAreasViaRoot(t, []string{"add", "concern", "schema", "--paths", "db/**"})
	if err == nil {
		t.Fatalf("a duplicate concern should be refused, got out %q", out)
	}
	if !strings.Contains(err.Error(), `concern "schema" already exists`) {
		t.Fatalf("refusal should read `%%s %%q already exists`, got %v", err)
	}
}

// The open count follows the beads that resolve to an area.
func TestAreas_OpenCountsResolvingIssues(t *testing.T) {
	st := newTempAreasStore(t)
	mkAreasIssue(t, st, "one", "## Files\n\n- server/src/db.ts\n")
	mkAreasIssue(t, st, "two", "## Files\n\n- web-app/src/App.tsx\n")
	mkAreasIssue(t, st, "none", "no files section")

	areas, err := st.ListAreas(context.Background())
	if err != nil {
		t.Fatalf("list areas: %v", err)
	}
	want := map[string]int{"server": 1, "web-app": 1, "shared": 0}
	for _, a := range areas {
		if a.Kind != store.AreaWorkspace {
			continue
		}
		if n, ok := want[a.Name]; ok && a.Open != n {
			t.Fatalf("workspace %s open = %d, want %d", a.Name, a.Open, n)
		}
	}
}
