package main

// The per-session expand dedupe (feature 16, Behaviours 5-9): a record
// already opened with --expand in this session renders as its id alone,
// because its full text is still in the reader's context. State is one
// file under ${TMPDIR:-/tmp}/bd-expand — one record id per line, appended —
// modelled on ~/.claude/hooks/read-guard's session file. Every failure of
// the file is ignored: the dedupe never suppresses a record it is not sure
// about. bd never cleans the directory up; it lives in TMPDIR and the
// operating system reaps it.

import (
	"os"
	"path/filepath"
	"strings"
)

// expandStateDirName is the state directory under ${TMPDIR:-/tmp}.
const expandStateDirName = "bd-expand"

// expandStateDir is ${TMPDIR:-/tmp}/bd-expand.
func expandStateDir() string {
	dir := os.Getenv("TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, expandStateDirName)
}

// sanitizeExpandSession maps a session id onto a file-name-safe form: every
// character outside [A-Za-z0-9_.-] becomes _, exactly as read-guard does.
func sanitizeExpandSession(session string) string {
	var b strings.Builder
	b.Grow(len(session))
	for i := 0; i < len(session); i++ {
		c := session[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '_' || c == '.' || c == '-' {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// expandStatePath is the session's state file.
func expandStatePath(session string) string {
	return filepath.Join(expandStateDir(), sanitizeExpandSession(session))
}

// expandTracker is one render's view of the session's expand state: the ids
// opened with --expand earlier in this session.
type expandTracker struct {
	session string
	seen    map[string]bool
}

// newExpandTracker resolves the session id — no flag, the two session
// environment variables in order — and reads the state file. With no
// session id resolvable the dedupe is off; so is it on any read failure.
func newExpandTracker() *expandTracker {
	session := resolveSessionID("")
	if session == "" {
		return &expandTracker{}
	}
	return &expandTracker{session: session, seen: readExpandState(session)}
}

// deduped reports whether the record was already expanded in this session.
// A nil map answers false: unsure means render in full.
func (t *expandTracker) deduped(id string) bool {
	return t != nil && t.seen[id]
}

// record appends one opened record id to the state file. Every failure is
// ignored: a record that cannot be remembered still renders in full now,
// and simply renders in full again next time.
func (t *expandTracker) record(id string) {
	if t == nil || t.session == "" || id == "" {
		return
	}
	appendExpandID(t.session, id)
}

// readExpandState reads the session's recorded ids; any error returns nil.
func readExpandState(session string) map[string]bool {
	data, err := os.ReadFile(expandStatePath(session))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			seen[line] = true
		}
	}
	return seen
}

// appendExpandID appends one id, creating the directory and file as needed.
func appendExpandID(session, id string) {
	if err := os.MkdirAll(expandStateDir(), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(expandStatePath(session), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(id + "\n")
}
