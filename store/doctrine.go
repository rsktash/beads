package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rsktash/beads"
)

// A doctrine is not a row kind and has no table of its own: it is exactly an
// active, project-scoped ruling carrying an area. This predicate is the whole
// definition, and every doctrine read goes through it, so `bd doctrine` and
// the DOCTRINE section of `bd authority` can never disagree about what counts.
const doctrinePredicate = `kind = 'ruling' AND issue_id IS NULL AND status = 'active' AND (concern != '' OR workspace != '')`

// DoctrineFilter narrows the listing to one area. Concern and Workspace
// together mean AND, the same reading `bd topics` gives them.
type DoctrineFilter struct {
	Concern   string
	Workspace string
}

// ListDoctrine returns the doctrine oldest first — the order the laws were
// ruled in, which is the order they read in on the rendered page: a later law
// qualifies the ones above it.
//
// The area filter runs in Go rather than in SQL because the concern column
// holds a comma-separated list on a ruling that inherited its areas from a
// bead, so membership, not equality, is the test.
func (s *Store) ListDoctrine(ctx context.Context, f DoctrineFilter) ([]beads.Statement, error) {
	q := s.rebind(`SELECT ` + statementSelectColumns + ` FROM statements WHERE ` + doctrinePredicate +
		` ORDER BY created_at ASC, id ASC`)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	concern := strings.TrimSpace(f.Concern)
	workspace := strings.TrimSpace(f.Workspace)
	var out []beads.Statement
	for rows.Next() {
		st, err := scanStatementRows(rows)
		if err != nil {
			return nil, err
		}
		if concern != "" && !containsArea(splitConcerns(st.Concern), concern) {
			continue
		}
		if workspace != "" && st.Workspace != workspace {
			continue
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

// containsArea reports whether names carries want.
func containsArea(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// PromoteDoctrine re-scopes an existing ruling to the project and stamps its
// concern. It updates the row in place and never inserts one, so every
// citation of the ruling id keeps resolving to the same record — the law is
// the ruling, promoted, not a copy of it.
func (s *Store) PromoteDoctrine(ctx context.Context, id, concern string) error {
	id = strings.TrimSpace(id)
	concern = strings.TrimSpace(concern)
	if id == "" {
		return fmt.Errorf("ruling id is required")
	}
	if concern == "" {
		return fmt.Errorf("concern is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var kind, status string
	var issueID sql.NullString
	row := tx.QueryRowContext(ctx, s.rebind(`SELECT kind, status, issue_id FROM statements WHERE id = ?`), id)
	if err := row.Scan(&kind, &status, &issueID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if kind != "ruling" {
		return fmt.Errorf("%s is a %s, not a ruling; only a ruling becomes doctrine", id, kind)
	}
	if status != "active" {
		return fmt.Errorf("%s is %s, not active; only an active ruling becomes doctrine", id, status)
	}

	at := time.Now().UTC()
	if _, err := tx.ExecContext(ctx,
		s.rebind(`UPDATE statements SET issue_id = NULL, concern = ?, changed_at = ? WHERE id = ?`),
		concern, at, id); err != nil {
		return err
	}
	// Bump the bead the ruling is leaving: its contract just lost a line.
	if issueID.Valid && issueID.String != "" {
		if err := touchIssueTx(ctx, tx, s, issueID.String, at); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
