package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
)

func newCreateCmd() *cobra.Command {
	var (
		desc, design, accept, notes string
		bodyFile, designFile        string
		typeStr                     string
		priority                    int
		assignee, owner             string
		labels                      []string
		dueStr, deferStr            string
		ephemeral                   bool
		sender                      string
		parentID                    string
	)
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new bead (issue/bug/epic/feature/message/event/...)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			t, err := beads.ParseType(typeStr)
			if err != nil {
				return err
			}
			// --body-file / --design-file override the inline string flags so
			// agents can write structured prose without shell-escaping pain.
			if bodyFile != "" {
				body, err := readFileContents(bodyFile)
				if err != nil {
					return fmt.Errorf("--body-file: %w", err)
				}
				desc = body
			}
			if designFile != "" {
				body, err := readFileContents(designFile)
				if err != nil {
					return fmt.Errorf("--design-file: %w", err)
				}
				design = body
			}
			due, err := parseOptTime(dueStr)
			if err != nil {
				return fmt.Errorf("--due: %w", err)
			}
			defer_, err := parseOptTime(deferStr)
			if err != nil {
				return fmt.Errorf("--defer: %w", err)
			}
			i := &beads.Issue{
				Title:              strings.TrimSpace(args[0]),
				Description:        desc,
				Design:             design,
				AcceptanceCriteria: accept,
				Notes:              notes,
				Type:               t,
				Status:             beads.StatusOpen,
				Priority:           priority,
				Assignee:           assignee,
				Owner:              owner,
				DueAt:              due,
				DeferUntil:         defer_,
				Ephemeral:          ephemeral,
				Sender:             sender,
			}
			// --parent: allocate a hierarchical id "<parent>.N" via the
			// child_counters table (atomic) and auto-add the parent-child
			// dependency. Mirrors upstream's behaviour.
			if parentID != "" {
				if _, err := cc.store.GetIssue(cc.ctx, parentID); err != nil {
					return fmt.Errorf("parent %s: %w", parentID, err)
				}
				childID, err := cc.store.NextChildID(cc.ctx, parentID)
				if err != nil {
					return fmt.Errorf("alloc child id under %s: %w", parentID, err)
				}
				i.ID = childID
			}
			if err := cc.store.CreateIssue(cc.ctx, i); err != nil {
				return err
			}
			if parentID != "" {
				if err := cc.store.AddDependency(cc.ctx, beads.Dependency{
					IssueID:     i.ID,
					DependsOnID: parentID,
					Type:        beads.DepParentChild,
					CreatedBy:   assigneeFromEnv(),
				}); err != nil {
					return fmt.Errorf("link parent-child %s -> %s: %w", i.ID, parentID, err)
				}
			}
			for _, l := range labels {
				if err := cc.store.AddLabel(cc.ctx, i.ID, l); err != nil {
					return fmt.Errorf("label %s: %w", l, err)
				}
			}
			i.Labels = labels
			if cc.json {
				return writeJSON(i)
			}
			fmt.Printf("%s  %s\n", i.ID, i.Title)
			return nil
		},
	}
	cmd.Flags().StringVarP(&desc, "desc", "d", "", "description body")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "read description body from file (overrides --desc)")
	cmd.Flags().StringVar(&design, "design", "", "design notes")
	cmd.Flags().StringVar(&designFile, "design-file", "", "read design notes from file (overrides --design)")
	cmd.Flags().StringVar(&accept, "accept", "", "acceptance criteria")
	cmd.Flags().StringVar(&notes, "notes", "", "extra notes")
	cmd.Flags().StringVarP(&typeStr, "type", "t", "task", "issue type (task|bug|epic|feature|message|wisp|molecule|role|event)")
	cmd.Flags().IntVarP(&priority, "priority", "p", 2, "priority 0..4 (0=highest)")
	cmd.Flags().StringVarP(&assignee, "assignee", "a", "", "assignee identifier")
	cmd.Flags().StringVar(&owner, "owner", "", "owner identifier")
	cmd.Flags().StringSliceVarP(&labels, "label", "l", nil, "label (repeatable)")
	cmd.Flags().StringVar(&dueStr, "due", "", "due-by RFC3339 timestamp")
	cmd.Flags().StringVar(&deferStr, "defer", "", "defer-until RFC3339 timestamp (excludes from `ready`)")
	cmd.Flags().BoolVar(&ephemeral, "ephemeral", false, "ephemeral bead (excluded from ready)")
	cmd.Flags().StringVar(&sender, "sender", "", "sender (for message beads)")
	cmd.Flags().StringVar(&parentID, "parent", "", "create as a child of this issue (allocates hierarchical id `<parent>.N` and links via parent-child)")
	return cmd
}

func parseOptTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
