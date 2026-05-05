package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
)

func newCommentCmd() *cobra.Command {
	var author string
	root := &cobra.Command{Use: "comment", Short: "Comment on a bead"}
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
				c.Author = assigneeFromEnv()
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
	add.Flags().StringVarP(&author, "author", "a", "", "comment author (defaults to current user)")
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
			if cc.json {
				return writeJSON(out)
			}
			for _, c := range out {
				fmt.Printf("[%s] %s: %s\n", c.CreatedAt.Format("2006-01-02 15:04"), c.Author, c.Text)
			}
			return nil
		},
	}
	root.AddCommand(add, list)
	return root
}
