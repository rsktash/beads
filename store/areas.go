package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Area kinds. A workspace is a directory root; a concern is a comma-separated
// glob list that cuts across roots.
const (
	AreaWorkspace = "workspace"
	AreaConcern   = "concern"
)

// AreaAll is the concern that matches every path. It is never returned by
// ResolvePath: if it came back on every resolve, every topic would carry it
// and the concern filter would stop narrowing. It stays queryable by name.
const AreaAll = "all"

// Area is one row of path_area_map plus the two counts the listing prints.
type Area struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Root  string `json:"root"`
	Paths string `json:"paths"`

	// Open is the number of non-closed issues resolving to this area.
	Open int `json:"open"`
	// Topics is the number of distinct non-empty statement topics on
	// statements carrying this area.
	Topics int `json:"topics"`
}

// Patterns splits the stored comma-separated glob list, dropping empties.
func (a Area) Patterns() []string {
	var out []string
	for _, p := range strings.Split(a.Paths, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Areas is a loaded vocabulary. Resolution is pure over it, so a caller that
// resolves many paths loads the vocabulary once.
type Areas []Area

// ErrAreaExists is returned when a (kind, name) is already in the vocabulary.
var ErrAreaExists = errors.New("area already exists")

// areaColumn maps an area kind to the statements column that names it.
func areaColumn(kind string) (string, error) {
	switch kind {
	case AreaWorkspace:
		return "workspace", nil
	case AreaConcern:
		return "concern", nil
	default:
		return "", fmt.Errorf("unknown area kind %q (want workspace or concern)", kind)
	}
}

// globMatch is the only path matcher in the codebase: path.Match semantics per
// segment, extended with ** for "any number of segments" (zero included).
func globMatch(pattern, p string) bool {
	return globSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func globSegments(pat, seg []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(seg); i++ {
				if globSegments(pat[1:], seg[i:]) {
					return true
				}
			}
			return false
		}
		if len(seg) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], seg[0])
		if err != nil || !ok {
			return false
		}
		pat, seg = pat[1:], seg[1:]
	}
	return len(seg) == 0
}

// ResolvePath returns the workspace whose root is a prefix of path (the
// longest one, so a nested root wins over its parent) and every concern with a
// glob matching it, in vocabulary order. The all concern is excluded.
func (as Areas) ResolvePath(p string) (workspace string, concerns []string) {
	p = strings.TrimPrefix(strings.TrimSpace(p), "./")
	if p == "" {
		return "", nil
	}
	bestRoot := 0
	for _, a := range as {
		switch a.Kind {
		case AreaWorkspace:
			if a.Root == "" || !strings.HasPrefix(p, a.Root) {
				continue
			}
			if len(a.Root) > bestRoot {
				bestRoot, workspace = len(a.Root), a.Name
			}
		case AreaConcern:
			if a.Name == AreaAll {
				continue
			}
			for _, pattern := range a.Patterns() {
				if globMatch(pattern, p) {
					concerns = append(concerns, a.Name)
					break
				}
			}
		}
	}
	return workspace, concerns
}

// resolvePaths resolves a set of paths to the union of their areas, each in
// vocabulary order and without duplicates.
func (as Areas) resolvePaths(paths []string) (workspaces, concerns []string) {
	ws := map[string]bool{}
	cs := map[string]bool{}
	for _, p := range paths {
		w, c := as.ResolvePath(p)
		if w != "" {
			ws[w] = true
		}
		for _, name := range c {
			cs[name] = true
		}
	}
	for _, a := range as {
		switch {
		case a.Kind == AreaWorkspace && ws[a.Name]:
			workspaces = append(workspaces, a.Name)
		case a.Kind == AreaConcern && cs[a.Name]:
			concerns = append(concerns, a.Name)
		}
	}
	return workspaces, concerns
}

// areaHeadingRE is the heading grammar cmd/bd/show.go's outlineHeadings uses.
// It is duplicated rather than shared: outlineHeadings lives in package main,
// which store cannot import, and the two must stay byte-compatible.
var areaHeadingRE = regexp.MustCompile(`^##+\s+(.+?)\s*$`)

var areaSlugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func areaSlugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = areaSlugNonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// filesSection returns the body of the description's `## Files` section, or ""
// when there is none.
func filesSection(desc string) string {
	if desc == "" {
		return ""
	}
	lines := strings.Split(desc, "\n")
	start := -1
	for i, l := range lines {
		m := areaHeadingRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if start >= 0 {
			return strings.Join(lines[start:i], "\n")
		}
		if areaSlugify(m[1]) == "files" {
			start = i + 1
		}
	}
	if start >= 0 {
		return strings.Join(lines[start:], "\n")
	}
	return ""
}

// listMarkerRE strips a leading bullet or ordered-list marker.
var listMarkerRE = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+`)

// sectionPaths reads one path per non-empty line: the list marker and every
// backtick are stripped, and the first whitespace-delimited token is the path.
// A trailing comma or semicolon is dropped so an inline list of paths yields
// its first entry rather than a token no glob can match.
func sectionPaths(body string) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		l = listMarkerRE.ReplaceAllString(l, "")
		l = strings.ReplaceAll(l, "`", "")
		fields := strings.Fields(l)
		if len(fields) == 0 {
			continue
		}
		p := strings.TrimRight(fields[0], ",;")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ListAreas returns the whole vocabulary, workspaces before concerns and in
// seed order within each kind, with the listing counts filled in.
func (s *Store) ListAreas(ctx context.Context) (Areas, error) {
	as, err := s.loadAreas(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.fillAreaCounts(ctx, as); err != nil {
		return nil, err
	}
	return as, nil
}

// loadAreas reads the vocabulary rows without the counts. Resolution paths use
// this: the counts cost a scan of every open issue.
func (s *Store) loadAreas(ctx context.Context) (Areas, error) {
	q := s.rebind(`SELECT id, kind, name, root, paths FROM path_area_map ORDER BY CASE kind WHEN 'workspace' THEN 0 ELSE 1 END, id`)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out Areas
	for rows.Next() {
		var a Area
		if err := rows.Scan(&a.ID, &a.Kind, &a.Name, &a.Root, &a.Paths); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// fillAreaCounts sets Open and Topics on every row.
func (s *Store) fillAreaCounts(ctx context.Context, as Areas) error {
	open := map[string]int{}
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT description FROM issues WHERE status != 'closed'`))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var desc sql.NullString
		if err := rows.Scan(&desc); err != nil {
			return err
		}
		ws, cs := as.resolvePaths(sectionPaths(filesSection(desc.String)))
		for _, w := range ws {
			open[AreaWorkspace+"\x00"+w]++
		}
		for _, c := range cs {
			open[AreaConcern+"\x00"+c]++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	topics := map[string]int{}
	for kind, col := range map[string]string{AreaWorkspace: "workspace", AreaConcern: "concern"} {
		q := s.rebind(fmt.Sprintf(`SELECT %s, COUNT(DISTINCT topic) FROM statements WHERE topic != '' AND %s != '' GROUP BY %s`, col, col, col))
		trows, err := s.db.QueryContext(ctx, q)
		if err != nil {
			return err
		}
		for trows.Next() {
			var name string
			var n int
			if err := trows.Scan(&name, &n); err != nil {
				trows.Close()
				return err
			}
			topics[kind+"\x00"+name] = n
		}
		err = trows.Err()
		trows.Close()
		if err != nil {
			return err
		}
	}

	for i := range as {
		key := as[i].Kind + "\x00" + as[i].Name
		as[i].Open = open[key]
		as[i].Topics = topics[key]
	}
	return nil
}

// AddArea inserts one vocabulary entry. A duplicate (kind, name) is refused by
// the table's unique index and reported as ErrAreaExists.
func (s *Store) AddArea(ctx context.Context, kind, name, root, paths string) error {
	if _, err := areaColumn(kind); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	q := s.rebind(`INSERT INTO path_area_map (kind, name, root, paths) VALUES (?, ?, ?, ?)`)
	if _, err := s.db.ExecContext(ctx, q, kind, name, root, paths); err != nil {
		if exists, qerr := s.areaExists(ctx, kind, name); qerr == nil && exists {
			return fmt.Errorf("%s %q already exists: %w", kind, name, ErrAreaExists)
		}
		return err
	}
	return nil
}

func (s *Store) areaExists(ctx context.Context, kind, name string) (bool, error) {
	var n int
	q := s.rebind(`SELECT COUNT(*) FROM path_area_map WHERE kind = ? AND name = ?`)
	if err := s.db.QueryRowContext(ctx, q, kind, name).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// RenameArea renames one vocabulary entry and rewrites every statement naming
// it, in one transaction, so no statement is left naming an entry that is gone.
func (s *Store) RenameArea(ctx context.Context, kind, oldName, newName string) error {
	col, err := areaColumn(kind)
	if err != nil {
		return err
	}
	oldName, newName = strings.TrimSpace(oldName), strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("old and new names are required")
	}
	if oldName == newName {
		return fmt.Errorf("%s %q: new name is the old name", kind, oldName)
	}
	exists, err := s.areaExists(ctx, kind, newName)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%s %q already exists: %w", kind, newName, ErrAreaExists)
	}
	return s.inAreaTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, s.rebind(`UPDATE path_area_map SET name = ? WHERE kind = ? AND name = ?`), newName, kind, oldName)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%s %q: %w", kind, oldName, ErrNotFound)
		}
		_, err = tx.ExecContext(ctx, s.rebind(fmt.Sprintf(`UPDATE statements SET %s = ? WHERE %s = ?`, col, col)), newName, oldName)
		return err
	})
}

// MergeAreas folds one vocabulary entry into another: every statement naming
// from is rewritten to into and the from row is dropped, in one transaction.
func (s *Store) MergeAreas(ctx context.Context, kind, from, into string) error {
	col, err := areaColumn(kind)
	if err != nil {
		return err
	}
	from, into = strings.TrimSpace(from), strings.TrimSpace(into)
	if from == "" || into == "" {
		return fmt.Errorf("from and into names are required")
	}
	if from == into {
		return fmt.Errorf("%s %q: cannot merge into itself", kind, from)
	}
	exists, err := s.areaExists(ctx, kind, into)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s %q: %w", kind, into, ErrNotFound)
	}
	return s.inAreaTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM path_area_map WHERE kind = ? AND name = ?`), kind, from)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%s %q: %w", kind, from, ErrNotFound)
		}
		_, err = tx.ExecContext(ctx, s.rebind(fmt.Sprintf(`UPDATE statements SET %s = ? WHERE %s = ?`, col, col)), into, from)
		return err
	})
}

func (s *Store) inAreaTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
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
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// ResolveIssue resolves a bead to the areas its `## Files` section binds. A
// bead with no such section resolves to nothing, which is not an error.
func (s *Store) ResolveIssue(ctx context.Context, issueID string) (workspaces, concerns []string, err error) {
	i, err := s.GetIssue(ctx, issueID)
	if err != nil {
		return nil, nil, err
	}
	as, err := s.loadAreas(ctx)
	if err != nil {
		return nil, nil, err
	}
	body := filesSection(i.Description)
	if strings.TrimSpace(body) == "" {
		return nil, nil, nil
	}
	ws, cs := as.resolvePaths(sectionPaths(body))
	return ws, cs, nil
}
