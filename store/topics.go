package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Topic statuses. A topic has no state of its own: its status is a function of
// the questions, rulings and beads that carry the slug, recomputed on read.
const (
	TopicOpen    = "open"
	TopicSettled = "settled"
	TopicDormant = "dormant"
)

// TopicFilter narrows ListTopics. An empty field is not a filter; both fields
// set means AND — one statement carrying the slug must satisfy both.
type TopicFilter struct {
	// Workspace matches a statement whose workspace column is exactly this.
	Workspace string
	// Concern matches a statement whose comma-separated concern list has this
	// as one of its entries.
	Concern string
}

// TopicRow is one line of the catalogue: the slug, its derived status, the
// question that opened it and the ruling that settled it.
type TopicRow struct {
	Slug   string `json:"slug"`
	Status string `json:"status"`
	// Question is the representative question's raw text: the oldest active
	// one, or the oldest of any status once the topic is settled. Empty when
	// no question ever carried the slug.
	Question string `json:"question,omitempty"`
	// RulingID is the terminal active ruling on the slug, empty when none.
	RulingID string `json:"ruling_id,omitempty"`
	// Questions counts the active questions carrying the slug. More than one
	// is the duplicate case the caller marks with ×N.
	Questions int `json:"questions"`
	// Workspaces and Concerns are the distinct areas the slug's statements
	// carry, so a caller can filter on areas the TopicFilter cannot express
	// (the union a bead's own areas need).
	Workspaces []string `json:"workspaces,omitempty"`
	Concerns   []string `json:"concerns,omitempty"`
}

// topicStatement is the slice of a statement row the catalogue reads.
type topicStatement struct {
	id           string
	kind         string
	issueID      string
	text         string
	status       string
	topic        string
	workspace    string
	concern      string
	supersedesID string
}

// splitConcerns splits the comma-separated concern column, dropping empties.
func splitConcerns(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// loadTopicStatements reads every statement carrying a topic, oldest first so
// "the oldest question" is the first one seen.
func (s *Store) loadTopicStatements(ctx context.Context) ([]topicStatement, error) {
	q := s.rebind(`SELECT id, kind, issue_id, text, status, topic, workspace, concern, supersedes_id
		FROM statements WHERE topic != '' ORDER BY created_at ASC, id ASC`)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []topicStatement
	for rows.Next() {
		var st topicStatement
		var issueID, supersedes sql.NullString
		if err := rows.Scan(&st.id, &st.kind, &issueID, &st.text, &st.status, &st.topic, &st.workspace, &st.concern, &supersedes); err != nil {
			return nil, err
		}
		st.issueID = issueID.String
		st.supersedesID = supersedes.String
		out = append(out, st)
	}
	return out, rows.Err()
}

// openBeads returns the subset of ids whose issue is not closed. An id the
// issues table does not have counts as not open.
func (s *Store) openBeads(ctx context.Context, ids []string) (map[string]bool, error) {
	open := map[string]bool{}
	if len(ids) == 0 {
		return open, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	q := s.rebind(fmt.Sprintf(`SELECT id FROM issues WHERE status != 'closed' AND id IN (%s)`, strings.Join(ph, ",")))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		open[id] = true
	}
	return open, rows.Err()
}

// ListTopics is the catalogue: one row per distinct slug, sorted by slug.
// Every row is derived from the statements carrying it — there is no topics
// table, so a topic exists exactly as long as a statement names it.
func (s *Store) ListTopics(ctx context.Context, f TopicFilter) ([]TopicRow, error) {
	all, err := s.loadTopicStatements(ctx)
	if err != nil {
		return nil, err
	}

	bySlug := map[string][]topicStatement{}
	var order []string
	for _, st := range all {
		if _, seen := bySlug[st.topic]; !seen {
			order = append(order, st.topic)
		}
		bySlug[st.topic] = append(bySlug[st.topic], st)
	}

	// One issues lookup for every bead any topic touches.
	var beadIDs []string
	seenBead := map[string]bool{}
	for _, st := range all {
		if st.issueID != "" && !seenBead[st.issueID] {
			seenBead[st.issueID] = true
			beadIDs = append(beadIDs, st.issueID)
		}
	}
	openBead, err := s.openBeads(ctx, beadIDs)
	if err != nil {
		return nil, err
	}

	var rows []TopicRow
	for _, slug := range order {
		sts := bySlug[slug]
		if !topicMatches(sts, f) {
			continue
		}
		rows = append(rows, buildTopicRow(slug, sts, openBead))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Slug < rows[j].Slug })
	return rows, nil
}

// topicMatches reports whether one statement carrying the slug satisfies every
// set field of the filter. Both fields set is AND on the same statement: a
// topic whose workspace and concern were never carried together is not the
// intersection the caller asked for.
func topicMatches(sts []topicStatement, f TopicFilter) bool {
	if f.Workspace == "" && f.Concern == "" {
		return true
	}
	for _, st := range sts {
		if f.Workspace != "" && st.workspace != f.Workspace {
			continue
		}
		if f.Concern != "" {
			found := false
			for _, c := range splitConcerns(st.concern) {
				if c == f.Concern {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		return true
	}
	return false
}

// buildTopicRow derives one catalogue row from the statements carrying a slug.
func buildTopicRow(slug string, sts []topicStatement, openBead map[string]bool) TopicRow {
	row := TopicRow{Slug: slug}

	// Areas, deduplicated, in first-seen order.
	seenWS, seenC := map[string]bool{}, map[string]bool{}
	for _, st := range sts {
		if st.workspace != "" && !seenWS[st.workspace] {
			seenWS[st.workspace] = true
			row.Workspaces = append(row.Workspaces, st.workspace)
		}
		for _, c := range splitConcerns(st.concern) {
			if !seenC[c] {
				seenC[c] = true
				row.Concerns = append(row.Concerns, c)
			}
		}
	}

	// Questions: the oldest active one speaks for the topic; failing that, the
	// oldest question of any status, so a settled topic still shows what was
	// asked.
	var firstActiveQ, firstQ string
	for _, st := range sts {
		if st.kind != "question" {
			continue
		}
		if firstQ == "" {
			firstQ = st.text
		}
		if st.status == "active" {
			row.Questions++
			if firstActiveQ == "" {
				firstActiveQ = st.text
			}
		}
	}
	row.Question = firstActiveQ
	if row.Question == "" {
		row.Question = firstQ
	}

	// Rulings: a superseded chain contributes only its terminal, the same
	// reduction `bd rulings` applies.
	superseded := map[string]bool{}
	for _, st := range sts {
		if st.kind == "ruling" && st.status == "active" && st.supersedesID != "" {
			superseded[st.supersedesID] = true
		}
	}
	activeRulings := 0
	projectRuling := false
	for _, st := range sts {
		if st.kind != "ruling" || st.status != "active" || superseded[st.id] {
			continue
		}
		activeRulings++
		if st.issueID == "" {
			projectRuling = true
		}
		row.RulingID = st.id
	}

	// Status. A slug nothing active carries is dormant: there is nothing left
	// to act on, and --all still shows it.
	switch {
	case row.Questions > 0:
		row.Status = TopicOpen
	case activeRulings > 0:
		row.Status = TopicSettled
		if !projectRuling && !topicHasOpenBead(sts, openBead) {
			row.Status = TopicDormant
		}
	default:
		row.Status = TopicDormant
	}
	return row
}

// topicHasOpenBead reports whether any bead carrying the slug is still open.
func topicHasOpenBead(sts []topicStatement, openBead map[string]bool) bool {
	for _, st := range sts {
		if st.issueID != "" && openBead[st.issueID] {
			return true
		}
	}
	return false
}

// RenameTopic moves every statement from one slug to another and returns the
// number moved. It refuses when the new slug already carries a statement:
// folding two live slugs together is MergeTopics, and doing it by accident
// through a typo'd rename is unrecoverable.
func (s *Store) RenameTopic(ctx context.Context, oldSlug, newSlug string) (int64, error) {
	oldSlug, newSlug = strings.TrimSpace(oldSlug), strings.TrimSpace(newSlug)
	if oldSlug == "" || newSlug == "" {
		return 0, fmt.Errorf("old and new slugs are required")
	}
	if oldSlug == newSlug {
		return 0, fmt.Errorf("topic %q: new slug is the old slug", oldSlug)
	}
	exists, err := s.topicExists(ctx, newSlug)
	if err != nil {
		return 0, err
	}
	if exists {
		return 0, fmt.Errorf("topic %q already exists — fold them with `bd topics merge %s %s`", newSlug, oldSlug, newSlug)
	}
	return s.moveTopic(ctx, oldSlug, newSlug)
}

// MergeTopics is RenameTopic without the already-exists refusal: folding one
// slug into a live one is the whole point.
func (s *Store) MergeTopics(ctx context.Context, from, into string) (int64, error) {
	from, into = strings.TrimSpace(from), strings.TrimSpace(into)
	if from == "" || into == "" {
		return 0, fmt.Errorf("from and into slugs are required")
	}
	if from == into {
		return 0, fmt.Errorf("topic %q: cannot merge into itself", from)
	}
	return s.moveTopic(ctx, from, into)
}

func (s *Store) moveTopic(ctx context.Context, from, into string) (int64, error) {
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE statements SET topic = ? WHERE topic = ?`), into, from)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fmt.Errorf("topic %q: %w", from, ErrNotFound)
	}
	return n, nil
}

// RetopicStatement moves a single statement to a new slug and returns the
// slug it carried before the move. Every sibling statement still carrying the
// old slug is untouched — that whole-slug move is RenameTopic or MergeTopics.
// It refuses an id no statement row has.
func (s *Store) RetopicStatement(ctx context.Context, id, newSlug string) (string, error) {
	id, newSlug = strings.TrimSpace(id), strings.TrimSpace(newSlug)
	if id == "" || newSlug == "" {
		return "", fmt.Errorf("statement id and new slug are required")
	}
	st, err := s.GetStatement(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", fmt.Errorf("statement %q: %w", id, ErrNotFound)
		}
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, s.rebind(`UPDATE statements SET topic = ? WHERE id = ?`), newSlug, id); err != nil {
		return "", err
	}
	return st.Topic, nil
}

func (s *Store) topicExists(ctx context.Context, slug string) (bool, error) {
	var n int
	q := s.rebind(`SELECT COUNT(*) FROM statements WHERE topic = ?`)
	if err := s.db.QueryRowContext(ctx, q, slug).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}
