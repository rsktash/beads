package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const maxCitationFileBytes = 2 << 20

var (
	citationRE = regexp.MustCompile(`(?m)\b([A-Za-z0-9_./-]+\.[A-Za-z0-9]+)::([A-Za-z_$][A-Za-z0-9_$]*)(?::(\d+))?\b`)
	bareLineRE = regexp.MustCompile(`(?m)\b([A-Za-z0-9_./-]+\.[A-Za-z0-9]+):(\d+)\b`)
)

// Citation identifies a declaration in a repository. Line is an optional hint.
type Citation struct {
	Path   string
	Symbol string
	Line   int
}

func (c Citation) String() string {
	ref := c.Path + "::" + c.Symbol
	if c.Line > 0 {
		ref += ":" + strconv.Itoa(c.Line)
	}
	return ref
}

// CitationStatus is the current working-tree state of a citation.
type CitationStatus string

const (
	CitationLive  CitationStatus = "live"
	CitationMoved CitationStatus = "moved"
	CitationStale CitationStatus = "stale"
)

// CitationState reports the state and the first declaration line, when found.
type CitationState struct {
	Status CitationStatus
	Line   int
}

// ParseCitations extracts symbol-first citations in source order.
func ParseCitations(text string) []Citation {
	matches := citationRE.FindAllStringSubmatch(text, -1)
	out := make([]Citation, 0, len(matches))
	for _, match := range matches {
		line := 0
		if match[3] != "" {
			line, _ = strconv.Atoi(match[3])
		}
		out = append(out, Citation{Path: match[1], Symbol: match[2], Line: line})
	}
	return out
}

// BareLineCitation returns the first path:line reference that has no symbol.
func BareLineCitation(text string) (path string, line int, ok bool) {
	for _, loc := range bareLineRE.FindAllStringSubmatchIndex(text, -1) {
		start := loc[0]
		if start >= 2 && text[start-2:start] == "::" {
			continue
		}
		line, _ := strconv.Atoi(text[loc[4]:loc[5]])
		return text[loc[2]:loc[3]], line, true
	}
	return "", 0, false
}

// ResolveCitation searches the citation's file in the current working tree.
func ResolveCitation(root string, c Citation) (CitationState, error) {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(c.Path)))
	if errors.Is(err, os.ErrNotExist) {
		return CitationState{Status: CitationStale}, nil
	}
	if err != nil {
		return CitationState{}, err
	}
	defer f.Close()

	content, err := io.ReadAll(io.LimitReader(f, maxCitationFileBytes))
	if err != nil {
		return CitationState{}, err
	}
	matches := citationDeclarationMatches(content, c.Symbol)
	if len(matches) == 0 {
		return CitationState{Status: CitationStale}, nil
	}
	line := 1 + strings.Count(string(content[:matches[0][1]]), "\n")
	if len(matches) > 1 || (c.Line > 0 && c.Line != line) {
		return CitationState{Status: CitationMoved, Line: line}, nil
	}
	return CitationState{Status: CitationLive, Line: line}, nil
}

// CitationDeclaration returns the first declaration line from historical source.
func CitationDeclaration(content []byte, c Citation) (string, bool) {
	matches := citationDeclarationMatches(content, c.Symbol)
	if len(matches) == 0 {
		return "", false
	}
	line := 1 + strings.Count(string(content[:matches[0][1]]), "\n")
	lines := strings.Split(string(content), "\n")
	if line < 1 || line > len(lines) {
		return "", false
	}
	return lines[line-1], true
}

func citationDeclarationMatches(content []byte, symbol string) [][]int {
	declarationRE := regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:async\s+)?(?:func|const|let|var|type|class|function|interface)\b[^\n]*\b` + regexp.QuoteMeta(symbol) + `\b`)
	return declarationRE.FindAllIndex(content, -1)
}

// SetStatementHeadSHA records the repository commit current when a statement is filed.
func (s *Store) SetStatementHeadSHA(ctx context.Context, id, sha string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE statements SET head_sha = ? WHERE id = ?`), sha, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// StatementHeadSHA reads the commit recorded for a statement.
func (s *Store) StatementHeadSHA(ctx context.Context, id string) (string, error) {
	var sha string
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT head_sha FROM statements WHERE id = ?`), id).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("statement head SHA: %w", err)
	}
	return sha, nil
}
