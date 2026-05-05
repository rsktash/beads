package types

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
)

func (s Status) Valid() bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusBlocked, StatusClosed:
		return true
	}
	return false
}

func ParseStatus(s string) (Status, error) {
	v := Status(strings.ToLower(strings.TrimSpace(s)))
	if !v.Valid() {
		return "", fmt.Errorf("invalid status %q (open|in_progress|blocked|closed)", s)
	}
	return v, nil
}

type IssueType string

const (
	TypeTask    IssueType = "task"
	TypeBug     IssueType = "bug"
	TypeEpic    IssueType = "epic"
	TypeFeature IssueType = "feature"
	TypeMessage IssueType = "message"
)

func (t IssueType) Valid() bool {
	switch t {
	case TypeTask, TypeBug, TypeEpic, TypeFeature, TypeMessage:
		return true
	}
	return false
}

func ParseType(s string) (IssueType, error) {
	v := IssueType(strings.ToLower(strings.TrimSpace(s)))
	if !v.Valid() {
		return "", fmt.Errorf("invalid type %q (task|bug|epic|feature|message)", s)
	}
	return v, nil
}

type DependencyType string

const (
	DepBlocks     DependencyType = "blocks"
	DepRelatesTo  DependencyType = "relates_to"
	DepDuplicates DependencyType = "duplicates"
	DepSupersedes DependencyType = "supersedes"
	DepRepliesTo  DependencyType = "replies_to"
	DepParentOf   DependencyType = "parent_of"
)

func (d DependencyType) Valid() bool {
	switch d {
	case DepBlocks, DepRelatesTo, DepDuplicates, DepSupersedes, DepRepliesTo, DepParentOf:
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

type Issue struct {
	ID          string    `db:"id" json:"id"`
	Title       string    `db:"title" json:"title"`
	Description string    `db:"description" json:"description"`
	Type        IssueType `db:"type" json:"type"`
	Status      Status    `db:"status" json:"status"`
	Priority    int       `db:"priority" json:"priority"`
	Assignee    string    `db:"assignee" json:"assignee"`
	Labels      Labels    `db:"labels" json:"labels"`
	ParentID    string    `db:"parent_id" json:"parent_id,omitempty"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
	ClosedAt    *time.Time `db:"closed_at" json:"closed_at,omitempty"`
}

type Dependency struct {
	FromID    string         `db:"from_id" json:"from_id"`
	ToID      string         `db:"to_id" json:"to_id"`
	Type      DependencyType `db:"type" json:"type"`
	CreatedAt time.Time      `db:"created_at" json:"created_at"`
}

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
