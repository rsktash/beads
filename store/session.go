package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rsktash/beads"
)

// ownerDecisionPrefixes is the closed list of acceptance words a session-close
// candidate comment must start with. The test is a literal, case-insensitive
// prefix match: mechanical question-to-answer matching is rejected on purpose,
// because a checklist that guesses reproduces the defect it exists to catch.
var ownerDecisionPrefixes = []string{"ruled:", "go with", "approved", "decided:"}

// SessionOwnerDecision is an owner comment written in the session that no
// statement in the same session typed.
type SessionOwnerDecision struct {
	CommentID string    `json:"comment_id"`
	IssueID   string    `json:"issue_id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionClaim is a bead the session claimed and left claimed.
type SessionClaim struct {
	IssueID   string    `json:"issue_id"`
	Status    string    `json:"status"`
	Assignee  string    `json:"assignee"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SessionCloseReport is what a session left untyped. Every slice is non-nil so
// a renderer can print an empty section rather than skip it: a missing section
// reads as "not checked".
type SessionCloseReport struct {
	SessionID        string                 `json:"session_id"`
	OwnerDecisions   []SessionOwnerDecision `json:"owner_decisions"`
	OpenQuestions    []beads.Statement      `json:"open_questions"`
	ClaimedNotClosed []SessionClaim         `json:"claimed_not_closed"`
}

// commentAuthorResolvesToOwner decides whether a comment author string is the
// owner speaking. The text before the first ':' is the actor word; owner and
// coordinator are the owner, and so is a bare username with no colon at all —
// comments written before bd comment add resolved its author carry the OS user
// alone. Every other actor word is an agent.
func commentAuthorResolvesToOwner(author string) bool {
	idx := strings.IndexByte(author, ':')
	if idx < 0 {
		return true
	}
	word := author[:idx]
	return word == "owner" || word == "coordinator"
}

// hasOwnerDecisionPrefix is the candidate test on the comment body.
func hasOwnerDecisionPrefix(text string) bool {
	s := strings.ToLower(strings.TrimSpace(text))
	for _, p := range ownerDecisionPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// SessionClose reads what the session left untyped. It writes nothing: the acts
// it names stay owner or coordinator acts.
func (s *Store) SessionClose(ctx context.Context, sessionID string) (SessionCloseReport, error) {
	rep := SessionCloseReport{
		SessionID:        sessionID,
		OwnerDecisions:   []SessionOwnerDecision{},
		OpenQuestions:    []beads.Statement{},
		ClaimedNotClosed: []SessionClaim{},
	}
	// An empty session id would match every row session_id defaults to.
	if strings.TrimSpace(sessionID) == "" {
		return rep, fmt.Errorf("session id is required")
	}

	typed, err := s.sessionTypedBeads(ctx, sessionID)
	if err != nil {
		return rep, err
	}
	if rep.OwnerDecisions, err = s.sessionOwnerDecisions(ctx, sessionID, typed); err != nil {
		return rep, err
	}
	if rep.OpenQuestions, err = s.sessionOpenQuestions(ctx, sessionID); err != nil {
		return rep, err
	}
	if rep.ClaimedNotClosed, err = s.sessionClaims(ctx, sessionID); err != nil {
		return rep, err
	}
	return rep, nil
}

// sessionTypedBeads is the set of beads some statement of the session names.
func (s *Store) sessionTypedBeads(ctx context.Context, sessionID string) (map[string]bool, error) {
	q := s.rebind(`SELECT DISTINCT issue_id FROM statements WHERE session_id = ? AND issue_id IS NOT NULL AND issue_id <> ''`)
	rows, err := s.db.QueryContext(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (s *Store) sessionOwnerDecisions(ctx context.Context, sessionID string, typed map[string]bool) ([]SessionOwnerDecision, error) {
	q := s.rebind(`SELECT id, issue_id, author, text, created_at FROM comments WHERE session_id = ? ORDER BY created_at, id`)
	rows, err := s.db.QueryContext(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionOwnerDecision{}
	for rows.Next() {
		var d SessionOwnerDecision
		if err := rows.Scan(&d.CommentID, &d.IssueID, &d.Author, &d.Text, &d.CreatedAt); err != nil {
			return nil, err
		}
		if !commentAuthorResolvesToOwner(d.Author) || !hasOwnerDecisionPrefix(d.Text) {
			continue
		}
		if typed[d.IssueID] {
			continue
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) sessionOpenQuestions(ctx context.Context, sessionID string) ([]beads.Statement, error) {
	q := s.rebind(`SELECT ` + statementSelectColumns + ` FROM statements WHERE kind = 'question' AND status = 'active' AND session_id = ? ORDER BY created_at, id`)
	rows, err := s.db.QueryContext(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []beads.Statement{}
	for rows.Next() {
		st, err := scanStatementRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

// sessionEarliestWrite is the first moment the session recorded anything —
// the floor a claim must sit at or after to be this session's doing. The
// second return is false when the session recorded no write at all, in which
// case there is no floor and nothing can be attributed to it.
func (s *Store) sessionEarliestWrite(ctx context.Context, sessionID string) (time.Time, bool, error) {
	q := s.rebind(`SELECT created_at FROM statements WHERE session_id = ?
UNION ALL
SELECT created_at FROM comments WHERE session_id = ?`)
	rows, err := s.db.QueryContext(ctx, q, sessionID, sessionID)
	if err != nil {
		return time.Time{}, false, err
	}
	defer rows.Close()
	var earliest time.Time
	found := false
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return time.Time{}, false, err
		}
		if !found || t.Before(earliest) {
			earliest = t
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, false, err
	}
	return earliest, found, nil
}

func (s *Store) sessionClaims(ctx context.Context, sessionID string) ([]SessionClaim, error) {
	out := []SessionClaim{}
	floor, ok, err := s.sessionEarliestWrite(ctx, sessionID)
	if err != nil || !ok {
		return out, err
	}
	q := s.rebind(`SELECT id, status, assignee, updated_at FROM issues WHERE status = 'in_progress' AND assignee <> '' ORDER BY updated_at, id`)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c SessionClaim
		if err := rows.Scan(&c.IssueID, &c.Status, &c.Assignee, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if c.UpdatedAt.Before(floor) {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
