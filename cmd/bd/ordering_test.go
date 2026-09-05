package main

// Feature 16 — brief ordering by binding distance, then change; expand
// dedupe. Every test sets TMPDIR at a fresh directory so the per-session
// expand state can never leak between tests or runs.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// orderWindow is the handoff instant the ordering fixtures hang off.
var orderWindow = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

// orderLine finds the section line carrying a marker, -1 when absent.
func orderLine(lines []string, marker string) int {
	for i, l := range lines {
		if strings.Contains(l, marker) {
			return i
		}
	}
	return -1
}

// mkOrderBindsRuling files a ruling on hostIssue whose binds_id names
// boundBead: the explicit binds arm of the resolver view.
func mkOrderBindsRuling(t *testing.T, st *store.Store, hostIssue, boundBead, text string, createdAt time.Time) *beads.Statement {
	t.Helper()
	host, bound := hostIssue, boundBead
	row := &beads.Statement{
		Kind: "ruling", Text: text, FiledBy: "owner:tester", Status: "active", Scope: "inherit",
		IssueID: &host, BindsID: &bound, CreatedAt: createdAt,
	}
	if err := st.CreateStatement(context.Background(), row); err != nil {
		t.Fatalf("create binds ruling: %v", err)
	}
	return row
}

// mkOrderParent wires child under parent.
func mkOrderParent(t *testing.T, st *store.Store, child, parent string) {
	t.Helper()
	if err := st.AddDependency(context.Background(), beads.Dependency{
		IssueID: child, DependsOnID: parent, Type: beads.DepParentChild,
	}); err != nil {
		t.Fatalf("parent dep %s under %s: %v", child, parent, err)
	}
}

// closeOrderIssue stamps an issue closed at the instant, the way a close
// ruling's state change does.
func closeOrderIssue(t *testing.T, st *store.Store, issueID string, at time.Time) {
	t.Helper()
	if _, err := st.DB().ExecContext(context.Background(),
		"UPDATE issues SET status = 'closed', closed_at = ? WHERE id = ?", at, issueID); err != nil {
		t.Fatalf("close %s: %v", issueID, err)
	}
}

// dumpOrderTable renders every row of a table so two dumps compare equal
// only when nothing was written between them.
func dumpOrderTable(t *testing.T, st *store.Store, table string) string {
	t.Helper()
	rows, err := st.DB().QueryContext(context.Background(), "SELECT * FROM "+table+" ORDER BY 1")
	if err != nil {
		t.Fatalf("dump %s: %v", table, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns %s: %v", table, err)
	}
	var buf strings.Builder
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		for i, v := range vals {
			fmt.Fprintf(&buf, "%s=%v|", cols[i], v)
		}
		fmt.Fprintln(&buf)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("dump %s: %v", table, err)
	}
	return buf.String()
}

// --- Behaviour 1: binding distance, nearest first ---

func TestOrder_SelfBeforeBinds(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "self before binds", "## Files\n- server/src/auth.ts\n")
	addSinceHandoff(t, st, "plan-sb", "active", "A", []string{bead.ID}, orderWindow)
	host := mkAuthIssue(t, st, "the binds host", "")

	// The self ruling is old and unchanged; the binds ruling is newer and
	// changed after the handoff. Distance must still decide: 0 before 1.
	before := orderWindow.Add(-2 * time.Hour)
	self := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the self ruling", createdAt: before})
	setStatementChangedAt(t, st, self.ID, &before)
	mkOrderBindsRuling(t, st, host.ID, bead.ID, "the binds ruling", orderWindow.Add(time.Hour))

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings")
	requireNoErr(t, err, errOut)
	lines := briefSection(out, "RULINGS")
	i, j := orderLine(lines, "the self ruling"), orderLine(lines, "the binds ruling")
	if i < 0 || j < 0 {
		t.Fatalf("a ruling went missing (self %d, binds %d):\n%s", i, j, out)
	}
	if i > j {
		t.Fatalf("the self ruling must precede the binds ruling:\n%s", out)
	}
	if !strings.HasPrefix(lines[j], "*") {
		t.Fatalf("the binds ruling changed since the handoff and must carry the star:\n%s", out)
	}
	if strings.HasPrefix(lines[i], "*") {
		t.Fatalf("the unchanged self ruling must not carry the star:\n%s", out)
	}
}

func TestOrder_BindsBeforeEpic(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "binds before epic", "## Files\n- server/src/auth.ts\n")
	epic := mkAuthIssue(t, st, "the epic", "")
	mkOrderParent(t, st, bead.ID, epic.ID)
	addSinceHandoff(t, st, "plan-be", "active", "A", []string{bead.ID}, orderWindow)
	host := mkAuthIssue(t, st, "the binds host", "")

	old := orderWindow.Add(-2 * time.Hour)
	binds := mkOrderBindsRuling(t, st, host.ID, bead.ID, "the binds ruling", old)
	setStatementChangedAt(t, st, binds.ID, &old)
	// The epic ruling changed after the handoff; the nearer binds origin
	// still wins.
	epicRuling := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: epic.ID, topic: "order", text: "the epic ruling", createdAt: orderWindow.Add(-48 * time.Hour)})
	after := orderWindow.Add(time.Hour)
	setStatementChangedAt(t, st, epicRuling.ID, &after)

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings")
	requireNoErr(t, err, errOut)
	lines := briefSection(out, "RULINGS")
	i, j := orderLine(lines, "the binds ruling"), orderLine(lines, "the epic ruling")
	if i < 0 || j < 0 {
		t.Fatalf("a ruling went missing (binds %d, epic %d):\n%s", i, j, out)
	}
	if i > j {
		t.Fatalf("the binds ruling must precede the epic ruling:\n%s", out)
	}
	if !strings.HasPrefix(lines[j], "*") {
		t.Fatalf("the changed epic ruling must carry the star:\n%s", out)
	}
}

func TestOrder_EpicNearestFirst(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "epic nearest first", "## Files\n- server/src/auth.ts\n")
	parent := mkAuthIssue(t, st, "the parent", "")
	grand := mkAuthIssue(t, st, "the grandparent", "")
	mkOrderParent(t, st, bead.ID, parent.ID)
	mkOrderParent(t, st, parent.ID, grand.ID)
	addSinceHandoff(t, st, "plan-en", "active", "A", []string{bead.ID}, orderWindow)

	// Depth 1 (the parent) and depth 2 (the grandparent) are both asserted.
	// The deeper ruling is newer and changed after the handoff; the nearer
	// ancestor still comes first.
	old := orderWindow.Add(-2 * time.Hour)
	near := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: parent.ID, topic: "order", text: "the depth one ruling", createdAt: old})
	setStatementChangedAt(t, st, near.ID, &old)
	far := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: grand.ID, topic: "order", text: "the depth two ruling", createdAt: orderWindow.Add(time.Hour)})
	after := orderWindow.Add(2 * time.Hour)
	setStatementChangedAt(t, st, far.ID, &after)

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings")
	requireNoErr(t, err, errOut)
	lines := briefSection(out, "RULINGS")
	i, j := orderLine(lines, "the depth one ruling"), orderLine(lines, "the depth two ruling")
	if i < 0 || j < 0 {
		t.Fatalf("a ruling went missing (depth one %d, depth two %d):\n%s", i, j, out)
	}
	if i > j {
		t.Fatalf("the depth one ruling must precede the depth two ruling:\n%s", out)
	}
	if !strings.HasPrefix(lines[j], "*") {
		t.Fatalf("the changed depth two ruling must carry the star:\n%s", out)
	}
}

func TestOrder_BlocksBeforeProject(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "blocks before project", "## Files\n- server/src/auth.ts\n")
	addSinceHandoff(t, st, "plan-bp", "active", "A", []string{bead.ID}, orderWindow)

	blocker := mkAuthIssue(t, st, "the blocker", "")
	if err := st.AddDependency(context.Background(), beads.Dependency{
		IssueID: bead.ID, DependsOnID: blocker.ID, Type: beads.DepBlocks,
	}); err != nil {
		t.Fatalf("blocks dep: %v", err)
	}

	// An inherited ruling at depth 8 (the view's deepest band) must still
	// order before every blocks ruling: the epic band tops out at 9 and
	// blocks sits at 10.
	chain := bead.ID
	for i := 0; i < 8; i++ {
		issue := mkAuthIssue(t, st, fmt.Sprintf("ancestor %d", i+1), "")
		mkOrderParent(t, st, chain, issue.ID)
		chain = issue.ID
	}
	deepOld := orderWindow.Add(-2 * time.Hour)
	deep := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: chain, topic: "order", text: "the depth eight ruling", createdAt: deepOld})
	setStatementChangedAt(t, st, deep.ID, &deepOld)

	old := orderWindow.Add(-time.Hour)
	blocks := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: blocker.ID, topic: "order", text: "the blocks ruling", createdAt: old})
	setStatementChangedAt(t, st, blocks.ID, &old)
	project := mkAuthStatement(t, st, authStatement{kind: "ruling", text: "the project ruling", topic: "order", createdAt: orderWindow.Add(-3 * time.Hour)})
	older := orderWindow.Add(-3 * time.Hour)
	setStatementChangedAt(t, st, project.ID, &older)

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings")
	requireNoErr(t, err, errOut)
	lines := briefSection(out, "RULINGS")
	d, b, p := orderLine(lines, "the depth eight ruling"), orderLine(lines, "the blocks ruling"), orderLine(lines, "the project ruling")
	if d < 0 || b < 0 || p < 0 {
		t.Fatalf("a ruling went missing (depth eight %d, blocks %d, project %d):\n%s", d, b, p, out)
	}
	if !(d < b && b < p) {
		t.Fatalf("want depth eight < blocks < project, got %d < %d < %d:\n%s", d, b, p, out)
	}
}

// --- Behaviour 2: change since handoff, and the typed diff ---

func TestOrder_ChangedSinceHandoffFirstWithinBand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "changed first", "## Files\n- server/src/auth.ts\n")
	addSinceHandoff(t, st, "plan-ch", "active", "A", []string{bead.ID}, orderWindow)

	// Same band (self). The changed record is the older one by created_at;
	// change since handoff must overtake recency.
	changedAt := orderWindow.Add(time.Hour)
	changed := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the changed ruling", createdAt: orderWindow.Add(-48 * time.Hour)})
	setStatementChangedAt(t, st, changed.ID, &changedAt)
	fresh := orderWindow.Add(-time.Hour)
	unchanged := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the fresh unchanged ruling", createdAt: fresh})
	setStatementChangedAt(t, st, unchanged.ID, &fresh)

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings")
	requireNoErr(t, err, errOut)
	lines := briefSection(out, "RULINGS")
	i, j := orderLine(lines, "the changed ruling"), orderLine(lines, "the fresh unchanged ruling")
	if i < 0 || j < 0 {
		t.Fatalf("a ruling went missing (changed %d, fresh %d):\n%s", i, j, out)
	}
	if i != 0 || j != 1 {
		t.Fatalf("the changed ruling must lead its band:\n%s", out)
	}
	if !strings.HasPrefix(lines[i], "*") || strings.HasPrefix(lines[j], "*") {
		t.Fatalf("the star must mark exactly the changed record:\n%s", out)
	}
}

func TestOrder_TypedDiffCounts(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "typed diff", "## Files\n- server/src/auth.ts\n")
	addSinceHandoff(t, st, "plan-td", "active", "A", []string{bead.ID}, orderWindow)

	// Two rulings changed after the handoff.
	for i, text := range []string{"diff ruling one", "diff ruling two"} {
		at := orderWindow.Add(time.Duration(i+1) * time.Hour)
		row := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: text, createdAt: at})
		setStatementChangedAt(t, st, row.ID, &at)
	}
	// One question answered after it.
	answeredAt := orderWindow.Add(3 * time.Hour)
	answered := mkAuthStatement(t, st, authStatement{kind: "question", issueID: bead.ID, topic: "order", text: "the answered question", status: "answered", createdAt: answeredAt})
	setStatementChangedAt(t, st, answered.ID, &answeredAt)
	// One finding filed after it.
	mkAuthStatement(t, st, authStatement{kind: "finding", issueID: bead.ID, topic: "order", text: "the diff finding", filedBy: "executor:agent", createdAt: orderWindow.Add(4 * time.Hour)})
	// Three dependencies whose depends-on beads closed at or after it — one
	// exactly at the instant (the >= boundary), two after. A fourth dep
	// closed before it, and a fifth never closed, count for nothing.
	for i, at := range []time.Time{orderWindow.Add(-time.Hour), orderWindow, orderWindow.Add(time.Hour), orderWindow.Add(2 * time.Hour)} {
		dep := mkAuthIssue(t, st, fmt.Sprintf("dep %d", i+1), "")
		if err := st.AddDependency(context.Background(), beads.Dependency{
			IssueID: bead.ID, DependsOnID: dep.ID, Type: beads.DepBlocks,
		}); err != nil {
			t.Fatalf("dep: %v", err)
		}
		if i > 0 {
			closeOrderIssue(t, st, dep.ID, at)
		}
	}
	open := mkAuthIssue(t, st, "the open dep", "")
	if err := st.AddDependency(context.Background(), beads.Dependency{
		IssueID: bead.ID, DependsOnID: open.ID, Type: beads.DepBlocks,
	}); err != nil {
		t.Fatalf("dep: %v", err)
	}

	out, errOut, err := runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)
	want := "(since handoff: 2 rulings, 1 question answered, 1 finding, 3 deps closed)"
	header := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "BRIEF") {
			header = l
			break
		}
	}
	if !strings.Contains(header, want) {
		t.Fatalf("the typed diff is missing or wrong on the BRIEF line:\n%s", out)
	}
}

// --- Behaviour 3: the final tie-breaks ---

func TestOrder_TieBreakCreatedAtThenId(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "tie breaks", "## Files\n- server/src/auth.ts\n")
	same := orderWindow
	newer := orderWindow.Add(time.Hour)
	first := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the first same-instant ruling", createdAt: same})
	second := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the second same-instant ruling", createdAt: same})
	mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the newer ruling", createdAt: newer})

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings")
	requireNoErr(t, err, errOut)
	lines := briefSection(out, "RULINGS")
	n, s, f := orderLine(lines, "the newer ruling"), orderLine(lines, second.ID), orderLine(lines, first.ID)
	if n < 0 || s < 0 || f < 0 {
		t.Fatalf("a ruling went missing (newer %d, second %d, first %d):\n%s", n, s, f, out)
	}
	// created_at DESC first, then id DESC among equals: the newer ruling
	// leads, and the later-filed same-instant ruling precedes the first.
	if !(n < s && s < f) {
		t.Fatalf("want newer < second < first by created_at then id, got %d %d %d:\n%s", n, s, f, out)
	}
}

// --- Behaviours 5-8: the expand dedupe ---

func TestExpand_SecondCallRendersIdOnly(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("CLAUDE_SESSION_ID", "expand-second-call")
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "expand twice", "## Files\n- server/src/auth.ts\n")
	long := "the first line of the ruling " + strings.Repeat("y", 150) + "\nand a second line only --expand shows"
	r := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "expand-me", text: long})

	out, errOut, err := runAuthority(t, "authority", bead.ID, "--expand", r.ID)
	requireNoErr(t, err, errOut)
	if !strings.Contains(out, "and a second line only --expand shows") {
		t.Fatalf("the first --expand did not open the record:\n%s", out)
	}

	// The second brief names it again — and a third does not name it at
	// all: either way it renders as its id alone.
	out, errOut, err = runAuthority(t, "authority", bead.ID, "--expand", r.ID)
	requireNoErr(t, err, errOut)
	if strings.Contains(out, "and a second line only --expand shows") {
		t.Fatalf("the second --expand re-opened the record:\n%s", out)
	}
	bare := r.ID + "  (expanded earlier this session)"
	if !strings.Contains(out, bare) {
		t.Fatalf("the deduplicated record is not the bare id line:\n%s", out)
	}

	out, errOut, err = runAuthority(t, "authority", bead.ID)
	requireNoErr(t, err, errOut)
	if strings.Contains(out, "and a second line only --expand shows") {
		t.Fatalf("a later brief without --expand re-opened the record:\n%s", out)
	}
	if !strings.Contains(out, bare) {
		t.Fatalf("the later brief does not render the bare id line:\n%s", out)
	}
}

func TestExpand_DedupeDoesNotReorder(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("CLAUDE_SESSION_ID", "expand-reorder")
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "dedupe order", "## Files\n- server/src/auth.ts\n")
	gamma := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the gamma ruling", createdAt: orderWindow.Add(-3 * time.Hour)})
	beta := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the beta ruling", createdAt: orderWindow.Add(-2 * time.Hour)})
	alpha := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the alpha ruling", createdAt: orderWindow.Add(-time.Hour)})
	for _, row := range []*beads.Statement{gamma, beta, alpha} {
		at := row.CreatedAt
		setStatementChangedAt(t, st, row.ID, &at)
	}

	first, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings")
	requireNoErr(t, err, errOut)

	opened, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings", "--expand", beta.ID)
	requireNoErr(t, err, errOut)

	later, errOut, err := runAuthority(t, "authority", bead.ID, "--kind", "rulings")
	requireNoErr(t, err, errOut)

	for name, out := range map[string]string{"first": first, "opened": opened, "later": later} {
		lines := briefSection(out, "RULINGS")
		a, b, g := orderLine(lines, "the alpha ruling"), orderLine(lines, beta.ID), orderLine(lines, "the gamma ruling")
		if a < 0 || b < 0 || g < 0 {
			t.Fatalf("%s: a ruling went missing (alpha %d, beta %d, gamma %d):\n%s", name, a, b, g, out)
		}
		if !(a < b && b < g) {
			t.Fatalf("%s: order changed (alpha %d, beta %d, gamma %d):\n%s", name, a, b, g, out)
		}
	}
	laterLines := briefSection(later, "RULINGS")
	firstLines := briefSection(first, "RULINGS")
	if got := laterLines[orderLine(laterLines, beta.ID)]; got != beta.ID+"  (expanded earlier this session)" {
		t.Fatalf("the later brief did not render the bare id line: %q", got)
	}
	if orderLine(laterLines, beta.ID) != orderLine(firstLines, beta.ID) {
		t.Fatalf("the deduplicated record moved: first %d, later %d",
			orderLine(firstLines, beta.ID), orderLine(laterLines, beta.ID))
	}
}

func TestExpand_UnwritableStateDirRendersFull(t *testing.T) {
	base := t.TempDir()
	t.Setenv("TMPDIR", filepath.Join(base, "no-write"))
	t.Setenv("CLAUDE_SESSION_ID", "expand-unwritable")
	if err := os.MkdirAll(filepath.Join(base, "no-write"), 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "unwritable state", "## Files\n- server/src/auth.ts\n")
	long := "the first line of the ruling " + strings.Repeat("y", 150) + "\nand a second line only --expand shows"
	r := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "expand-me", text: long})

	for i := 0; i < 2; i++ {
		out, errOut, err := runAuthority(t, "authority", bead.ID, "--expand", r.ID)
		requireNoErr(t, err, errOut)
		if !strings.Contains(out, "and a second line only --expand shows") {
			t.Fatalf("render %d: the unwritable state dir suppressed a record it was not sure about:\n%s", i+1, out)
		}
		if strings.Contains(out, "(expanded earlier this session)") {
			t.Fatalf("render %d: the dedupe fired without a readable state file:\n%s", i+1, out)
		}
	}
}

func TestExpand_NoSessionIdDisablesDedupe(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("BD_SESSION_ID", "")
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "no session", "## Files\n- server/src/auth.ts\n")
	long := "the first line of the ruling " + strings.Repeat("y", 150) + "\nand a second line only --expand shows"
	r := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "expand-me", text: long})

	for i := 0; i < 2; i++ {
		out, errOut, err := runAuthority(t, "authority", bead.ID, "--expand", r.ID)
		requireNoErr(t, err, errOut)
		if !strings.Contains(out, "and a second line only --expand shows") {
			t.Fatalf("render %d: with no session id the dedupe must be off:\n%s", i+1, out)
		}
	}
}

// --- Pre-flight amendment 2-E: a bead in no active plan lane, or a missing
// handoff, must not fail a brief rendered without --since ---

// assertOrderingDegraded checks one degraded handoff resolution: the brief
// rendered, no record carries the * mark, the header reports no typed diff,
// and exactly one stderr line says why.
func assertOrderingDegraded(t *testing.T, out, errOut, why string) {
	t.Helper()
	for _, l := range briefSection(out, "RULINGS") {
		if strings.HasPrefix(l, "*") {
			t.Fatalf("a record carries the * mark with no handoff resolved:\n%s", out)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "BRIEF") && strings.Contains(l, "(since handoff:") {
			t.Fatalf("the typed diff is reported with no handoff resolved:\n%s", out)
		}
	}
	var lines []string
	for _, l := range strings.Split(errOut, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("want exactly one stderr line, got %d (%q)", len(lines), errOut)
	}
	if !strings.Contains(lines[0], "no * marks or typed diff") || !strings.Contains(lines[0], why) {
		t.Fatalf("the stderr line does not say why (%q), want %q", lines[0], why)
	}
}

func TestOrder_AmbiguousPlanDoesNotFailBrief(t *testing.T) {
	ctx := context.Background()
	changedFixture := func(t *testing.T, st *store.Store) string {
		t.Helper()
		bead := mkAuthIssue(t, st, "degraded handoff", "## Files\n- server/src/auth.ts\n")
		row := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the changed ruling", createdAt: orderWindow.Add(-time.Hour)})
		after := orderWindow.Add(time.Hour)
		setStatementChangedAt(t, st, row.ID, &after)
		return bead.ID
	}

	t.Run("two active plans, bead in no lane", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		st := newTempAuthorityStore(t)
		bead := changedFixture(t, st)
		for _, id := range []string{"plan-order-a", "plan-order-b"} {
			if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: id, Title: id}); err != nil {
				t.Fatalf("create %s: %v", id, err)
			}
		}
		out, errOut, err := runAuthority(t, "authority", bead)
		requireNoErr(t, err, errOut)
		assertOrderingDegraded(t, out, errOut, "is in no execution plan lane")
	})

	t.Run("lane without handoff", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		st := newTempAuthorityStore(t)
		bead := changedFixture(t, st)
		if err := st.CreatePlan(ctx, &store.ExecutionPlan{ID: "plan-order-nh", Title: "plan-order-nh", Status: "active"}); err != nil {
			t.Fatalf("create plan: %v", err)
		}
		if err := st.AddLane(ctx, &store.PlanLane{PlanID: "plan-order-nh", Lane: "Z", Queue: []string{bead}}); err != nil {
			t.Fatalf("add lane: %v", err)
		}
		out, errOut, err := runAuthority(t, "authority", bead)
		requireNoErr(t, err, errOut)
		assertOrderingDegraded(t, out, errOut, "lane Z has no handoff")
	})

	t.Run("no active plan", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		st := newTempAuthorityStore(t)
		bead := changedFixture(t, st)
		out, errOut, err := runAuthority(t, "authority", bead)
		requireNoErr(t, err, errOut)
		assertOrderingDegraded(t, out, errOut, "is in no execution plan lane")
	})
}

// --- Behaviour 4: no read tracking ---

func TestOrder_NoReadTracking(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("CLAUDE_SESSION_ID", "no-read-tracking")
	st := newTempAuthorityStore(t)
	bead := mkAuthIssue(t, st, "no read tracking", "## Files\n- server/src/auth.ts\n")
	addSinceHandoff(t, st, "plan-nr", "active", "A", []string{bead.ID}, orderWindow)
	r := mkAuthStatement(t, st, authStatement{kind: "ruling", issueID: bead.ID, topic: "order", text: "the watched ruling", createdAt: orderWindow.Add(time.Hour)})
	after := orderWindow.Add(2 * time.Hour)
	setStatementChangedAt(t, st, r.ID, &after)
	mkAuthStatement(t, st, authStatement{kind: "question", issueID: bead.ID, topic: "order", text: "the watched question", createdAt: orderWindow.Add(time.Hour)})
	mkAuthStatement(t, st, authStatement{kind: "finding", issueID: bead.ID, topic: "order", text: "the watched finding", filedBy: "executor:agent", createdAt: orderWindow.Add(time.Hour)})

	before := dumpOrderTable(t, st, "statements") + dumpOrderTable(t, st, "issues")
	for _, args := range [][]string{
		{"authority", bead.ID},
		{"authority", bead.ID, "--expand", r.ID},
	} {
		_, errOut, err := runAuthority(t, args...)
		requireNoErr(t, err, errOut)
	}
	afterDump := dumpOrderTable(t, st, "statements") + dumpOrderTable(t, st, "issues")
	if before != afterDump {
		t.Fatalf("a brief render wrote to the database:\nbefore:\n%s\nafter:\n%s", before, afterDump)
	}
}
