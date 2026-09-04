package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
)

// decideTitleRE matches a title that asks for a decision: the bead is a
// decision point, so it cannot exist without its question attached.
var decideTitleRE = regexp.MustCompile(`(?i)^\s*decide\b[:\s]`)

// validateCreateQuestion applies the Decide gate and the --question/--topic
// rules before any write. A "Decide:" title without its question is the exact
// artefact the gate exists to prevent, so it is refused before a store is
// even opened. --question on a plain title is allowed: the gate is a floor,
// not a ceiling.
func validateCreateQuestion(title, question, topic string) error {
	question = strings.TrimSpace(question)
	topic = strings.TrimSpace(topic)
	if decideTitleRE.MatchString(title) && question == "" {
		return fmt.Errorf("a \"Decide:\" bead needs the question attached: bd create \"<title>\" --question \"<the fork>\" --topic <slug>")
	}
	if question == "" {
		return nil
	}
	if topic == "" {
		return fmt.Errorf("--question requires --topic")
	}
	return validateTopicSlug("--topic", topic)
}

func newCreateCmd() *cobra.Command {
	var (
		desc, accept, notes string
		bodyFile            string
		typeStr             string
		priority            int
		assignee, owner     string
		labels              []string
		dueStr, deferStr    string
		ephemeral           bool
		sender              string
		parentID            string
		questionText        string
		questionTopic       string
	)
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new bead (issue/bug/epic/feature/message/event/...)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := strings.TrimSpace(args[0])
			if err := validateCreateQuestion(title, questionText, questionTopic); err != nil {
				return err
			}
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			t, err := beads.ParseType(typeStr)
			if err != nil {
				return err
			}
			// --body-file overrides the inline string flag so agents can write
			// structured prose without shell-escaping pain.
			if bodyFile != "" {
				body, err := readFileContents(bodyFile)
				if err != nil {
					return fmt.Errorf("--body-file: %w", err)
				}
				desc = body
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
				Title:              title,
				Description:        desc,
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
			// --question: the bead and its question land in ONE transaction
			// (store.CreateIssueWithQuestion), so a failure to file the question
			// rolls the bead back instead of leaving a "Decide:" bead with no
			// question — the artefact the gate exists to prevent.
			if question := strings.TrimSpace(questionText); question != "" {
				identity, _ := resolveActor()
				q := &beads.Statement{
					Kind:    "question",
					Text:    question,
					FiledBy: identity,
					Topic:   strings.TrimSpace(questionTopic),
				}
				if parentID != "" {
					i.CreatedBy = assigneeFromEnv()
				}
				if err := cc.store.CreateIssueWithQuestion(cc.ctx, parentID, i, labels, q); err != nil {
					return err
				}
			} else if parentID != "" {
				// --parent: allocate a hierarchical id "<parent>.N", insert the
				// issue, link the parent-child edge, and apply labels — all in ONE
				// transaction (store.CreateChild) so a failed insert can't leave the
				// child counter advanced with no bead (a gap) or an orphan bead.
				i.CreatedBy = assigneeFromEnv()
				if err := cc.store.CreateChild(cc.ctx, parentID, i, labels); err != nil {
					return err
				}
			} else {
				if err := cc.store.CreateIssue(cc.ctx, i); err != nil {
					return err
				}
				for _, l := range labels {
					if err := cc.store.AddLabel(cc.ctx, i.ID, l); err != nil {
						return fmt.Errorf("label %s: %w", l, err)
					}
				}
				i.Labels = labels
			}
			if cc.json {
				return writeJSON(i)
			}
			fmt.Printf("%s  %s\n", i.ID, i.Title)
			return nil
		},
	}
	cmd.Flags().StringVarP(&desc, "desc", "d", "", "description body")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "read description body from file (overrides --desc)")
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
	cmd.Flags().StringVar(&questionText, "question", "", "file this question on the new bead in the same transaction (required for a \"Decide:\" title)")
	cmd.Flags().StringVar(&questionTopic, "topic", "", "topic slug for --question (required with --question; [a-z0-9-]{2,48})")
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
