package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rsktash/beads/store"
)

func TestParse_PathSymbolLine(t *testing.T) {
	got := store.ParseCitations("see server/src/routes/exports.ts::buildCursor:214")
	want := store.Citation{Path: "server/src/routes/exports.ts", Symbol: "buildCursor", Line: 214}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ParseCitations returned %+v, want %+v", got, want)
	}
}

func TestParse_PathSymbolNoLine(t *testing.T) {
	got := store.ParseCitations("server/src/routes/exports.ts::buildCursor")
	want := store.Citation{Path: "server/src/routes/exports.ts", Symbol: "buildCursor"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ParseCitations returned %+v, want %+v", got, want)
	}
}

func TestParse_IgnoresPlainPath(t *testing.T) {
	if got := store.ParseCitations("read server/src/routes/exports.ts"); len(got) != 0 {
		t.Fatalf("plain path parsed as citation: %+v", got)
	}
}

func TestParse_MultiplePerText(t *testing.T) {
	got := store.ParseCitations("a.go::first:2 and web/x.ts::second")
	want := []store.Citation{
		{Path: "a.go", Symbol: "first", Line: 2},
		{Path: "web/x.ts", Symbol: "second"},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseCitations returned %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("citation %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func writeCitationFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func resolveCitation(t *testing.T, root string, c store.Citation) store.CitationState {
	t.Helper()
	got, err := store.ResolveCitation(root, c)
	if err != nil {
		t.Fatalf("ResolveCitation: %v", err)
	}
	return got
}

func TestResolve_LiveExactLine(t *testing.T) {
	root := t.TempDir()
	writeCitationFixture(t, root, "pkg/x.go", "package pkg\n\nfunc target() {}\n")
	got := resolveCitation(t, root, store.Citation{Path: "pkg/x.go", Symbol: "target", Line: 3})
	if got.Status != store.CitationLive || got.Line != 3 {
		t.Fatalf("state = %+v, want live at line 3", got)
	}
}

func TestResolve_LiveNoHint(t *testing.T) {
	root := t.TempDir()
	writeCitationFixture(t, root, "pkg/x.go", "package pkg\n\nfunc target() {}\n")
	got := resolveCitation(t, root, store.Citation{Path: "pkg/x.go", Symbol: "target"})
	if got.Status != store.CitationLive || got.Line != 3 {
		t.Fatalf("state = %+v, want live at line 3", got)
	}
}

func TestResolve_MovedNamesNewLine(t *testing.T) {
	root := t.TempDir()
	writeCitationFixture(t, root, "pkg/x.go", "package pkg\n\n\n\nfunc target() {}\n")
	got := resolveCitation(t, root, store.Citation{Path: "pkg/x.go", Symbol: "target", Line: 3})
	if got.Status != store.CitationMoved || got.Line != 5 {
		t.Fatalf("state = %+v, want moved to line 5", got)
	}
}

func TestResolve_StaleWhenSymbolGone(t *testing.T) {
	root := t.TempDir()
	writeCitationFixture(t, root, "pkg/x.go", "package pkg\n\nfunc other() {}\n")
	got := resolveCitation(t, root, store.Citation{Path: "pkg/x.go", Symbol: "target", Line: 3})
	if got.Status != store.CitationStale {
		t.Fatalf("state = %+v, want stale", got)
	}
}

func TestResolve_StaleWhenFileGone(t *testing.T) {
	got, err := store.ResolveCitation(t.TempDir(), store.Citation{Path: "gone.go", Symbol: "target", Line: 3})
	if err != nil {
		t.Fatalf("missing file returned error: %v", err)
	}
	if got.Status != store.CitationStale {
		t.Fatalf("state = %+v, want stale", got)
	}
}

func TestResolve_TwoMatchesAreMoved(t *testing.T) {
	root := t.TempDir()
	writeCitationFixture(t, root, "pkg/x.go", "package pkg\n\nfunc target() {}\n\nfunc target() {}\n")
	got := resolveCitation(t, root, store.Citation{Path: "pkg/x.go", Symbol: "target", Line: 5})
	if got.Status != store.CitationMoved || got.Line != 3 {
		t.Fatalf("state = %+v, want moved naming first match at line 3", got)
	}
}

func TestResolve_MatchesExportConstAndClass(t *testing.T) {
	root := t.TempDir()
	const rel = "src/declarations.ts"
	writeCitationFixture(t, root, rel, "func f() {}\nconst g = 1\nexport class H {}\nfunction i() {}\ntype J = string\n")

	for _, tc := range []struct {
		symbol string
		line   int
	}{
		{symbol: "f", line: 1},
		{symbol: "g", line: 2},
		{symbol: "H", line: 3},
		{symbol: "i", line: 4},
		{symbol: "J", line: 5},
	} {
		t.Run(tc.symbol, func(t *testing.T) {
			got := resolveCitation(t, root, store.Citation{Path: rel, Symbol: tc.symbol, Line: tc.line})
			if got.Status != store.CitationLive || got.Line != tc.line {
				t.Fatalf("%s state = %+v, want live at line %d", tc.symbol, got, tc.line)
			}
		})
	}
}
