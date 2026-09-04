package main

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// topicSlugRE is the whole topic grammar. slugify is deliberately not applied
// to a --topic value: a slug the writer did not type is a slug nobody can
// guess again, and the catalogue only works when writers reuse each other's.
var topicSlugRE = regexp.MustCompile(`^[a-z0-9-]{2,48}$`)

// validateTopicSlug checks one slug. label names the thing being validated so
// a flag reports the flag and a positional argument reports the noun.
func validateTopicSlug(label, slug string) error {
	if !topicSlugRE.MatchString(slug) {
		return fmt.Errorf("invalid %s %q (lowercase letters, digits and hyphens, 2-48 chars)", label, slug)
	}
	return nil
}

func newTopicsCmd() *cobra.Command {
	var concern, workspace string
	var all bool
	root := &cobra.Command{
		Use:   "topics",
		Short: "Show the topic catalogue",
		Long: `Print one line per topic: the slug, its status, the question that opened it
and the ruling that settled it.

A topic has no row of its own. Its status is derived on every read: open while
an active question carries the slug, settled once an active ruling does, and
dormant when nothing is left to act on — every bead carrying it closed and no
project-scoped ruling on it. Dormant topics are hidden unless --all.

--concern and --workspace narrow the catalogue; together they mean AND.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTopics(cmd, concern, workspace, all)
		},
	}
	root.Flags().StringVar(&concern, "concern", "", "only topics carrying this concern")
	root.Flags().StringVar(&workspace, "workspace", "", "only topics carrying this workspace")
	root.Flags().BoolVar(&all, "all", false, "include dormant topics")
	root.AddCommand(newTopicsRenameCmd())
	root.AddCommand(newTopicsMergeCmd())
	return root
}

func runTopics(cmd *cobra.Command, concern, workspace string, all bool) error {
	cc, err := openStore(cmd)
	if err != nil {
		return err
	}
	defer cc.store.Close()

	rows, err := cc.store.ListTopics(cc.ctx, store.TopicFilter{
		Concern:   strings.TrimSpace(concern),
		Workspace: strings.TrimSpace(workspace),
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	shown, dormant := rows, 0
	if !all {
		shown, dormant = liveTopics(rows)
	}
	if len(shown) == 0 && dormant == 0 {
		fmt.Fprintln(out, "(no topics)")
		return nil
	}
	writeTopicTable(out, shown, "")
	if dormant > 0 {
		fmt.Fprintf(out, "(%d dormant)\n", dormant)
	}
	return nil
}

// liveTopics splits the catalogue into the rows worth showing and the count of
// dormant rows dropped.
func liveTopics(rows []store.TopicRow) ([]store.TopicRow, int) {
	var live []store.TopicRow
	dormant := 0
	for _, r := range rows {
		if r.Status == store.TopicDormant {
			dormant++
			continue
		}
		live = append(live, r)
	}
	return live, dormant
}

// topicStatusCell is the status column: the status word, plus ×N when more
// than one active question carries the slug (they compact to one row).
func topicStatusCell(r store.TopicRow) string {
	if r.Questions > 1 {
		return fmt.Sprintf("%s ×%d", r.Status, r.Questions)
	}
	return r.Status
}

// writeTopicTable prints the catalogue, every column padded to its widest cell
// so the ruling ids line up. indent prefixes each line (the refusal indents).
func writeTopicTable(w io.Writer, rows []store.TopicRow, indent string) {
	slugW, statusW, questionW := 0, 0, 0
	for _, r := range rows {
		slugW = max(slugW, utf8.RuneCountInString(r.Slug))
		statusW = max(statusW, utf8.RuneCountInString(topicStatusCell(r)))
		questionW = max(questionW, utf8.RuneCountInString(headlineText(r.Question)))
	}
	for _, r := range rows {
		ruling := r.RulingID
		if ruling == "" {
			ruling = "—"
		}
		fmt.Fprintf(w, "%s%s  %s  %s  %s\n", indent,
			padTopicCell(r.Slug, slugW),
			padTopicCell(topicStatusCell(r), statusW),
			padTopicCell(headlineText(r.Question), questionW),
			ruling)
	}
}

// padTopicCell pads by runes, not bytes: the question column carries the
// ellipsis headlineText appends and every non-ASCII character in the text.
func padTopicCell(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// resolveTopicAreas returns the areas a bead binds, reduced to the one
// workspace a statement can carry. A bead whose Files list spans two
// workspaces has none: it belongs under every concern query and under no
// workspace query, which is truer than silently picking the first.
func resolveTopicAreas(cc *cmdCtx, issueID string) (workspace string, concerns []string, err error) {
	ws, cs, err := cc.store.ResolveIssue(cc.ctx, issueID)
	if err != nil {
		return "", nil, err
	}
	if len(ws) == 1 {
		workspace = ws[0]
	}
	return workspace, cs, nil
}

// refuseMissingTopic prints the pick menu for the bead's areas and returns the
// refusal. It is the only place a writer learns which slugs already exist, so
// it prints even when the bead resolves to no areas at all (then the whole
// catalogue, which is still a better menu than none).
func refuseMissingTopic(cc *cmdCtx, errW io.Writer, issueID string) error {
	rows, err := cc.store.ListTopics(cc.ctx, store.TopicFilter{})
	if err != nil {
		return err
	}
	header := "Topics for the project"
	if issueID != "" {
		workspace, concerns, err := resolveTopicAreas(cc, issueID)
		if err != nil {
			return err
		}
		header = fmt.Sprintf("Topics for %s (%s)", issueID, topicAreaPhrase(workspace, concerns))
		if workspace != "" || len(concerns) > 0 {
			rows = topicsInAreas(rows, workspace, concerns)
		}
	}
	rows, _ = liveTopics(rows)

	fmt.Fprintf(errW, "--topic is required. %s:\n", header)
	if len(rows) == 0 {
		fmt.Fprintln(errW, "  (no topics yet — mint the first slug)")
	} else {
		writeTopicTable(errW, rows, "  ")
	}
	fmt.Fprintln(errW, "Pick one with --topic <slug>, or mint a new slug with --topic <new-slug>.")
	return fmt.Errorf("--topic is required")
}

// topicsInAreas keeps the topics touching any of the bead's areas. The union,
// not the intersection: the menu exists to stop a writer minting a second slug
// for a topic that already has one, and a near-miss slug is still a hit.
func topicsInAreas(rows []store.TopicRow, workspace string, concerns []string) []store.TopicRow {
	want := map[string]bool{}
	for _, c := range concerns {
		want[c] = true
	}
	var out []store.TopicRow
	for _, r := range rows {
		hit := false
		for _, w := range r.Workspaces {
			if workspace != "" && w == workspace {
				hit = true
			}
		}
		for _, c := range r.Concerns {
			if want[c] {
				hit = true
			}
		}
		if hit {
			out = append(out, r)
		}
	}
	return out
}

// topicAreaPhrase renders the areas in the refusal header.
func topicAreaPhrase(workspace string, concerns []string) string {
	ws := "no workspace"
	if workspace != "" {
		ws = "workspace " + workspace
	}
	cs := "no concerns"
	switch len(concerns) {
	case 0:
	case 1:
		cs = "concern " + concerns[0]
	default:
		cs = "concerns " + strings.Join(concerns, ", ")
	}
	return ws + ", " + cs
}

func newTopicsRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename a topic across every statement carrying it (actor-gated)",
		Long: `Move every statement from one slug to another. It refuses when the new slug
already carries a statement — folding two live slugs together is merge.

Actor gating: BD_ACTOR=executor is refused.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
			}
			oldSlug, newSlug := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
			if err := validateTopicSlug("topic", oldSlug); err != nil {
				return err
			}
			if err := validateTopicSlug("topic", newSlug); err != nil {
				return err
			}
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			n, err := cc.store.RenameTopic(cc.ctx, oldSlug, newSlug)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "renamed %s -> %s (%d statements)\n", oldSlug, newSlug, n)
			return nil
		},
	}
	return cmd
}

func newTopicsMergeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge <from> <into>",
		Short: "Fold one topic into another (actor-gated)",
		Long: `Move every statement carrying <from> onto <into>, whether or not <into>
already exists, and print how many moved.

Actor gating: BD_ACTOR=executor is refused.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
			}
			from, into := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
			if err := validateTopicSlug("topic", from); err != nil {
				return err
			}
			if err := validateTopicSlug("topic", into); err != nil {
				return err
			}
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			n, err := cc.store.MergeTopics(cc.ctx, from, into)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "merged %s into %s (%d statements)\n", from, into, n)
			return nil
		},
	}
	return cmd
}

// resolveStatementTopic is the shared write path for `bd question add` and
// `bd finding add`: it validates the slug and stamps the bead's areas on the
// row. An empty slug is the refusal, catalogue and all.
func resolveStatementTopic(cc *cmdCtx, errW io.Writer, issueID, topic string) (slug, workspace, concern string, err error) {
	slug = strings.TrimSpace(topic)
	if slug == "" {
		return "", "", "", refuseMissingTopic(cc, errW, issueID)
	}
	if err := validateTopicSlug("--topic", slug); err != nil {
		return "", "", "", err
	}
	if issueID == "" {
		return slug, "", "", nil
	}
	workspace, concerns, err := resolveTopicAreas(cc, issueID)
	if err != nil {
		return "", "", "", err
	}
	return slug, workspace, strings.Join(concerns, ","), nil
}

// applyRulingTopic fills topic, workspace and concern on a ruling about to be
// written. With --answers the question is the source of all three, so a chain
// question → ruling stays on one slug without the writer retyping it; without
// --answers the slug is required, project-scoped rulings included.
func applyRulingTopic(cc *cmdCtx, errW io.Writer, st *beads.Statement, answersID, topic, concern string) error {
	slug := strings.TrimSpace(topic)
	if slug != "" {
		if err := validateTopicSlug("--topic", slug); err != nil {
			return err
		}
	}
	issueID := ""
	if st.IssueID != nil {
		issueID = *st.IssueID
	}
	concern = strings.TrimSpace(concern)
	if concern != "" && issueID != "" {
		return fmt.Errorf("--concern is only for a project-scoped ruling; an issue-scoped ruling takes its areas from the bead")
	}

	if answersID != "" {
		q, err := cc.store.GetStatement(cc.ctx, answersID)
		if err != nil {
			return fmt.Errorf("question %s not found: %w", answersID, err)
		}
		// A question filed before topics were required carries none; the
		// writer's own --topic then supplies it rather than refusing a ruling
		// on a question that cannot be fixed retroactively.
		if q.Topic != "" {
			if slug != "" && slug != q.Topic {
				return fmt.Errorf("--topic %q contradicts %s's topic %q", slug, answersID, q.Topic)
			}
			st.Topic, st.Workspace, st.Concern = q.Topic, q.Workspace, q.Concern
			return nil
		}
	}

	if slug == "" {
		return refuseMissingTopic(cc, errW, issueID)
	}
	st.Topic = slug
	if issueID == "" {
		st.Concern = concern
		return nil
	}
	workspace, concerns, err := resolveTopicAreas(cc, issueID)
	if err != nil {
		return err
	}
	st.Workspace, st.Concern = workspace, strings.Join(concerns, ",")
	return nil
}
