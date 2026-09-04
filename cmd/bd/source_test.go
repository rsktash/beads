package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runSource(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	cmd := newSourceCmd()
	bufOut := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	cmd.SetOut(bufOut)
	cmd.SetErr(bufErr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return bufOut.String(), bufErr.String(), err
}

// fixtureSlug mirrors the project-directory naming independently of the code
// under test: every '/' and '.' of the working directory becomes '-'.
func fixtureSlug(t *testing.T, dir string) string {
	t.Helper()
	out := make([]rune, 0, len(dir))
	for _, r := range dir {
		if r == '/' || r == '.' {
			r = '-'
		}
		out = append(out, r)
	}
	return string(out)
}

// writeTranscript plants a transcript at $HOME/.claude/projects/<slug>/<sid>.jsonl
// for the current working directory and returns its path.
func writeTranscript(t *testing.T, home, sessionID string, lines []string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := filepath.Join(home, ".claude", "projects", fixtureSlug(t, cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path
}

func jsonLine(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// ownerRecord is a typed owner message: content is a plain string.
func ownerRecord(t *testing.T, uuid, text string) string {
	return jsonLine(t, map[string]any{
		"type": "user", "uuid": uuid, "timestamp": "2026-09-02T22:14:03.123Z",
		"message": map[string]any{"role": "user", "content": text},
	})
}

// toolUseRecord is the assistant turn that carries the tool call.
func toolUseRecord(t *testing.T, uuid, msgID, toolUseID string) string {
	return jsonLine(t, map[string]any{
		"type": "assistant", "uuid": uuid, "timestamp": "2026-09-02T22:14:09.456Z",
		"message": map[string]any{"role": "assistant", "id": msgID, "content": []any{
			map[string]any{"type": "tool_use", "id": toolUseID, "name": "Bash", "input": map[string]any{"command": "bd ruling add"}},
		}},
	})
}

// toolResultRecord is the user-typed record the harness writes back.
func toolResultRecord(t *testing.T, uuid, toolUseID, content string) string {
	return jsonLine(t, map[string]any{
		"type": "user", "uuid": uuid, "timestamp": "2026-09-02T22:14:10.001Z",
		"message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": content},
		}},
	})
}

func assistantTextRecord(t *testing.T, uuid, text string) string {
	return jsonLine(t, map[string]any{
		"type": "assistant", "uuid": uuid, "timestamp": "2026-09-02T22:14:11.001Z",
		"message": map[string]any{"role": "assistant", "id": "msg_after", "content": []any{
			map[string]any{"type": "text", "text": text},
		}},
	})
}

// annotatedRuling makes a ruling carrying a pointer and returns its id.
func annotatedRuling(t *testing.T, title, sessionID, msgID, toolUseID string) string {
	t.Helper()
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, title)
	rID := mkRulingStatement(t, st, issue.ID, "a ruling with provenance")
	_ = st.Close()
	if _, _, err := runAnnotate(t, []string{rID, "--session", sessionID, "--msg", msgID, "--tool", toolUseID}); err != nil {
		t.Fatalf("annotate: %v", err)
	}
	return rID
}

func TestSource_PrintsPrecedingOwnerMessage(t *testing.T) {
	const sid = "2f4f7030-4c46-4f20-b27c-c0d7c48f1d3c"
	const toolUse = "toolu_015HjFDCcRMbXHtkpyqdbt45"
	rID := annotatedRuling(t, "source prints owner message", sid, "u-toolcall", toolUse)

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTranscript(t, home, sid, []string{
		ownerRecord(t, "u-1", "EARLIER owner message, two turns back"),
		ownerRecord(t, "u-2", "TARGET the sentence right before the call"),
		toolUseRecord(t, "u-toolcall", "msg_01call", toolUse),
		toolResultRecord(t, "u-4", toolUse, "AFTERONE the tool result body"),
		assistantTextRecord(t, "u-5", "AFTERTWO the assistant wrap-up"),
	})

	out, _, err := runSource(t, []string{rID})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if !strings.Contains(out, "TARGET the sentence right before the call") {
		t.Fatalf("expected the owner message before the tool call, got:\n%s", out)
	}
	for _, unwanted := range []string{"AFTERONE", "AFTERTWO", "EARLIER"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("output must not carry %s, got:\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "source: session "+sid) {
		t.Fatalf("expected the session header, got:\n%s", out)
	}
	if !strings.Contains(out, "grep: rg -n '2f4f7030'") {
		t.Fatalf("expected the grep line, got:\n%s", out)
	}
}

func TestSource_MissingTranscript(t *testing.T) {
	const sid = "aaaaaaaa-1111-2222-3333-444444444444"
	rID := annotatedRuling(t, "source missing transcript", sid, "u-1", "toolu_missing")

	t.Setenv("HOME", t.TempDir())

	out, _, err := runSource(t, []string{rID})
	if err != nil {
		t.Fatalf("a missing transcript must exit 0, got: %v", err)
	}
	if !strings.Contains(out, "transcript not on this machine") {
		t.Fatalf("expected the not-on-this-machine line, got:\n%s", out)
	}
}

func TestSource_NoPointer(t *testing.T) {
	_, st := newTempRulingStore(t, "bd")
	issue := mkIssueForRuling(t, st, "source with no pointer")
	rID := mkRulingStatement(t, st, issue.ID, "never annotated")
	_ = st.Close()

	out, _, err := runSource(t, []string{rID})
	if err != nil {
		t.Fatalf("an un-annotated statement must exit 0, got: %v", err)
	}
	if strings.TrimSpace(out) != "source: none recorded" {
		t.Fatalf("expected %q, got %q", "source: none recorded", out)
	}
}

func TestSource_TruncatesAt400(t *testing.T) {
	const sid = "bbbbbbbb-1111-2222-3333-444444444444"
	const toolUse = "toolu_long"
	rID := annotatedRuling(t, "source truncates", sid, "u-toolcall", toolUse)

	long := strings.Repeat("x", 900)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTranscript(t, home, sid, []string{
		ownerRecord(t, "u-1", long),
		toolUseRecord(t, "u-toolcall", "msg_01call", toolUse),
	})

	out, _, err := runSource(t, []string{rID})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	quote := ownerQuote(t, out)
	if n := len([]rune(quote)); n != 401 {
		t.Fatalf("expected a 401-rune quote, got %d: %q", n, quote)
	}
	if !strings.HasSuffix(quote, "…") {
		t.Fatalf("expected the quote to end with an ellipsis, got %q", quote[len(quote)-8:])
	}
}

// ownerQuote pulls the quoted sentence out of the `owner said:` line.
func ownerQuote(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(line, "owner said: ")
		if !ok {
			continue
		}
		return strings.TrimSuffix(strings.TrimPrefix(rest, `"`), `"`)
	}
	t.Fatalf("no owner said line in:\n%s", out)
	return ""
}

// attachmentRecord is a harness record of an unfamiliar type, carrying no
// message at all — the kind the ring must never mistake for owner speech.
func attachmentRecord(t *testing.T, uuid string) string {
	return jsonLine(t, map[string]any{
		"type": "attachment", "uuid": uuid, "timestamp": "2026-09-02T22:14:05.000Z",
	})
}

func TestSource_SurvivesHarnessRecordsBetweenOwnerAndCall(t *testing.T) {
	const sid = "dddddddd-1111-2222-3333-444444444444"
	const toolUse = "toolu_survives"
	rID := annotatedRuling(t, "source survives harness records", sid, "u-toolcall", toolUse)

	lines := []string{ownerRecord(t, "u-owner", "TARGET the owner sentence before the harness noise")}
	for i := 0; i < 16; i++ {
		uuid := "u-harness-" + string(rune('a'+i))
		switch i % 4 {
		case 0, 1:
			lines = append(lines, assistantTextRecord(t, uuid, "assistant chatter"))
		case 2:
			lines = append(lines, toolResultRecord(t, uuid, "toolu_unrelated", "some tool result body"))
		case 3:
			lines = append(lines, attachmentRecord(t, uuid))
		}
	}
	lines = append(lines,
		toolUseRecord(t, "u-toolcall", "msg_01call", toolUse),
		toolResultRecord(t, "u-result", toolUse, "the tool result body"),
	)

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTranscript(t, home, sid, lines)

	out, _, err := runSource(t, []string{rID})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if strings.Contains(out, "no owner message") {
		t.Fatalf("expected the owner sentence to survive 16 harness records, got:\n%s", out)
	}
	if !strings.Contains(out, "TARGET the owner sentence before the harness noise") {
		t.Fatalf("expected the owner sentence, got:\n%s", out)
	}
}

func TestSource_NeverPrintsWholeFile(t *testing.T) {
	const sid = "cccccccc-1111-2222-3333-444444444444"
	const toolUse = "toolu_bulk"
	rID := annotatedRuling(t, "source stays bounded", sid, "u-toolcall", toolUse)

	lines := make([]string, 0, 501)
	for i := 0; i < 499; i++ {
		lines = append(lines, ownerRecord(t, "u-fill", strings.Repeat("filler words that would flood a context ", 20)))
	}
	lines = append(lines, ownerRecord(t, "u-last", "the short sentence before the call"))
	lines = append(lines, toolUseRecord(t, "u-toolcall", "msg_01call", toolUse))

	home := t.TempDir()
	t.Setenv("HOME", home)
	path := writeTranscript(t, home, sid, lines)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() < 100_000 {
		t.Fatalf("fixture should be far larger than the output bound, got %d bytes", info.Size())
	}

	out, _, err := runSource(t, []string{rID})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if len(out) >= 1200 {
		t.Fatalf("stdout must stay under 1200 bytes, got %d:\n%s", len(out), out[:200])
	}
	if !strings.Contains(out, "the short sentence before the call") {
		t.Fatalf("expected the last owner sentence, got:\n%s", out)
	}
}
