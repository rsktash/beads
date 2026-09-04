package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/internal/config"
	"github.com/rsktash/beads/store"
)

// ownerQuoteRunes bounds the printed owner sentence. This command exists to
// recover the sentence, so the bound is generous enough to carry one and still
// far too small to flood a context.
const ownerQuoteRunes = 400

// transcriptNeighbours is how far back the scan looks for the owner message:
// the record before the tool call and its two neighbours.
const transcriptNeighbours = 3

// maxTranscriptLine caps one decoded line. A record longer than this ends the
// scan rather than growing the buffer without limit.
const maxTranscriptLine = 8 << 20

// Transcript record shape, read off a real
// ~/.claude/projects/<slug>/<session>.jsonl on 2026-09-04. Records carry
// `type` ("user"/"assistant"/"system"/"summary" and non-message kinds such as
// "mode" and "file-history-snapshot"), `uuid`, `parentUuid`, `sessionId` and
// `timestamp`; `message` carries `role`, `id` (the assistant msg_… id) and
// `content`, which is a plain string for a typed owner message and an array of
// blocks otherwise. A `tool_use` block carries `id` (toolu_…) and `name`; the
// matching `tool_result` block, on the following user record, carries
// `tool_use_id`.
type transcriptRecord struct {
	Type      string             `json:"type"`
	UUID      string             `json:"uuid"`
	Timestamp string             `json:"timestamp"`
	Message   *transcriptMessage `json:"message"`
}

type transcriptMessage struct {
	Role    string          `json:"role"`
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
}

type transcriptBlock struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Text      string `json:"text"`
	ToolUseID string `json:"tool_use_id"`
}

// blocks decodes the content array, or nil when content is a plain string.
func (m *transcriptMessage) blocks() []transcriptBlock {
	if m == nil || len(m.Content) == 0 || m.Content[0] != '[' {
		return nil
	}
	var bs []transcriptBlock
	if err := json.Unmarshal(m.Content, &bs); err != nil {
		return nil
	}
	return bs
}

// ownerSaid returns the owner's own words in this record. A user record that
// carries a tool_result is the harness replying to a tool call, not the owner,
// so it is not a candidate.
func (r *transcriptRecord) ownerSaid() (string, bool) {
	if r.Type != "user" || r.Message == nil || r.Message.Role != "user" {
		return "", false
	}
	if len(r.Message.Content) > 0 && r.Message.Content[0] == '"' {
		var s string
		if err := json.Unmarshal(r.Message.Content, &s); err != nil {
			return "", false
		}
		s = strings.TrimSpace(s)
		return s, s != ""
	}
	var parts []string
	for _, b := range r.Message.blocks() {
		if b.Type == "tool_result" {
			return "", false
		}
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, strings.TrimSpace(b.Text))
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

// matchesToolUse reports whether this record is the tool call the pointer names,
// either as the assistant's tool_use block or as the user's tool_result reply.
func (r *transcriptRecord) matchesToolUse(toolUseID string) bool {
	if toolUseID == "" || r.Message == nil {
		return false
	}
	for _, b := range r.Message.blocks() {
		if b.Type == "tool_use" && b.ID == toolUseID {
			return true
		}
		if b.Type == "tool_result" && b.ToolUseID == toolUseID {
			return true
		}
	}
	return false
}

func (r *transcriptRecord) matchesMsg(msgID string) bool {
	if msgID == "" {
		return false
	}
	if r.UUID == msgID {
		return true
	}
	return r.Message != nil && r.Message.ID == msgID
}

// truncateQuote bounds the owner sentence to ownerQuoteRunes runes, marking a
// cut with an ellipsis.
func truncateQuote(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	rs := []rune(s)
	if len(rs) <= ownerQuoteRunes {
		return s
	}
	return string(rs[:ownerQuoteRunes]) + "…"
}

// transcriptSlug is the project directory name for a working directory: the
// path with each '/' and '.' replaced by '-'. Confirmed against
// ~/.claude/projects on 2026-09-04 (/Users/rustam/.claude/outsource/review-runs
// is stored as -Users-rustam--claude-outsource-review-runs).
func transcriptSlug(dir string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(dir)
}

// transcriptPath resolves $HOME/.claude/projects/<slug>/<session_id>.jsonl for
// the current working directory.
func transcriptPath(sessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects", transcriptSlug(cwd), sessionID+".jsonl"), nil
}

// resolveTranscript picks the transcript file behind a pointer: the stored
// path when it is set and exists, else the cwd-derived path when it exists,
// else none. A pointer written from another project's session carries the
// path that actually exists; an old pointer falls back to the cwd guess.
func resolveTranscript(p store.ProvenancePointer) (string, bool, error) {
	if p.TranscriptPath != "" {
		if _, err := os.Stat(p.TranscriptPath); err == nil {
			return p.TranscriptPath, true, nil
		}
	}
	cwdPath, err := transcriptPath(p.SessionID)
	if err != nil {
		return "", false, err
	}
	if _, err := os.Stat(cwdPath); err == nil {
		return cwdPath, true, nil
	}
	return "", false, nil
}

// sourceHit is what the scan recovers: the owner sentence before the tool call
// and the timestamp of the call itself.
type sourceHit struct {
	owner     string
	found     bool
	timestamp string
}

// scanTranscript streams the transcript line by line, keeping only the last
// transcriptNeighbours owner candidates, and stops at the record the pointer
// names. The file is never held in memory and never printed.
func scanTranscript(r io.Reader, p store.ProvenancePointer) sourceHit {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxTranscriptLine)

	// ring holds the last transcriptNeighbours owner candidates seen, oldest
	// first, as already-truncated owner text. A record that is not owner
	// speech is never pushed, so it never evicts one.
	ring := make([]string, 0, transcriptNeighbours)
	push := func(s string) {
		if len(ring) == transcriptNeighbours {
			ring = ring[1:]
		}
		ring = append(ring, s)
	}
	newest := func() (string, bool) {
		for i := len(ring) - 1; i >= 0; i-- {
			if ring[i] != "" {
				return ring[i], true
			}
		}
		return "", false
	}

	var fallback sourceHit
	var fallbackTaken bool
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var rec transcriptRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.matchesToolUse(p.ToolUseID) {
			owner, ok := newest()
			return sourceHit{owner: owner, found: ok, timestamp: rec.Timestamp}
		}
		if !fallbackTaken && rec.matchesMsg(p.MsgID) {
			owner, ok := newest()
			fallback = sourceHit{owner: owner, found: ok, timestamp: rec.Timestamp}
			fallbackTaken = true
		}
		if s, ok := rec.ownerSaid(); ok {
			push(truncateQuote(s))
		}
	}
	return fallback
}

// formatStamp renders a transcript RFC3339 timestamp for a human, local time.
func formatStamp(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

func newSourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "source <id>",
		Short: "Show the owner message behind a statement or comment",
		Long: `Read back the provenance pointer written by bd annotate: the session, the
owner message that immediately preceded the write, and a grep line for the
transcript.

Bounded by design — the owner sentence is truncated, only the records around
the tool call are read, and the transcript is never printed. Exits 0 when
nothing was recorded or the transcript is not on this machine.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("id is required")
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			statement := isStatementID(id)
			var p store.ProvenancePointer
			if statement {
				p, err = cc.store.StatementPointer(cc.ctx, id)
			} else {
				p, err = cc.store.CommentPointer(cc.ctx, id)
			}
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if err := writeRecordedSource(out, p); err != nil {
				return err
			}
			if statement {
				return writeCitationRecovery(cmd, cc, out, id)
			}
			return nil
		},
	}
	return cmd
}

func writeRecordedSource(out io.Writer, p store.ProvenancePointer) error {
	if p.SessionID == "" {
		fmt.Fprintln(out, "source: none recorded")
		return nil
	}
	path, ok, err := resolveTranscript(p)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintf(out, "source: session %s — transcript not on this machine\n", p.SessionID)
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(out, "source: session %s — transcript not on this machine\n", p.SessionID)
		return nil
	}
	defer f.Close()

	hit := scanTranscript(f, p)
	head := "source: session " + p.SessionID
	if stamp := formatStamp(hit.timestamp); stamp != "" {
		head += "  " + stamp
	}
	fmt.Fprintln(out, head)
	if hit.found {
		fmt.Fprintf(out, "owner said: %q\n", hit.owner)
	} else {
		fmt.Fprintln(out, "owner said: (no owner message in the records before the tool call)")
	}
	fmt.Fprintf(out, "grep: rg -n '%s' %s\n", shortSession(p.SessionID), path)
	return nil
}

func writeCitationRecovery(cmd *cobra.Command, cc *cmdCtx, out io.Writer, id string) error {
	statement, err := cc.store.GetStatement(cc.ctx, id)
	if err != nil {
		return err
	}
	text := statement.Text
	if statement.Evidence != "" {
		text += "\n" + statement.Evidence
	}
	citations := store.ParseCitations(text)
	if len(citations) == 0 {
		return nil
	}
	cfg, err := config.Resolve(flagDB)
	if err != nil {
		return err
	}
	for _, citation := range citations {
		state, err := store.ResolveCitation(cfg.ProjectRoot, citation)
		if err != nil {
			return err
		}
		if state.Status != store.CitationStale {
			continue
		}
		sha, err := cc.store.StatementHeadSHA(cc.ctx, id)
		if err != nil {
			return err
		}
		if sha == "" {
			fmt.Fprintf(out, "no HEAD recorded for %s\n", id)
			continue
		}
		git := exec.CommandContext(cmd.Context(), "git", "show", sha+":"+citation.Path)
		git.Dir = cfg.ProjectRoot
		content, err := git.Output()
		if err != nil {
			return fmt.Errorf("recover %s: %w", citation.String(), err)
		}
		writeHistoricalCitation(out, content, citation)
	}
	return nil
}

func writeHistoricalCitation(out io.Writer, content []byte, citation store.Citation) {
	if citation.Line == 0 {
		if declaration, ok := store.CitationDeclaration(content, citation); ok {
			fmt.Fprintln(out, declaration)
		}
		return
	}
	lines := strings.Split(string(content), "\n")
	start := max(0, citation.Line-11)
	end := min(len(lines), start+20)
	for _, line := range lines[start:end] {
		fmt.Fprintln(out, line)
	}
}

// shortSession is the session id prefix that is enough to grep with.
func shortSession(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	return sid
}
