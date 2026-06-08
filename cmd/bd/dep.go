package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
)

func newDepCmd() *cobra.Command {
	dep := &cobra.Command{Use: "dep", Short: "Manage dependencies"}
	dep.AddCommand(newDepAddCmd(), newDepRmCmd(), newDepListCmd())
	return dep
}

// depDisplayLabel returns the verb to render between issue_id and depends_on_id
// for printable output. The stored `blocks` type means "issue_id is BLOCKED BY
// depends_on_id" (depends_on_id is the blocker — see the ready query in
// sql/queries.sql). Rendering the row as `A -blocks-> B` reads naturally as
// "A blocks B", which is the inverse of the truth. Display `blocked-by`
// instead. Other type names already read correctly with issue_id on the left.
func depDisplayLabel(t beads.DependencyType) string {
	if t == beads.DepBlocks {
		return "blocked-by"
	}
	return string(t)
}

// depRelation renders a dependency edge from the perspective of issue `id` as
// a natural verb phrase plus the other issue's id (e.g. "blocked by bd-12").
// Stored edges are `IssueID <type> DependsOnID`. Two types read INVERTED from
// their literal name and are the source of the "id -> blocks X" misreading
// trap: `blocks` means IssueID is BLOCKED BY DependsOnID (DependsOnID is the
// blocker — see the ready query in sql/queries.sql), and `parent-child` means
// IssueID is the CHILD of DependsOnID (see get.go/close.go). Rendering the
// relationship as an explicit, direction-aware verb removes the ambiguity that
// a bare arrow + raw type name creates. `forward` is true when `id` sits on the
// left of the stored edge (id == IssueID).
func depRelation(id string, d beads.Dependency) (verb, other string) {
	forward := d.IssueID == id
	if forward {
		other = d.DependsOnID
	} else {
		other = d.IssueID
	}
	switch d.Type {
	case beads.DepBlocks:
		if forward {
			return "blocked by", other
		}
		return "blocks", other
	case beads.DepParentChild:
		if forward {
			return "child of", other
		}
		return "parent of", other
	case beads.DepDuplicates:
		if forward {
			return "duplicates", other
		}
		return "duplicated by", other
	case beads.DepSupersedes:
		if forward {
			return "supersedes", other
		}
		return "superseded by", other
	case beads.DepRepliesTo:
		if forward {
			return "replies to", other
		}
		return "replied to by", other
	case beads.DepDiscoveredBy:
		if forward {
			return "discovered by", other
		}
		return "discovered", other
	default: // DepRelatesTo and any future symmetric type
		return "related to", other
	}
}

func newDepAddCmd() *cobra.Command {
	var typeStr, threadID string
	cmd := &cobra.Command{
		Use:   "add <issue> <depends-on>",
		Short: "Add dependency: <issue> depends on <depends-on> (with --type). For blocks-type, <depends-on> is the blocker.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			dt, err := beads.ParseDependencyType(typeStr)
			if err != nil {
				return err
			}
			d := beads.Dependency{
				IssueID: args[0], DependsOnID: args[1], Type: dt, ThreadID: threadID,
				CreatedBy: assigneeFromEnv(),
			}
			if err := cc.store.AddDependency(cc.ctx, d); err != nil {
				return err
			}
			fmt.Printf("%s -%s-> %s\n", args[0], depDisplayLabel(dt), args[1])
			return nil
		},
	}
	cmd.Flags().StringVarP(&typeStr, "type", "t", string(beads.DepBlocks), "dependency type (blocks|related|duplicates|supersedes|replies-to|parent-child|discovered-by)")
	cmd.Flags().StringVar(&threadID, "thread", "", "thread id (for message threads)")
	return cmd
}

func newDepRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <issue> <depends-on>",
		Short: "Remove a dependency",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			return cc.store.RemoveDependency(cc.ctx, args[0], args[1])
		},
	}
}

func newDepListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <id>",
		Short: "List dependencies touching an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			deps, err := cc.store.ListDependencies(cc.ctx, args[0])
			if err != nil {
				return err
			}
			if cc.json {
				return writeJSON(deps)
			}
			for _, d := range deps {
				fmt.Printf("%s -%s-> %s\n", d.IssueID, depDisplayLabel(d.Type), d.DependsOnID)
			}
			return nil
		},
	}
}
