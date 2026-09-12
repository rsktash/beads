package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/internal/config"
	"github.com/rsktash/beads/store"
)

func newRulingCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ruling",
		Short: "Manage rulings (typed authority records)",
	}
	root.AddCommand(newRulingAddCmd())
	root.AddCommand(newRulingRetireCmd())
	root.AddCommand(newRulingListCmd())
	return root
}

// newRulingListCmd is `bd ruling list`, a sibling alias for `bd rulings`
// (registered separately, not via cobra's Aliases, because the alias sits
// under a different parent command). Same flags, same runRulings body, so
// output is byte-identical for the same arguments.
func newRulingListCmd() *cobra.Command {
	var scope string
	var grep string
	cmd := &cobra.Command{
		Use:   "list [issue-id]",
		Short: "List active rulings (alias for `bd rulings`)",
		Long:  "List active rulings. Without an issue id, lists every active ruling in the project newest first. With an issue id, lists what that bead is bound by via the inheritance resolver (same set the contract shows). Use --scope project to list only project-scoped rulings (issue_id IS NULL). Use --grep <kw> to substring-match across every scope (case-insensitive, over law+text); it cannot be combined with an issue id.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRulings(cmd, args, scope, grep)
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "filter scope: project (only project-scoped rulings)")
	cmd.Flags().StringVar(&grep, "grep", "", "case-insensitive substring match over law+text, across every scope")
	return cmd
}

func newRulingAddCmd() *cobra.Command {
	var (
		supersedes string
		answers    string
		scope      string
		deferStr   string
		closeFlag  bool
		parkFlag   bool
		verbatim   string
		binds      string
		reach      string
		topic      string
		concern    string
		law        string
		rationale  string
		doctrine   bool
	)
	cmd := &cobra.Command{
		Use:   "add [<issue-id>] <text> --topic <slug>",
		Short: "File a ruling (actor-gated: BD_ACTOR=executor is refused)",
		Long: `File a ruling. With one arg, files a project-scoped ruling (issue_id NULL). With two args, first is issue id, second is text.

With --answers the bead is derived from the question: the ruling lands on the
question's own bead, on the nearest epic above it with --reach epic, or
project-scoped with --reach project. A positional bead that disagrees with the
resolved reach is refused, so a task's answer cannot land on its epic by habit.

Flags --defer, --park and --close atomically update the bead's state in the same transaction as the ruling row.
--park defers the bead to the far future (9999-12-31) and adds label 'parked'; it is deferred+parked label, no new status.

--topic is required, project-scoped rulings included, unless --answers names a
question: a ruling then inherits that question's topic, workspace and concern,
and an explicit --topic that disagrees with it is refused. --concern names the
area of a project-scoped ruling, which has no bead to read areas from, and must
name a concern the area vocabulary carries.

--doctrine files a standing law: a project-scoped ruling carrying an area, a
one-sentence --law and an optional --rationale, which bd doctrine render writes
into the doctrine page. It takes no issue id — a bead's ruling becomes doctrine
through bd doctrine promote. Before the write, every law already standing over
the same area prints to stderr, so a contradiction is on screen while there is
still time to supersede instead.

Actor gating: BD_ACTOR=executor cannot file rulings; use a finding or question instead. BD_ACTOR=coordinator or unset (owner) is allowed.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
			}

			// Determine issue id and text
			var issueID *string
			var text string
			if len(args) == 1 {
				text = strings.TrimSpace(args[0])
				// project-scoped: issueID stays nil
			} else {
				id := strings.TrimSpace(args[0])
				if id == "" {
					return fmt.Errorf("issue id is required when two args given")
				}
				issueID = &id
				text = strings.TrimSpace(args[1])
			}
			if text == "" {
				return fmt.Errorf("text is required")
			}
			if path, line, ok := store.BareLineCitation(text); ok {
				return fmt.Errorf("cite the symbol: %s::<symbol>:%d, not %s:%d", path, line, path, line)
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			// The reach: where an answer binds. Resolved before every check
			// that reads the bead, so --close, --binds and --doctrine see the
			// derived bead, not the absent positional.
			answersID := strings.TrimSpace(answers)
			issueID, err = resolveRulingReach(cc, issueID, answersID, strings.TrimSpace(reach))
			if err != nil {
				return err
			}

			// A doctrine is a project law. An issue id would file it on a
			// bead, where nothing reads it as doctrine at all.
			if doctrine && issueID != nil {
				return fmt.Errorf("--doctrine files a project law and takes no issue id; re-scope a bead's ruling with `bd doctrine promote %s --concern <name>`", *issueID)
			}
			if doctrine || strings.TrimSpace(law) != "" {
				if err := validateDoctrineLaw(law); err != nil {
					return err
				}
			}
			if err := validateDoctrineRationale(rationale); err != nil {
				return err
			}

			// Validate mutually exclusive state flags
			f := cmd.Flags()
			hasDefer := f.Changed("defer")
			hasClose := f.Changed("close")
			hasPark := parkFlag
			if hasDefer && hasClose {
				return fmt.Errorf("--defer and --close are mutually exclusive")
			}
			if hasPark && hasDefer {
				return fmt.Errorf("--park and --defer are mutually exclusive")
			}
			if hasPark && hasClose {
				return fmt.Errorf("--park and --close are mutually exclusive")
			}
			// defer/park/close require issue id
			if (hasDefer || hasClose || hasPark) && issueID == nil {
				return fmt.Errorf("--defer, --park and --close require an issue id")
			}

			// Scope
			scopeVal := "inherit"
			if f.Changed("scope") {
				if scope == "self" {
					scopeVal = "self"
				} else if scope == "inherit" {
					scopeVal = "inherit"
				} else {
					return fmt.Errorf("invalid --scope %q (self|inherit)", scope)
				}
			}

			// Build statement
			st := &beads.Statement{
				Kind:      "ruling",
				IssueID:   issueID,
				Text:      text,
				FiledBy:   identity,
				Status:    "active",
				Scope:     scopeVal,
				Verbatim:  strings.TrimSpace(verbatim),
				Law:       strings.TrimSpace(law),
				Rationale: strings.TrimSpace(rationale),
			}
			if f.Changed("supersedes") && supersedes != "" {
				s := strings.TrimSpace(supersedes)
				st.SupersedesID = &s
			}
			if f.Changed("binds") && binds != "" {
				if issueID == nil {
					return fmt.Errorf("--binds is redundant on a project-scoped ruling")
				}
				b := strings.TrimSpace(binds)
				if b == *issueID {
					return fmt.Errorf("--binds names the bead the ruling is already on")
				}
				st.BindsID = &b
			}
			// Build issue update if needed
			var upd *store.IssueUpdate
			if hasDefer {
				t, err := time.Parse(time.RFC3339, deferStr)
				if err != nil {
					return fmt.Errorf("--defer: %w", err)
				}
				upd = &store.IssueUpdate{DeferUntil: &t}
			}
			if hasPark {
				t := store.ParkDeferUntil
				upd = &store.IssueUpdate{DeferUntil: &t, AddLabels: []string{"parked"}}
			}
			if hasClose {
				closed := beads.StatusClosed
				upd = &store.IssueUpdate{Status: &closed}
			}

			// Topic before the duplicate listing: a ruling with no topic is
			// refused with the catalogue, and printing both menus at once
			// would bury the one the writer must act on.
			if err := validateAreaName(cc, store.AreaConcern, concern); err != nil {
				return err
			}
			if err := applyRulingTopic(cc, cmd.ErrOrStderr(), st, answersID, topic, concern); err != nil {
				return err
			}
			// A law with no area binds nothing: `bd doctrine` and the DOCTRINE
			// section both read an area, so an arealess law would be filed and
			// never seen again.
			if doctrine && st.Concern == "" && st.Workspace == "" {
				return fmt.Errorf("--doctrine needs an area: name it with --concern <name>, or answer a question that carries one")
			}

			// Print the bead's own existing rulings to stderr, and refuse a
			// write that repeats one of their headlines — unless this ruling
			// supersedes one explicitly, which is the sanctioned way to
			// restate.
			if err := printExistingRulingsAndCheckDuplicate(cc, cmd.ErrOrStderr(), issueID, text, st.SupersedesID != nil, st.Concern); err != nil {
				return err
			}

			// Use atomic transaction that spans statements and issues.
			if answersID != "" {
				err = cc.store.CreateRulingWithStateChange(cc.ctx, st, upd, answersID)
			} else {
				err = cc.store.CreateStatementWithIssueUpdate(cc.ctx, st, upd)
			}
			if err != nil {
				return err
			}
			if err := stampStatementHeadSHA(cc, st.ID); err != nil {
				return err
			}

			// Print new id to caller's stdout (cobra managed)
			fmt.Fprintln(cmd.OutOrStdout(), st.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&supersedes, "supersedes", "", "ruling id this supersedes (sets old to superseded)")
	cmd.Flags().StringVar(&answers, "answers", "", "question id this ruling answers")
	cmd.Flags().StringVar(&scope, "scope", "", "scope: self or inherit (default inherit)")
	cmd.Flags().StringVar(&deferStr, "defer", "", "defer bead until RFC3339 timestamp (atomic with ruling)")
	cmd.Flags().BoolVar(&closeFlag, "close", false, "close the bead (atomic with ruling)")
	cmd.Flags().BoolVar(&parkFlag, "park", false, "park the bead (defer far future + label 'parked', atomic with ruling)")
	cmd.Flags().StringVar(&verbatim, "verbatim", "", "the owner's verbatim sentence backing this ruling (stored untouched, never in the default headline)")
	cmd.Flags().StringVar(&binds, "binds", "", "attach this ruling explicitly to a second bead (issue-scoped rulings only)")
	cmd.Flags().StringVar(&reach, "reach", "", "with --answers: where the answer binds — task (default: the question's bead), epic (the nearest epic above it) or project")
	cmd.Flags().StringVar(&topic, "topic", "", "topic slug this ruling belongs to (required unless --answers supplies it)")
	cmd.Flags().StringVar(&concern, "concern", "", "concern for a project-scoped ruling, which has no bead to read areas from")
	cmd.Flags().BoolVar(&doctrine, "doctrine", false, "file a standing law: a project-scoped ruling with an area, a --law and a --rationale")
	cmd.Flags().StringVar(&law, "law", "", "the law itself: one imperative sentence ending in a full stop (max 200 characters)")
	cmd.Flags().StringVar(&rationale, "rationale", "", "why the law holds, and the pointers behind it (max 600 characters)")
	return cmd
}

// resolveRulingReach returns the bead a ruling lands on. Without --answers it
// is the positional bead as given (nil = project-scoped) and --reach is
// refused. With --answers the question's bead is the default, --reach epic
// lifts it to the nearest epic above, --reach project drops it to project
// scope; a positional bead that disagrees with that result is refused.
func resolveRulingReach(cc *cmdCtx, positional *string, answersID, reach string) (*string, error) {
	if answersID == "" {
		if reach != "" {
			return nil, fmt.Errorf("--reach is only for a ruling that answers a question: pair it with --answers Q-n")
		}
		return positional, nil
	}
	switch reach {
	case "", "task", "epic", "project":
	default:
		return nil, fmt.Errorf("invalid --reach %q (task|epic|project)", reach)
	}
	q, err := cc.store.GetStatement(cc.ctx, answersID)
	if err != nil {
		return nil, fmt.Errorf("question %s not found: %w", answersID, err)
	}
	if q.IssueID == nil {
		// A project-scoped question has no bead to derive from; only a
		// project-scoped answer fits it.
		if positional != nil || (reach != "" && reach != "project") {
			return nil, fmt.Errorf("%s is project-scoped; its answer takes no bead and no --reach but project", answersID)
		}
		return nil, nil
	}
	var target *string
	switch reach {
	case "", "task":
		target = q.IssueID
	case "epic":
		epicID, err := nearestEpicAbove(cc, *q.IssueID)
		if err != nil {
			return nil, err
		}
		target = &epicID
	case "project":
		if positional != nil {
			return nil, fmt.Errorf("--reach project files project-scoped and takes no issue id (got %s)", *positional)
		}
		return nil, nil
	}
	if positional != nil && *positional != *target {
		return nil, fmt.Errorf("%s sits on %s, so its answer lands there by default; a ruling on %s needs --reach epic (its epic) or --reach project", answersID, *q.IssueID, *positional)
	}
	return target, nil
}

// nearestEpicAbove walks the parent-child chain upward from issueID and returns
// the first bead of type epic.
func nearestEpicAbove(cc *cmdCtx, issueID string) (string, error) {
	ancestors, err := cc.store.Ancestors(cc.ctx, issueID)
	if err != nil {
		return "", err
	}
	for _, id := range ancestors {
		i, err := cc.store.GetIssue(cc.ctx, id)
		if err != nil {
			return "", err
		}
		if i.Type == beads.TypeEpic {
			return id, nil
		}
	}
	return "", fmt.Errorf("--reach epic: %s has no epic above it", issueID)
}

func stampStatementHeadSHA(cc *cmdCtx, id string) error {
	cfg, err := config.Resolve(flagDB)
	if err != nil || cfg.ProjectRoot == "" {
		return nil
	}
	git := exec.CommandContext(cc.ctx, "git", "rev-parse", "HEAD")
	git.Dir = cfg.ProjectRoot
	out, err := git.Output()
	if err != nil {
		return nil
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return nil
	}
	return cc.store.SetStatementHeadSHA(cc.ctx, id, sha)
}

// printExistingRulingsAndCheckDuplicate lists the set a new ruling would
// join — the bead's own active rulings for an issue-scoped ruling, or the
// project-scoped rows for a project ruling (never the inheritance-resolved
// set: the refusal below must stay actionable with --supersedes, and a
// child bead cannot supersede a ruling it only inherited). The listing
// always prints, even when the write goes on to succeed or the set is
// empty, and always to errW (stderr) so the command's stdout stays new-id-only.
// When skipDuplicateCheck is false, it also refuses a text whose headline
// (case-insensitive, first 120 runes) repeats an existing one.
//
// concern narrows a project listing to one area. A law is written against the
// laws that already stand over its area, and the project set as a whole is too
// long to read at the moment of writing one.
func printExistingRulingsAndCheckDuplicate(cc *cmdCtx, errW io.Writer, issueID *string, text string, skipDuplicateCheck bool, concern string) error {
	f := store.StatementFilter{Kinds: []string{"ruling"}, Statuses: []string{"active"}}
	var scopeLabel, headlineIssueID string
	if issueID != nil {
		f.IssueIDs = []string{*issueID}
		scopeLabel = *issueID
		headlineIssueID = *issueID
	} else {
		f.IssueIDs = []string{""}
		scopeLabel = "the project"
	}

	list, err := cc.store.ListStatements(cc.ctx, f)
	if err != nil {
		return err
	}
	if issueID == nil {
		if area := strings.TrimSpace(concern); area != "" {
			list = rulingsInConcern(list, area)
			scopeLabel = "the project, concern " + area
		}
	}

	if len(list) == 0 {
		fmt.Fprintf(errW, "no existing rulings on %s\n", scopeLabel)
	} else {
		fmt.Fprintf(errW, "existing rulings on %s (%d):\n", scopeLabel, len(list))
		for _, r := range list {
			fmt.Fprintln(errW, "  "+rulingHeadline(r, headlineIssueID))
		}
	}

	if skipDuplicateCheck {
		return nil
	}

	newHeadline := strings.ToLower(headlineText(text))
	for _, r := range list {
		if strings.ToLower(headlineText(r.Text)) == newHeadline {
			addArgs := fmt.Sprintf("%q", "<text>")
			if issueID != nil {
				addArgs = fmt.Sprintf("%s %q", *issueID, "<text>")
			}
			return fmt.Errorf("%s already says this on %s — amend it with `bd ruling add %s --supersedes %s`, or file a question if it is genuinely a new decision", r.ID, scopeLabel, addArgs, r.ID)
		}
	}
	return nil
}
