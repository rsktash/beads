package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/internal/idgen"
	"github.com/rsktash/beads/store"
)

// `bd plan` — an execution plan is its own record ordering beads drawn from
// any number of epics. Each lane is a concurrent slice with one cursor and at
// most one holding session; a session closes by appending a typed handoff
// entry. The plan is never a second dependency graph: readiness always comes
// from store.Ready and the question rows, never from queue order.
//
// No command here is actor-gated. A night-run session is an executor and must
// be able to create a plan, add lanes and hand off.

const threadLineCap = 5

// planModes is the closed vocabulary migration 0008 CHECKs; validated here so
// a typo reports a lane mode rather than a SQL constraint.
var planModes = map[string]bool{"inline": true, "subagent": true, "codex": true}

func newPlanCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "plan",
		Short: "Execution plans — lanes, cursors and typed handoffs",
	}
	root.AddCommand(
		newPlanCreateCmd(),
		newPlanLaneCmd(),
		newPlanShowCmd(),
		newPlanJoinCmd(),
		newPlanClaimCmd(),
		newPlanHandoffCmd(),
		newPlanDoneCmd(),
		newPlanAbandonCmd(),
	)
	return root
}

// newPlanID is slugify(title) plus a four-character suffix from the shared id
// generator, so a plan reads as `night-run-0904` rather than a bare hash.
func newPlanID(title, creator string) string {
	slug := slugify(title)
	if slug == "" {
		slug = "plan"
	}
	return idgen.GenerateHashID(slug, title, "", creator, time.Now(), 4, 0)
}

func newPlanCreateCmd() *cobra.Command {
	var id, preflight string
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create an execution plan (open to every actor)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := strings.TrimSpace(args[0])
			if title == "" {
				return fmt.Errorf("title is required")
			}
			identity, _ := resolveActor()
			p := &store.ExecutionPlan{
				ID:        strings.TrimSpace(id),
				Title:     title,
				Preflight: preflight,
				CreatedBy: identity,
			}
			if p.ID == "" {
				p.ID = newPlanID(title, identity)
			}
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			if err := cc.store.CreatePlan(cc.ctx, p); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), p.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "plan id (default: slug of the title plus a four-character suffix)")
	cmd.Flags().StringVar(&preflight, "preflight", "", "pre-flight text carried by the plan")
	return cmd
}

func newPlanLaneCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "lane",
		Short: "Manage the lanes of a plan",
	}
	root.AddCommand(newPlanLaneAddCmd())
	return root
}

func newPlanLaneAddCmd() *cobra.Command {
	var queue, mode, rulings string
	cmd := &cobra.Command{
		Use:   "add <plan> <lane>",
		Short: "Add a lane with its queue of bead ids",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := strings.TrimSpace(args[0])
			lane := strings.TrimSpace(args[1])
			if lane == "" {
				return fmt.Errorf("lane name is required")
			}
			if !planModes[mode] {
				return fmt.Errorf("mode %q is not one of inline, subagent, codex", mode)
			}
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			if _, err := cc.store.GetPlan(cc.ctx, planID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("plan %s not found", planID)
				}
				return err
			}
			l := &store.PlanLane{
				PlanID:  planID,
				Lane:    lane,
				Queue:   splitCSV(queue),
				Mode:    mode,
				Rulings: splitCSV(rulings),
			}
			if err := cc.store.AddLane(cc.ctx, l); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "lane %s added to %s with %d queued\n", l.Lane, planID, len(l.Queue))
			return nil
		},
	}
	cmd.Flags().StringVar(&queue, "queue", "", "ordered bead ids, comma-separated")
	cmd.Flags().StringVar(&mode, "mode", "subagent", "inline|subagent|codex")
	cmd.Flags().StringVar(&rulings, "rulings", "", "ruling ids in force for the lane, comma-separated")
	return cmd
}

func newPlanJoinCmd() *cobra.Command {
	var session, lane string
	cmd := &cobra.Command{
		Use:   "join <plan>",
		Short: "Join a plan, optionally taking a lane",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := strings.TrimSpace(args[0])
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			if err := cc.store.JoinPlan(cc.ctx, planID, session, lane); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if lane == "" {
				fmt.Fprintf(out, "session %s joined %s, waiting (no lane)\n", session, planID)
				return nil
			}
			// A held lane leaves the session waiting rather than failing.
			err = cc.store.ClaimLane(cc.ctx, planID, lane, session)
			var held *store.LaneHeldError
			if errors.As(err, &held) {
				fmt.Fprintf(out, "session %s joined %s, waiting: %v\n", session, planID, held)
				return nil
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "session %s joined %s and claimed lane %s\n", session, planID, lane)
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "session id")
	cmd.Flags().StringVar(&lane, "lane", "", "lane to take; omit to wait in the pool")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

func newPlanClaimCmd() *cobra.Command {
	var session, lane string
	cmd := &cobra.Command{
		Use:   "claim <plan>",
		Short: "Take a lane for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := strings.TrimSpace(args[0])
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			if err := cc.store.ClaimLane(cc.ctx, planID, lane, session); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "session %s holds lane %s of %s\n", session, lane, planID)
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "session id")
	cmd.Flags().StringVar(&lane, "lane", "", "lane to take")
	_ = cmd.MarkFlagRequired("session")
	_ = cmd.MarkFlagRequired("lane")
	return cmd
}

func newPlanHandoffCmd() *cobra.Command {
	var session, lane, done, next, parked, threadFile string
	cmd := &cobra.Command{
		Use:   "handoff <plan>",
		Short: "Append a typed handoff entry and release the lane",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := strings.TrimSpace(args[0])
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			l, err := cc.store.GetLane(cc.ctx, planID, lane)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("plan %s has no lane %s", planID, lane)
				}
				return err
			}
			next = strings.TrimSpace(next)
			cursor, err := cursorFor(*l, next)
			if err != nil {
				return err
			}
			parkedPairs, err := validateParked(cc, splitCSV(parked))
			if err != nil {
				return err
			}
			thread := ""
			if threadFile != "" {
				// A file rather than an inline string, matching --body-file:
				// a thread never passes through a shell quote.
				body, err := readFileContents(threadFile)
				if err != nil {
					return fmt.Errorf("--thread-file: %w", err)
				}
				if thread, err = capThread(threadFile, body); err != nil {
					return err
				}
			}
			identity, _ := resolveActor()
			h := &store.PlanHandoff{
				ID:        idgen.GenerateHashID("ph", planID+"|"+lane, identity, session, time.Now(), 8, 0),
				PlanID:    planID,
				Lane:      lane,
				SessionID: session,
				Done:      splitCSV(done),
				NextID:    next,
				Parked:    parkedPairs,
				Thread:    thread,
			}
			if err := cc.store.Handoff(cc.ctx, h, cursor); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "lane %s released by %s, cursor %d/%d\n", lane, session, cursor, len(l.Queue))
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "session id (must hold the lane)")
	cmd.Flags().StringVar(&lane, "lane", "", "lane being released")
	cmd.Flags().StringVar(&done, "done", "", "<bead-id>:<commit-sha> pairs, comma-separated")
	cmd.Flags().StringVar(&next, "next", "", "bead id the lane resumes at; omit when the queue is finished")
	cmd.Flags().StringVar(&parked, "parked", "", "<bead-id>:<question-id> pairs, comma-separated")
	cmd.Flags().StringVar(&threadFile, "thread-file", "", "file holding the handoff thread, five lines at most")
	_ = cmd.MarkFlagRequired("session")
	_ = cmd.MarkFlagRequired("lane")
	return cmd
}

// cursorFor places the lane's cursor on --next. The cursor is what `plan show`
// reads the next item from, so the two can never disagree; an empty --next
// means the queue is finished.
func cursorFor(l store.PlanLane, next string) (int, error) {
	if next == "" {
		return len(l.Queue), nil
	}
	for i, id := range l.Queue {
		if id == next {
			return i, nil
		}
	}
	return 0, fmt.Errorf("--next %s is not in lane %s's queue", next, l.Lane)
}

// capThread refuses a thread longer than five lines, naming the line count.
func capThread(path, body string) (string, error) {
	trimmed := strings.TrimRight(body, "\n")
	if trimmed == "" {
		return "", nil
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > threadLineCap {
		return "", fmt.Errorf("--thread-file %s has %d lines; the cap is %d", path, len(lines), threadLineCap)
	}
	return trimmed, nil
}

// validateParked refuses prose parking: every pair must name an existing
// question. There is no way to park an item without one.
func validateParked(cc *cmdCtx, pairs []string) ([]string, error) {
	out := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		beadID, questionID, ok := strings.Cut(pair, ":")
		beadID = strings.TrimSpace(beadID)
		questionID = strings.TrimSpace(questionID)
		if !ok || beadID == "" || questionID == "" {
			return nil, parkNeedsQuestion(strings.TrimSpace(pair))
		}
		st, err := cc.store.GetStatement(cc.ctx, questionID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, parkNeedsQuestion(beadID)
			}
			return nil, err
		}
		if st.Kind != "question" {
			return nil, parkNeedsQuestion(beadID)
		}
		out = append(out, beadID+":"+questionID)
	}
	return out, nil
}

func parkNeedsQuestion(beadID string) error {
	return fmt.Errorf("park %s needs a question id — file one with bd question add", beadID)
}

// newPlanDoneCmd and newPlanAbandonCmd are the two ways a plan leaves active.
// They share one runner: only the recorded status differs.
func newPlanDoneCmd() *cobra.Command {
	return newPlanEndCmd("done", "Mark a finished plan done", "the plan is finished")
}

func newPlanAbandonCmd() *cobra.Command {
	return newPlanEndCmd("abandon", "Abandon a plan that will never finish", "the plan will never finish")
}

func newPlanEndCmd(verb, short, why string) *cobra.Command {
	status := verb
	if verb == "abandon" {
		status = "abandoned"
	}
	var force bool
	cmd := &cobra.Command{
		Use:   verb + " <plan>",
		Short: short,
		Long:  short + " — " + why + ", so it stops ordering ready and leaves the PLAN column.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := strings.TrimSpace(args[0])
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			err = cc.store.EndPlan(cc.ctx, planID, status, force)
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("plan %s not found", planID)
			}
			var unfinished *store.PlanUnfinishedError
			if errors.As(err, &unfinished) {
				return fmt.Errorf("%v — pass --force to end it anyway", unfinished)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "plan %s %s\n", planID, status)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "end the plan even while a lane is held or short of its end")
	return cmd
}

func newPlanShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <plan>",
		Short: "Show every lane with cursor, holder, last handoff and readiness",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := strings.TrimSpace(args[0])
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			p, err := cc.store.GetPlan(cc.ctx, planID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("plan %s not found", planID)
				}
				return err
			}
			lanes, err := cc.store.ListLanes(cc.ctx, planID)
			if err != nil {
				return err
			}
			last, err := cc.store.LastHandoffPerLane(cc.ctx, planID)
			if err != nil {
				return err
			}
			readiness, err := laneReadiness(cc, lanes)
			if err != nil {
				return err
			}
			renderPlan(cmd.OutOrStdout(), p, lanes, last, readiness)
			return nil
		},
	}
	return cmd
}

// laneReadiness words each lane's next item from the graph, never from the
// queue: ready when store.Ready carries it, blocked by its question when it
// has an active one, blocked otherwise.
func laneReadiness(cc *cmdCtx, lanes []store.PlanLane) (map[string]string, error) {
	nexts := make([]string, 0, len(lanes))
	for _, l := range lanes {
		if n := l.Next(); n != "" {
			nexts = append(nexts, n)
		}
	}
	out := make(map[string]string, len(nexts))
	if len(nexts) == 0 {
		return out, nil
	}
	ready, err := cc.store.Ready(cc.ctx)
	if err != nil {
		return nil, err
	}
	isReady := make(map[string]bool, len(ready))
	for _, i := range ready {
		isReady[i.ID] = true
	}
	questions, err := cc.store.ListStatements(cc.ctx, store.StatementFilter{
		IssueIDs: nexts,
		Kinds:    []string{"question"},
		Statuses: []string{"active"},
	})
	if err != nil {
		return nil, err
	}
	blocker := make(map[string]string, len(questions))
	for _, q := range questions {
		if q.IssueID == nil {
			continue
		}
		if _, seen := blocker[*q.IssueID]; !seen {
			blocker[*q.IssueID] = q.ID
		}
	}
	for _, id := range nexts {
		switch {
		case isReady[id]:
			out[id] = "ready"
		case blocker[id] != "":
			out[id] = "blocked by " + blocker[id]
		default:
			// Queue order and the graph disagree; the graph wins.
			out[id] = "blocked (queue order says go; the graph says wait — the graph wins)"
		}
	}
	return out, nil
}

// renderPlan writes the documented shape: one PLAN line, one LANE line per
// lane, and the last handoff entry indented under its lane. An active plan
// whose lanes have all run out carries a finishable hint under the PLAN line.
func renderPlan(w io.Writer, p *store.ExecutionPlan, lanes []store.PlanLane, last map[string]store.PlanHandoff, readiness map[string]string) {
	fmt.Fprintf(w, "PLAN  %s  %s  %s\n", p.ID, p.Status, p.Title)
	// A plan every lane of which has run out is over but still ordering ready.
	// The hint names the verb; only the verb changes the status.
	if store.Finishable(*p, lanes) {
		fmt.Fprintf(w, "      every lane is done — end it with bd plan done %s\n", p.ID)
	}
	for _, l := range lanes {
		fields := []string{"LANE", l.Lane, fmt.Sprintf("cursor %d/%d", l.Cursor, len(l.Queue))}
		h, handed := last[l.Lane]
		if l.Done() {
			fields = append(fields, "done")
		} else {
			switch {
			case l.Holder != "":
				fields = append(fields, "holder "+l.Holder)
			case handed:
				fields = append(fields, "handed, unclaimed")
			default:
				fields = append(fields, "unclaimed")
			}
			next := l.Next()
			fields = append(fields, "mode "+l.Mode, "next "+next, readiness[next])
		}
		fmt.Fprintln(w, strings.Join(fields, "  "))
		if !handed {
			continue
		}
		entry := []string{fmt.Sprintf("handoff %s %s", h.CreatedAt.UTC().Format("2006-01-02 15:04"), h.SessionID)}
		if len(h.Done) > 0 {
			entry = append(entry, "done "+strings.Join(h.Done, ", "))
		}
		if len(h.Parked) > 0 {
			entry = append(entry, "parked "+strings.Join(h.Parked, ", "))
		}
		fmt.Fprintf(w, "      %s\n", strings.Join(entry, "  "))
		for i, line := range strings.Split(h.Thread, "\n") {
			if h.Thread == "" {
				break
			}
			if i == 0 {
				fmt.Fprintf(w, "      thread: %s\n", line)
				continue
			}
			fmt.Fprintf(w, "              %s\n", line)
		}
	}
}

// splitCSV parses a comma-separated flag value, dropping empty entries.
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
