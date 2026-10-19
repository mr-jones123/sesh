package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mr-jones123/sesh/internal/session"
)

func TestImportLinksToolResultsAndSkipsNoise(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"claude-1","cwd":"/work/app","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"Fix it"}}
{"type":"attachment","uuid":"at1","parentUuid":"u1","sessionId":"claude-1","timestamp":"2026-01-01T00:00:00Z","attachment":{}}
{"type":"user","uuid":"meta1","parentUuid":"at1","isMeta":true,"sessionId":"claude-1","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"<injected context>"}}
{"type":"assistant","uuid":"a1","parentUuid":"meta1","sessionId":"claude-1","timestamp":"2026-01-01T00:00:01Z","message":{"role":"assistant","model":"claude-x","content":[{"type":"thinking","thinking":"run tests","signature":"sig"},{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"go test ./..."}}]}}
{"type":"user","uuid":"r1","parentUuid":"a1","sessionId":"claude-1","timestamp":"2026-01-01T00:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":[{"type":"text","text":"ok"}],"is_error":true}]}}
{"type":"last-prompt","lastPrompt":"Fix it","sessionId":"claude-1"}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "claude-1" || s.Workspace != "/work/app" {
		t.Fatalf("session = %#v", s)
	}
	if len(s.Events) != 4 {
		t.Fatalf("events = %#v, want 4", s.Events)
	}

	user, thinking, call, result := s.Events[0], s.Events[1], s.Events[2], s.Events[3]
	if user.Text != "Fix it" || user.Role != session.RoleUser {
		t.Fatalf("user = %#v", user)
	}
	// The attachment and meta record are skipped, so the assistant hangs off the user.
	if thinking.Type != session.EventReasoning || thinking.Text != "run tests" || thinking.ParentID != user.ID || thinking.Model != "claude-x" {
		t.Fatalf("thinking = %#v", thinking)
	}
	if call.Call.Name != "Bash" || string(call.Call.Args) != `{"command":"go test ./..."}` {
		t.Fatalf("call = %#v", call.Call)
	}
	if result.Type != session.EventToolResult || result.ParentID != call.ID ||
		*result.Result != (session.ToolResult{CallID: "tool-1", Name: "Bash", Output: "ok", IsError: true}) {
		t.Fatalf("result = %#v", result.Result)
	}
}

func TestImportMarksCompactSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"user","uuid":"c1","sessionId":"claude-1","timestamp":"2026-01-01T00:00:00Z","isCompactSummary":true,"message":{"role":"user","content":"Summary of earlier work"}}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Events; len(got) != 1 || got[0].Type != session.EventSummary || got[0].Text != "Summary of earlier work" {
		t.Fatalf("events = %#v", got)
	}
}

func TestImportKeepsSubagentTranscriptThread(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-a1.jsonl")
	data := `{"type":"user","uuid":"s1","isSidechain":true,"sessionId":"claude-1","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"Review the diff"}}
{"type":"assistant","uuid":"s2","parentUuid":"s1","isSidechain":true,"sessionId":"claude-1","timestamp":"2026-01-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"Looks fine"}]}}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Events; len(got) != 2 || got[1].ParentID != got[0].ID {
		t.Fatalf("events = %#v", got)
	}
}

func TestImportChainsParallelToolResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	// Claude parents each result on its own tool_use record.
	data := `{"type":"assistant","uuid":"c1","sessionId":"claude-1","timestamp":"2026-01-01T00:00:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"a"}}]}}
{"type":"assistant","uuid":"c2","parentUuid":"c1","sessionId":"claude-1","timestamp":"2026-01-01T00:00:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"b"}}]}}
{"type":"user","uuid":"r1","parentUuid":"c1","sessionId":"claude-1","timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"A"}]}}
{"type":"user","uuid":"r2","parentUuid":"c2","sessionId":"claude-1","timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"B"}]}}
{"type":"assistant","uuid":"a3","parentUuid":"r2","sessionId":"claude-1","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"both done"}]}}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	// One linear chain, so the last event's ancestry reaches both results.
	events := s.Events
	for i := 1; i < len(events); i++ {
		if events[i].ParentID != events[i-1].ID {
			t.Fatalf("event %d (%s) parent = %q, want %q", i, events[i].Type, events[i].ParentID, events[i-1].ID)
		}
	}
}

func TestSummarizeSkipsInjectedPromptAndStopsAfterFirstRealPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"user","uuid":"m1","sessionId":"claude-1","cwd":"/work/app","isMeta":true,"message":{"role":"user","content":"<system-reminder>injected</system-reminder>"}}
{"type":"user","uuid":"s1","sessionId":"claude-1","cwd":"/work/app","isCompactSummary":true,"message":{"role":"user","content":"Earlier work"}}
{"type":"user","uuid":"u1","sessionId":"claude-1","cwd":"/work/app","message":{"role":"user","content":"  Fix\n   the tests  "}}
{invalid after prompt
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "claude-1" || got.Workspace != "/work/app" || got.Prompt != "Fix the tests" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestSummarizeWithoutPromptReturnsMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"attachment","sessionId":"claude-empty","cwd":"/work/empty"}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "claude-empty" || got.Workspace != "/work/empty" || got.Prompt != "" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestNativeTitlePrecedenceAndAppendedRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"ai-title","aiTitle":"Generated first","sessionId":"claude-title"}
{"type":"user","uuid":"u1","sessionId":"claude-title","cwd":"/work/app","message":{"role":"user","content":"Original prompt"}}
{malformed unrelated tail
{"type":"custom-title","customTitle":"Manual rename","sessionId":"claude-title"}
{"type":"ai-title","aiTitle":"Later generated name","sessionId":"claude-title"}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Manual rename" || got.Prompt != "Original prompt" {
		t.Fatalf("summary = %#v", got)
	}

	valid := strings.Replace(data, "{malformed unrelated tail\n", "", 1)
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	imported, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Title != "Manual rename" {
		t.Fatalf("import title = %q", imported.Title)
	}
}

func TestSummarizePreservesErrorsBeforePrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"ai-title","aiTitle":"Name","sessionId":"claude-title"}
{malformed before prompt
{"type":"user","uuid":"u1","sessionId":"claude-title","cwd":"/work/app","message":{"role":"user","content":"Prompt"}}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Summarize(context.Background(), path); err == nil {
		t.Fatal("Summarize ignored malformed data before the first prompt")
	}
}
