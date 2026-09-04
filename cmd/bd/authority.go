package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/internal/config"
	"github.com/rsktash/beads/store"
)

// `bd authority <bead-id | words…>` — one capped brief ordered by authority.
// It is the first command run on any bead or owner question: what binds it,
// what is still asked about it, and where to look next.

// briefByteCap is the whole brief's ceiling — the page's ~1,500 tokens at four
// bytes a token. Every section is capped first, so this is a backstop, not the
// working limit.
const briefByteCap = 6000

// Per-section row caps. A section that hits its cap ends with the exact
// command that shows the rest, so the cap never hides a record without saying
// where it went.
const (
	briefRulingsCap   = 12
	briefTopicsCap    = 5
	briefQuestionsCap = 8
	briefFindingsCap  = 6
	briefDoctrineCap  = 6
	briefRelatedCap   = 8
	briefCommentsCap  = 6
)

// briefLabelWidth pads the section label column. Every section line after the
// first carries an empty label, so the records line up under each other.
const briefLabelWidth = 9

// briefRow is one rendered record: its line, plus the lines --expand opened
// under it. Expansions are attached to the row and never counted against the
// section cap — they were asked for by id.
type briefRow struct {
	line  string
	extra []string
}

func newAuthorityCmd() *cobra.Command {
	var (
		kinds     []string
		workspace string
		concern   string
		topic     string
		status    string
		author    string
		since     string
		depth     int
		grep      string
		expand    []string
		maxBytes  int
	)
	cmd := &cobra.Command{
		Use:   "authority <bead-id | words…>",
		Short: "The authority brief: what binds this bead, in one capped read",
		Long: `Print one brief ordered by authority: BRIEF, RULINGS, TOPIC, QUESTIONS,
FINDINGS, DOCTRINE, RELATED. Run it first on any bead or owner question.

Every section is capped and ends with the exact command that shows the rest;
the whole brief is capped at 6000 bytes, which --max-bytes overrides. --json
emits the same structure uncapped.

With words instead of a bead id the concern must already be resolved:
bd authority "…" --concern authority. With neither a bead nor --concern the
command prints the concern names and refuses — asking for one word beats
guessing terms. --grep is for tie-breaks and never substitutes for --concern.

The TOPIC section is filled by structure, never by wording: the bead's epic
chain, its areas, and the shared and all areas.

RELATED never reads a transcript. It prints the hit command for you to run.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if depth < 0 {
				return fmt.Errorf("invalid --depth %d (1 to 3)", depth)
			}
			if depth > 3 {
				return fmt.Errorf("invalid --depth %d (maximum 3)", depth)
			}
			kinds, err := parseBriefKinds(kinds)
			if err != nil {
				return err
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			req := store.BriefRequest{
				Kinds:     kinds,
				Workspace: strings.TrimSpace(workspace),
				Concern:   strings.TrimSpace(concern),
				Topic:     strings.TrimSpace(topic),
				Status:    strings.TrimSpace(status),
				Author:    strings.TrimSpace(author),
				Depth:     depth,
				Grep:      strings.TrimSpace(grep),
			}
			if len(args) == 1 && beadIDRE.MatchString(args[0]) {
				req.IssueID = args[0]
			} else if len(args) > 0 {
				req.Words = store.SignificantWords(strings.Join(args, " "))
			}
			if req.IssueID == "" && req.Concern == "" {
				return refuseMissingConcern(cc, cmd.ErrOrStderr())
			}

			req.Since, req.SinceLabel, err = resolveBriefSince(cc, since, req.IssueID)
			if err != nil {
				return err
			}
			if err := resolveOrderingHandoff(cc, cmd, since, &req); err != nil {
				return err
			}

			res, err := cc.store.Brief(cc.ctx, req)
			if err != nil {
				return err
			}

			if cc.json {
				return writeJSONTo(cmd.OutOrStdout(), res)
			}
			out := bufferedWriter(maxBytes)
			renderBrief(out, cc, res, req, parseExpandSet(expand), newExpandTracker())
			return out.flush(cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringSliceVar(&kinds, "kind", nil, "sections to print: "+strings.Join(store.BriefKinds, "|")+" (repeatable; default all)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "read the bead from this workspace instead of the one its Files list resolves to")
	cmd.Flags().StringVar(&concern, "concern", "", "the concern the brief is drawn from (required for a words query)")
	cmd.Flags().StringVar(&topic, "topic", "", "only records carrying this topic slug")
	cmd.Flags().StringVar(&status, "status", "", "only statements with this status (open = an active question)")
	cmd.Flags().StringVar(&author, "author", "", "only records by this actor: owner|agent")
	cmd.Flags().StringVar(&since, "since", "", "only records newer than this date (YYYY-MM-DD) or 'handoff' for the bead's last lane handoff")
	cmd.Flags().IntVar(&depth, "depth", 1, "link hops in RELATED (1 to 3)")
	cmd.Flags().StringVar(&grep, "grep", "", "tie-break filter over the built sections (case-insensitive)")
	cmd.Flags().StringSliceVar(&expand, "expand", nil, "record id(s) to open in full under their line, repeatable")
	cmd.Flags().IntVar(&maxBytes, "max-bytes", briefByteCap, "cap stdout at N bytes and append a truncation marker (0 = uncapped)")
	return cmd
}

// parseBriefKinds validates --kind against the accepted vocabulary.
func parseBriefKinds(raw []string) ([]string, error) {
	var out []string
	for _, k := range raw {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		known := false
		for _, want := range store.BriefKinds {
			if k == want {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("invalid --kind %q (%s)", k, strings.Join(store.BriefKinds, "|"))
		}
		out = append(out, k)
	}
	return out, nil
}

// refuseMissingConcern is rule 6: with neither a bead nor a concern the model
// has not resolved an area, and one word from the owner beats guessing terms.
func refuseMissingConcern(cc *cmdCtx, errW io.Writer) error {
	names, err := briefConcernNames(cc)
	if err != nil {
		return err
	}
	fmt.Fprintln(errW, "A words query needs a concern. The project's concerns are:")
	for _, n := range names {
		fmt.Fprintf(errW, "  %s\n", n)
	}
	return fmt.Errorf("name the concern: --concern <one of: %s>", strings.Join(names, ", "))
}

// briefConcernNames reads the concern vocabulary, dropping the all concern:
// it matches every path, so naming it narrows nothing.
func briefConcernNames(cc *cmdCtx) ([]string, error) {
	areas, err := cc.store.ListAreas(cc.ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, a := range areas {
		if a.Kind == store.AreaConcern && a.Name != store.AreaAll {
			names = append(names, a.Name)
		}
	}
	return names, nil
}

// resolveBriefSince turns --since into an instant. "handoff" is the newest
// handoff on the bead's lane; with no plan and no handoff it is an error
// naming the flag, never a silent full history.
func resolveBriefSince(cc *cmdCtx, raw, issueID string) (time.Time, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, "", nil
	}
	if raw == "handoff" {
		if issueID == "" {
			return time.Time{}, "", fmt.Errorf("--since handoff needs a bead id: a words query is in no lane")
		}
		at, lane, ok, err := cc.store.LastHandoffForBead(cc.ctx, issueID)
		if err != nil {
			var ambiguous *store.ErrTwoActivePlans
			if errors.As(err, &ambiguous) {
				return time.Time{}, "", fmt.Errorf("--since handoff is ambiguous: plans %s and %s are both active", ambiguous.A, ambiguous.B)
			}
			return time.Time{}, "", err
		}
		if !ok {
			if lane != "" {
				return time.Time{}, "", fmt.Errorf("--since handoff: lane %s has no handoff", lane)
			}
			return time.Time{}, "", fmt.Errorf("--since handoff: %s is in no execution plan lane that has been handed off", issueID)
		}
		label := fmt.Sprintf("since %s — lane %s handoff", at.UTC().Format("2006-01-02 15:04"), lane)
		return at, label, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, "since " + t.UTC().Format("2006-01-02 15:04"), nil
		}
	}
	return time.Time{}, "", fmt.Errorf("--since takes YYYY-MM-DD, an RFC3339 timestamp, or the word handoff")
}

// resolveOrderingHandoff sets the instant the brief's change ordering, its
// * marks and its typed diff hang off. `--since handoff` has already
// resolved it, and its refusals are feature 15's to make. On every other
// brief an ambiguous plan or a missing handoff must not fail the render:
// the marks and the diff are absent and one stderr line says why.
func resolveOrderingHandoff(cc *cmdCtx, cmd *cobra.Command, since string, req *store.BriefRequest) error {
	if req.IssueID == "" {
		return nil
	}
	if strings.TrimSpace(since) == "handoff" {
		at := req.Since
		req.Handoff = &at
		return nil
	}
	at, lane, ok, err := cc.store.LastHandoffForBead(cc.ctx, req.IssueID)
	switch {
	case err != nil:
		fmt.Fprintf(cmd.ErrOrStderr(), "bd: no * marks or typed diff: %v\n", err)
	case ok:
		req.Handoff = &at
	case lane != "":
		fmt.Fprintf(cmd.ErrOrStderr(), "bd: no * marks or typed diff: lane %s has no handoff\n", lane)
	default:
		fmt.Fprintf(cmd.ErrOrStderr(), "bd: no * marks or typed diff: %s is in no execution plan lane\n", req.IssueID)
	}
	return nil
}

// renderBrief writes the seven labelled sections in their fixed order.
func renderBrief(w io.Writer, cc *cmdCtx, res store.BriefResult, req store.BriefRequest, expand map[string]bool, tr *expandTracker) {
	writeBriefLine(w, "BRIEF", briefHeaderLine(res, req))

	if req.Wants(store.BriefKindRulings) {
		writeBriefSection(w, "RULINGS", briefRulingRows(res, expand, cc, tr),
			briefRulingsCap, briefMoreCmd("bd rulings", res.IssueID), req.SinceLabel)
	}
	if len(req.Kinds) == 0 {
		writeBriefSection(w, "TOPIC", briefTopicRows(res), briefTopicsCap, briefTopicsCmd(res), req.SinceLabel)
	}
	if req.Wants(store.BriefKindQuestions) {
		writeBriefSection(w, "QUESTIONS", briefQuestionRows(res, expand, cc, tr),
			briefQuestionsCap, briefMoreCmd("bd question list", res.IssueID), req.SinceLabel)
	}
	if req.Wants(store.BriefKindFindings) {
		writeBriefSection(w, "FINDINGS", briefFindingRows(res, expand, cc, tr),
			briefFindingsCap, briefMoreCmd("bd show", res.IssueID), req.SinceLabel)
	}
	if req.Wants(store.BriefKindDoctrine) {
		writeBriefDoctrine(w, res, expand, cc, tr, req.SinceLabel)
	}
	writeBriefRelated(w, res, req)
	if req.Wants(store.BriefKindComments) && (len(res.Comments) > 0 || req.SinceLabel != "") {
		writeBriefSection(w, "COMMENTS", briefCommentRows(res), briefCommentsCap,
			briefMoreCmd("bd comment list", res.IssueID), req.SinceLabel)
	}
}

func writeBriefLine(w io.Writer, label, line string) {
	fmt.Fprintf(w, "%-*s %s\n", briefLabelWidth, label, line)
}

// writeBriefSection prints one section: its rows down to the cap, the lines
// --expand opened, and the count of what the cap dropped.
func writeBriefSection(w io.Writer, label string, rows []briefRow, limit int, moreCmd, sinceLabel string) {
	if len(rows) == 0 && sinceLabel == "" {
		return
	}
	if sinceLabel != "" {
		writeBriefLine(w, label, "("+sinceLabel+")")
		if len(rows) == 0 {
			writeBriefLine(w, "", "(nothing since the window)")
			return
		}
	}
	shown := rows
	if limit > 0 && len(rows) > limit {
		shown = rows[:limit]
	}
	for i, r := range shown {
		if i == 0 && sinceLabel == "" {
			writeBriefLine(w, label, r.line)
		} else {
			writeBriefLine(w, "", r.line)
		}
		for _, e := range r.extra {
			writeBriefLine(w, "", "  "+e)
		}
	}
	if n := len(rows) - len(shown); n > 0 {
		writeBriefLine(w, "", fmt.Sprintf("(%d more — %s)", n, moreCmd))
	}
}

func briefMoreCmd(base, issueID string) string {
	if issueID == "" {
		return base
	}
	return base + " " + issueID
}

func briefTopicsCmd(res store.BriefResult) string {
	if len(res.Concerns) > 0 {
		return "bd topics --concern " + res.Concerns[0]
	}
	if len(res.Workspaces) > 0 {
		return "bd topics --workspace " + res.Workspaces[0]
	}
	return "bd topics"
}

// briefHeaderLine is the BRIEF line: the bead's identity, or the areas a words
// query was drawn from. When a handoff ordered the brief, the line closes
// with the typed diff — the four counts of what changed since it.
func briefHeaderLine(res store.BriefResult, req store.BriefRequest) string {
	if res.IssueID != "" {
		line := fmt.Sprintf("%s  [%s] %s p%d  %s", res.IssueID, res.Status, res.Type, res.Priority, res.Title)
		if res.Handoff != nil {
			line += "  " + briefTypedDiffLine(res.Diff)
		}
		return line
	}
	line := "concern " + req.Concern
	if req.Workspace != "" {
		line += "  workspace " + req.Workspace
	}
	if len(res.Query) > 0 {
		line += "  " + strings.Join(res.Query, " ")
	}
	return line
}

// briefTypedDiffLine renders the typed diff: one parenthesised list of the
// four counts.
func briefTypedDiffLine(d store.BriefDiff) string {
	return fmt.Sprintf("(since handoff: %s, %s, %s, %s)",
		briefPlural(d.Rulings, "ruling", "rulings"),
		briefPlural(d.QuestionsAnswered, "question answered", "questions answered"),
		briefPlural(d.Findings, "finding", "findings"),
		briefPlural(d.DepsClosed, "dep closed", "deps closed"))
}

// briefPlural is "n one" for one, else "n many" — the counted nouns put
// their plural where the phrase needs it (deps closed, questions answered).
func briefPlural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// briefChangeMark is the leading star a record changed since the ordering
// handoff carries on its line.
func briefChangeMark(st beads.Statement, handoff *time.Time) string {
	if handoff == nil {
		return ""
	}
	at := st.CreatedAt
	if st.ChangedAt != nil {
		at = *st.ChangedAt
	}
	if at.After(*handoff) {
		return "* "
	}
	return ""
}

// briefDedupeRow is the bare line a record already expanded earlier in this
// session renders as — deduplication, not prioritization: the row keeps its
// place in the order and shows nothing else.
func briefDedupeRow(id string) briefRow {
	return briefRow{line: id + "  (expanded earlier this session)"}
}

func briefRulingRows(res store.BriefResult, expand map[string]bool, cc *cmdCtx, tr *expandTracker) []briefRow {
	rows := make([]briefRow, 0, len(res.Rulings))
	for _, r := range res.Rulings {
		if tr.deduped(r.ID) {
			rows = append(rows, briefDedupeRow(r.ID))
			continue
		}
		rows = append(rows, briefRow{
			line:  briefChangeMark(r, res.Handoff) + rulingHeadline(r, res.IssueID),
			extra: append(briefCitationLines(cc, r.ID, r.Text), briefExpansion(r, expand, cc, tr)...),
		})
	}
	return rows
}

func briefFindingRows(res store.BriefResult, expand map[string]bool, cc *cmdCtx, tr *expandTracker) []briefRow {
	idW := briefWidth(res.Findings, func(f beads.Statement) string { return f.ID })
	rows := make([]briefRow, 0, len(res.Findings))
	for _, f := range res.Findings {
		if tr.deduped(f.ID) {
			rows = append(rows, briefDedupeRow(f.ID))
			continue
		}
		rows = append(rows, briefRow{
			line: fmt.Sprintf("%s  %s  %s  %s", padTopicCell(briefChangeMark(f, res.Handoff)+f.ID, idW+2),
				f.CreatedAt.Format("2006-01-02"), authorColumn(f.FiledBy), headlineText(f.Text)),
			extra: append(briefCitationLines(cc, f.ID, f.Text+"\n"+f.Evidence), briefExpansion(f, expand, cc, tr)...),
		})
	}
	return rows
}

// briefQuestionRows prints the status word a reader acts on — an active
// question is open — and what answered a closed one, so no id needs a lookup.
func briefQuestionRows(res store.BriefResult, expand map[string]bool, cc *cmdCtx, tr *expandTracker) []briefRow {
	idW := briefWidth(res.Questions, func(q beads.Statement) string { return q.ID })
	statusW := briefWidth(res.Questions, briefQuestionStatus)
	answerW := briefWidth(res.Questions, func(q beads.Statement) string {
		if q.AnsweredBy != nil {
			return *q.AnsweredBy
		}
		return ""
	})
	rows := make([]briefRow, 0, len(res.Questions))
	for _, q := range res.Questions {
		if tr.deduped(q.ID) {
			rows = append(rows, briefDedupeRow(q.ID))
			continue
		}
		line := fmt.Sprintf("%s  %s  %s  %s", padTopicCell(briefChangeMark(q, res.Handoff)+q.ID, idW+2),
			q.CreatedAt.Format("2006-01-02"), authorColumn(q.FiledBy),
			padTopicCell(briefQuestionStatus(q), statusW))
		if answerW > 0 {
			answer := ""
			if q.AnsweredBy != nil {
				answer = *q.AnsweredBy
			}
			line += "  " + padTopicCell(answer, answerW)
		}
		line += "  " + headlineText(q.Text)
		extra := append(briefCitationLines(cc, q.ID, q.Text), briefExpansion(q, expand, cc, tr)...)
		if note := questionExpiryNote(q, time.Now().UTC(), defaultStaleDays); note != "" {
			marker, command := expiryNoteParts(note)
			line += "  " + marker
			extra = append([]string{command}, extra...)
		}
		rows = append(rows, briefRow{line: line, extra: extra})
	}
	return rows
}

func briefCitationLines(cc *cmdCtx, id, text string) []string {
	cfg, err := config.Resolve(flagDB)
	if err != nil || cfg.ProjectRoot == "" {
		return nil
	}
	var out []string
	for _, citation := range store.ParseCitations(text) {
		state, err := store.ResolveCitation(cfg.ProjectRoot, citation)
		if err != nil {
			continue
		}
		line := "cite " + citation.String() + "  " + string(state.Status)
		switch state.Status {
		case store.CitationMoved:
			line += fmt.Sprintf(" -> :%d", state.Line)
		case store.CitationStale:
			line += fmt.Sprintf(" (bd source %s recovers it)", id)
		}
		out = append(out, line)
	}
	return out
}

// briefQuestionStatus renders active as open: "active" is the stored word,
// "open" is what the reader is looking for.
func briefQuestionStatus(q beads.Statement) string {
	if q.Status == "active" {
		return "open"
	}
	return q.Status
}

func briefCommentRows(res store.BriefResult) []briefRow {
	rows := make([]briefRow, 0, len(res.Comments))
	for _, c := range res.Comments {
		rows = append(rows, briefRow{line: fmt.Sprintf("%s  %s  %s",
			c.CreatedAt.Format("2006-01-02"), c.Author, headlineText(c.Text))})
	}
	return rows
}

// briefTopicRows reuses `bd topics`' own table renderer, so a slug reads the
// same in both places.
func briefTopicRows(res store.BriefResult) []briefRow {
	if len(res.Topics) == 0 {
		return nil
	}
	shown := res.Topics
	if len(shown) > briefTopicsCap {
		shown = shown[:briefTopicsCap]
	}
	var buf bytes.Buffer
	writeTopicTable(&buf, shown, "")
	var rows []briefRow
	for _, l := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		rows = append(rows, briefRow{line: l})
	}
	// The table's columns were padded over the capped set only, so the dropped
	// rows are re-attached unpadded: they never print, they only carry the
	// count the section's (N more) line reports.
	for _, r := range res.Topics[len(shown):] {
		rows = append(rows, briefRow{line: r.Slug})
	}
	return rows
}

// writeBriefDoctrine prints the laws over this bead's areas — ruling id,
// concern, law — and reports every other active law as a count. A law column
// nothing writes yet falls back to the ruling's own headline.
func writeBriefDoctrine(w io.Writer, res store.BriefResult, expand map[string]bool, cc *cmdCtx, tr *expandTracker, sinceLabel string) {
	idW, concernW := 0, 0
	for _, d := range res.Doctrine {
		idW = max(idW, len(d.ID))
		concernW = max(concernW, len(briefDoctrineArea(d)))
	}
	rows := make([]briefRow, 0, len(res.Doctrine))
	for _, d := range res.Doctrine {
		if tr.deduped(d.ID) {
			rows = append(rows, briefDedupeRow(d.ID))
			continue
		}
		law := strings.TrimSpace(d.Law)
		if law == "" {
			law = d.Text
		}
		rows = append(rows, briefRow{
			line: fmt.Sprintf("%s  %s  %s", padTopicCell(d.ID, idW),
				padTopicCell(briefDoctrineArea(d), concernW), headlineText(law)),
			extra: briefExpansionByID(cc, d.ID, expand, tr, d.Text, "", d.Law),
		})
	}
	writeBriefSection(w, "DOCTRINE", rows, briefDoctrineCap, "bd rulings --scope project", sinceLabel)
	if res.DoctrineOutside > 0 {
		label := ""
		if len(rows) == 0 && sinceLabel == "" {
			label = "DOCTRINE"
		}
		noun := "laws"
		if res.DoctrineOutside == 1 {
			noun = "law"
		}
		writeBriefLine(w, label, fmt.Sprintf("(%d more doctrine %s outside this bead's areas)", res.DoctrineOutside, noun))
	}
}

// briefDoctrineArea is the law's own area: its concern list, or its workspace
// when it carries no concern.
func briefDoctrineArea(d store.DoctrineRow) string {
	if d.Concern != "" {
		return d.Concern
	}
	if d.Workspace != "" {
		return d.Workspace
	}
	return "project"
}

// writeBriefRelated prints the linked beads, the memories the query words hit,
// and one transcript line. The transcript line is a footer, not a row: it is
// the pointer that keeps this command out of the transcript, so a full
// RELATED section must never drop it.
func writeBriefRelated(w io.Writer, res store.BriefResult, req store.BriefRequest) {
	if !req.Wants(store.BriefKindBeads) {
		return
	}
	var rows []briefRow
	for _, r := range res.Related {
		rows = append(rows, briefRow{line: fmt.Sprintf("%s  [%s] %s  %s", r.ID, r.Status, r.Type, r.Title)})
	}
	for _, m := range res.Memories {
		rows = append(rows, briefRow{line: "memory: " + headlineText(m.Value)})
	}
	shown := rows
	if len(shown) > briefRelatedCap {
		shown = shown[:briefRelatedCap]
	}
	if req.SinceLabel != "" {
		writeBriefLine(w, "RELATED", "("+req.SinceLabel+")")
	}
	for i, r := range shown {
		if i == 0 && req.SinceLabel == "" {
			writeBriefLine(w, "RELATED", r.line)
		} else {
			writeBriefLine(w, "", r.line)
		}
	}
	label := ""
	if len(shown) == 0 && req.SinceLabel == "" {
		label = "RELATED"
	}
	writeBriefLine(w, label, briefTranscriptLine(res.Query, res.IssueID))
	if n := len(rows) - len(shown); n > 0 {
		writeBriefLine(w, "", fmt.Sprintf("(%d more — %s)", n, briefMoreCmd("bd dep list", res.IssueID)))
	}
}

// briefTranscriptLine reports how many session transcripts this project has
// and the exact rg command over them. It counts files by name and opens none:
// reading a transcript is the caller's act, never this command's.
func briefTranscriptLine(query []string, issueID string) string {
	pattern := strings.Join(query, "|")
	if pattern == "" {
		pattern = issueID
	}
	glob := briefTranscriptGlob()
	n := 0
	if glob != "" {
		if matches, err := filepath.Glob(glob); err == nil {
			n = len(matches)
		}
	}
	return fmt.Sprintf("transcript: %d sessions — rg -n '%s' %s", n, pattern, glob)
}

// briefTranscriptGlob is $HOME/.claude/projects/<cwd slug>/*.jsonl.
func briefTranscriptGlob() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects", transcriptSlug(cwd), "*.jsonl")
}

// briefExpansion opens one statement in full under its line: the whole text,
// the owner's verbatim sentence, the rationale, and where it came from. The
// opened id is recorded in the session's expand state so a later brief can
// render it as its id alone.
func briefExpansion(st beads.Statement, expand map[string]bool, cc *cmdCtx, tr *expandTracker) []string {
	return briefExpansionByID(cc, st.ID, expand, tr, st.Text, st.Verbatim, st.Rationale)
}

func briefExpansionByID(cc *cmdCtx, id string, expand map[string]bool, tr *expandTracker, text, verbatim, rationale string) []string {
	if !expand[id] {
		return nil
	}
	tr.record(id)
	var out []string
	out = append(out, strings.Split(text, "\n")...)
	if strings.TrimSpace(verbatim) != "" {
		out = append(out, "verbatim: "+verbatim)
	}
	if strings.TrimSpace(rationale) != "" {
		out = append(out, "rationale: "+rationale)
	}
	out = append(out, briefSourceLine(cc, id))
	return out
}

// briefSourceLine is `bd source <id>`'s first line: the session behind the
// record, or why there is none. It never prints the transcript.
func briefSourceLine(cc *cmdCtx, id string) string {
	p, err := cc.store.StatementPointer(cc.ctx, id)
	if err != nil || p.SessionID == "" {
		return "source: none recorded"
	}
	_, ok, err := resolveTranscript(p)
	if err != nil {
		return "source: session " + p.SessionID
	}
	if !ok {
		return fmt.Sprintf("source: session %s — transcript not on this machine", p.SessionID)
	}
	return fmt.Sprintf("source: session %s — bd source %s", p.SessionID, id)
}

// briefWidth is the widest cell a column holds, in runes.
func briefWidth[T any](rows []T, cell func(T) string) int {
	w := 0
	for _, r := range rows {
		w = max(w, len([]rune(cell(r))))
	}
	return w
}
