package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
)

// commentTag returns the lowercased tag of a comment body's leading
// "[tag]" token, or "" if the body doesn't start with one.
func commentTag(text string) string {
	s := strings.TrimLeft(text, " \t")
	if !strings.HasPrefix(s, "[") {
		return ""
	}
	end := strings.IndexByte(s, ']')
	if end < 0 {
		return ""
	}
	return strings.ToLower(s[1:end])
}

func newCommentCmd() *cobra.Command {
	var author string
	root := &cobra.Command{
		Use:     "comment",
		Aliases: []string{"comments"},
		Short:   "Comment on a bead",
	}
	add := &cobra.Command{
		Use: "add <id> <text>", Short: "Add a comment", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			c := &beads.Comment{IssueID: args[0], Text: args[1], Author: author}
			if c.Author == "" {
				// The same actor resolution every other record uses, so an
				// owner comment reads as owner and an agent comment as agent.
				c.Author, _ = resolveActor()
			}
			if err := cc.store.AddComment(cc.ctx, c); err != nil {
				return err
			}
			if cc.json {
				return writeJSON(c)
			}
			fmt.Printf("%s\n", c.ID)
			return nil
		},
	}
	add.Flags().StringVarP(&author, "author", "a", "", "comment author (defaults to the resolved actor identity, e.g. owner:alice)")
	var tags []string
	var lastN int
	var includeRetracted bool
	list := &cobra.Command{
		Use: "list <id>", Short: "List comments", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			// --json always sees the whole record, flag or not: a machine
			// reader must be able to read a retraction.
			var out []beads.Comment
			if includeRetracted || cc.json {
				out, err = cc.store.ListCommentsIncludingRetracted(cc.ctx, args[0])
			} else {
				out, err = cc.store.ListComments(cc.ctx, args[0])
			}
			if err != nil {
				return err
			}
			if len(tags) > 0 {
				want := make(map[string]bool, len(tags))
				for _, t := range tags {
					want[strings.ToLower(strings.TrimSpace(t))] = true
				}
				filtered := out[:0:0]
				for _, c := range out {
					if t := commentTag(c.Text); t == "all" || want[t] {
						filtered = append(filtered, c)
					}
				}
				out = filtered
			}
			if lastN > 0 && lastN < len(out) {
				out = out[len(out)-lastN:]
			}
			if cc.json {
				return writeJSON(out)
			}
			for _, c := range out {
				if c.RetractedAt != nil {
					fmt.Printf("[%s] %s: RETRACTED %s by %s — %s\n", c.CreatedAt.Format("2006-01-02 15:04"), c.Author, c.RetractedAt.Format("2006-01-02"), actorWord(c.RetractedBy), c.RetractNote)
					fmt.Printf("    (original) %s\n", c.Text)
					continue
				}
				fmt.Printf("[%s] %s: %s\n", c.CreatedAt.Format("2006-01-02 15:04"), c.Author, c.Text)
			}
			return nil
		},
	}
	list.Flags().StringArrayVar(&tags, "tag", nil, "only comments addressed to this tag (repeatable; ORs; [all] always matches)")
	list.Flags().IntVar(&lastN, "last", 0, "only the newest N comments (applied after --tag filtering)")
	list.Flags().BoolVar(&includeRetracted, "include-retracted", false, "also show retracted comments, rendered as RETRACTED with their original text")

	var note string
	retract := &cobra.Command{
		Use:   "retract <comment-id>",
		Short: "Retract a comment by mark, never delete (actor-gated: BD_ACTOR=executor is refused)",
		Long: `Retract a comment: the row is marked retracted in place, never deleted and
never edited. A retracted comment stops rendering in bd show, in both
untyped-history placements and the CONTRACT-line comment count, and stays
readable via bd comment list --include-retracted and --json. There is no
un-retract: a wrongly retracted comment is superseded by a new comment
saying so.

--note is required and records why the comment is retracted.

Actor gating: BD_ACTOR=executor cannot retract comments; use a finding or question instead. BD_ACTOR=coordinator or unset (owner) is allowed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
			}

			noteVal := strings.TrimSpace(note)
			if noteVal == "" {
				return fmt.Errorf("--note is required")
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			commentID := strings.TrimSpace(args[0])
			if err := cc.store.RetractComment(cc.ctx, commentID, identity, noteVal); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), commentID)
			return nil
		},
	}
	retract.Flags().StringVar(&note, "note", "", "why the comment is being retracted (required)")
	root.AddCommand(add, list, retract)
	return root
}
