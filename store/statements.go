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
	// ClosedQuestions holds the bead's own questions that have stopped blocking.
	// The resolved_statements view carries only active rows, and only rulings
	// inherit past depth 0, so these are read separately and are never inherited.
	ClosedQuestions []beads.Statement `json:"closed_questions,omitempty"`
	// AnswerKinds maps an answered_by target id to its kind, so a renderer can
	// say whether a ruling, a finding or another question settled the question
	// without resolving ids itself.
	AnswerKinds map[string]string `json:"answer_kinds,omitempty"`
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

func nullTimePtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func nullTimeToPtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	v := nt.Time
	return &v
}

// authorFromFiledBy derives the closed-vocabulary statements.author column
// (owner, coordinator, or any other actor word treated as agent by search)
// from the full filed_by identity (word:user). It mirrors cmd/bd's
// actorWord rather than importing it: store cannot depend on the cmd/bd
// binary package.
func authorFromFiledBy(filedBy string) string {
	word := filedBy
	if idx := strings.IndexByte(word, ':'); idx >= 0 {
		word = word[:idx]
	}
	if word == "" {
		word = "executor"
	}
	return word
}

// statementSelectColumns is the shared column list for statements reads
// (GetStatement, ListStatements), kept in lockstep with scanStatementRow and
// scanStatementRows below.
const statementSelectColumns = `id, kind, issue_id, text, created_at, filed_by, status, scope, supersedes_id, answered_by, source_comment_id, evidence, topic, workspace, concern, law, rationale, verbatim, author, session_id, msg_id, tool_use_id, retire_note, changed_at, binds_id`

// insertStatementSQL is the column list for statements inserts.
const insertStatementSQL = `INSERT INTO statements (id, kind, issue_id, text, created_at, filed_by, status, scope, supersedes_id, answered_by, source_comment_id, evidence, topic, workspace, concern, law, rationale, verbatim, author, session_id, msg_id, tool_use_id, retire_note, changed_at, binds_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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
		st.Topic,
		st.Workspace,
		st.Concern,
		st.Law,
		st.Rationale,
		st.Verbatim,
		authorFromFiledBy(st.FiledBy),
		st.SessionID,
		st.MsgID,
		st.ToolUseID,
		st.RetireNote,
		nullTimePtr(st.ChangedAt),
		nullStrPtr(st.BindsID),
	)
	return err
}

// CreateStatement allocates an ID from the per-kind counter inside the same
// transaction as the insert when ID is empty, and sets CreatedAt when zero.
// If either step fails, neither the counter nor the row is persisted. This is
// the write path for standalone findings and questions (bd finding add, bd
// question add): filing one stamps changed_at and, unless the statement is
// project-scoped, bumps the bound bead in the same transaction.
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
	at := time.Now().UTC()
	if st.CreatedAt.IsZero() {
		st.CreatedAt = at
	}
	st.ChangedAt = &at

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

	if st.ID == "" {
		// Allocate per-kind counter inside the transaction.
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

	if st.IssueID != nil && *st.IssueID != "" {
		if err := touchIssueTx(ctx, tx, s, *st.IssueID, at); err != nil {
			return err
		}
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
	at := time.Now().UTC()
	if st.CreatedAt.IsZero() {
		st.CreatedAt = at
	}
	st.ChangedAt = &at
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

	// The filed statement's own bead is touched, unless it is project-scoped
	// (issue_id IS NULL): rule 4 says only changed_at is set for those.
	if st.IssueID != nil && *st.IssueID != "" {
		if err := touchIssueTx(ctx, tx, s, *st.IssueID, at); err != nil {
			return err
		}
	}

	// Supersede link: flip old ruling to superseded, stamping its own
	// changed_at, and bump its bead too — a supersede can cross beads.
	if st.SupersedesID != nil && *st.SupersedesID != "" {
		supersededIssue, err := statementIssueIDTx(ctx, tx, s, *st.SupersedesID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("superseded statement %s not found: %w", *st.SupersedesID, ErrNotFound)
			}
			return err
		}
		q := s.rebind(`UPDATE statements SET status = 'superseded', changed_at = ? WHERE id = ?`)
		if _, err := tx.ExecContext(ctx, q, at, *st.SupersedesID); err != nil {
			return err
		}
		if supersededIssue.Valid && supersededIssue.String != "" {
			if err := touchIssueTx(ctx, tx, s, supersededIssue.String, at); err != nil {
				return err
			}
		}
	}

	// Answer link: set question's answered_by and status, stamping its own
	// changed_at, and bump its bead — the new statement's own bead was
	// already touched above.
	if answersID != nil && *answersID != "" {
		answeredIssue, err := statementIssueIDTx(ctx, tx, s, *answersID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("question %s not found: %w", *answersID, ErrNotFound)
			}
			return err
		}
		q := s.rebind(`UPDATE statements SET answered_by = ?, status = 'answered', changed_at = ? WHERE id = ?`)
		if _, err := tx.ExecContext(ctx, q, st.ID, at, *answersID); err != nil {
			return err
		}
		if answeredIssue.Valid && answeredIssue.String != "" {
			if err := touchIssueTx(ctx, tx, s, answeredIssue.String, at); err != nil {
				return err
			}
		}
	}

	// Issue state change, if any.
	if upd != nil {
		if st.IssueID == nil || *st.IssueID == "" {
			return fmt.Errorf("state change requires issue_id")
		}
		if err := applyIssueUpdateTx(ctx, tx, s, *st.IssueID, *upd, at); err != nil {
			return err
		}
		// Parked label (and any future AddLabels) must land in the same tx.
		for _, l := range upd.AddLabels {
			q := s.rebind(`INSERT INTO labels (issue_id, label) VALUES (?, ?)`)
			if _, err := tx.ExecContext(ctx, q, *st.IssueID, l); err != nil {
				if isUniqueViolation(err) {
					continue
				}
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func applyIssueUpdateTx(ctx context.Context, tx *sql.Tx, s *Store, issueID string, u IssueUpdate, at time.Time) error {
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
	if u.ClearDeferUntil {
		sets = append(sets, "defer_until=NULL")
	} else if u.DeferUntil != nil {
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
	if len(sets) == 0 && len(u.AddLabels) == 0 {
		return nil
	}
	if len(sets) == 0 {
		// Only labels to add — no row update needed; caller will insert labels.
		return nil
	}
	add("updated_at", at)
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
	q := s.rebind(`SELECT ` + statementSelectColumns + ` FROM statements WHERE id = ?`)
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

// touchIssueTx bumps the bead's updated_at to at, inside tx. Every statement
// or comment write path that changes a record bound to a bead calls this, so
// the UPDATE is never written out a second time.
func touchIssueTx(ctx context.Context, tx *sql.Tx, s *Store, issueID string, at time.Time) error {
	q := s.rebind(`UPDATE issues SET updated_at = ? WHERE id = ?`)
	_, err := tx.ExecContext(ctx, q, at, issueID)
	return err
}

// statementIssueIDTx reads the issue_id of a statement inside tx, for the
// write paths below that need to bump a bead the id column alone doesn't name.
func statementIssueIDTx(ctx context.Context, tx *sql.Tx, s *Store, id string) (sql.NullString, error) {
	var issueID sql.NullString
	q := s.rebind(`SELECT issue_id FROM statements WHERE id = ?`)
	err := tx.QueryRowContext(ctx, q, id).Scan(&issueID)
	return issueID, err
}

// UpdateStatementStatus sets status and changed_at, and bumps the statement's
// bead; returns ErrNotFound when no row changed.
func (s *Store) UpdateStatementStatus(ctx context.Context, id string, status string) error {
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

	at := time.Now().UTC()
	q := s.rebind(`UPDATE statements SET status = ?, changed_at = ? WHERE id = ?`)
	res, err := tx.ExecContext(ctx, q, status, at, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}

	issueID, err := statementIssueIDTx(ctx, tx, s, id)
	if err != nil {
		return err
	}
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

// RetireRuling flips a ruling to retired in place, storing the note and the
// change timestamp, and bumps its bead. It never inserts a row. Returns
// ErrNotFound when the target isn't a ruling still open to retirement
// (already retired or superseded, or missing).
func (s *Store) RetireRuling(ctx context.Context, id string, note string) error {
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

	at := time.Now().UTC()
	q := s.rebind(`UPDATE statements SET status = 'retired', retire_note = ?, changed_at = ? WHERE id = ? AND kind = 'ruling' AND status NOT IN ('retired','superseded')`)
	res, err := tx.ExecContext(ctx, q, note, at, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}

	issueID, err := statementIssueIDTx(ctx, tx, s, id)
	if err != nil {
		return err
	}
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

// SetAnsweredBy links a question to the statement that answered it and marks it
// answered. The target may be a ruling or a finding; answered_by carries no kind
// constraint, and the caller enforces which kinds it accepts. It stamps the
// question's changed_at, and bumps both the question's bead and the
// answering statement's bead.
func (s *Store) SetAnsweredBy(ctx context.Context, questionID, answerID string) error {
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

	at := time.Now().UTC()
	q := s.rebind(`UPDATE statements SET answered_by = ?, status = 'answered', changed_at = ? WHERE id = ?`)
	res, err := tx.ExecContext(ctx, q, strPtrToNullString(answerID), at, questionID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}

	for _, id := range []string{questionID, answerID} {
		issueID, err := statementIssueIDTx(ctx, tx, s, id)
		if err != nil {
			return err
		}
		if issueID.Valid && issueID.String != "" {
			if err := touchIssueTx(ctx, tx, s, issueID.String, at); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// ClosedQuestionStatuses are the statuses a question can hold once it has stopped
// blocking. They are the complement of 'active' for questions.
var ClosedQuestionStatuses = []string{"superseded", "answered", "retracted"}

// CloseQuestion records a closure that mints no ruling: it writes the new status,
// stores the reason and note in evidence, and links the surviving or replacing
// question through answered_by when one was named. The UPDATE is guarded on the
// row still being an active question, so a concurrent close cannot double-apply.
// It stamps changed_at and bumps the question's bead.
func (s *Store) CloseQuestion(ctx context.Context, questionID, status, evidence, ofID string) error {
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

	at := time.Now().UTC()
	q := s.rebind(`UPDATE statements SET status = ?, evidence = ?, answered_by = ?, changed_at = ? WHERE id = ? AND kind = 'question' AND status = 'active'`)
	res, err := tx.ExecContext(ctx, q, status, evidence, strPtrToNullString(ofID), at, questionID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}

	issueID, err := statementIssueIDTx(ctx, tx, s, questionID)
	if err != nil {
		return err
	}
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

// ListStatements returns statements matching the filter, newest first.
func (s *Store) ListStatements(ctx context.Context, f StatementFilter) ([]beads.Statement, error) {
	base := `SELECT ` + statementSelectColumns + ` FROM statements`
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
// It selects from the resolved_statements view which already encodes inheritance,
// project scope, depth and supersession filtering. One query, no Go-side walk.
func (s *Store) ContractStatements(ctx context.Context, issueID string) (ContractView, error) {
	// The view carries no verbatim, law or rationale column (rulings are the
	// only kind that uses them), so join back to statements for them rather
	// than widening the view. law and rationale are what makes a doctrine
	// render as its law in the contract's ACTIVE RULINGS block.
	q := s.rebind(`SELECT rs.statement_id, rs.kind, rs.text, rs.created_at, rs.filed_by, rs.evidence, rs.origin_kind, rs.origin_issue_id, rs.depth, stmt.verbatim, stmt.law, stmt.rationale
		FROM resolved_statements rs
		JOIN statements stmt ON stmt.id = rs.statement_id
		WHERE rs.issue_id = ?
		ORDER BY CASE rs.origin_kind WHEN 'self' THEN 0 WHEN 'binds' THEN 1 WHEN 'epic' THEN 2 WHEN 'blocks' THEN 3 ELSE 4 END, COALESCE(rs.depth, 999), rs.created_at DESC, rs.statement_id DESC`)
	rows, err := s.db.QueryContext(ctx, q, issueID)
	if err != nil {
		return ContractView{}, err
	}
	defer rows.Close()

	var rulings, questions, findings []beads.Statement
	// seen dedupes a statement reachable by two arms (e.g. bound explicitly
	// and also inherited through the epic chain), keeping the first row —
	// the ORDER BY above has already put the nearest origin first.
	seen := map[string]bool{}
	for rows.Next() {
		var statementID, kind, text, filedBy, evidence, originKind, verbatim, law, rationale string
		var createdAt time.Time
		var originIssueID sql.NullString
		var depth sql.NullInt64
		if err := rows.Scan(&statementID, &kind, &text, &createdAt, &filedBy, &evidence, &originKind, &originIssueID, &depth, &verbatim, &law, &rationale); err != nil {
			return ContractView{}, err
		}
		if seen[statementID] {
			continue
		}
		seen[statementID] = true
		st := beads.Statement{
			ID:        statementID,
			Kind:      kind,
			Text:      text,
			CreatedAt: createdAt,
			FiledBy:   filedBy,
			Evidence:  evidence,
			Status:    "active",
			Scope:     "inherit",
			Verbatim:  verbatim,
			Law:       law,
			Rationale: rationale,
		}
		// Map view's origin to Statement.IssueID so existing renderer keeps its
		// bracket logic: self -> own id (no bracket), epic/blocks/binds ->
		// origin bead, project -> nil ([project]).
		switch originKind {
		case "project":
			st.IssueID = nil
		case "self":
			v := issueID
			st.IssueID = &v
		case "epic":
			if originIssueID.Valid {
				v := originIssueID.String
				st.IssueID = &v
			}
		case "blocks":
			if originIssueID.Valid {
				v := originIssueID.String
				st.IssueID = &v
			}
		case "binds":
			if originIssueID.Valid {
				v := originIssueID.String
				st.IssueID = &v
			}
		default:
			if originIssueID.Valid {
				v := originIssueID.String
				st.IssueID = &v
			}
		}
		switch kind {
		case "ruling":
			rulings = append(rulings, st)
		case "question":
			questions = append(questions, st)
		case "finding":
			findings = append(findings, st)
		}
	}
	if err := rows.Err(); err != nil {
		return ContractView{}, err
	}
	closed, err := s.ListStatements(ctx, StatementFilter{
		IssueIDs: []string{issueID},
		Kinds:    []string{"question"},
		Statuses: ClosedQuestionStatuses,
	})
	if err != nil {
		return ContractView{}, err
	}
	answerKinds := map[string]string{}
	for _, q := range closed {
		if q.AnsweredBy == nil || *q.AnsweredBy == "" {
			continue
		}
		if _, seen := answerKinds[*q.AnsweredBy]; seen {
			continue
		}
		a, err := s.GetStatement(ctx, *q.AnsweredBy)
		if err != nil {
			continue
		}
		answerKinds[*q.AnsweredBy] = a.Kind
	}
	return ContractView{
		Rulings:         rulings,
		Questions:       questions,
		Findings:        findings,
		ClosedQuestions: closed,
		AnswerKinds:     answerKinds,
	}, nil
}

func scanStatementRow(row *sql.Row) (*beads.Statement, error) {
	var (
		id, kind, text, filedBy, status, scope, evidence            string
		topic, workspace, concern, law, rationale, verbatim, author string
		sessionID, msgID, toolUseID, retireNote                     string
		createdAt                                                   time.Time
		nsIssue, nsSupersedes, nsAnswered, nsSource, nsBinds        sql.NullString
		ntChanged                                                   sql.NullTime
	)
	if err := row.Scan(&id, &kind, &nsIssue, &text, &createdAt, &filedBy, &status, &scope, &nsSupersedes, &nsAnswered, &nsSource, &evidence,
		&topic, &workspace, &concern, &law, &rationale, &verbatim, &author, &sessionID, &msgID, &toolUseID, &retireNote, &ntChanged, &nsBinds); err != nil {
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
		Topic:           topic,
		Workspace:       workspace,
		Concern:         concern,
		Law:             law,
		Rationale:       rationale,
		Verbatim:        verbatim,
		Author:          author,
		SessionID:       sessionID,
		MsgID:           msgID,
		ToolUseID:       toolUseID,
		RetireNote:      retireNote,
		ChangedAt:       nullTimeToPtr(ntChanged),
		BindsID:         nullStringToPtr(nsBinds),
	}, nil
}

func scanStatementRows(rows *sql.Rows) (*beads.Statement, error) {
	var (
		id, kind, text, filedBy, status, scope, evidence            string
		topic, workspace, concern, law, rationale, verbatim, author string
		sessionID, msgID, toolUseID, retireNote                     string
		createdAt                                                   time.Time
		nsIssue, nsSupersedes, nsAnswered, nsSource, nsBinds        sql.NullString
		ntChanged                                                   sql.NullTime
	)
	if err := rows.Scan(&id, &kind, &nsIssue, &text, &createdAt, &filedBy, &status, &scope, &nsSupersedes, &nsAnswered, &nsSource, &evidence,
		&topic, &workspace, &concern, &law, &rationale, &verbatim, &author, &sessionID, &msgID, &toolUseID, &retireNote, &ntChanged, &nsBinds); err != nil {
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
		Topic:           topic,
		Workspace:       workspace,
		Concern:         concern,
		Law:             law,
		Rationale:       rationale,
		Verbatim:        verbatim,
		Author:          author,
		SessionID:       sessionID,
		MsgID:           msgID,
		ToolUseID:       toolUseID,
		RetireNote:      retireNote,
		ChangedAt:       nullTimeToPtr(ntChanged),
		BindsID:         nullStringToPtr(nsBinds),
	}, nil
}

func (s *Store) ListAllComments(ctx context.Context) ([]beads.Comment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, issue_id, author, text, created_at FROM comments ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []beads.Comment
	for rows.Next() {
		var c beads.Comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Text, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ProvenancePointer locates the session, message and tool call that wrote a
// statement or a comment. It is a locator, not authority: annotating twice
// overwrites, and an empty SessionID means nothing was ever recorded.
type ProvenancePointer struct {
	SessionID string `json:"session_id"`
	MsgID     string `json:"msg_id"`
	ToolUseID string `json:"tool_use_id"`
}

// AnnotateStatement writes the provenance pointer onto a statement, overwriting
// whatever was there. Returns ErrNotFound when no statement carries the id.
func (s *Store) AnnotateStatement(ctx context.Context, id, sessionID, msgID, toolUseID string) error {
	q := s.rebind(`UPDATE statements SET session_id = ?, msg_id = ?, tool_use_id = ? WHERE id = ?`)
	res, err := s.db.ExecContext(ctx, q, sessionID, msgID, toolUseID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// StatementPointer reads the provenance pointer off a statement. The three
// columns are read with plain SQL rather than through beads.Statement, which
// does not carry them.
func (s *Store) StatementPointer(ctx context.Context, id string) (ProvenancePointer, error) {
	q := s.rebind(`SELECT session_id, msg_id, tool_use_id FROM statements WHERE id = ?`)
	var p ProvenancePointer
	err := s.db.QueryRowContext(ctx, q, id).Scan(&p.SessionID, &p.MsgID, &p.ToolUseID)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}
