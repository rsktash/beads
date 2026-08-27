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

// StatementFilter narrows ListStatements.
type StatementFilter struct {
	IssueIDs []string
	Kinds    []string
	Statuses []string
}

// ContractView is the inheritance resolver result. Each slice holds
// statements already ordered for rendering (own → ancestors nearest-first → project).
type ContractView struct {
	Rulings   []beads.Statement `json:"rulings"`
	Questions []beads.Statement `json:"questions"`
	Findings  []beads.Statement `json:"findings"`
}

// kindPrefix maps statement kind to its citable prefix.
func kindPrefix(kind string) string {
	switch kind {
	case "ruling":
		return "R"
	case "question":
		return "Q"
	case "finding":
		return "F"
	default:
		return ""
	}
}

func nullStrPtr(s *string) sql.NullString {
	if s == nil || *s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func strPtrToNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

// insertStatementSQL is the column list for statements inserts.
const insertStatementSQL = `INSERT INTO statements (id, kind, issue_id, text, created_at, filed_by, status, scope, supersedes_id, answered_by, source_comment_id, evidence) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func insertStatementExec(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, s *Store, st *beads.Statement) error {
	q := s.rebind(insertStatementSQL)
	_, err := execer.ExecContext(ctx, q,
		st.ID,
		st.Kind,
		nullStrPtr(st.IssueID),
		st.Text,
		st.CreatedAt,
		st.FiledBy,
		st.Status,
		st.Scope,
		nullStrPtr(st.SupersedesID),
		nullStrPtr(st.AnsweredBy),
		nullStrPtr(st.SourceCommentID),
		st.Evidence,
	)
	return err
}

// CreateStatement allocates an ID from the per-kind counter inside the same
// transaction as the insert when ID is empty, and sets CreatedAt when zero.
// If either step fails, neither the counter nor the row is persisted.
func (s *Store) CreateStatement(ctx context.Context, st *beads.Statement) error {
	if st.Kind == "" {
		return fmt.Errorf("kind is required")
	}
	if kindPrefix(st.Kind) == "" {
		return fmt.Errorf("invalid kind %q", st.Kind)
	}
	if st.Text == "" {
		return fmt.Errorf("text is required")
	}
	if st.Status == "" {
		st.Status = "active"
	}
	if st.Scope == "" {
		st.Scope = "inherit"
	}
	if st.CreatedAt.IsZero() {
		st.CreatedAt = time.Now().UTC()
	}

	if st.ID != "" {
		return insertStatementExec(ctx, s.db, s, st)
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

	// Allocate per-kind counter inside the transaction.
	q := s.rebind(`INSERT INTO statement_counters (kind, last_id) VALUES (?, 1) ON CONFLICT(kind) DO UPDATE SET last_id = statement_counters.last_id + 1 RETURNING last_id`)
	var n int64
	if err := tx.QueryRowContext(ctx, q, st.Kind).Scan(&n); err != nil {
		return err
	}
	prefix := kindPrefix(st.Kind)
	st.ID = fmt.Sprintf("%s-%d", prefix, n)

	if err := insertStatementExec(ctx, tx, s, st); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// CreateStatementWithIssueUpdate is the atomic file-ruling-with-state-change transaction.
// It writes the statement and, if upd != nil, updates the target issue (st.IssueID) in the same DB transaction.
// It also applies supersede and answer links if present. Nothing partial survives an error.
func (s *Store) CreateStatementWithIssueUpdate(ctx context.Context, st *beads.Statement, upd *IssueUpdate) error {
	return s.createStatementTx(ctx, st, upd, nil)
}

// CreateStatementWithStateChange is an alias for CreateStatementWithIssueUpdate.
func (s *Store) CreateStatementWithStateChange(ctx context.Context, st *beads.Statement, upd *IssueUpdate) error {
	return s.CreateStatementWithIssueUpdate(ctx, st, upd)
}

// FileRuling is an alias for CreateStatementWithIssueUpdate.
func (s *Store) FileRuling(ctx context.Context, st *beads.Statement, upd *IssueUpdate) error {
	return s.CreateStatementWithIssueUpdate(ctx, st, upd)
}

// CreateRulingWithStateChange files a ruling and optionally updates an issue and links an answered question, atomically.
func (s *Store) CreateRulingWithStateChange(ctx context.Context, st *beads.Statement, upd *IssueUpdate, answersQuestionID string) error {
	var ansPtr *string
	if answersQuestionID != "" {
		ansPtr = &answersQuestionID
	}
	return s.createStatementTx(ctx, st, upd, ansPtr)
}

// FileRulingWithStateChange is an alias for CreateRulingWithStateChange.
func (s *Store) FileRulingWithStateChange(ctx context.Context, st *beads.Statement, upd *IssueUpdate, answersQuestionID string) error {
	return s.CreateRulingWithStateChange(ctx, st, upd, answersQuestionID)
}

func (s *Store) createStatementTx(ctx context.Context, st *beads.Statement, upd *IssueUpdate, answersID *string) error {
	if st.Kind == "" {
		return fmt.Errorf("kind is required")
	}
	if kindPrefix(st.Kind) == "" {
		return fmt.Errorf("invalid kind %q", st.Kind)
	}
	if st.Text == "" {
		return fmt.Errorf("text is required")
	}
	if st.Status == "" {
		st.Status = "active"
	}
	if st.Scope == "" {
		st.Scope = "inherit"
	}
	if st.CreatedAt.IsZero() {
		st.CreatedAt = time.Now().UTC()
	}
	// Handle answer via st.AnsweredBy misuse for the two-param case:
	// if answersID is nil and st.Kind is ruling and AnsweredBy is set, treat it as question id to answer.
	if answersID == nil && st.Kind == "ruling" && st.AnsweredBy != nil && *st.AnsweredBy != "" {
		// stash and clear so the ruling row doesn't store the question id as its own answered_by
		v := *st.AnsweredBy
		answersID = &v
		st.AnsweredBy = nil
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

	// Allocate id if empty inside tx.
	if st.ID == "" {
		q := s.rebind(`INSERT INTO statement_counters (kind, last_id) VALUES (?, 1) ON CONFLICT(kind) DO UPDATE SET last_id = statement_counters.last_id + 1 RETURNING last_id`)
		var n int64
		if err := tx.QueryRowContext(ctx, q, st.Kind).Scan(&n); err != nil {
			return err
		}
		prefix := kindPrefix(st.Kind)
		st.ID = fmt.Sprintf("%s-%d", prefix, n)
	}

	if err := insertStatementExec(ctx, tx, s, st); err != nil {
		return err
	}

	// Supersede link: flip old ruling to superseded.
	if st.SupersedesID != nil && *st.SupersedesID != "" {
		q := s.rebind(`UPDATE statements SET status = 'superseded' WHERE id = ?`)
		res, err := tx.ExecContext(ctx, q, *st.SupersedesID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("superseded statement %s not found: %w", *st.SupersedesID, ErrNotFound)
		}
	}

	// Answer link: set question's answered_by and status.
	if answersID != nil && *answersID != "" {
		q := s.rebind(`UPDATE statements SET answered_by = ?, status = 'answered' WHERE id = ?`)
		res, err := tx.ExecContext(ctx, q, st.ID, *answersID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("question %s not found: %w", *answersID, ErrNotFound)
		}
	}

	// Issue state change, if any.
	if upd != nil {
		if st.IssueID == nil || *st.IssueID == "" {
			return fmt.Errorf("state change requires issue_id")
		}
		if err := applyIssueUpdateTx(ctx, tx, s, *st.IssueID, *upd); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func applyIssueUpdateTx(ctx context.Context, tx *sql.Tx, s *Store, issueID string, u IssueUpdate) error {
	var sets []string
	var args []any
	add := func(col string, v any) { sets = append(sets, col+"=?"); args = append(args, v) }
	if u.Title != nil {
		add("title", *u.Title)
	}
	if u.Description != nil {
		add("description", *u.Description)
	}
	if u.Design != nil {
		add("design", *u.Design)
	}
	if u.AcceptanceCriteria != nil {
		add("acceptance_criteria", *u.AcceptanceCriteria)
	}
	if u.Notes != nil {
		add("notes", *u.Notes)
	}
	if u.Type != nil {
		add("issue_type", string(*u.Type))
	}
	if u.Status != nil {
		add("status", string(*u.Status))
		if *u.Status == beads.StatusClosed {
			add("closed_at", time.Now().UTC())
		}
		if *u.Status == beads.StatusInProgress {
			add("started_at", time.Now().UTC())
		}
	}
	if u.Priority != nil {
		add("priority", *u.Priority)
	}
	if u.Assignee != nil {
		add("assignee", *u.Assignee)
	}
	if u.Owner != nil {
		add("owner", *u.Owner)
	}
	if u.EstimatedMinutes != nil {
		add("estimated_minutes", *u.EstimatedMinutes)
	}
	if u.Metadata != nil {
		add("metadata", *u.Metadata)
	}
	if u.CloseReason != nil {
		add("close_reason", *u.CloseReason)
	}
	if u.DueAt != nil {
		add("due_at", *u.DueAt)
	}
	if u.DeferUntil != nil {
		add("defer_until", *u.DeferUntil)
	}
	if u.StartedAt != nil {
		add("started_at", *u.StartedAt)
	}
	if u.Ephemeral != nil {
		add("ephemeral", boolToInt64(*u.Ephemeral))
	}
	if u.Pinned != nil {
		add("pinned", boolToInt64(*u.Pinned))
	}
	if len(sets) == 0 {
		return nil
	}
	add("updated_at", time.Now().UTC())
	args = append(args, issueID)
	q := s.rebind("UPDATE issues SET " + strings.Join(sets, ", ") + " WHERE id=?")
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetStatement returns a single statement or ErrNotFound.
func (s *Store) GetStatement(ctx context.Context, id string) (*beads.Statement, error) {
	q := s.rebind(`SELECT id, kind, issue_id, text, created_at, filed_by, status, scope, supersedes_id, answered_by, source_comment_id, evidence FROM statements WHERE id = ?`)
	row := s.db.QueryRowContext(ctx, q, id)
	st, err := scanStatementRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return st, nil
}

// UpdateStatementStatus sets status; returns ErrNotFound when no row changed.
func (s *Store) UpdateStatementStatus(ctx context.Context, id string, status string) error {
	q := s.rebind(`UPDATE statements SET status = ? WHERE id = ?`)
	res, err := s.db.ExecContext(ctx, q, status, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAnsweredBy links a question to its answering ruling and marks it answered.
func (s *Store) SetAnsweredBy(ctx context.Context, questionID, rulingID string) error {
	q := s.rebind(`UPDATE statements SET answered_by = ?, status = 'answered' WHERE id = ?`)
	res, err := s.db.ExecContext(ctx, q, strPtrToNullString(rulingID), questionID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListStatements returns statements matching the filter, newest first.
func (s *Store) ListStatements(ctx context.Context, f StatementFilter) ([]beads.Statement, error) {
	base := `SELECT id, kind, issue_id, text, created_at, filed_by, status, scope, supersedes_id, answered_by, source_comment_id, evidence FROM statements`
	var where []string
	var args []any

	if len(f.IssueIDs) > 0 {
		var nonEmpty []string
		hasNull := false
		for _, id := range f.IssueIDs {
			if id == "" {
				hasNull = true
			} else {
				nonEmpty = append(nonEmpty, id)
			}
		}
		var parts []string
		if len(nonEmpty) > 0 {
			ph := make([]string, len(nonEmpty))
			for i, v := range nonEmpty {
				ph[i] = "?"
				args = append(args, v)
			}
			parts = append(parts, fmt.Sprintf("issue_id IN (%s)", strings.Join(ph, ",")))
		}
		if hasNull {
			parts = append(parts, "issue_id IS NULL")
		}
		if len(parts) == 1 {
			where = append(where, parts[0])
		} else if len(parts) > 1 {
			where = append(where, "("+strings.Join(parts, " OR ")+")")
		}
	}
	if len(f.Kinds) > 0 {
		ph := make([]string, len(f.Kinds))
		for i, v := range f.Kinds {
			ph[i] = "?"
			args = append(args, v)
		}
		where = append(where, fmt.Sprintf("kind IN (%s)", strings.Join(ph, ",")))
	}
	if len(f.Statuses) > 0 {
		ph := make([]string, len(f.Statuses))
		for i, v := range f.Statuses {
			ph[i] = "?"
			args = append(args, v)
		}
		where = append(where, fmt.Sprintf("status IN (%s)", strings.Join(ph, ",")))
	}

	q := base
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at DESC, id DESC"
	q = s.rebind(q)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []beads.Statement
	for rows.Next() {
		st, err := scanStatementRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

// Ancestors returns the parent-child chain upward, nearest first, capped at depth 8.
func (s *Store) Ancestors(ctx context.Context, issueID string) ([]string, error) {
	maxDepth := 8
	cte := `WITH RECURSIVE tree AS (
		SELECT depends_on_id, 1 AS depth FROM dependencies WHERE issue_id = ? AND type = 'parent-child'
		UNION ALL
		SELECT d.depends_on_id, t.depth + 1 FROM dependencies d JOIN tree t ON d.issue_id = t.depends_on_id WHERE d.type = 'parent-child' AND t.depth < ?
	)
	SELECT depends_on_id, MIN(depth) FROM tree GROUP BY depends_on_id ORDER BY MIN(depth)`
	q := s.rebind(cte)
	rows, err := s.db.QueryContext(ctx, q, issueID, maxDepth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		var depth int
		if err := rows.Scan(&id, &depth); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ContractStatements is the inheritance resolver.
// It returns active rulings (terminal-of-chain only, scoped), active questions, and findings
// with inheritance applied (own → ancestors → project) and scope='self' ancestors dropped.
func (s *Store) ContractStatements(ctx context.Context, issueID string) (ContractView, error) {
	ancestors, err := s.Ancestors(ctx, issueID)
	if err != nil {
		return ContractView{}, err
	}

	// Build weight map for ordering: own=0, ancestors[0]=1,... project= len(ancestors)+1
	weight := map[string]int{}
	weight[issueID] = 0
	for i, a := range ancestors {
		weight[a] = i + 1
	}
	projectWeight := len(ancestors) + 1

	// Build single query for all relevant statements: status='active' and (issue_id = own OR issue_id IN ancestors OR issue_id IS NULL)
	// We fetch in one round trip regardless of count.
	var args []any
	var ors []string

	// own
	ors = append(ors, "issue_id = ?")
	args = append(args, issueID)

	// ancestors
	if len(ancestors) > 0 {
		ph := make([]string, len(ancestors))
		for i, a := range ancestors {
			ph[i] = "?"
			args = append(args, a)
		}
		ors = append(ors, fmt.Sprintf("issue_id IN (%s)", strings.Join(ph, ",")))
	}

	// project-scoped
	ors = append(ors, "issue_id IS NULL")

	where := fmt.Sprintf("status = 'active' AND (%s)", strings.Join(ors, " OR "))
	q := s.rebind(fmt.Sprintf(`SELECT id, kind, issue_id, text, created_at, filed_by, status, scope, supersedes_id, answered_by, source_comment_id, evidence FROM statements WHERE %s`, where))

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return ContractView{}, err
	}
	defer rows.Close()

	var all []beads.Statement
	for rows.Next() {
		st, err := scanStatementRows(rows)
		if err != nil {
			return ContractView{}, err
		}
		all = append(all, *st)
	}
	if err := rows.Err(); err != nil {
		return ContractView{}, err
	}

	// Apply scope filtering: ancestor statements with scope='self' are dropped; own and project never dropped.
	var filtered []beads.Statement
	for _, st := range all {
		if st.IssueID == nil {
			// project-scoped: always keep
			filtered = append(filtered, st)
			continue
		}
		iid := *st.IssueID
		if iid == issueID {
			// own: never dropped by scope
			filtered = append(filtered, st)
			continue
		}
		// ancestor?
		if _, isAncestor := weight[iid]; isAncestor {
			if st.Scope == "self" {
				continue
			}
			filtered = append(filtered, st)
			continue
		}
		// Should not happen: issue_id not in ancestors or own but also not NULL.
		// This would be an unrelated issue; skip.
	}

	// Split by kind
	var rulings, questions, findings []beads.Statement
	for _, st := range filtered {
		switch st.Kind {
		case "ruling":
			rulings = append(rulings, st)
		case "question":
			questions = append(questions, st)
		case "finding":
			findings = append(findings, st)
		}
	}

	// Reduce rulings to terminal-of-chain only.
	if len(rulings) > 0 {
		superseded := map[string]bool{}
		for _, r := range rulings {
			if r.SupersedesID != nil && *r.SupersedesID != "" {
				superseded[*r.SupersedesID] = true
			}
		}
		var terminals []beads.Statement
		for _, r := range rulings {
			if !superseded[r.ID] {
				terminals = append(terminals, r)
			}
		}
		rulings = terminals
	}

	sortByWeight := func(slice []beads.Statement) {
		for i := 0; i < len(slice); i++ {
			for j := i + 1; j < len(slice); j++ {
				wi := projectWeight
				wj := projectWeight
				if slice[i].IssueID != nil {
					if w, ok := weight[*slice[i].IssueID]; ok {
						wi = w
					}
				}
				if slice[j].IssueID != nil {
					if w, ok := weight[*slice[j].IssueID]; ok {
						wj = w
					}
				}
				shouldSwap := false
				if wi != wj {
					shouldSwap = wi > wj
				} else {
					// same weight: newest first
					if !slice[i].CreatedAt.Equal(slice[j].CreatedAt) {
						shouldSwap = slice[i].CreatedAt.Before(slice[j].CreatedAt)
					} else {
						shouldSwap = slice[i].ID < slice[j].ID
					}
				}
				if shouldSwap {
					slice[i], slice[j] = slice[j], slice[i]
				}
			}
		}
	}
	sortByWeight(rulings)
	sortByWeight(questions)
	sortByWeight(findings)

	return ContractView{
		Rulings:   rulings,
		Questions: questions,
		Findings:  findings,
	}, nil
}

func scanStatementRow(row *sql.Row) (*beads.Statement, error) {
	var (
		id, kind, text, filedBy, status, scope, evidence string
		createdAt                                         time.Time
		nsIssue, nsSupersedes, nsAnswered, nsSource       sql.NullString
	)
	if err := row.Scan(&id, &kind, &nsIssue, &text, &createdAt, &filedBy, &status, &scope, &nsSupersedes, &nsAnswered, &nsSource, &evidence); err != nil {
		return nil, err
	}
	return &beads.Statement{
		ID:              id,
		Kind:            kind,
		IssueID:         nullStringToPtr(nsIssue),
		Text:            text,
		CreatedAt:       createdAt,
		FiledBy:         filedBy,
		Status:          status,
		Scope:           scope,
		SupersedesID:    nullStringToPtr(nsSupersedes),
		AnsweredBy:      nullStringToPtr(nsAnswered),
		SourceCommentID: nullStringToPtr(nsSource),
		Evidence:        evidence,
	}, nil
}

func scanStatementRows(rows *sql.Rows) (*beads.Statement, error) {
	var (
		id, kind, text, filedBy, status, scope, evidence string
		createdAt                                         time.Time
		nsIssue, nsSupersedes, nsAnswered, nsSource       sql.NullString
	)
	if err := rows.Scan(&id, &kind, &nsIssue, &text, &createdAt, &filedBy, &status, &scope, &nsSupersedes, &nsAnswered, &nsSource, &evidence); err != nil {
		return nil, err
	}
	return &beads.Statement{
		ID:              id,
		Kind:            kind,
		IssueID:         nullStringToPtr(nsIssue),
		Text:            text,
		CreatedAt:       createdAt,
		FiledBy:         filedBy,
		Status:          status,
		Scope:           scope,
		SupersedesID:    nullStringToPtr(nsSupersedes),
		AnsweredBy:      nullStringToPtr(nsAnswered),
		SourceCommentID: nullStringToPtr(nsSource),
		Evidence:        evidence,
	}, nil
}
