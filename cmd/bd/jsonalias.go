package main

import "github.com/rsktash/beads"

// beadsIssueAlias is a type alias used so the embedded `beads.Issue` in the
// JSON output of `bd show --json` flattens its fields onto the outer object
// (instead of getting wrapped under "issue"). Skills consume the result as
// `.[0].title` etc.
type beadsIssueAlias beads.Issue
