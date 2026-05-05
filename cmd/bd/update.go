package main

import (
	"fmt"
	"os/user"

	"github.com/spf13/cobra"

	"github.com/rustamsmax/beads/internal/storage"
	"github.com/rustamsmax/beads/internal/types"
)

func newUpdateCmd() *cobra.Command {
	var (
		title    string
		desc     string
		typeStr  string
		statStr  string
		priority int
		assignee string
		labels   []string
		claim    bool
	)
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			u := storage.IssueUpdate{}
			if cmd.Flags().Changed("title") {
				u.Title = &title
			}
			if cmd.Flags().Changed("desc") {
				u.Description = &desc
			}
			if cmd.Flags().Changed("type") {
				t, err := types.ParseType(typeStr)
				if err != nil {
					return err
				}
				u.Type = &t
			}
			if cmd.Flags().Changed("status") {
				s, err := types.ParseStatus(statStr)
				if err != nil {
					return err
				}
				u.Status = &s
			}
			if cmd.Flags().Changed("priority") {
				u.Priority = &priority
			}
			if cmd.Flags().Changed("assignee") {
				u.Assignee = &assignee
			}
			if cmd.Flags().Changed("label") {
				ls := types.Labels(labels)
				u.Labels = &ls
			}
			if claim {
				me := assigneeFromEnv()
				u.Assignee = &me
				ip := types.StatusInProgress
				u.Status = &ip
			}
			out, err := cc.store.UpdateIssue(cc.ctx, args[0], u)
			if err != nil {
				return err
			}
			if cc.json {
				return writeJSON(out)
			}
			fmt.Printf("%s  [%s] p%d %s\n", out.ID, out.Status, out.Priority, out.Title)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVarP(&desc, "desc", "d", "", "new description")
	cmd.Flags().StringVarP(&typeStr, "type", "t", "", "new type")
	cmd.Flags().StringVarP(&statStr, "status", "s", "", "new status")
	cmd.Flags().IntVarP(&priority, "priority", "p", 0, "new priority")
	cmd.Flags().StringVarP(&assignee, "assignee", "a", "", "new assignee")
	cmd.Flags().StringSliceVarP(&labels, "label", "l", nil, "labels (replaces)")
	cmd.Flags().BoolVar(&claim, "claim", false, "claim: assign to current user and set in_progress")
	return cmd
}

func assigneeFromEnv() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "unknown"
}
