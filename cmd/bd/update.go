package main

import (
	"fmt"
	"os/user"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newUpdateCmd() *cobra.Command {
	var (
		title, desc, design, accept, notes string
		typeStr, statStr                   string
		priority                           int
		assignee, owner                    string
		closeReason                        string
		dueStr, deferStr                   string
		claim                              bool
	)
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a bead",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			u := store.IssueUpdate{}
			f := cmd.Flags()
			if f.Changed("title") {
				u.Title = &title
			}
			if f.Changed("desc") {
				u.Description = &desc
			}
			if f.Changed("design") {
				u.Design = &design
			}
			if f.Changed("accept") {
				u.AcceptanceCriteria = &accept
			}
			if f.Changed("notes") {
				u.Notes = &notes
			}
			if f.Changed("type") {
				t, err := beads.ParseType(typeStr)
				if err != nil {
					return err
				}
				u.Type = &t
			}
			if f.Changed("status") {
				s, err := beads.ParseStatus(statStr)
				if err != nil {
					return err
				}
				u.Status = &s
			}
			if f.Changed("priority") {
				u.Priority = &priority
			}
			if f.Changed("assignee") {
				u.Assignee = &assignee
			}
			if f.Changed("owner") {
				u.Owner = &owner
			}
			if f.Changed("close-reason") {
				u.CloseReason = &closeReason
			}
			if f.Changed("due") {
				t, err := parseOptTime(dueStr)
				if err != nil {
					return err
				}
				u.DueAt = t
			}
			if f.Changed("defer") {
				t, err := parseOptTime(deferStr)
				if err != nil {
					return err
				}
				u.DeferUntil = t
			}
			if claim {
				me := assigneeFromEnv()
				u.Assignee = &me
				ip := beads.StatusInProgress
				u.Status = &ip
			}
			out, err := cc.store.UpdateIssue(cc.ctx, args[0], u)
			if err != nil {
				return err
			}
			if cc.json {
				return writeJSON(out)
			}
			fmt.Printf("%s  [%s] %s p%d %s\n", out.ID, out.Status, out.Type, out.Priority, out.Title)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVarP(&desc, "desc", "d", "", "new description")
	cmd.Flags().StringVar(&design, "design", "", "new design notes")
	cmd.Flags().StringVar(&accept, "accept", "", "new acceptance criteria")
	cmd.Flags().StringVar(&notes, "notes", "", "new notes")
	cmd.Flags().StringVarP(&typeStr, "type", "t", "", "new type")
	cmd.Flags().StringVarP(&statStr, "status", "s", "", "new status")
	cmd.Flags().IntVarP(&priority, "priority", "p", 0, "new priority")
	cmd.Flags().StringVarP(&assignee, "assignee", "a", "", "new assignee")
	cmd.Flags().StringVar(&owner, "owner", "", "new owner")
	cmd.Flags().StringVar(&closeReason, "close-reason", "", "close reason")
	cmd.Flags().StringVar(&dueStr, "due", "", "new due time (RFC3339)")
	cmd.Flags().StringVar(&deferStr, "defer", "", "new defer-until time (RFC3339)")
	cmd.Flags().BoolVar(&claim, "claim", false, "claim: assign to current user and set in_progress")
	return cmd
}

func assigneeFromEnv() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "unknown"
}
