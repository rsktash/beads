// This file is an internal test (package store, not store_test): globMatch and
// ResolvePath's helpers are unexported and cannot be reached from the external
// test package the rest of store/ uses.
package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rsktash/beads"
)

// newAreasStore opens a fresh sqlite store, so every test reads the vocabulary
// migration 0007 seeds rather than a hand-built fixture.
func newAreasStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "areas.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.SetConfig(context.Background(), CfgIssuePrefix, "bd"); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func seededAreas(t *testing.T) Areas {
	t.Helper()
	as, err := newAreasStore(t).ListAreas(context.Background())
	if err != nil {
		t.Fatalf("list areas: %v", err)
	}
	return as
}

// ** spans any number of segments.
func TestGlob_StarStarSpansSegments(t *testing.T) {
	if !globMatch("tasnif-sync/**", "tasnif-sync/src/a/b.ts") {
		t.Fatalf("tasnif-sync/** should match tasnif-sync/src/a/b.ts")
	}
	if !globMatch("**", "anything/at/all.go") {
		t.Fatalf("** should match every path")
	}
	if globMatch("tasnif-sync/**", "server/src/a.ts") {
		t.Fatalf("tasnif-sync/** should not match a path outside the root")
	}
}

// A single * stops at the segment boundary.
func TestGlob_SingleStarStopsAtSegment(t *testing.T) {
	if globMatch("db/*.sql", "db/migrations/x.sql") {
		t.Fatalf("db/*.sql should not match db/migrations/x.sql")
	}
	if !globMatch("db/*.sql", "db/schema.sql") {
		t.Fatalf("db/*.sql should match db/schema.sql")
	}
	if !globMatch("server/src/routes/admin*.ts", "server/src/routes/admin-grants.ts") {
		t.Fatalf("admin*.ts should match admin-grants.ts in the same directory")
	}
	if globMatch("server/src/routes/admin*.ts", "server/src/routes/nested/admin.ts") {
		t.Fatalf("admin*.ts should not reach into a nested directory")
	}
}

// A path under a workspace root resolves to that workspace.
func TestResolvePath_Workspace(t *testing.T) {
	as := seededAreas(t)
	for _, tc := range []struct{ path, want string }{
		{"server/src/routes/catalog.ts", "server"},
		{"web-app/src/App.tsx", "web-app"},
		{"mobile/cm-app/lib/main.dart", "mobile/cm-app"},
		{"README.md", ""},
	} {
		got, _ := as.ResolvePath(tc.path)
		if got != tc.want {
			t.Fatalf("ResolvePath(%q) workspace = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// Concern resolution is a set: a path can carry several, and a path the page
// names carries exactly one.
func TestResolvePath_MultipleConcerns(t *testing.T) {
	as := seededAreas(t)

	_, cs := as.ResolvePath("server/src/db.ts")
	if len(cs) != 1 || cs[0] != "schema" {
		t.Fatalf("server/src/db.ts should resolve to schema only, got %v", cs)
	}

	ws, cs := as.ResolvePath("tasnif-sync/src/fetch.ts")
	if ws != "tasnif-sync" {
		t.Fatalf("tasnif-sync/src/fetch.ts workspace = %q, want tasnif-sync", ws)
	}
	if len(cs) != 2 || cs[0] != "sync" || cs[1] != "catalog" {
		t.Fatalf("tasnif-sync/src/fetch.ts should resolve to sync and catalog in vocabulary order, got %v", cs)
	}
}

// The all concern matches everything and is never returned by a resolve; it
// stays in the vocabulary so it can be named.
func TestResolvePath_AllExcluded(t *testing.T) {
	as := seededAreas(t)
	for _, p := range []string{"server/src/db.ts", "README.md", "anything/else.txt"} {
		_, cs := as.ResolvePath(p)
		for _, c := range cs {
			if c == AreaAll {
				t.Fatalf("ResolvePath(%q) returned the all concern: %v", p, cs)
			}
		}
	}
	if !globMatch("**", "README.md") {
		t.Fatalf("the all concern's glob should match every path")
	}
	var found bool
	for _, a := range as {
		if a.Kind == AreaConcern && a.Name == AreaAll {
			found = true
		}
	}
	if !found {
		t.Fatalf("the all concern should stay in the vocabulary")
	}
}

// A fresh store carries the seeded vocabulary: seven workspaces, seven concerns.
func TestSeed_HasSevenWorkspacesAndSevenConcerns(t *testing.T) {
	as := seededAreas(t)
	var ws, cs int
	for _, a := range as {
		switch a.Kind {
		case AreaWorkspace:
			ws++
		case AreaConcern:
			cs++
		}
	}
	if ws != 7 || cs != 7 {
		t.Fatalf("seed should give 7 workspaces and 7 concerns, got %d and %d", ws, cs)
	}
	// Workspaces come first, so the listing prints them as one block.
	for i, a := range as {
		if i < 7 && a.Kind != AreaWorkspace {
			t.Fatalf("row %d should be a workspace, got %s %s", i, a.Kind, a.Name)
		}
		if i >= 7 && a.Kind != AreaConcern {
			t.Fatalf("row %d should be a concern, got %s %s", i, a.Kind, a.Name)
		}
	}
}

// A bead resolves through the paths in its ## Files section, and only that
// section: one path per line, first whitespace token.
func TestResolvePath_IssueFilesSection(t *testing.T) {
	st := newAreasStore(t)
	ctx := context.Background()
	desc := "## Scope\n\nweb-app/src/App.tsx\n\n## Files\n\n- `server/src/db.ts`\n- tasnif-sync/src/fetch.ts, server/src/auth.ts\n\n## Gate\n\nnothing\n"
	i := &beads.Issue{Title: "T", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1, Description: desc}
	if err := st.CreateIssue(ctx, i); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	ws, cs, err := st.ResolveIssue(ctx, i.ID)
	if err != nil {
		t.Fatalf("resolve issue: %v", err)
	}
	if len(ws) != 2 || ws[0] != "server" || ws[1] != "tasnif-sync" {
		t.Fatalf("workspaces = %v, want [server tasnif-sync] (web-app is outside ## Files)", ws)
	}
	// server/src/auth.ts is the second token on its line, so authority is absent.
	if len(cs) != 3 || cs[0] != "sync" || cs[1] != "schema" || cs[2] != "catalog" {
		t.Fatalf("concerns = %v, want [sync schema catalog]", cs)
	}

	// A bead with no ## Files section resolves to nothing, without an error.
	bare := &beads.Issue{Title: "bare", Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1, Description: "no sections here"}
	if err := st.CreateIssue(ctx, bare); err != nil {
		t.Fatalf("create bare issue: %v", err)
	}
	ws, cs, err = st.ResolveIssue(ctx, bare.ID)
	if err != nil {
		t.Fatalf("resolve bare issue: %v", err)
	}
	if len(ws) != 0 || len(cs) != 0 {
		t.Fatalf("a bead without ## Files should resolve to nothing, got %v %v", ws, cs)
	}
}
