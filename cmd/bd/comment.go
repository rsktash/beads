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
	list := &cobra.Command{
		Use: "list <id>", Short: "List comments", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			out, err := cc.store.ListComments(cc.ctx, args[0])
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
				fmt.Printf("[%s] %s: %s\n", c.CreatedAt.Format("2006-01-02 15:04"), c.Author, c.Text)
			}
			return nil
		},
	}
	list.Flags().StringArrayVar(&tags, "tag", nil, "only comments addressed to this tag (repeatable; ORs; [all] always matches)")
	list.Flags().IntVar(&lastN, "last", 0, "only the newest N comments (applied after --tag filtering)")
	root.AddCommand(add, list)
	return root
}
