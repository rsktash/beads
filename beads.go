// Package beads is the public API of the beads issue tracker. The core types
// (Issue, Status, Priority, IssueType, DependencyType, Comment, Event, Labels)
// live here; storage logic is in store/; the CLI is cmd/bd.
package beads

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusClosed     Status = "closed"
	StatusPinned     Status = "pinned" // upstream: blockers in 'pinned' don't actually block
)

func (s Status) Valid() bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusBlocked, StatusClosed, StatusPinned:
		return true
	}
	return false
}

func ParseStatus(s string) (Status, error) {
	v := Status(strings.ToLower(strings.TrimSpace(s)))
	if !v.Valid() {
		return "", fmt.Errorf("invalid status %q (open|in_progress|blocked|closed|pinned)", s)
	}
	return v, nil
}

type IssueType string

// Bead types from upstream. Most map 1:1 to a row of this type with extra
// columns set: messages have `sender`+`thread_id`; events fill `event_kind`/
// `actor`/`target`/`payload`; molecules/roles/wisps use `mol_type`/`role_type`/
// `wisp_type`.
const (
	TypeTask     IssueType = "task"
	TypeBug      IssueType = "bug"
	TypeEpic     IssueType = "epic"
	TypeFeature  IssueType = "feature"
	TypeMessage  IssueType = "message"
	TypeWisp     IssueType = "wisp"
	TypeMolecule IssueType = "molecule"
	TypeRole     IssueType = "role"
	TypeEvent    IssueType = "event"
)

func (t IssueType) Valid() bool {
	switch t {
	case TypeTask, TypeBug, TypeEpic, TypeFeature, TypeMessage,
		TypeWisp, TypeMolecule, TypeRole, TypeEvent:
		return true
	}
	return false
}

func ParseType(s string) (IssueType, error) {
	v := IssueType(strings.ToLower(strings.TrimSpace(s)))
	if !v.Valid() {
		return "", fmt.Errorf("invalid issue_type %q", s)
	}
	return v, nil
}

type DependencyType string

const (
	DepBlocks       DependencyType = "blocks"
	DepRelatesTo    DependencyType = "related"
	DepDuplicates   DependencyType = "duplicates"
	DepSupersedes   DependencyType = "supersedes"
	DepRepliesTo    DependencyType = "replies-to"
	DepParentChild  DependencyType = "parent-child"
	DepDiscoveredBy DependencyType = "discovered-by"
)

func (d DependencyType) Valid() bool {
	switch d {
	case DepBlocks, DepRelatesTo, DepDuplicates, DepSupersedes,
		DepRepliesTo, DepParentChild, DepDiscoveredBy:
		return true
	}
	return false
}

func ParseDependencyType(s string) (DependencyType, error) {
	v := DependencyType(strings.ToLower(strings.TrimSpace(s)))
	if !v.Valid() {
		return "", fmt.Errorf("invalid dependency type %q", s)
	}
	return v, nil
}

// Issue is the polymorphic bead row. Type-specific subsystems (await/hook/
// agent/compaction) are not yet implemented; their columns are deliberately
// omitted from this v0.1 model.
type Issue struct {
	ID                 string `db:"id" json:"id"`
	ContentHash        string `db:"content_hash" json:"content_hash,omitempty"`
	Title              string `db:"title" json:"title"`
	Description        string `db:"description" json:"description,omitempty"`
	Design             string `db:"design" json:"design,omitempty"`
	AcceptanceCriteria string `db:"acceptance_criteria" json:"acceptance_criteria,omitempty"`
	Notes              string `db:"notes" json:"notes,omitempty"`

	Status           Status    `db:"status" json:"status"`
	Priority         int       `db:"priority" json:"priority"`
	Type             IssueType `db:"issue_type" json:"issue_type"`
	Assignee         string    `db:"assignee" json:"assignee,omitempty"`
	EstimatedMinutes int       `db:"estimated_minutes" json:"estimated_minutes,omitempty"`

	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	CreatedBy       string     `db:"created_by" json:"created_by,omitempty"`
	Owner           string     `db:"owner" json:"owner,omitempty"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
	ClosedAt        *time.Time `db:"closed_at" json:"closed_at,omitempty"`
	ClosedBySession string     `db:"closed_by_session" json:"closed_by_session,omitempty"`

	ExternalRef  string `db:"external_ref" json:"external_ref,omitempty"`
	SpecID       string `db:"spec_id" json:"spec_id,omitempty"`
	Metadata     string `db:"metadata" json:"metadata,omitempty"` // JSON
	SourceRepo   string `db:"source_repo" json:"source_repo,omitempty"`
	SourceSystem string `db:"source_system" json:"source_system,omitempty"`
	CloseReason  string `db:"close_reason" json:"close_reason,omitempty"`

	// Discriminator/type-specific:
	Sender     string `db:"sender" json:"sender,omitempty"`
	Ephemeral  bool   `db:"ephemeral" json:"ephemeral,omitempty"`
	Pinned     bool   `db:"pinned" json:"pinned,omitempty"`
	IsTemplate bool   `db:"is_template" json:"is_template,omitempty"`
	WispType   string `db:"wisp_type" json:"wisp_type,omitempty"`
	MolType    string `db:"mol_type" json:"mol_type,omitempty"`
	RoleType   string `db:"role_type" json:"role_type,omitempty"`
	EventKind  string `db:"event_kind" json:"event_kind,omitempty"`
	Actor      string `db:"actor" json:"actor,omitempty"`
	Target     string `db:"target" json:"target,omitempty"`
	Payload    string `db:"payload" json:"payload,omitempty"`

	StartedAt  *time.Time `db:"started_at" json:"started_at,omitempty"`
	DueAt      *time.Time `db:"due_at" json:"due_at,omitempty"`
	DeferUntil *time.Time `db:"defer_until" json:"defer_until,omitempty"`

	// Populated only by reads that join: store.GetIssue does not load these.
	Labels Labels `db:"-" json:"labels,omitempty"`
}

// Dependency is an edge between two issues. PK is (IssueID, DependsOnID),
// matching upstream — i.e. one pair carries one type at a time.
type Dependency struct {
	IssueID     string         `db:"issue_id" json:"issue_id"`
	DependsOnID string         `db:"depends_on_id" json:"depends_on_id"`
	Type        DependencyType `db:"type" json:"type"`
	CreatedAt   time.Time      `db:"created_at" json:"created_at"`
	CreatedBy   string         `db:"created_by" json:"created_by,omitempty"`
	Metadata    string         `db:"metadata" json:"metadata,omitempty"`
	ThreadID    string         `db:"thread_id" json:"thread_id,omitempty"`
}

type Comment struct {
	ID        string    `db:"id" json:"id"`
	IssueID   string    `db:"issue_id" json:"issue_id"`
	Author    string    `db:"author" json:"author"`
	Text      string    `db:"text" json:"text"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type Event struct {
	ID        string    `db:"id" json:"id"`
	IssueID   string    `db:"issue_id" json:"issue_id"`
	EventType string    `db:"event_type" json:"event_type"`
	Actor     string    `db:"actor" json:"actor"`
	OldValue  string    `db:"old_value" json:"old_value,omitempty"`
	NewValue  string    `db:"new_value" json:"new_value,omitempty"`
	Comment   string    `db:"comment" json:"comment,omitempty"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// Labels is the slice of label strings attached to an issue. JSON marshals as
// an array.
type Labels []string

func (l Labels) Value() (string, error) {
	if len(l) == 0 {
		return "", nil
	}
	b, err := json.Marshal([]string(l))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (l *Labels) Scan(src any) error {
	if src == nil {
		*l = nil
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("unsupported labels src %T", src)
	}
	if len(b) == 0 {
		*l = nil
		return nil
	}
	return json.Unmarshal(b, (*[]string)(l))
}
