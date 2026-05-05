package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// editForm is the on-disk YAML shape opened in $EDITOR. Read-only fields
// (id, timestamps) are dumped as a comment header; only the editable fields
// round-trip back into UpdateIssue.
type editForm struct {
	Title              string   `yaml:"title"`
	Description        string   `yaml:"description"`
	Design             string   `yaml:"design,omitempty"`
	AcceptanceCriteria string   `yaml:"acceptance_criteria,omitempty"`
	Notes              string   `yaml:"notes,omitempty"`
	Type               string   `yaml:"type"`
	Status             string   `yaml:"status"`
	Priority           int      `yaml:"priority"`
	Assignee           string   `yaml:"assignee,omitempty"`
	Owner              string   `yaml:"owner,omitempty"`
	Labels             []string `yaml:"labels,omitempty"`
	Due                string   `yaml:"due,omitempty"`
	Defer              string   `yaml:"defer,omitempty"`
	Ephemeral          bool     `yaml:"ephemeral,omitempty"`
}

func newEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit <id>",
		Short: "Open a bead in $EDITOR as YAML and apply the changes on save",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			id := args[0]
			i, err := cc.store.GetIssue(cc.ctx, id)
			if err != nil {
				return err
			}
			labels, err := cc.store.ListLabels(cc.ctx, id)
			if err != nil {
				return err
			}
			form := editForm{
				Title:              i.Title,
				Description:        i.Description,
				Design:             i.Design,
				AcceptanceCriteria: i.AcceptanceCriteria,
				Notes:              i.Notes,
				Type:               string(i.Type),
				Status:             string(i.Status),
				Priority:           i.Priority,
				Assignee:           i.Assignee,
				Owner:              i.Owner,
				Labels:             labels,
				Ephemeral:          i.Ephemeral,
			}
			if i.DueAt != nil {
				form.Due = i.DueAt.Format(time.RFC3339)
			}
			if i.DeferUntil != nil {
				form.Defer = i.DeferUntil.Format(time.RFC3339)
			}
			body, err := yaml.Marshal(form)
			if err != nil {
				return err
			}
			header := fmt.Sprintf(
				"# bd edit %s\n# created: %s   updated: %s\n# read-only fields are not in this form.\n\n",
				i.ID, i.CreatedAt.Format(time.RFC3339), i.UpdatedAt.Format(time.RFC3339),
			)
			edited, err := openEditor([]byte(header + string(body)))
			if err != nil {
				return err
			}
			var newForm editForm
			if err := yaml.Unmarshal(edited, &newForm); err != nil {
				return fmt.Errorf("parse edited yaml: %w", err)
			}
			return applyEdit(cc, id, &form, &newForm)
		},
	}
}

func applyEdit(cc *cmdCtx, id string, before, after *editForm) error {
	u := store.IssueUpdate{}
	if before.Title != after.Title {
		u.Title = &after.Title
	}
	if before.Description != after.Description {
		u.Description = &after.Description
	}
	if before.Design != after.Design {
		u.Design = &after.Design
	}
	if before.AcceptanceCriteria != after.AcceptanceCriteria {
		u.AcceptanceCriteria = &after.AcceptanceCriteria
	}
	if before.Notes != after.Notes {
		u.Notes = &after.Notes
	}
	if before.Priority != after.Priority {
		p := after.Priority
		u.Priority = &p
	}
	if before.Assignee != after.Assignee {
		u.Assignee = &after.Assignee
	}
	if before.Owner != after.Owner {
		u.Owner = &after.Owner
	}
	if before.Type != after.Type {
		t, err := beads.ParseType(after.Type)
		if err != nil {
			return err
		}
		u.Type = &t
	}
	if before.Status != after.Status {
		s, err := beads.ParseStatus(after.Status)
		if err != nil {
			return err
		}
		u.Status = &s
	}
	if before.Ephemeral != after.Ephemeral {
		b := after.Ephemeral
		u.Ephemeral = &b
	}
	if before.Due != after.Due {
		t, err := parseOptTime(after.Due)
		if err != nil {
			return err
		}
		u.DueAt = t
	}
	if before.Defer != after.Defer {
		t, err := parseOptTime(after.Defer)
		if err != nil {
			return err
		}
		u.DeferUntil = t
	}
	if _, err := cc.store.UpdateIssue(cc.ctx, id, u); err != nil {
		return err
	}
	if !sameStrings(before.Labels, after.Labels) {
		// resync labels: remove ones that left, add ones that joined
		oldSet := toSet(before.Labels)
		newSet := toSet(after.Labels)
		for l := range oldSet {
			if !newSet[l] {
				if err := cc.store.RemoveLabel(cc.ctx, id, l); err != nil {
					return err
				}
			}
		}
		for l := range newSet {
			if !oldSet[l] {
				if err := cc.store.AddLabel(cc.ctx, id, l); err != nil {
					return err
				}
			}
		}
	}
	fmt.Printf("updated %s\n", id)
	return nil
}

// openEditor writes initial content to a temp file, runs $EDITOR (or vi), and
// returns the resulting bytes. EDITOR=cat is supported for non-interactive
// testing.
func openEditor(initial []byte) ([]byte, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	tmp, err := os.CreateTemp("", "bd-edit-*.yaml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(initial); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	cmd := exec.Command(splitFirst(editor)[0], append(splitFirst(editor)[1:], tmp.Name())...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("editor %q: %w", editor, err)
	}
	return os.ReadFile(filepath.Clean(tmp.Name()))
}

// splitFirst splits an EDITOR string like `code -w` or `vim` into argv.
func splitFirst(s string) []string {
	out := []string{}
	for _, p := range strings.Fields(s) {
		out = append(out, p)
	}
	if len(out) == 0 {
		return []string{"vi"}
	}
	return out
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	am := toSet(a)
	bm := toSet(b)
	for k := range am {
		if !bm[k] {
			return false
		}
	}
	return true
}
