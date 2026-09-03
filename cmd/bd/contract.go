package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// renderContractSections renders the EXECUTION CONTRACT sections after the
// metadata header. It is the single source of truth for body, dependencies
// and untyped history rendering; both printShowHuman and workfile go through it.
func renderContractSections(w io.Writer, issue *beads.Issue, cv store.ContractView, deps []beads.Dependency, comments []beads.Comment, opts showOpts) error {
	hasStatements := len(cv.Rulings) > 0 || len(cv.Questions) > 0 || len(cv.Findings) > 0 || len(cv.ClosedQuestions) > 0

	// Sort comments newest first for both placements.
	sorted := make([]beads.Comment, len(comments))
	copy(sorted, comments)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
		}
		return sorted[i].ID > sorted[j].ID
	})

	if !hasStatements && len(sorted) > 0 {
		// Legacy degradation: untyped history above base text with provenance banner.
		fmt.Fprintln(w, "\nUNTYPED HISTORY — provenance unknown, rulings may be buried here — newest first")
		for _, c := range sorted {
			fmt.Fprintf(w, "  [%s] %s: %s\n", c.CreatedAt.Format("2006-01-02 15:04"), c.Author, c.Text)
		}
		if err := renderBaseText(w, issue, opts); err != nil {
			return err
		}
		if len(deps) > 0 {
			fmt.Fprintln(w, "\nDEPENDENCIES")
			for _, d := range deps {
				verb, other := depRelation(issue.ID, d)
				fmt.Fprintf(w, "  %s %s\n", verb, other)
			}
		}
		return nil
	}

	// Normal path: typed sections above base text, history below dependencies.
	if len(cv.Rulings) > 0 {
		fmt.Fprintln(w, "\nACTIVE RULINGS — MUST OBEY")
		for _, r := range cv.Rulings {
			if opts.rulingsMode == rulingsModeFull {
				fmt.Fprintln(w, "  "+rulingFullLine(r, issue.ID))
				continue
			}
			fmt.Fprintln(w, "  "+rulingHeadline(r, issue.ID))
			if opts.expand[r.ID] {
				for _, ln := range strings.Split(r.Text, "\n") {
					fmt.Fprintln(w, "      "+ln)
				}
			}
		}
	}
	if len(cv.Questions) > 0 {
		fmt.Fprintln(w, "\nOPEN QUESTIONS — EXECUTION BLOCKERS")
		for _, q := range cv.Questions {
			fmt.Fprintf(w, "  %s  %s  %s\n", q.ID, q.CreatedAt.Format("2006-01-02"), q.Text)
		}
	}
	if len(cv.ClosedQuestions) > 0 {
		fmt.Fprintln(w, "\nCLOSED QUESTIONS — NO LONGER BLOCKING")
		for _, q := range cv.ClosedQuestions {
			fmt.Fprintln(w, formatClosedQuestionLine(q, cv.AnswerKinds))
		}
	}
	if len(cv.Findings) > 0 {
		fmt.Fprintln(w, "\nFINDINGS")
		for _, f := range cv.Findings {
			base := fmt.Sprintf("  %s  %s  [%s]  %s", f.ID, f.CreatedAt.Format("2006-01-02"), f.FiledBy, f.Text)
			if strings.TrimSpace(f.Evidence) != "" {
				base += fmt.Sprintf("  evidence: %s", f.Evidence)
			}
			fmt.Fprintln(w, base)
		}
	}

	if err := renderBaseText(w, issue, opts); err != nil {
		return err
	}

	if len(deps) > 0 {
		fmt.Fprintln(w, "\nDEPENDENCIES")
		for _, d := range deps {
			verb, other := depRelation(issue.ID, d)
			fmt.Fprintf(w, "  %s %s\n", verb, other)
		}
	}

	if len(sorted) > 0 {
		fmt.Fprintln(w, "\nNOTES / UNTYPED HISTORY (newest first)")
		for _, c := range sorted {
			fmt.Fprintf(w, "  [%s] %s: %s\n", c.CreatedAt.Format("2006-01-02 15:04"), c.Author, c.Text)
		}
	}

	return nil
}

// formatClosedQuestionLine renders one question that has stopped blocking. It
// must show enough that a reader never has to look an id up: how it stopped
// blocking, why, and what settled it.
func formatClosedQuestionLine(q beads.Statement, answerKinds map[string]string) string {
	line := fmt.Sprintf("  %s  %s  %s", q.ID, q.CreatedAt.Format("2006-01-02"), q.Status)
	reason, note, isClosure := decodeClosure(q.Evidence)
	// The superseded reason writes the status of the same name; printing both
	// would read as "superseded (superseded)".
	if isClosure && reason != q.Status {
		line += fmt.Sprintf(" (%s)", reason)
	}
	if q.AnsweredBy != nil && *q.AnsweredBy != "" {
		kind := answerKinds[*q.AnsweredBy]
		if kind == "" {
			kind = "statement"
		}
		line += fmt.Sprintf("  %s: %s", kind, *q.AnsweredBy)
	}
	line += "  " + q.Text
	if isClosure && note != "" {
		line += fmt.Sprintf("  note: %s", note)
	}
	return line
}

// rulingsModeFull is the `--rulings` flag value that prints untruncated
// ruling text in place of the headline. Any other value (including the
// unset default) renders headlines.
const rulingsModeFull = "full"

// rulingFields builds the shared four-field ruling line — id, date, author,
// an optional origin marker, then text — used by both the headline and full
// renderers below. issueID is the bead being rendered so a ruling's own
// issue never carries a redundant marker; an empty issueID (the
// project-wide `bd rulings` listing) means every scoped ruling always
// carries its marker.
func rulingFields(st beads.Statement, issueID, text string) string {
	date := st.CreatedAt.Format("2006-01-02")
	author := "unknown"
	if st.FiledBy != "" {
		author = actorWord(st.FiledBy)
	}
	line := fmt.Sprintf("%s  %s  %s", st.ID, date, author)
	if st.IssueID == nil {
		line += "  [project]"
	} else if *st.IssueID != issueID {
		line += fmt.Sprintf("  [%s]", *st.IssueID)
	}
	line += fmt.Sprintf("  %s", text)
	return line
}

// rulingHeadline is the single renderer for a ruling's default, truncated
// line: id, date, author, optional origin marker, then the first 120 runes
// of the text. Both `bd show` (with a leading two-space indent added by the
// caller) and `bd rulings` (without it) go through this.
func rulingHeadline(st beads.Statement, issueID string) string {
	return rulingFields(st, issueID, headlineText(st.Text))
}

// rulingFullLine is rulingHeadline's untruncated counterpart, used under
// `--rulings full`.
func rulingFullLine(st beads.Statement, issueID string) string {
	return rulingFields(st, issueID, st.Text)
}

// headlineText collapses a statement's text to a single line and truncates
// it to 120 runes, appending an ellipsis when it truncated.
func headlineText(s string) string {
	replaced := strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(s)
	collapsed := strings.Join(strings.Fields(replaced), " ")
	runes := []rune(collapsed)
	if len(runes) > 120 {
		return string(runes[:120]) + "…"
	}
	return collapsed
}

func renderBaseText(w io.Writer, issue *beads.Issue, opts showOpts) error {
	fmt.Fprintf(w, "\nBASE TEXT (written %s — amendments above supersede it)\n", issue.CreatedAt.Format("2006-01-02"))
	desc := issue.Description
	if opts.section != "" {
		body, ok := extractSection(desc, opts.section)
		if !ok {
			return sectionNotFoundError(issue.ID, desc, opts.section)
		}
		body = opts.lineSlice.apply(body)
		if body != "" {
			fmt.Fprintln(w, body)
		}
		return nil
	}
	if !opts.lineSlice.empty() {
		body := opts.lineSlice.apply(desc)
		if body != "" {
			fmt.Fprintln(w, body)
		}
		return nil
	}
	if opts.outline || (!opts.full && len(desc) >= outlineDefaultThreshold) {
		fmt.Fprintf(w, "description: %d chars  (use --full or --section <slug>)\n", len(desc))
		if hs := outlineHeadings(desc); len(hs) > 0 {
			fmt.Fprintln(w, "sections:")
			for _, h := range hs {
				fmt.Fprintf(w, "  %-32s lines %d-%d\n", h.heading, h.startLine, h.endLine)
			}
		} else {
			fmt.Fprintln(w, "(no ## headings — use --full to read the body)")
		}
		return nil
	}
	if desc != "" {
		fmt.Fprintln(w, desc)
	}
	return nil
}
