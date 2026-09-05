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

// newTempTopicsStore opens a fresh store and points the CLI's --db flag at it.
func newTempTopicsStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "topics.db")
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
	// Filing a ruling is actor-gated; the fixtures file as a coordinator so the
	// suite does not depend on the ambient BD_ACTOR.
	t.Setenv("BD_ACTOR", "coordinator")
	return st
}

// runTopicsRoot drives the real root command, so nothing here inherits the
// --topic the legacy fixtures inject through their own runners.
func runTopicsRoot(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	savedDB, savedJSON := flagDB, flagJSON
	root := newRoot()
	flagDB, flagJSON = savedDB, savedJSON
	bufOut, bufErr := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(bufOut)
	root.SetErr(bufErr)
	root.SetArgs(args)
	err := root.Execute()
	return bufOut.String(), bufErr.String(), err
}

func mkTopicsIssue(t *testing.T, st *store.Store, title, desc string) *beads.Issue {
	t.Helper()
	i := &beads.Issue{Title: title, Type: beads.TypeTask, Status: beads.StatusOpen, Priority: 1, Description: desc}
	if err := st.CreateIssue(context.Background(), i); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return i
}

func closeTopicsIssue(t *testing.T, st *store.Store, id string) {
	t.Helper()
	closed := beads.StatusClosed
	if _, err := st.UpdateIssue(context.Background(), id, store.IssueUpdate{Status: &closed}); err != nil {
		t.Fatalf("close %s: %v", id, err)
	}
}

// mkTopicStatement writes one statement straight through the store, for the
// fixtures that need an exact (topic, workspace, concern) triple.
func mkTopicStatement(t *testing.T, st *store.Store, kind, issueID, topic, workspace, concern string) *beads.Statement {
	t.Helper()
	s := &beads.Statement{
		Kind:      kind,
		Text:      kind + " on " + topic,
		FiledBy:   "owner:tester",
		Status:    "active",
		Scope:     "inherit",
		Topic:     topic,
		Workspace: workspace,
		Concern:   concern,
	}
	if issueID != "" {
		id := issueID
		s.IssueID = &id
	}
	if err := st.CreateStatement(context.Background(), s); err != nil {
		t.Fatalf("create %s: %v", kind, err)
	}
	return s
}

func getTopicStatement(t *testing.T, st *store.Store, id string) *beads.Statement {
	t.Helper()
	s, err := st.GetStatement(context.Background(), id)
	if err != nil {
		t.Fatalf("get statement %s: %v", id, err)
	}
	return s
}

func countTopicStatements(t *testing.T, st *store.Store) int {
	t.Helper()
	list, err := st.ListStatements(context.Background(), store.StatementFilter{})
	if err != nil {
		t.Fatalf("list statements: %v", err)
	}
	return len(list)
}

// --- the placeholder slug ---

func TestSlugify_DottedHierarchicalID(t *testing.T) {
	got := pendingTopicSlug("superpowers-bqp.8")
	want := "pending-superpowers-bqp-8"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if err := validateTopicSlug("slug", got); err != nil {
		t.Fatalf("derived slug %q fails validateTopicSlug: %v", got, err)
	}
}

func TestSlugify_UppercaseID(t *testing.T) {
	got := pendingTopicSlug("ZANJIR-4dly.3")
	want := "pending-zanjir-4dly-3"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if err := validateTopicSlug("slug", got); err != nil {
		t.Fatalf("derived slug %q fails validateTopicSlug: %v", got, err)
	}
}

func TestSlugify_PunctuationRunCollapses(t *testing.T) {
	got := pendingTopicSlug("beads-bin..1__sub")
	want := "pending-beads-bin-1-sub"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if err := validateTopicSlug("slug", got); err != nil {
		t.Fatalf("derived slug %q fails validateTopicSlug: %v", got, err)
	}
}

func TestSlugify_TrimsToFortyEightChars(t *testing.T) {
	got := pendingTopicSlug("superpowers-bqp.14.some-very-long-child-suffix-id")
	want := "pending-superpowers-bqp-14-some-very-long-child-"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if len(got) != 48 {
		t.Fatalf("expected 48 chars, got %d (%q)", len(got), got)
	}
	if err := validateTopicSlug("slug", got); err != nil {
		t.Fatalf("derived slug %q fails validateTopicSlug: %v", got, err)
	}
}

// setBogusFlagDB points --db at a path nothing can open, so a verb that
// reaches openStore fails loudly instead of quietly succeeding on a real
// fixture. slugify must derive its string without ever getting there.
func setBogusFlagDB(t *testing.T) {
	t.Helper()
	oldDB, oldJSON := flagDB, flagJSON
	flagDB, flagJSON = "/nonexistent/does-not-exist/beads.db", false
	t.Cleanup(func() { flagDB, flagJSON = oldDB, oldJSON })
}

func TestTopics_SlugifyVerbPrintsSlug(t *testing.T) {
	setBogusFlagDB(t)

	out, _, err := runTopicsRoot(t, "topics", "slugify", "superpowers-bqp.8")
	if err != nil {
		t.Fatalf("slugify: %v", err)
	}
	if out != "pending-superpowers-bqp-8\n" {
		t.Fatalf("got %q", out)
	}
}

func TestTopics_SlugifyVerbRequiresOneArg(t *testing.T) {
	setBogusFlagDB(t)

	if _, _, err := runTopicsRoot(t, "topics", "slugify"); err == nil {
		t.Fatalf("slugify with no argument should be a usage error")
	}
	if _, _, err := runTopicsRoot(t, "topics", "slugify", "one", "two"); err == nil {
		t.Fatalf("slugify with two arguments should be a usage error")
	}
}

// --- the write path ---

func TestTopic_QuestionRequiresTopic(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "needs a topic", "")
	mkTopicStatement(t, st, "question", issue.ID, "candidate-account", "server", "authority")
	before := countTopicStatements(t, st)

	out, errStr, err := runTopicsRoot(t, "question", "add", issue.ID, "a question with no slug")
	if err == nil {
		t.Fatalf("question add without --topic should fail, out %q", out)
	}
	if !strings.Contains(errStr, "--topic is required") {
		t.Fatalf("stderr should carry the refusal, got %q", errStr)
	}
	if !strings.Contains(errStr, "candidate-account") {
		t.Fatalf("stderr should print the catalogue to pick from, got %q", errStr)
	}
	if !strings.Contains(errStr, "Pick one with --topic <slug>") {
		t.Fatalf("stderr should name the way out, got %q", errStr)
	}
	if after := countTopicStatements(t, st); after != before {
		t.Fatalf("refusal should write nothing, before %d after %d", before, after)
	}
}

func TestTopic_FindingRequiresTopic(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "finding needs a topic", "")
	before := countTopicStatements(t, st)

	_, errStr, err := runTopicsRoot(t, "finding", "add", issue.ID, "a finding with no slug")
	if err == nil {
		t.Fatalf("finding add without --topic should fail")
	}
	if !strings.Contains(errStr, "--topic is required") {
		t.Fatalf("stderr should carry the refusal, got %q", errStr)
	}
	if after := countTopicStatements(t, st); after != before {
		t.Fatalf("refusal should write nothing, before %d after %d", before, after)
	}
}

func TestTopic_RejectsBadSlug(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "bad slug", "")

	_, _, err := runTopicsRoot(t, "question", "add", issue.ID, "text", "--topic", "Not A Slug")
	if err == nil {
		t.Fatalf("a slug outside the grammar should be refused")
	}
	if !strings.Contains(err.Error(), "lowercase letters, digits and hyphens, 2-48 chars") {
		t.Fatalf("error should name the character class, got %q", err)
	}
	if !strings.Contains(err.Error(), "Not A Slug") {
		t.Fatalf("error should quote the slug, got %q", err)
	}
	if n := countTopicStatements(t, st); n != 0 {
		t.Fatalf("bad slug should write nothing, got %d statements", n)
	}
}

func TestTopic_RulingInheritsThroughAnswers(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "inherit", "## Files\n\n- server/src/auth.ts\n")

	out, _, err := runTopicsRoot(t, "question", "add", issue.ID, "which delimiter?", "--topic", "mxik-name-drift")
	if err != nil {
		t.Fatalf("question add: %v", err)
	}
	qID := strings.TrimSpace(out)

	out, _, err = runTopicsRoot(t, "ruling", "add", issue.ID, "use the colon", "--answers", qID)
	if err != nil {
		t.Fatalf("ruling add --answers: %v", err)
	}
	r := getTopicStatement(t, st, strings.TrimSpace(out))
	if r.Topic != "mxik-name-drift" {
		t.Fatalf("ruling should inherit the question's topic, got %q", r.Topic)
	}
	if r.Workspace != "server" {
		t.Fatalf("ruling should inherit the question's workspace, got %q", r.Workspace)
	}
	if !strings.Contains(r.Concern, "authority") {
		t.Fatalf("ruling should inherit the question's concerns, got %q", r.Concern)
	}
}

func TestTopic_RulingRejectsContradictingTopic(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "contradiction", "")

	out, _, err := runTopicsRoot(t, "question", "add", issue.ID, "the question", "--topic", "topic-one")
	if err != nil {
		t.Fatalf("question add: %v", err)
	}
	qID := strings.TrimSpace(out)
	before := countTopicStatements(t, st)

	_, _, err = runTopicsRoot(t, "ruling", "add", issue.ID, "the ruling", "--answers", qID, "--topic", "topic-two")
	if err == nil {
		t.Fatalf("a --topic contradicting the answered question should be refused")
	}
	if !strings.Contains(err.Error(), "topic-one") || !strings.Contains(err.Error(), "topic-two") {
		t.Fatalf("error should name both slugs, got %q", err)
	}
	if !strings.Contains(err.Error(), qID) {
		t.Fatalf("error should name the question, got %q", err)
	}
	if after := countTopicStatements(t, st); after != before {
		t.Fatalf("refusal should write nothing, before %d after %d", before, after)
	}
}

func TestTopic_RulingWithoutAnswersRequiresTopic(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "ruling needs a topic", "")

	_, errStr, err := runTopicsRoot(t, "ruling", "add", issue.ID, "a ruling with no slug")
	if err == nil {
		t.Fatalf("ruling add without --topic should fail")
	}
	if !strings.Contains(errStr, "--topic is required") {
		t.Fatalf("stderr should carry the refusal, got %q", errStr)
	}
	if n := countTopicStatements(t, st); n != 0 {
		t.Fatalf("refusal should write nothing, got %d statements", n)
	}

	// A project-scoped ruling is no exception.
	_, errStr, err = runTopicsRoot(t, "ruling", "add", "a project-wide ruling with no slug")
	if err == nil {
		t.Fatalf("project-scoped ruling add without --topic should fail")
	}
	if !strings.Contains(errStr, "--topic is required") {
		t.Fatalf("stderr should carry the refusal for a project ruling, got %q", errStr)
	}
	if n := countTopicStatements(t, st); n != 0 {
		t.Fatalf("refusal should write nothing, got %d statements", n)
	}
}

func TestTopic_CarriesBeadAreas(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "auth work", "## Files\n\n- server/src/auth.ts\n")

	out, _, err := runTopicsRoot(t, "question", "add", issue.ID, "does a candidate keep its row?", "--topic", "candidate-account")
	if err != nil {
		t.Fatalf("question add: %v", err)
	}
	q := getTopicStatement(t, st, strings.TrimSpace(out))
	if q.Workspace != "server" {
		t.Fatalf("workspace should come from the bead's Files section, got %q", q.Workspace)
	}
	if !strings.Contains(q.Concern, "authority") {
		t.Fatalf("concern should contain authority, got %q", q.Concern)
	}
}

func TestTopic_MultiWorkspaceBeadHasNoWorkspace(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "spans two", "## Files\n\n- server/src/auth.ts\n- web-app/src/sessionRole.ts\n")

	out, _, err := runTopicsRoot(t, "question", "add", issue.ID, "who decides?", "--topic", "two-workspaces")
	if err != nil {
		t.Fatalf("question add: %v", err)
	}
	q := getTopicStatement(t, st, strings.TrimSpace(out))
	if q.Workspace != "" {
		t.Fatalf("a bead spanning two workspaces should carry none, got %q", q.Workspace)
	}
	if !strings.Contains(q.Concern, "authority") {
		t.Fatalf("its concerns still apply, got %q", q.Concern)
	}
}

// --- the catalogue ---

func TestTopics_StatusOpenSettledDormant(t *testing.T) {
	st := newTempTopicsStore(t)
	openBead := mkTopicsIssue(t, st, "open bead", "")
	settledBead := mkTopicsIssue(t, st, "settled bead", "")
	dormantBead := mkTopicsIssue(t, st, "dormant bead", "")

	mkTopicStatement(t, st, "question", openBead.ID, "slug-open", "server", "authority")
	mkTopicStatement(t, st, "ruling", settledBead.ID, "slug-settled", "server", "authority")
	mkTopicStatement(t, st, "ruling", dormantBead.ID, "slug-dormant", "server", "authority")
	closeTopicsIssue(t, st, dormantBead.ID)

	out, _, err := runTopicsRoot(t, "topics", "--all")
	if err != nil {
		t.Fatalf("topics: %v", err)
	}
	for _, want := range []struct{ slug, status string }{
		{"slug-open", "open"},
		{"slug-settled", "settled"},
		{"slug-dormant", "dormant"},
	} {
		line := topicLine(t, out, want.slug)
		if !strings.Contains(line, want.status) {
			t.Fatalf("%s should be %s, got line %q", want.slug, want.status, line)
		}
	}
}

func TestTopics_ProjectRulingNeverDormant(t *testing.T) {
	st := newTempTopicsStore(t)
	bead := mkTopicsIssue(t, st, "closed bead", "")
	mkTopicStatement(t, st, "ruling", bead.ID, "slug-project", "server", "authority")
	mkTopicStatement(t, st, "ruling", "", "slug-project", "", "authority")
	closeTopicsIssue(t, st, bead.ID)

	// Every bead carrying the slug is closed, but a project-scoped ruling
	// outlives them all, so the topic stays settled and stays visible.
	out, _, err := runTopicsRoot(t, "topics")
	if err != nil {
		t.Fatalf("topics: %v", err)
	}
	line := topicLine(t, out, "slug-project")
	if !strings.Contains(line, "settled") {
		t.Fatalf("a project-scoped ruling should keep the topic settled, got %q", line)
	}
	if strings.Contains(out, "dormant") {
		t.Fatalf("nothing should be dormant here, got %q", out)
	}
}

func TestTopics_ConcernAndWorkspaceAreAND(t *testing.T) {
	st := newTempTopicsStore(t)
	bead := mkTopicsIssue(t, st, "four topics", "")
	mkTopicStatement(t, st, "question", bead.ID, "slug-server-authority", "server", "authority")
	mkTopicStatement(t, st, "question", bead.ID, "slug-server-catalog", "server", "catalog")
	mkTopicStatement(t, st, "question", bead.ID, "slug-web-authority", "web-app", "authority")
	mkTopicStatement(t, st, "question", bead.ID, "slug-web-catalog", "web-app", "catalog")

	out, _, err := runTopicsRoot(t, "topics", "--concern", "authority", "--workspace", "server")
	if err != nil {
		t.Fatalf("topics: %v", err)
	}
	lines := topicLines(out)
	if len(lines) != 1 {
		t.Fatalf("both filters should AND to one row, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "slug-server-authority") {
		t.Fatalf("wrong row survived: %q", lines[0])
	}
}

func TestTopics_DormantHiddenWithCount(t *testing.T) {
	st := newTempTopicsStore(t)
	live := mkTopicsIssue(t, st, "live bead", "")
	dead := mkTopicsIssue(t, st, "dead bead", "")
	mkTopicStatement(t, st, "question", live.ID, "slug-live", "server", "authority")
	mkTopicStatement(t, st, "ruling", dead.ID, "slug-dead", "server", "authority")
	closeTopicsIssue(t, st, dead.ID)

	out, _, err := runTopicsRoot(t, "topics")
	if err != nil {
		t.Fatalf("topics: %v", err)
	}
	if strings.Contains(out, "slug-dead") {
		t.Fatalf("a dormant topic should be hidden by default, got %q", out)
	}
	if !strings.HasSuffix(out, "(1 dormant)\n") {
		t.Fatalf("output should end with the dormant count, got %q", out)
	}

	out, _, err = runTopicsRoot(t, "topics", "--all")
	if err != nil {
		t.Fatalf("topics --all: %v", err)
	}
	if !strings.Contains(out, "slug-dead") {
		t.Fatalf("--all should show the dormant topic, got %q", out)
	}
	if strings.Contains(out, "dormant)") {
		t.Fatalf("--all hides nothing, so it should print no count line, got %q", out)
	}
}

func TestTopics_SupersededChainCompacts(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "chain", "")

	out, _, err := runTopicsRoot(t, "ruling", "add", issue.ID, "the first answer", "--topic", "slug-chain")
	if err != nil {
		t.Fatalf("first ruling: %v", err)
	}
	r1 := strings.TrimSpace(out)
	out, _, err = runTopicsRoot(t, "ruling", "add", issue.ID, "the amended answer", "--topic", "slug-chain", "--supersedes", r1)
	if err != nil {
		t.Fatalf("second ruling: %v", err)
	}
	r2 := strings.TrimSpace(out)

	out, _, err = runTopicsRoot(t, "topics")
	if err != nil {
		t.Fatalf("topics: %v", err)
	}
	lines := topicLines(out)
	if len(lines) != 1 {
		t.Fatalf("a superseded chain is one row, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], r2) {
		t.Fatalf("the row should name the terminal ruling %s, got %q", r2, lines[0])
	}
	if strings.Contains(lines[0], r1+" ") || strings.HasSuffix(lines[0], r1) {
		t.Fatalf("the row should not name the superseded ruling %s, got %q", r1, lines[0])
	}

	// The same chain written straight through the store, where the superseded
	// row keeps status active: the row still names the terminal ruling.
	other := mkTopicsIssue(t, st, "raw chain", "")
	first := mkTopicStatement(t, st, "ruling", other.ID, "slug-raw", "server", "authority")
	second := &beads.Statement{
		Kind: "ruling", IssueID: &other.ID, Text: "the raw amendment", FiledBy: "owner:tester",
		Status: "active", Scope: "inherit", Topic: "slug-raw", Workspace: "server", Concern: "authority",
		SupersedesID: &first.ID,
	}
	if err := st.CreateStatement(context.Background(), second); err != nil {
		t.Fatalf("create superseding ruling: %v", err)
	}
	// Filing the supersede flips the old row to superseded; put it back to
	// active, which is the shape the terminal reduction exists for.
	if err := st.UpdateStatementStatus(context.Background(), first.ID, "active"); err != nil {
		t.Fatalf("reactivate: %v", err)
	}

	out, _, err = runTopicsRoot(t, "topics", "--all")
	if err != nil {
		t.Fatalf("topics: %v", err)
	}
	line := topicLine(t, out, "slug-raw")
	if !strings.HasSuffix(line, second.ID) {
		t.Fatalf("the row should name the terminal ruling %s, got %q", second.ID, line)
	}
}

// Behaviour 9's second half: duplicate questions on one slug compact to one
// row, and the row says how many there were.
func TestTopics_DuplicateQuestionsMarkCount(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "asked twice", "")
	mkTopicStatement(t, st, "question", issue.ID, "slug-twice", "server", "authority")
	mkTopicStatement(t, st, "question", issue.ID, "slug-twice", "server", "authority")

	out, _, err := runTopicsRoot(t, "topics")
	if err != nil {
		t.Fatalf("topics: %v", err)
	}
	lines := topicLines(out)
	if len(lines) != 1 {
		t.Fatalf("two questions on one slug are one row, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "open ×2") {
		t.Fatalf("the row should mark the duplicate count after the status, got %q", lines[0])
	}
}

// --- rename and merge ---

func TestTopics_RenameMovesRows(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "rename", "")
	a := mkTopicStatement(t, st, "question", issue.ID, "slug-old", "server", "authority")
	b := mkTopicStatement(t, st, "ruling", issue.ID, "slug-old", "server", "authority")

	out, _, err := runTopicsRoot(t, "topics", "rename", "slug-old", "slug-new")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !strings.Contains(out, "slug-old") || !strings.Contains(out, "slug-new") {
		t.Fatalf("rename should report both slugs, got %q", out)
	}
	for _, id := range []string{a.ID, b.ID} {
		if got := getTopicStatement(t, st, id).Topic; got != "slug-new" {
			t.Fatalf("statement %s should carry the new slug, got %q", id, got)
		}
	}
}

func TestTopics_RenameRefusesExisting(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "collide", "")
	keep := mkTopicStatement(t, st, "question", issue.ID, "slug-one", "server", "authority")
	mkTopicStatement(t, st, "question", issue.ID, "slug-two", "server", "authority")

	_, _, err := runTopicsRoot(t, "topics", "rename", "slug-one", "slug-two")
	if err == nil {
		t.Fatalf("renaming onto a live slug should be refused")
	}
	if !strings.Contains(err.Error(), "merge") {
		t.Fatalf("the refusal should name merge as the way to do it, got %q", err)
	}
	if got := getTopicStatement(t, st, keep.ID).Topic; got != "slug-one" {
		t.Fatalf("the refused rename should have moved nothing, got %q", got)
	}
}

func TestTopics_MergeCountsRows(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "merge", "")
	a := mkTopicStatement(t, st, "question", issue.ID, "slug-from", "server", "authority")
	b := mkTopicStatement(t, st, "ruling", issue.ID, "slug-from", "server", "authority")
	mkTopicStatement(t, st, "question", issue.ID, "slug-into", "server", "authority")

	out, _, err := runTopicsRoot(t, "topics", "merge", "slug-from", "slug-into")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !strings.Contains(out, "2") {
		t.Fatalf("merge should print the moved-row count, got %q", out)
	}
	for _, id := range []string{a.ID, b.ID} {
		if got := getTopicStatement(t, st, id).Topic; got != "slug-into" {
			t.Fatalf("statement %s should have moved, got %q", id, got)
		}
	}
}

func TestTopics_RenameRefusesExecutor(t *testing.T) {
	st := newTempTopicsStore(t)
	issue := mkTopicsIssue(t, st, "gated", "")
	keep := mkTopicStatement(t, st, "question", issue.ID, "slug-gated", "server", "authority")
	t.Setenv("BD_ACTOR", "executor")

	_, _, err := runTopicsRoot(t, "topics", "rename", "slug-gated", "slug-renamed")
	if err == nil {
		t.Fatalf("an executor should not rename a topic")
	}
	if !strings.Contains(err.Error(), "executors cannot file rulings") {
		t.Fatalf("the refusal should be the shared one, got %q", err)
	}
	if _, _, err := runTopicsRoot(t, "topics", "merge", "slug-gated", "slug-other"); err == nil {
		t.Fatalf("an executor should not merge topics")
	}
	if got := getTopicStatement(t, st, keep.ID).Topic; got != "slug-gated" {
		t.Fatalf("the refused commands should have moved nothing, got %q", got)
	}
}

// topicLines returns the table rows, dropping the trailing count line.
func topicLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l == "" || strings.HasPrefix(l, "(") {
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

func topicLine(t *testing.T, out, slug string) string {
	t.Helper()
	for _, l := range topicLines(out) {
		if strings.HasPrefix(l, slug+" ") || l == slug {
			return l
		}
	}
	t.Fatalf("no row for %s in:\n%s", slug, out)
	return ""
}
