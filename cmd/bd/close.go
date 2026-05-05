package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newCloseCmd() *cobra.Command {
	var reason string
	var noCascade bool
	cmd := &cobra.Command{
		Use:   "close <id>...",
		Short: "Close one or more beads (auto-closes parent epic when its last open child closes)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			closed := beads.StatusClosed
			for _, id := range args {
				u := store.IssueUpdate{Status: &closed}
				if reason != "" {
					u.CloseReason = &reason
				}
				if _, err := cc.store.UpdateIssue(cc.ctx, id, u); err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
				fmt.Printf("closed %s\n", id)
				if !noCascade {
					if err := cascadeCloseParent(cc.ctx, cc.store, id, reason); err != nil {
						return fmt.Errorf("%s parent cascade: %w", id, err)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&reason, "reason", "r", "", "close reason")
	cmd.Flags().BoolVar(&noCascade, "no-cascade", false, "do NOT auto-close parent epic when its last open child closes")
	return cmd
}

// cascadeCloseParent walks up the parent-child chain. After a child closes,
// if its parent has no more open/in_progress/blocked children, close the
// parent too — recursively. Skills assume this behaviour ("bd close on the
// last open child of an epic may auto-close the parent epic").
func cascadeCloseParent(ctx context.Context, st *store.Store, childID, reason string) error {
	deps, err := st.ListDependencies(ctx, childID)
	if err != nil {
		return err
	}
	var parentID string
	for _, d := range deps {
		if d.Type == beads.DepParentChild && d.IssueID == childID {
			parentID = d.DependsOnID
			break
		}
	}
	if parentID == "" {
		return nil
	}
	// Are there any non-closed siblings? Walk parent's children.
	siblingDeps, err := st.ListDependencies(ctx, parentID)
	if err != nil {
		return err
	}
	for _, d := range siblingDeps {
		if d.Type != beads.DepParentChild || d.DependsOnID != parentID {
			continue
		}
		sib, err := st.GetIssue(ctx, d.IssueID)
		if err != nil {
			continue
		}
		if sib.Status != beads.StatusClosed {
			return nil // still has open work
		}
	}
	// All children closed. Close the parent if it isn't already.
	parent, err := st.GetIssue(ctx, parentID)
	if err != nil {
		return err
	}
	if parent.Status == beads.StatusClosed {
		return nil
	}
	closed := beads.StatusClosed
	u := store.IssueUpdate{Status: &closed}
	if reason != "" {
		r := "auto: all children closed (" + reason + ")"
		u.CloseReason = &r
	} else {
		r := "auto: all children closed"
		u.CloseReason = &r
	}
	if _, err := st.UpdateIssue(ctx, parentID, u); err != nil {
		return err
	}
	fmt.Printf("auto-closed parent %s (all children closed)\n", parentID)
	// Recurse — parent may itself be a child of a higher epic.
	return cascadeCloseParent(ctx, st, parentID, reason)
}
