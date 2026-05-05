package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// newBatchCmd implements upstream's `bd batch`: a line-oriented script of
// operations applied to the store. v0.1 runs them sequentially — first error
// aborts; already-applied ops are NOT rolled back. Atomic transactional mode
// is a follow-up.
//
// Grammar (one op per line; blank lines and `#` comments ignored):
//
//	create <type> <priority> "title with spaces"  [key=value ...]
//	update <id> key=value [key=value ...]
//	close  <id> [reason words...]
//	dep    add <issue> <depends-on> [type]
//	dep    rm  <issue> <depends-on>
//	label  add <id> <label>
//	label  rm  <id> <label>
//	comment <id> "text"
//
// `key` for create/update is one of: title, desc, design, accept, notes,
// status, priority, type, assignee, owner, due, defer, ephemeral. Tokens are
// whitespace-separated; double-quote a token to include spaces (\" and \\
// escape).
func newBatchCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Apply a line-oriented script of operations from stdin or --file",
		Long: `Run multiple beads ops in one shot. Reads from stdin by default; pass
--file <path> to read from a file. Lines starting with '#' are comments.

Example script:
  # bootstrap a small project
  create epic 0 "Auth rewrite"
  create task 1 "Login endpoint"  assignee=alice
  create task 1 "Logout flow"
  dep add bd-XXXX bd-YYYY
  label add bd-YYYY infra
  comment bd-YYYY "owner is alice"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			var r io.Reader = os.Stdin
			if file != "" {
				f, err := os.Open(file)
				if err != nil {
					return err
				}
				defer f.Close()
				r = f
			}
			return runBatch(cc, r)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "read script from file (default stdin)")
	return cmd
}

func runBatch(cc *cmdCtx, r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	created, updated, closed := 0, 0, 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		toks, err := tokenize(line)
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
		if len(toks) == 0 {
			continue
		}
		switch toks[0] {
		case "create":
			id, err := batchCreate(cc, toks[1:])
			if err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
			fmt.Printf("created %s\n", id)
			created++
		case "update":
			if err := batchUpdate(cc, toks[1:]); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
			updated++
		case "close":
			if err := batchClose(cc, toks[1:]); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
			closed++
		case "dep":
			if err := batchDep(cc, toks[1:]); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
		case "label":
			if err := batchLabel(cc, toks[1:]); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
		case "comment":
			if err := batchComment(cc, toks[1:]); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
		default:
			return fmt.Errorf("line %d: unknown op %q", lineNo, toks[0])
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	fmt.Printf("ok: %d created, %d updated, %d closed\n", created, updated, closed)
	return nil
}

func batchCreate(cc *cmdCtx, args []string) (string, error) {
	if len(args) < 3 {
		return "", fmt.Errorf("create: usage `create <type> <priority> <title> [key=value ...]`")
	}
	t, err := beads.ParseType(args[0])
	if err != nil {
		return "", err
	}
	prio, err := strconv.Atoi(args[1])
	if err != nil {
		return "", fmt.Errorf("create: priority must be int: %w", err)
	}
	i := &beads.Issue{
		Title:    args[2],
		Type:     t,
		Status:   beads.StatusOpen,
		Priority: prio,
	}
	for _, kv := range args[3:] {
		if err := applyKV(i, kv); err != nil {
			return "", err
		}
	}
	if err := cc.store.CreateIssue(cc.ctx, i); err != nil {
		return "", err
	}
	return i.ID, nil
}

func batchUpdate(cc *cmdCtx, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("update: usage `update <id> key=value [...]`")
	}
	id := args[0]
	u := store.IssueUpdate{}
	for _, kv := range args[1:] {
		if err := applyUpdateKV(&u, kv); err != nil {
			return err
		}
	}
	_, err := cc.store.UpdateIssue(cc.ctx, id, u)
	return err
}

func batchClose(cc *cmdCtx, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("close: usage `close <id> [reason...]`")
	}
	closed := beads.StatusClosed
	u := store.IssueUpdate{Status: &closed}
	if len(args) > 1 {
		reason := strings.Join(args[1:], " ")
		u.CloseReason = &reason
	}
	_, err := cc.store.UpdateIssue(cc.ctx, args[0], u)
	return err
}

func batchDep(cc *cmdCtx, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("dep: usage `dep add|rm <issue> <depends-on> [type]`")
	}
	switch args[0] {
	case "add":
		dt := beads.DepBlocks
		if len(args) >= 4 {
			parsed, err := beads.ParseDependencyType(args[3])
			if err != nil {
				return err
			}
			dt = parsed
		}
		return cc.store.AddDependency(cc.ctx, beads.Dependency{
			IssueID: args[1], DependsOnID: args[2], Type: dt,
			CreatedBy: assigneeFromEnv(),
		})
	case "rm", "remove":
		return cc.store.RemoveDependency(cc.ctx, args[1], args[2])
	}
	return fmt.Errorf("dep: unknown subop %q", args[0])
}

func batchLabel(cc *cmdCtx, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("label: usage `label add|rm <id> <label>`")
	}
	switch args[0] {
	case "add":
		return cc.store.AddLabel(cc.ctx, args[1], args[2])
	case "rm", "remove":
		return cc.store.RemoveLabel(cc.ctx, args[1], args[2])
	}
	return fmt.Errorf("label: unknown subop %q", args[0])
}

func batchComment(cc *cmdCtx, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("comment: usage `comment <id> <text>`")
	}
	c := &beads.Comment{
		IssueID: args[0], Author: assigneeFromEnv(),
		Text: strings.Join(args[1:], " "),
	}
	return cc.store.AddComment(cc.ctx, c)
}

// applyKV applies a key=value to a *beads.Issue (used by batch create).
func applyKV(i *beads.Issue, kv string) error {
	k, v, ok := strings.Cut(kv, "=")
	if !ok {
		return fmt.Errorf("expected key=value, got %q", kv)
	}
	switch k {
	case "title":
		i.Title = v
	case "desc", "description":
		i.Description = v
	case "design":
		i.Design = v
	case "accept", "acceptance":
		i.AcceptanceCriteria = v
	case "notes":
		i.Notes = v
	case "status":
		s, err := beads.ParseStatus(v)
		if err != nil {
			return err
		}
		i.Status = s
	case "priority":
		p, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("priority: %w", err)
		}
		i.Priority = p
	case "type":
		t, err := beads.ParseType(v)
		if err != nil {
			return err
		}
		i.Type = t
	case "assignee":
		i.Assignee = v
	case "owner":
		i.Owner = v
	case "ephemeral":
		i.Ephemeral = isTruthy(v)
	case "sender":
		i.Sender = v
	case "due":
		t, err := parseOptTime(v)
		if err != nil {
			return err
		}
		i.DueAt = t
	case "defer":
		t, err := parseOptTime(v)
		if err != nil {
			return err
		}
		i.DeferUntil = t
	default:
		return fmt.Errorf("unknown key %q", k)
	}
	return nil
}

// applyUpdateKV applies a key=value to a store.IssueUpdate (used by batch update).
func applyUpdateKV(u *store.IssueUpdate, kv string) error {
	k, v, ok := strings.Cut(kv, "=")
	if !ok {
		return fmt.Errorf("expected key=value, got %q", kv)
	}
	switch k {
	case "title":
		s := v
		u.Title = &s
	case "desc", "description":
		s := v
		u.Description = &s
	case "design":
		s := v
		u.Design = &s
	case "accept", "acceptance":
		s := v
		u.AcceptanceCriteria = &s
	case "notes":
		s := v
		u.Notes = &s
	case "status":
		st, err := beads.ParseStatus(v)
		if err != nil {
			return err
		}
		u.Status = &st
	case "priority":
		p, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		u.Priority = &p
	case "type":
		t, err := beads.ParseType(v)
		if err != nil {
			return err
		}
		u.Type = &t
	case "assignee":
		s := v
		u.Assignee = &s
	case "owner":
		s := v
		u.Owner = &s
	case "due":
		t, err := parseOptTime(v)
		if err != nil {
			return err
		}
		u.DueAt = t
	case "defer":
		t, err := parseOptTime(v)
		if err != nil {
			return err
		}
		u.DeferUntil = t
	case "ephemeral":
		b := isTruthy(v)
		u.Ephemeral = &b
	default:
		return fmt.Errorf("unknown key %q", k)
	}
	return nil
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "t":
		return true
	}
	return false
}

// tokenize splits a batch line on whitespace, honouring "double-quoted strings"
// with \" and \\ escapes.
func tokenize(line string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inQuote := false
	escape := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range line {
		if escape {
			cur.WriteRune(r)
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && (r == ' ' || r == '\t') {
			flush()
			continue
		}
		cur.WriteRune(r)
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return out, nil
}
