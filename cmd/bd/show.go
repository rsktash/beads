package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// outlineDefaultThreshold — descriptions longer than this default to an
// outline ('## headings' table + line ranges) rather than dumping the body.
// Picked off the histogram in the bd-usage analysis: typical task bodies are
// 200-800 bytes; epics are 2-17KB. 2048 sits in the dead zone between them.
const outlineDefaultThreshold = 2048

func newShowCmd() *cobra.Command {
	var (
		section  string
		full     bool
		outline  bool
		include  []string
		maxBytes int
		linesStr string
		headN    int
		tailN    int
	)
	cmd := &cobra.Command{
		Use:   "show <id> [<id> ...]",
		Short: "Show one or more beads. Long descriptions default to an outline; use --full or --section <slug>.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			lineSlice, err := parseLineSlice(linesStr, headN, tailN)
			if err != nil {
				return err
			}
			opts := showOpts{
				section:    section,
				full:       full,
				outline:    outline,
				lineSlice:  lineSlice,
				include:    parseIncludeSet(include),
				wantJSON:   cc.json,
				multiCount: len(args),
			}

			out := bufferedWriter(maxBytes)

			if cc.json {
				rows := make([]any, 0, len(args))
				for _, id := range args {
					row, err := buildShowJSON(cc, id, opts)
					if err != nil {
						return err
					}
					rows = append(rows, row)
				}
				if err := writeJSONTo(out, rows); err != nil {
					return err
				}
			} else {
				for idx, id := range args {
					if idx > 0 {
						fmt.Fprintln(out, "\n---")
					}
					if err := printShowHuman(out, cc, id, opts); err != nil {
						return err
					}
				}
			}
			return out.flush(os.Stdout)
		},
	}
	cmd.Flags().StringVar(&section, "section", "", "show only one markdown ## section (slug match) of the description")
	cmd.Flags().BoolVar(&full, "full", false, "force full description body even when long (overrides outline default)")
	cmd.Flags().BoolVar(&outline, "outline", false, "force outline of the description (don't print the body)")
	cmd.Flags().StringSliceVar(&include, "include", nil, "extra payload: comments,labels,deps (deps+labels are included by default in text mode; comments are not)")
	cmd.Flags().IntVar(&maxBytes, "max-bytes", 0, "cap stdout at N bytes and append a truncation marker")
	cmd.Flags().StringVar(&linesStr, "lines", "", "slice description by 1-indexed line range: START-END, START-, or -END")
	cmd.Flags().IntVar(&headN, "head", 0, "show only the first N lines of the description (mutually exclusive with --lines/--tail)")
	cmd.Flags().IntVar(&tailN, "tail", 0, "show only the last N lines of the description (mutually exclusive with --lines/--head)")
	return cmd
}

type showOpts struct {
	section    string
	full       bool
	outline    bool
	lineSlice  lineSlice
	include    includeSet
	wantJSON   bool
	multiCount int
}

type includeSet struct {
	comments bool
	labels   bool
	deps     bool
	all      bool
}

func parseIncludeSet(raw []string) includeSet {
	var s includeSet
	for _, r := range raw {
		switch strings.ToLower(strings.TrimSpace(r)) {
		case "comments":
			s.comments = true
		case "labels":
			s.labels = true
		case "deps", "dependencies":
			s.deps = true
		case "all":
			s.all = true
			s.comments = true
			s.labels = true
			s.deps = true
		}
	}
	return s
}

func buildShowJSON(cc *cmdCtx, id string, opts showOpts) (any, error) {
	i, err := cc.store.GetIssue(cc.ctx, id)
	if err != nil {
		return nil, err
	}

	type body struct {
		*beadsIssueAlias
		Dependencies       any    `json:"dependencies,omitempty"`
		Comments           any    `json:"comments,omitempty"`
		CommentsCount      *int   `json:"comments_count,omitempty"`
		Outline            any    `json:"description_outline,omitempty"`
		DescriptionSection string `json:"description_section,omitempty"`
		DescriptionSlice   string `json:"description_slice,omitempty"`
	}
	out := body{beadsIssueAlias: (*beadsIssueAlias)(i)}

	// Labels: kept lightweight, include in default JSON (it's already in the
	// existing surface and rarely large).
	if opts.include.labels || !opts.include.deps && !opts.include.comments {
		labels, err := cc.store.ListLabels(cc.ctx, id)
		if err != nil {
			return nil, err
		}
		i.Labels = labels
	}

	// Dependencies: still default-included for JSON since skills already
	// consume `.[0].dependencies`. Skipping them costs an extra round trip
	// downstream more often than it saves bytes.
	deps, err := cc.store.ListDependencies(cc.ctx, id)
	if err != nil {
		return nil, err
	}
	out.Dependencies = deps

	// Comments: behind --include comments. Default emits a count only.
	if opts.include.comments {
		cs, err := cc.store.ListComments(cc.ctx, id)
		if err != nil {
			return nil, err
		}
		out.Comments = cs
		n := len(cs)
		out.CommentsCount = &n
	} else {
		cs, err := cc.store.ListComments(cc.ctx, id)
		if err != nil {
			return nil, err
		}
		n := len(cs)
		out.CommentsCount = &n
	}

	// Description handling: section/outline/slice rewrite the Description.
	// Order: --section narrows first, then --lines/--head/--tail slice the
	// remainder. If neither is given, fall back to outline-vs-body default.
	desc := i.Description
	switch {
	case opts.section != "":
		picked, ok := extractSection(desc, opts.section)
		if !ok {
			return nil, sectionNotFoundError(id, desc, opts.section)
		}
		i.Description = opts.lineSlice.apply(picked)
		out.DescriptionSection = opts.section
		if s := opts.lineSlice.describe(); s != "" {
			out.DescriptionSlice = s
		}
	case !opts.lineSlice.empty():
		i.Description = opts.lineSlice.apply(desc)
		out.DescriptionSlice = opts.lineSlice.describe()
	case opts.outline || (!opts.full && len(desc) >= outlineDefaultThreshold):
		ol := describeOutline(desc)
		out.Outline = ol
		i.Description = ""
	}
	return out, nil
}

func printShowHuman(w io.Writer, cc *cmdCtx, id string, opts showOpts) error {
	i, err := cc.store.GetIssue(cc.ctx, id)
	if err != nil {
		return err
	}
	labels, err := cc.store.ListLabels(cc.ctx, id)
	if err != nil {
		return err
	}
	i.Labels = labels
	deps, err := cc.store.ListDependencies(cc.ctx, id)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "%s  [%s] %s p%d %s\n", i.ID, i.Status, i.Type, i.Priority, i.Title)
	if i.Assignee != "" {
		fmt.Fprintf(w, "assignee: %s\n", i.Assignee)
	}
	if len(labels) > 0 {
		fmt.Fprintf(w, "labels:   %s\n", strings.Join(labels, ", "))
	}
	fmt.Fprintf(w, "created:  %s\n", i.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "updated:  %s\n", i.UpdatedAt.Format("2006-01-02 15:04:05"))
	if i.DueAt != nil {
		fmt.Fprintf(w, "due:      %s\n", i.DueAt.Format("2006-01-02 15:04:05"))
	}
	if i.DeferUntil != nil {
		fmt.Fprintf(w, "defer:    %s\n", i.DeferUntil.Format("2006-01-02 15:04:05"))
	}
	if i.ClosedAt != nil {
		fmt.Fprintf(w, "closed:   %s (%s)\n", i.ClosedAt.Format("2006-01-02 15:04:05"), i.CloseReason)
	}

	desc := i.Description
	if opts.section != "" {
		body, ok := extractSection(desc, opts.section)
		if !ok {
			return sectionNotFoundError(id, desc, opts.section)
		}
		body = opts.lineSlice.apply(body)
		if body != "" {
			fmt.Fprintln(w, "\n"+body)
		}
	} else if !opts.lineSlice.empty() {
		body := opts.lineSlice.apply(desc)
		if body != "" {
			fmt.Fprintln(w, "\n"+body)
		}
	} else if opts.outline || (!opts.full && len(desc) >= outlineDefaultThreshold) {
		fmt.Fprintf(w, "\ndescription: %d chars  (use --full or --section <slug>)\n", len(desc))
		if hs := outlineHeadings(desc); len(hs) > 0 {
			fmt.Fprintln(w, "sections:")
			for _, h := range hs {
				fmt.Fprintf(w, "  %-32s lines %d-%d\n", h.heading, h.startLine, h.endLine)
			}
		} else {
			fmt.Fprintln(w, "(no ## headings — use --full to read the body)")
		}
	} else if desc != "" {
		fmt.Fprintln(w, "\n"+desc)
	}

	if len(deps) > 0 {
		fmt.Fprintln(w, "\ndependencies:")
		for _, d := range deps {
			arrow, other := "->", d.DependsOnID
			if d.DependsOnID == id {
				arrow, other = "<-", d.IssueID
			}
			fmt.Fprintf(w, "  %s %s %s\n", arrow, d.Type, other)
		}
	}

	// Comments: include count in text mode always (it's tiny). Body behind
	// --include comments.
	cs, err := cc.store.ListComments(cc.ctx, id)
	if err != nil {
		return err
	}
	if len(cs) > 0 {
		if opts.include.comments {
			fmt.Fprintln(w, "\n--- comments ---")
			for _, c := range cs {
				fmt.Fprintf(w, "  [%s] %s: %s\n", c.CreatedAt.Format("2006-01-02 15:04"), c.Author, c.Text)
			}
		} else {
			fmt.Fprintf(w, "\ncomments: %d  (use --include comments)\n", len(cs))
		}
	}
	return nil
}

// --- description outlining ---

var headingRE = regexp.MustCompile(`^##+\s+(.+?)\s*$`)

type sectionInfo struct {
	heading   string
	slug      string
	startLine int
	endLine   int
	body      string
}

func outlineHeadings(desc string) []sectionInfo {
	if desc == "" {
		return nil
	}
	lines := strings.Split(desc, "\n")
	var out []sectionInfo
	for idx, l := range lines {
		m := headingRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		out = append(out, sectionInfo{
			heading:   strings.TrimSpace(m[1]),
			slug:      slugify(m[1]),
			startLine: idx + 1,
		})
	}
	for i := range out {
		if i+1 < len(out) {
			out[i].endLine = out[i+1].startLine - 1
		} else {
			out[i].endLine = len(lines)
		}
	}
	return out
}

func describeOutline(desc string) []map[string]any {
	hs := outlineHeadings(desc)
	if len(hs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(hs))
	for _, h := range hs {
		out = append(out, map[string]any{
			"heading":    h.heading,
			"slug":       h.slug,
			"start_line": h.startLine,
			"end_line":   h.endLine,
		})
	}
	return out
}

// sectionNotFoundError builds the user-facing error returned when --section
// doesn't match. Cobra writes the error to stderr and main.go sets exit=1.
// We include the available slug list so the caller can fix the typo without
// re-running `bd show <id>` to discover the outline.
func sectionNotFoundError(id, desc, requested string) error {
	hs := outlineHeadings(desc)
	if len(hs) == 0 {
		return fmt.Errorf("section %q not found in %s (the description has no ## headings)", requested, id)
	}
	slugs := make([]string, len(hs))
	for i, h := range hs {
		slugs[i] = h.slug
	}
	return fmt.Errorf("section %q not found in %s. available: %s", requested, id, strings.Join(slugs, ", "))
}

// extractSection returns the body of the section whose slug matches `key`
// (substring match in priority order: exact slug, prefix slug, substring on
// heading). The body excludes the heading line itself.
func extractSection(desc, key string) (string, bool) {
	hs := outlineHeadings(desc)
	if len(hs) == 0 {
		return "", false
	}
	want := slugify(key)
	candidates := []func(sectionInfo) bool{
		func(s sectionInfo) bool { return s.slug == want },
		func(s sectionInfo) bool { return strings.HasPrefix(s.slug, want) },
		func(s sectionInfo) bool { return strings.Contains(s.slug, want) },
	}
	lines := strings.Split(desc, "\n")
	for _, match := range candidates {
		var picks []sectionInfo
		for _, h := range hs {
			if match(h) {
				picks = append(picks, h)
			}
		}
		if len(picks) == 0 {
			continue
		}
		// Stable ordering by source position.
		sort.SliceStable(picks, func(i, j int) bool { return picks[i].startLine < picks[j].startLine })
		var b strings.Builder
		for n, p := range picks {
			if n > 0 {
				b.WriteString("\n")
			}
			// body = lines (startLine .. endLine-1] excluding the heading line
			if p.startLine < p.endLine {
				body := strings.Join(lines[p.startLine:p.endLine], "\n")
				b.WriteString(strings.Trim(body, "\n"))
			}
		}
		return b.String(), true
	}
	return "", false
}

// --- line-range slicing ---

// lineSlice is a 1-indexed half-open description selector. Zero value means
// "no slice". When `head` or `tail` is set, `start`/`end` are ignored.
type lineSlice struct {
	start int // 1-indexed inclusive; 0 means "from line 1"
	end   int // 1-indexed inclusive; 0 means "to last line"
	head  int // first N lines; 0 means unset
	tail  int // last N lines; 0 means unset
}

func (s lineSlice) empty() bool {
	return s.start == 0 && s.end == 0 && s.head == 0 && s.tail == 0
}

// describe renders the slice as a short human/JSON-friendly string. Used by
// `bd show --json` to populate description_slice so consumers can tell the
// description is an excerpt rather than the full body.
func (s lineSlice) describe() string {
	switch {
	case s.head > 0:
		return fmt.Sprintf("head %d", s.head)
	case s.tail > 0:
		return fmt.Sprintf("tail %d", s.tail)
	case s.start > 0 && s.end > 0 && s.start == s.end:
		return fmt.Sprintf("line %d", s.start)
	case s.start > 0 && s.end > 0:
		return fmt.Sprintf("lines %d-%d", s.start, s.end)
	case s.start > 0:
		return fmt.Sprintf("lines %d-", s.start)
	case s.end > 0:
		return fmt.Sprintf("lines -%d", s.end)
	}
	return ""
}

func (s lineSlice) apply(body string) string {
	if body == "" || s.empty() {
		return body
	}
	// Strip trailing newline so split doesn't yield a spurious empty line.
	trimmed := strings.TrimRight(body, "\n")
	lines := strings.Split(trimmed, "\n")
	n := len(lines)
	switch {
	case s.head > 0:
		if s.head >= n {
			return body
		}
		return strings.Join(lines[:s.head], "\n")
	case s.tail > 0:
		if s.tail >= n {
			return body
		}
		return strings.Join(lines[n-s.tail:], "\n")
	}
	lo, hi := s.start, s.end
	if lo <= 0 {
		lo = 1
	}
	if hi <= 0 || hi > n {
		hi = n
	}
	if lo > n {
		return ""
	}
	if lo > hi {
		return ""
	}
	return strings.Join(lines[lo-1:hi], "\n")
}

var lineRangeRE = regexp.MustCompile(`^\s*(\d*)\s*-\s*(\d*)\s*$`)

// parseLineSlice unifies --lines / --head / --tail into a single selector.
// At most one flag may be set; passing more than one is a user error.
func parseLineSlice(raw string, head, tail int) (lineSlice, error) {
	set := 0
	if strings.TrimSpace(raw) != "" {
		set++
	}
	if head > 0 {
		set++
	}
	if tail > 0 {
		set++
	}
	if set > 1 {
		return lineSlice{}, fmt.Errorf("--lines, --head, --tail are mutually exclusive")
	}
	if head > 0 {
		return lineSlice{head: head}, nil
	}
	if tail > 0 {
		return lineSlice{tail: tail}, nil
	}
	r := strings.TrimSpace(raw)
	if r == "" {
		return lineSlice{}, nil
	}
	// Allow bare "N" as shorthand for "N-N" (read one line).
	if !strings.Contains(r, "-") {
		var n int
		if _, err := fmt.Sscanf(r, "%d", &n); err != nil || n <= 0 {
			return lineSlice{}, fmt.Errorf("invalid --lines %q", raw)
		}
		return lineSlice{start: n, end: n}, nil
	}
	m := lineRangeRE.FindStringSubmatch(r)
	if m == nil {
		return lineSlice{}, fmt.Errorf("invalid --lines %q (expected START-END, START-, -END, or N)", raw)
	}
	var s, e int
	if m[1] != "" {
		fmt.Sscanf(m[1], "%d", &s)
	}
	if m[2] != "" {
		fmt.Sscanf(m[2], "%d", &e)
	}
	if s == 0 && e == 0 {
		return lineSlice{}, fmt.Errorf("invalid --lines %q (empty range)", raw)
	}
	if s > 0 && e > 0 && s > e {
		return lineSlice{}, fmt.Errorf("invalid --lines %q (start > end)", raw)
	}
	return lineSlice{start: s, end: e}, nil
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// --- max-bytes truncation ---

type capWriter struct {
	buf *bytes.Buffer
	cap int
}

func bufferedWriter(maxBytes int) *capWriter {
	return &capWriter{buf: &bytes.Buffer{}, cap: maxBytes}
}

func (c *capWriter) Write(p []byte) (int, error) {
	return c.buf.Write(p)
}

func (c *capWriter) flush(dst io.Writer) error {
	b := c.buf.Bytes()
	if c.cap > 0 && len(b) > c.cap {
		extra := len(b) - c.cap
		if _, err := dst.Write(b[:c.cap]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(dst, "\n[truncated, +%d more bytes]\n", extra)
		return err
	}
	_, err := dst.Write(b)
	return err
}
