package claude

import (
	"context"
	"os"
	"path/filepath"
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

	bundle, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	s := bundle.Session
	if s.ID != "claude-1" || s.Workspace != "/work/app" || len(bundle.RawRecords) != 6 {
		t.Fatalf("session = %#v, raw = %d", s, len(bundle.RawRecords))
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
	bundle, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := bundle.Session.Events; len(got) != 1 || got[0].Type != session.EventSummary || got[0].Text != "Summary of earlier work" {
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
	bundle, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := bundle.Session.Events; len(got) != 2 || got[1].ParentID != got[0].ID {
		t.Fatalf("events = %#v", got)
	}
}
