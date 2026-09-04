package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/internal/idgen"
)

// CreateIssueWithQuestion creates a bead and files its first question in ONE
// transaction: a "Decide:" bead with no question attached is the exact
// artefact the Decide gate exists to prevent, so a failure to file the
// question rolls the bead back — nothing partial survives. parentID follows
// CreateChild's semantics (hierarchical id plus parent-child edge); an empty
// parentID takes the plain CreateIssue path inside the same transaction. The
// question takes the new bead's resolved areas (workspace, concerns joined
// with a comma) the way resolveStatementTopic stamps them, and its id is
// allocated and inserted the way createStatementTx does it.
func (s *Store) CreateIssueWithQuestion(ctx context.Context, parentID string, i *beads.Issue, labels []string, q *beads.Statement) error {
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

	// A tx-scoped view of the store (CreateChild's pattern): swapping the
	// engine field runs every Store method inside this transaction.
	ts := *s
	switch s.driver {
	case DriverSQLite:
		ts.sqlite = s.sqlite.WithTx(tx)
	case DriverPostgres:
		ts.pg = s.pg.WithTx(tx)
	}

	if parentID != "" {
		if idgen.HierarchyDepth(parentID) >= idgen.MaxHierarchyDepth {
			return ErrDepthExceeded
		}
		if _, err := ts.GetIssue(ctx, parentID); err != nil {
			return fmt.Errorf("parent %s: %w", parentID, err)
		}
		var n int
		switch s.driver {
		case DriverSQLite:
			v, err := ts.sqlite.NextChildIndex(ctx, parentID)
			if err != nil {
				return err
			}
			n = int(v)
		case DriverPostgres:
			v, err := ts.pg.NextChildIndex(ctx, parentID)
			if err != nil {
				return err
			}
			n = int(v)
		}
		i.ID = idgen.ChildID(parentID, n)
		if err := ts.CreateIssue(ctx, i); err != nil {
			return err
		}
		if err := ts.AddDependency(ctx, beads.Dependency{
			IssueID:     i.ID,
			DependsOnID: parentID,
			Type:        beads.DepParentChild,
			CreatedBy:   i.CreatedBy,
		}); err != nil {
			return fmt.Errorf("link parent-child %s -> %s: %w", i.ID, parentID, err)
		}
	} else if err := ts.CreateIssue(ctx, i); err != nil {
		return err
	}
	for _, l := range labels {
		if err := ts.AddLabel(ctx, i.ID, l); err != nil {
			return fmt.Errorf("label %s: %w", l, err)
		}
	}
	i.Labels = labels

	// The question carries the bead's resolved areas: the one workspace a
	// statement can carry when the bead resolves to exactly one, and the
	// concerns joined with a comma, the way resolveStatementTopic stamps them.
	ws, concerns, err := ts.ResolveIssue(ctx, i.ID)
	if err != nil {
		return err
	}
	if len(ws) == 1 {
		q.Workspace = ws[0]
	}
	q.Concern = strings.Join(concerns, ",")

	// Statement validation, defaults and write, the createStatementTx way.
	// Validating after the bead insert is deliberate: an invalid question is
	// an in-transaction failure, and the rollback is the behaviour under test.
	if q.Kind == "" {
		return fmt.Errorf("kind is required")
	}
	if kindPrefix(q.Kind) == "" {
		return fmt.Errorf("invalid kind %q", q.Kind)
	}
	if q.Text == "" {
		return fmt.Errorf("text is required")
	}
	if q.Status == "" {
		q.Status = "active"
	}
	if q.Scope == "" {
		q.Scope = "inherit"
	}
	at := time.Now().UTC()
	if q.CreatedAt.IsZero() {
		q.CreatedAt = at
	}
	q.ChangedAt = &at
	issueID := i.ID
	q.IssueID = &issueID

	if q.ID == "" {
		cq := s.rebind(`INSERT INTO statement_counters (kind, last_id) VALUES (?, 1) ON CONFLICT(kind) DO UPDATE SET last_id = statement_counters.last_id + 1 RETURNING last_id`)
		var n int64
		if err := tx.QueryRowContext(ctx, cq, q.Kind).Scan(&n); err != nil {
			return err
		}
		q.ID = fmt.Sprintf("%s-%d", kindPrefix(q.Kind), n)
	}

	if err := insertStatementExec(ctx, tx, s, q); err != nil {
		return err
	}
	if err := touchIssueTx(ctx, tx, s, i.ID, at); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
