package store

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/rsktash/beads"
)

// The authority brief is one read that answers "what binds this bead": the
// rulings it inherits, the topics its areas already opened, what is still
// asked, what execution found, the project-wide doctrine over its areas, and
// what it links to. Brief gathers; it never caps and never renders. The
// per-section caps and the byte cap belong to the text renderer, so `--json`
// can emit the same structure uncapped.

// Brief section names. These are the `--kind` vocabulary: naming one narrows
// the brief to it. briefKindTopics is deliberately outside that vocabulary —
// TOPIC names no record kind, so it renders only in an unfiltered brief.
const (
	BriefKindRulings   = "rulings"
	BriefKindQuestions = "questions"
	BriefKindFindings  = "findings"
	BriefKindComments  = "comments"
	BriefKindBeads     = "beads"
	BriefKindDoctrine  = "doctrine"

	briefKindTopics = "topics"
)

// BriefKinds is the accepted `--kind` vocabulary, in the order the sections
// are printed.
var BriefKinds = []string{
	BriefKindRulings, BriefKindQuestions, BriefKindFindings,
	BriefKindComments, BriefKindBeads, BriefKindDoctrine,
}

// briefMaxDepth bounds --depth: three hops already reaches an epic's siblings,
// and every further hop multiplies the RELATED section by the graph's fan-out.
const briefMaxDepth = 3

// BriefRequest is one brief's question. Either IssueID or Concern is set —
// with neither, the caller has not resolved an area and the command refuses
// rather than guessing terms.
type BriefRequest struct {
	// IssueID is the bead the brief is about; empty for a words query.
	IssueID string
	// Words is the words query, and the words RELATED matches memories on.
	Words []string
	// Kinds narrows the brief to the named sections; empty means every one.
	Kinds []string
	// Workspace and Concern override the areas the bead resolves to. For a
	// words query Concern is the area the whole brief is drawn from.
	Workspace string
	Concern   string
	// Topic keeps only records carrying this slug.
	Topic string
	// Status keeps only statements with this status ("open" is accepted for
	// an active question, the word the brief prints).
	Status string
	// Author is owner, agent, or any exact actor word.
	Author string
	// Since drops every record older than this instant.
	Since time.Time
	// SinceLabel is the render-only description shown on section headers.
	SinceLabel string
	// Handoff is the lane's last handoff instant when the caller resolved
	// one. The brief orders records changed after it first within a
	// distance band, counts the typed diff from it, and BriefResult
	// carries it back for the renderer's * marks. Nil orders by distance
	// and recency alone.
	Handoff *time.Time
	// Depth caps link hops in RELATED; 0 means the default of one.
	Depth int
	// Grep filters the built sections by case-insensitive substring.
	Grep string
}

// Wants reports whether a section survives the --kind filter. Renderers ask
// it too, so the flag is read in exactly one place.
func (r BriefRequest) Wants(section string) bool {
	if len(r.Kinds) == 0 {
		return true
	}
	for _, k := range r.Kinds {
		if k == section {
			return true
		}
	}
	return false
}

// DoctrineRow is one project-scoped law. Law is the stored one-line statement
// of the rule; nothing writes that column yet, so Text carries the ruling text
// the renderer falls back to.
type DoctrineRow struct {
	ID        string    `json:"id"`
	Concern   string    `json:"concern,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	Law       string    `json:"law,omitempty"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	FiledBy   string    `json:"filed_by,omitempty"`
}

// RelatedBead is one bead reached over a dependency edge, with the hop count
// that reached it.
type RelatedBead struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Depth  int    `json:"depth"`
}

// BriefResult is everything the brief found, uncapped.
type BriefResult struct {
	IssueID  string `json:"issue_id,omitempty"`
	Title    string `json:"title,omitempty"`
	Status   string `json:"status,omitempty"`
	Type     string `json:"type,omitempty"`
	Priority int    `json:"priority,omitempty"`

	// Query is what RELATED matches on: the words query, or the bead title's
	// significant words.
	Query      []string `json:"query,omitempty"`
	Workspaces []string `json:"workspaces,omitempty"`
	Concerns   []string `json:"concerns,omitempty"`

	Rulings   []beads.Statement `json:"rulings,omitempty"`
	Topics    []TopicRow        `json:"topics,omitempty"`
	Questions []beads.Statement `json:"questions,omitempty"`
	Findings  []beads.Statement `json:"findings,omitempty"`

	Doctrine []DoctrineRow `json:"doctrine,omitempty"`
	// DoctrineOutside counts the active project laws outside these areas.
	// They are a count, never lines: a law that does not bind this bead costs
	// context without changing what the reader must do.
	DoctrineOutside int `json:"doctrine_outside,omitempty"`

	// Handoff is the ordering instant — the lane's last handoff. Nil means
	// none resolved: no record is marked and no typed diff is reported.
	Handoff *time.Time `json:"handoff,omitempty"`
	// Diff counts what changed on the bead since Handoff. Zero when
	// Handoff is nil.
	Diff BriefDiff `json:"diff,omitempty"`

	Related  []RelatedBead   `json:"related,omitempty"`
	Memories []beads.Memory  `json:"memories,omitempty"`
	Comments []beads.Comment `json:"comments,omitempty"`
}

// BriefDiff is the typed diff the brief header reports since the lane's
// last handoff: four counts, one per record kind plus closed dependencies.
type BriefDiff struct {
	// Rulings counts rulings binding the bead whose change instant
	// (changed_at, else created_at) falls after the handoff.
	Rulings int `json:"rulings"`
	// QuestionsAnswered counts questions of the bead whose status is
	// answered and whose change instant falls after the handoff.
	QuestionsAnswered int `json:"questions_answered"`
	// Findings counts findings of the bead filed after the handoff.
	Findings int `json:"findings"`
	// DepsClosed counts the bead's dependencies whose depends-on bead
	// closed at or after the handoff.
	DepsClosed int `json:"deps_closed"`
}

// Brief gathers the authority brief for one bead or one words query.
func (s *Store) Brief(ctx context.Context, req BriefRequest) (BriefResult, error) {
	var res BriefResult
	res.Query = req.Words

	workspaces, concerns, err := s.briefAreas(ctx, req, &res)
	if err != nil {
		return res, err
	}
	res.Workspaces, res.Concerns = workspaces, concerns

	// One statements scan feeds every section: the resolver view carries
	// neither topic nor law, the topic union needs every ruling, and doctrine
	// needs every project-scoped one.
	all, err := s.ListStatements(ctx, StatementFilter{})
	if err != nil {
		return res, err
	}
	byID := make(map[string]beads.Statement, len(all))
	superseded := map[string]bool{}
	for _, st := range all {
		byID[st.ID] = st
		if st.Kind == "ruling" && st.Status == "active" && st.SupersedesID != nil && *st.SupersedesID != "" {
			superseded[*st.SupersedesID] = true
		}
	}

	if err := s.briefStatements(ctx, req, &res, all, byID, superseded, concerns); err != nil {
		return res, err
	}
	if req.Wants(briefKindTopics) {
		res.Topics, err = s.briefTopics(ctx, req, all, workspaces, concerns)
		if err != nil {
			return res, err
		}
	}
	if req.Wants(BriefKindDoctrine) {
		res.Doctrine, res.DoctrineOutside = briefDoctrine(req, all, superseded, workspaces, concerns)
	}
	if req.Wants(BriefKindBeads) {
		if err := s.briefRelated(ctx, req, &res); err != nil {
			return res, err
		}
	}
	if req.Wants(BriefKindComments) && req.IssueID != "" {
		cs, err := s.ListComments(ctx, req.IssueID)
		if err != nil {
			return res, err
		}
		res.Comments = briefFilterComments(req, cs)
	}
	return res, nil
}

// briefAreas resolves the areas every structural match is made over, and fills
// the header fields. An explicit --workspace or --concern replaces what the
// bead resolves to rather than adding to it: the flag exists to look at the
// bead from one area.
func (s *Store) briefAreas(ctx context.Context, req BriefRequest, res *BriefResult) (workspaces, concerns []string, err error) {
	if req.IssueID != "" {
		i, err := s.GetIssue(ctx, req.IssueID)
		if err != nil {
			return nil, nil, err
		}
		res.IssueID, res.Title = i.ID, i.Title
		res.Status, res.Type, res.Priority = string(i.Status), string(i.Type), i.Priority
		if len(res.Query) == 0 {
			res.Query = SignificantWords(i.Title)
		}
		workspaces, concerns, err = s.ResolveIssue(ctx, i.ID)
		if err != nil {
			return nil, nil, err
		}
	}
	if req.Workspace != "" {
		workspaces = []string{req.Workspace}
	}
	if req.Concern != "" {
		concerns = []string{req.Concern}
	}
	return workspaces, concerns, nil
}

// briefStatements fills RULINGS, QUESTIONS and FINDINGS.
func (s *Store) briefStatements(ctx context.Context, req BriefRequest, res *BriefResult,
	all []beads.Statement, byID map[string]beads.Statement, superseded map[string]bool, concerns []string) error {

	wantR, wantQ, wantF := req.Wants(BriefKindRulings), req.Wants(BriefKindQuestions), req.Wants(BriefKindFindings)

	if req.IssueID == "" {
		if !wantR && !wantQ && !wantF {
			return nil
		}
		// Words query: the concern is the whole scope.
		for _, st := range all {
			// A backfill candidate has not been promoted to authority yet.
			if st.Status == "candidate" {
				continue
			}
			if st.Kind == "ruling" && (st.Status != "active" || superseded[st.ID]) {
				continue
			}
			if !briefAreaMatches(st.Workspace, st.Concern, nil, concerns) {
				continue
			}
			switch st.Kind {
			case "ruling":
				if wantR {
					res.Rulings = append(res.Rulings, st)
				}
			case "question":
				if wantQ {
					res.Questions = append(res.Questions, st)
				}
			case "finding":
				if wantF {
					res.Findings = append(res.Findings, st)
				}
			}
		}
		res.Rulings = briefFilterStatements(req, res.Rulings)
		res.Questions = briefFilterStatements(req, res.Questions)
		res.Findings = briefFilterStatements(req, res.Findings)
		// No bead means no distance: change-then-recency is the whole order.
		sortBriefStatements(res.Rulings, nil, req.Handoff)
		sortBriefStatements(res.Questions, nil, req.Handoff)
		sortBriefStatements(res.Findings, nil, req.Handoff)
		return nil
	}

	if !wantR && !wantQ && !wantF && req.Handoff == nil {
		return nil
	}
	cv, err := s.ContractStatements(ctx, req.IssueID)
	if err != nil {
		return err
	}

	// Every ruling that binds the bead, enriched with the columns the
	// resolver view does not select: it feeds RULINGS and the typed diff
	// alike, whether or not the section survives the --kind filter.
	binding := make([]beads.Statement, 0, len(cv.Rulings))
	seen := map[string]bool{}
	for _, r := range cv.Rulings {
		row := r
		briefEnrich(&row, byID[r.ID])
		seen[r.ID] = true
		binding = append(binding, row)
	}
	// Rule 4's union: a ruling filed elsewhere on a topic this bead
	// carries binds it just as surely as one it inherits.
	for _, slug := range briefBeadTopics(all, req.IssueID) {
		for _, st := range all {
			if st.Kind != "ruling" || st.Status != "active" || seen[st.ID] || superseded[st.ID] {
				continue
			}
			if st.Topic != slug {
				continue
			}
			seen[st.ID] = true
			binding = append(binding, st)
		}
	}

	if req.Handoff != nil {
		res.Handoff = req.Handoff
		res.Diff = briefTypedDiff(binding, cv, *req.Handoff)
		res.Diff.DepsClosed, err = s.countDepsClosedSince(ctx, req.IssueID, *req.Handoff)
		if err != nil {
			return err
		}
	}

	if !wantR && !wantQ && !wantF {
		return nil
	}
	if wantR {
		res.Rulings = briefFilterStatements(req, binding)
		sortBriefStatements(res.Rulings, cv.Origins, req.Handoff)
	}
	if wantQ {
		for _, q := range append(append([]beads.Statement{}, cv.Questions...), cv.ClosedQuestions...) {
			row := q
			briefEnrich(&row, byID[q.ID])
			res.Questions = append(res.Questions, row)
		}
		res.Questions = briefFilterStatements(req, res.Questions)
		sortBriefStatements(res.Questions, cv.Origins, req.Handoff)
	}
	if wantF {
		for _, f := range cv.Findings {
			row := f
			briefEnrich(&row, byID[f.ID])
			res.Findings = append(res.Findings, row)
		}
		res.Findings = briefFilterStatements(req, res.Findings)
		sortBriefStatements(res.Findings, cv.Origins, req.Handoff)
	}
	return nil
}

// briefBeadTopics is the set of slugs the bead's own statements carry, in
// first-seen order.
func briefBeadTopics(all []beads.Statement, issueID string) []string {
	seen := map[string]bool{}
	var out []string
	for _, st := range all {
		if st.Topic == "" || st.IssueID == nil || *st.IssueID != issueID || seen[st.Topic] {
			continue
		}
		seen[st.Topic] = true
		out = append(out, st.Topic)
	}
	return out
}

// briefEnrich copies the columns the resolver view does not select onto a
// statement it returned. The origin fields (IssueID, the bracket the renderer
// prints) are the view's and are left alone.
func briefEnrich(dst *beads.Statement, src beads.Statement) {
	if src.ID == "" {
		return
	}
	dst.Topic, dst.Workspace, dst.Concern = src.Topic, src.Workspace, src.Concern
	dst.Law, dst.Rationale, dst.Author = src.Law, src.Rationale, src.Author
	dst.Status, dst.SupersedesID, dst.AnsweredBy = src.Status, src.SupersedesID, src.AnsweredBy
	dst.ChangedAt = src.ChangedAt
	if dst.Verbatim == "" {
		dst.Verbatim = src.Verbatim
	}
}

// briefTopics is rule 5: the TOPIC section is filled by structure, never by
// wording. A topic is a candidate when the bead or its epic chain carries the
// slug, when its concerns meet the bead's, when its workspace is the bead's,
// or when it is shared or all. Rows are ordered by how many of those ways they
// matched, then by recency.
func (s *Store) briefTopics(ctx context.Context, req BriefRequest, all []beads.Statement, workspaces, concerns []string) ([]TopicRow, error) {
	rows, err := s.ListTopics(ctx, TopicFilter{})
	if err != nil {
		return nil, err
	}

	chain := map[string]bool{}
	if req.IssueID != "" {
		chain[req.IssueID] = true
		ancestors, err := s.Ancestors(ctx, req.IssueID)
		if err != nil {
			return nil, err
		}
		for _, id := range ancestors {
			chain[id] = true
		}
	}
	attached := map[string]bool{}
	recent := map[string]time.Time{}
	for _, st := range all {
		if st.Topic == "" {
			continue
		}
		if st.CreatedAt.After(recent[st.Topic]) {
			recent[st.Topic] = st.CreatedAt
		}
		if st.IssueID != nil && chain[*st.IssueID] {
			attached[st.Topic] = true
		}
	}

	wantWS := map[string]bool{}
	for _, w := range workspaces {
		wantWS[w] = true
	}
	wantC := map[string]bool{}
	for _, c := range concerns {
		wantC[c] = true
	}

	type scored struct {
		row     TopicRow
		matches int
	}
	var picked []scored
	for _, r := range rows {
		if req.Topic != "" && r.Slug != req.Topic {
			continue
		}
		n := 0
		if attached[r.Slug] {
			n++
		}
		for _, c := range r.Concerns {
			if wantC[c] {
				n++
				break
			}
		}
		for _, w := range r.Workspaces {
			if wantWS[w] {
				n++
				break
			}
		}
		if briefIsShared(r.Workspaces, r.Concerns) {
			n++
		}
		if n == 0 {
			continue
		}
		if req.Grep != "" && !briefGrepHit(req.Grep, r.Slug, r.Question, r.RulingID) {
			continue
		}
		picked = append(picked, scored{row: r, matches: n})
	}
	sort.SliceStable(picked, func(i, j int) bool {
		if picked[i].matches != picked[j].matches {
			return picked[i].matches > picked[j].matches
		}
		ri, rj := recent[picked[i].row.Slug], recent[picked[j].row.Slug]
		if !ri.Equal(rj) {
			return ri.After(rj)
		}
		return picked[i].row.Slug < picked[j].row.Slug
	})
	out := make([]TopicRow, 0, len(picked))
	for _, p := range picked {
		out = append(out, p.row)
	}
	return out, nil
}

// briefIsShared reports the areas that bind everything: the shared workspace
// and the all concern.
func briefIsShared(workspaces, concerns []string) bool {
	for _, w := range workspaces {
		if w == "shared" {
			return true
		}
	}
	for _, c := range concerns {
		if c == AreaAll {
			return true
		}
	}
	return false
}

// briefAreaMatches reports whether a statement's own areas meet the brief's,
// counting shared and all as a match against any area.
func briefAreaMatches(workspace, concern string, workspaces, concerns []string) bool {
	sc := splitConcerns(concern)
	var sw []string
	if workspace != "" {
		sw = []string{workspace}
	}
	if briefIsShared(sw, sc) {
		return true
	}
	for _, c := range sc {
		for _, want := range concerns {
			if c == want {
				return true
			}
		}
	}
	for _, want := range workspaces {
		if workspace != "" && workspace == want {
			return true
		}
	}
	return false
}

// briefDoctrine is rule 8: the active project-scoped laws over these areas,
// and a count of every other one.
func briefDoctrine(req BriefRequest, all []beads.Statement, superseded map[string]bool, workspaces, concerns []string) ([]DoctrineRow, int) {
	var rows []DoctrineRow
	outside := 0
	for _, st := range all {
		if st.Kind != "ruling" || st.Status != "active" || superseded[st.ID] {
			continue
		}
		if st.IssueID != nil && *st.IssueID != "" {
			continue
		}
		if !briefAreaMatches(st.Workspace, st.Concern, workspaces, concerns) {
			outside++
			continue
		}
		if !briefKeepStatement(req, st) {
			continue
		}
		rows = append(rows, DoctrineRow{
			ID:        st.ID,
			Concern:   st.Concern,
			Workspace: st.Workspace,
			Law:       st.Law,
			Text:      st.Text,
			CreatedAt: st.CreatedAt,
			FiledBy:   st.FiledBy,
		})
	}
	return rows, outside
}

// briefRelated is rule 9's first two entries: the beads a dependency edge
// reaches within --depth hops, and the memories the query words hit. The
// transcript pointer is the renderer's — this never opens one.
func (s *Store) briefRelated(ctx context.Context, req BriefRequest, res *BriefResult) error {
	depth := req.Depth
	if depth <= 0 {
		depth = 1
	}
	if depth > briefMaxDepth {
		depth = briefMaxDepth
	}

	if req.IssueID != "" {
		seen := map[string]bool{req.IssueID: true}
		frontier := []string{req.IssueID}
		for hop := 1; hop <= depth && len(frontier) > 0; hop++ {
			var next []string
			for _, id := range frontier {
				deps, err := s.ListDependencies(ctx, id)
				if err != nil {
					return err
				}
				for _, d := range deps {
					other := d.DependsOnID
					if other == id {
						other = d.IssueID
					}
					if other == "" || seen[other] {
						continue
					}
					seen[other] = true
					next = append(next, other)
					i, err := s.GetIssue(ctx, other)
					if err != nil {
						continue
					}
					res.Related = append(res.Related, RelatedBead{
						ID: i.ID, Status: string(i.Status), Type: string(i.Type),
						Title: i.Title, Depth: hop,
					})
				}
			}
			frontier = next
		}
		sort.SliceStable(res.Related, func(i, j int) bool {
			if res.Related[i].Depth != res.Related[j].Depth {
				return res.Related[i].Depth < res.Related[j].Depth
			}
			return res.Related[i].ID < res.Related[j].ID
		})
		if req.Grep != "" {
			var kept []RelatedBead
			for _, r := range res.Related {
				if briefGrepHit(req.Grep, r.ID, r.Title) {
					kept = append(kept, r)
				}
			}
			res.Related = kept
		}
	}

	memories, err := s.ListMemories(ctx, "")
	if err != nil {
		return err
	}
	for _, m := range memories {
		if !briefWordHit(res.Query, m.Key, m.Value) {
			continue
		}
		if req.Grep != "" && !briefGrepHit(req.Grep, m.Key, m.Value) {
			continue
		}
		res.Memories = append(res.Memories, m)
	}
	return nil
}

// briefFilterStatements applies --topic, --status, --author, --since and
// --grep to a built section, in that order.
func briefFilterStatements(req BriefRequest, in []beads.Statement) []beads.Statement {
	var out []beads.Statement
	for _, st := range in {
		if briefKeepStatement(req, st) {
			out = append(out, st)
		}
	}
	return out
}

func briefKeepStatement(req BriefRequest, st beads.Statement) bool {
	if req.Topic != "" && st.Topic != req.Topic {
		return false
	}
	if req.Status != "" && !briefStatusMatches(req.Status, st.Status) {
		return false
	}
	if req.Author != "" && !briefAuthorMatches(req.Author, st.FiledBy) {
		return false
	}
	windowAt := st.CreatedAt
	if st.ChangedAt != nil {
		windowAt = *st.ChangedAt
	}
	if !req.Since.IsZero() && windowAt.Before(req.Since) {
		return false
	}
	if req.Grep != "" && !briefGrepHit(req.Grep, st.Law, st.Text) {
		return false
	}
	return true
}

func briefFilterComments(req BriefRequest, in []beads.Comment) []beads.Comment {
	var out []beads.Comment
	for _, c := range in {
		if req.Author != "" && !briefAuthorMatches(req.Author, c.Author) {
			continue
		}
		if !req.Since.IsZero() && c.CreatedAt.Before(req.Since) {
			continue
		}
		if req.Grep != "" && !briefGrepHit(req.Grep, c.Text) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// briefStatusMatches accepts "open" for an active question — the word the
// brief prints in the status column, so a reader can filter on what they read.
func briefStatusMatches(want, have string) bool {
	if want == "open" {
		return have == "active"
	}
	return want == have
}

// briefAuthorMatches reads owner as the exact actor word and agent as
// everything that is not the owner; any other value is an exact actor word.
func briefAuthorMatches(want, filedBy string) bool {
	word := authorFromFiledBy(filedBy)
	if want == "agent" {
		return word != "owner"
	}
	return word == want
}

func briefGrepHit(kw string, fields ...string) bool {
	kw = strings.ToLower(kw)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), kw) {
			return true
		}
	}
	return false
}

// briefWordHit reports whether any query word appears in the fields. An empty
// query matches nothing: a brief that cannot say what it is looking for should
// not dump every memory in the project.
func briefWordHit(words []string, fields ...string) bool {
	for _, w := range words {
		if briefGrepHit(w, fields...) {
			return true
		}
	}
	return false
}

// briefStopWords are the words a title shares with every other title; matching
// on them would return the whole project.
var briefStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"that": true, "this": true, "into": true, "onto": true, "its": true,
	"task": true, "bead": true, "add": true, "new": true, "use": true,
}

// SignificantWords reduces a title to the words worth matching on: lowercase,
// at least three runes, not a stop word, at most six of them.
func SignificantWords(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		f = strings.Trim(f, "-_")
		if len([]rune(f)) < 3 || briefStopWords[f] {
			continue
		}
		out = append(out, f)
		if len(out) == 6 {
			break
		}
	}
	return out
}

// Binding distances. The SQL CASE in ContractStatements fixes the order of
// the origin kinds (self < binds < epic < blocks < project); these bands
// preserve that order — the depth-8 test is the guard. Epic distance is
// 2 + min(depth, 7), so the deepest epic band (9) stays strictly before
// blocks (10); project, and any record the resolver did not reach (the
// topic union's rulings, closed questions), is 20.
const (
	distanceSelf    = 0
	distanceBinds   = 1
	distanceEpic    = 2
	distanceBlocks  = 10
	distanceProject = 20
)

// statementDistance maps a resolver origin onto its band. The band numbers
// are a rendering detail; the order they impose is the SQL's.
func statementDistance(o StatementOrigin) int {
	switch o.Kind {
	case "self":
		return distanceSelf
	case "binds":
		return distanceBinds
	case "epic":
		return distanceEpic + min(o.Depth, 7)
	case "blocks":
		return distanceBlocks
	default:
		return distanceProject
	}
}

// statementInstant is a record's change instant: changed_at when the row
// carries one, else created_at.
func statementInstant(st beads.Statement) time.Time {
	if st.ChangedAt != nil {
		return *st.ChangedAt
	}
	return st.CreatedAt
}

// sortBriefStatements orders one section's records by binding distance
// (nearest first), then changed-since-handoff first, then created_at
// descending, then id descending — so ordering never depends on row
// insertion. A nil origins map (a words query) has no distance at all:
// the comparator falls through to change, then recency.
func sortBriefStatements(rows []beads.Statement, origins map[string]StatementOrigin, handoff *time.Time) {
	sort.SliceStable(rows, func(i, j int) bool {
		if origins != nil {
			di, dj := statementDistance(origins[rows[i].ID]), statementDistance(origins[rows[j].ID])
			if di != dj {
				return di < dj
			}
		}
		if handoff != nil {
			ci, cj := statementInstant(rows[i]).After(*handoff), statementInstant(rows[j]).After(*handoff)
			if ci != cj {
				return ci
			}
		}
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		}
		return rows[i].ID > rows[j].ID
	})
}

// briefTypedDiff counts the three statement kinds of the typed diff from
// the bead's binding records; the closed-dependencies count is a query and
// is filled by the caller.
func briefTypedDiff(rulings []beads.Statement, cv ContractView, handoff time.Time) BriefDiff {
	var d BriefDiff
	for _, r := range rulings {
		if statementInstant(r).After(handoff) {
			d.Rulings++
		}
	}
	for _, q := range append(append([]beads.Statement{}, cv.Questions...), cv.ClosedQuestions...) {
		if q.Status != "answered" {
			continue
		}
		if statementInstant(q).After(handoff) {
			d.QuestionsAnswered++
		}
	}
	for _, f := range cv.Findings {
		if f.CreatedAt.After(handoff) {
			d.Findings++
		}
	}
	return d
}

// countDepsClosedSince counts the bead's dependencies whose depends-on bead
// closed at or after the instant — the fourth count of the typed diff, one
// query.
func (s *Store) countDepsClosedSince(ctx context.Context, issueID string, at time.Time) (int, error) {
	q := s.rebind(`SELECT COUNT(*) FROM dependencies d JOIN issues i ON i.id = d.depends_on_id WHERE d.issue_id = ? AND i.closed_at IS NOT NULL AND i.closed_at >= ?`)
	var n int
	if err := s.db.QueryRowContext(ctx, q, issueID, at).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// LastHandoffForBead is what `--since handoff` resolves to: the newest handoff
// on the bead's lane, searching every active plan's lanes in id order. lane is
// returned even when it has no handoff so the caller can name the missing
// handoff precisely.
func (s *Store) LastHandoffForBead(ctx context.Context, issueID string) (at time.Time, lane string, ok bool, err error) {
	plans, err := s.ActivePlans(ctx)
	if err != nil {
		return time.Time{}, "", false, err
	}
	for _, plan := range plans {
		lanes, err := s.ListLanes(ctx, plan.ID)
		if err != nil {
			return time.Time{}, "", false, err
		}
		for _, candidate := range lanes {
			for _, id := range candidate.Queue {
				if id != issueID {
					continue
				}
				at, err := s.LastHandoffAt(ctx, plan.ID, candidate.Lane)
				if err != nil {
					return time.Time{}, candidate.Lane, false, err
				}
				if at == nil {
					return time.Time{}, candidate.Lane, false, nil
				}
				return *at, candidate.Lane, true, nil
			}
		}
	}
	return time.Time{}, "", false, nil
}
