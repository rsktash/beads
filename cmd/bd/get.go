package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
)

// newGetCmd implements `bd get <id> <field>` — a narrow field accessor that
// replaces the `bd show --json | jq -r '.[0].<field>'` dance. Each supported
// field issues only the store calls it needs, so reading a title doesn't pay
// for dependencies/comments/labels.
//
// Supported fields:
//
//	title              raw title string
//	status             one-word status
//	priority           integer
//	type               type discriminator
//	assignee           assignee or empty
//	owner              owner or empty
//	description        raw description body (use bd show --section for slices)
//	design             raw design body
//	accept             raw acceptance criteria
//	notes              raw notes
//	parent             parent id via the parent-child edge, empty if none
//	deps               one DependsOnID per line (this issue's outgoing edges)
//	rdeps              one IssueID per line (incoming edges)
//	labels             one label per line
//	comments-count     integer
//	updated_at         RFC3339
//	created_at         RFC3339
//	closed_at          RFC3339 or empty
func newGetCmd() *cobra.Command {
	var (
		linesStr string
		headN    int
		tailN    int
		section  string
	)
	cmd := &cobra.Command{
		Use:   "get <id> <field>",
		Short: "Read a single field from a bead (raw, no quoting). Use `bd get <id> fields` to list available fields.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, field := args[0], strings.ToLower(strings.TrimSpace(args[1]))
			if field == "fields" || field == "help" {
				fmt.Println(strings.Join(getFieldNames(), "\n"))
				return nil
			}
			sliceSel, err := parseLineSlice(linesStr, headN, tailN)
			if err != nil {
				return err
			}
			emitBody := func(s string) {
				if section != "" {
					if body, ok := extractSection(s, section); ok {
						s = body
					} else {
						s = ""
					}
				}
				s = sliceSel.apply(s)
				fmt.Print(s)
				if !strings.HasSuffix(s, "\n") {
					fmt.Println()
				}
			}
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			// Fields that don't need the issue row.
			switch field {
			case "deps", "dependencies":
				deps, err := cc.store.ListDependencies(cc.ctx, id)
				if err != nil {
					return err
				}
				for _, d := range deps {
					if d.IssueID == id {
						fmt.Println(d.DependsOnID)
					}
				}
				return nil
			case "rdeps", "rdependencies":
				deps, err := cc.store.ListDependencies(cc.ctx, id)
				if err != nil {
					return err
				}
				for _, d := range deps {
					if d.DependsOnID == id {
						fmt.Println(d.IssueID)
					}
				}
				return nil
			case "parent":
				deps, err := cc.store.ListDependencies(cc.ctx, id)
				if err != nil {
					return err
				}
				// child --parent-child--> parent: row where IssueID==id.
				for _, d := range deps {
					if d.Type == beads.DepParentChild && d.IssueID == id {
						fmt.Println(d.DependsOnID)
						return nil
					}
				}
				return nil
			case "labels":
				ls, err := cc.store.ListLabels(cc.ctx, id)
				if err != nil {
					return err
				}
				for _, l := range ls {
					fmt.Println(l)
				}
				return nil
			case "comments-count", "comment-count":
				cs, err := cc.store.ListComments(cc.ctx, id)
				if err != nil {
					return err
				}
				fmt.Println(len(cs))
				return nil
			}

			i, err := cc.store.GetIssue(cc.ctx, id)
			if err != nil {
				return err
			}
			switch field {
			case "id":
				fmt.Println(i.ID)
			case "title":
				fmt.Println(i.Title)
			case "status":
				fmt.Println(i.Status)
			case "priority":
				fmt.Println(i.Priority)
			case "type":
				fmt.Println(i.Type)
			case "assignee":
				fmt.Println(i.Assignee)
			case "owner":
				fmt.Println(i.Owner)
			case "description", "desc", "body":
				emitBody(i.Description)
			case "design":
				emitBody(i.Design)
			case "accept", "acceptance", "acceptance_criteria":
				emitBody(i.AcceptanceCriteria)
			case "notes":
				emitBody(i.Notes)
			case "created_at", "created":
				fmt.Println(i.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
			case "updated_at", "updated":
				fmt.Println(i.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
			case "closed_at", "closed":
				if i.ClosedAt != nil {
					fmt.Println(i.ClosedAt.Format("2006-01-02T15:04:05Z07:00"))
				}
			case "close_reason":
				fmt.Println(i.CloseReason)
			default:
				return fmt.Errorf("unknown field %q (try `bd get <id> fields`)", args[1])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&linesStr, "lines", "", "for description/design/accept/notes: 1-indexed range START-END, START-, -END, or N")
	cmd.Flags().IntVar(&headN, "head", 0, "first N lines of the field (long-form fields only)")
	cmd.Flags().IntVar(&tailN, "tail", 0, "last N lines of the field (long-form fields only)")
	cmd.Flags().StringVar(&section, "section", "", "for description: extract one markdown ## section before slicing")
	return cmd
}

func getFieldNames() []string {
	return []string{
		"id", "title", "status", "priority", "type", "assignee", "owner",
		"description", "design", "accept", "notes",
		"parent", "deps", "rdeps", "labels", "comments-count",
		"created_at", "updated_at", "closed_at", "close_reason",
	}
}
