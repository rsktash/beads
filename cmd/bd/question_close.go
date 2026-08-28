package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// closureEvidencePrefix marks a question's evidence column as carrying a closure
// record rather than provenance. The reason is a single word from a closed set,
// so the first space after the prefix separates it from the free-text note.
const closureEvidencePrefix = "closed:"

// questionCloseReasons maps each --reason code to the status it writes and
// whether it requires --of.
var questionCloseReasons = map[string]struct {
	status  string
	needsOf bool
}{
	"moot":       {status: "retracted", needsOf: false},
	"duplicate":  {status: "retracted", needsOf: true},
	"superseded": {status: "superseded", needsOf: true},
}

// questionCloseReasonList is the codes in help/error order.
var questionCloseReasonList = []string{"moot", "duplicate", "superseded"}

// encodeClosure builds the evidence string for a closed question.
func encodeClosure(reason, note string) string {
	return closureEvidencePrefix + reason + " " + note
}

// decodeClosure splits a closure evidence string back into reason and note.
// It reports false for evidence that is not a closure record.
func decodeClosure(evidence string) (reason, note string, ok bool) {
	if !strings.HasPrefix(evidence, closureEvidencePrefix) {
		return "", "", false
	}
	reason, note, _ = strings.Cut(strings.TrimPrefix(evidence, closureEvidencePrefix), " ")
	if reason == "" {
		return "", "", false
	}
	return reason, note, true
}

func newQuestionCloseCmd() *cobra.Command {
	var (
		reason string
		note   string
		of     string
	)
	cmd := &cobra.Command{
		Use:   "close <question-id> --reason <code> --note <why>",
		Short: "Close a question without minting a ruling (actor-gated: BD_ACTOR=executor is refused)",
		Long: `Close a question that stopped being an execution blocker, without filing a ruling.

Reasons:
  --reason moot        the question no longer applies; status becomes retracted. --of is not allowed.
  --reason duplicate   another question already covers it; status becomes retracted. --of is required and names the surviving question.
  --reason superseded  a better question replaces it; status becomes superseded. --of is required and names the replacing question.

--note is required for every reason. A reason code alone is not a record.

The --of link is written to the closed question's answered_by column, so the
closed question points at the question that took its place. The named question
itself is not modified.

Nothing is deleted. A closed question keeps rendering on bd show under
CLOSED QUESTIONS — NO LONGER BLOCKING, with its reason and note.

Actor gating: BD_ACTOR=executor cannot close questions; use a finding or question instead. BD_ACTOR=coordinator or unset (owner) is allowed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
			}

			questionID := strings.TrimSpace(args[0])
			if questionID == "" {
				return fmt.Errorf("question id is required")
			}
			reasonVal := strings.TrimSpace(reason)
			spec, known := questionCloseReasons[reasonVal]
			if !known {
				return fmt.Errorf("invalid --reason %q (%s)", reasonVal, strings.Join(questionCloseReasonList, "|"))
			}
			noteVal := strings.TrimSpace(note)
			if noteVal == "" {
				return fmt.Errorf("--note is required and cannot be blank")
			}
			ofVal := strings.TrimSpace(of)
			if spec.needsOf && ofVal == "" {
				return fmt.Errorf("--of is required with --reason %s (the %s question id)", reasonVal, survivorWord(reasonVal))
			}
			if !spec.needsOf && ofVal != "" {
				return fmt.Errorf("--of is not allowed with --reason %s", reasonVal)
			}
			if ofVal != "" && ofVal == questionID {
				return fmt.Errorf("--of cannot name the question being closed (%s)", questionID)
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			q, err := cc.store.GetStatement(cc.ctx, questionID)
			if err != nil {
				return fmt.Errorf("question %s not found: %w", questionID, err)
			}
			if q.Kind != "question" {
				return fmt.Errorf("statement %s is not a question (is %s)", questionID, q.Kind)
			}
			if q.Status != "active" {
				return fmt.Errorf("question %s is not active (is %s); it is already closed", questionID, q.Status)
			}
			if ofVal != "" {
				other, err := cc.store.GetStatement(cc.ctx, ofVal)
				if err != nil {
					return fmt.Errorf("--of %s not found: %w", ofVal, err)
				}
				if other.Kind != "question" {
					return fmt.Errorf("--of %s is not a question (is %s)", ofVal, other.Kind)
				}
			}

			if err := cc.store.CloseQuestion(cc.ctx, questionID, spec.status, encodeClosure(reasonVal, noteVal), ofVal); err != nil {
				return err
			}

			updated, err := cc.store.GetStatement(cc.ctx, questionID)
			if err != nil {
				return err
			}
			if cc.json || flagJSON {
				return writeJSONTo(cmd.OutOrStdout(), updated)
			}
			fmt.Fprintln(cmd.OutOrStdout(), updated.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why it stopped blocking: moot|duplicate|superseded (required)")
	cmd.Flags().StringVar(&note, "note", "", "the record of why, in words (required)")
	cmd.Flags().StringVar(&of, "of", "", "the surviving (duplicate) or replacing (superseded) question id")
	_ = cmd.MarkFlagRequired("reason")
	_ = cmd.MarkFlagRequired("note")
	return cmd
}

// survivorWord names what --of points at for a given reason.
func survivorWord(reason string) string {
	if reason == "superseded" {
		return "replacing"
	}
	return "surviving"
}
