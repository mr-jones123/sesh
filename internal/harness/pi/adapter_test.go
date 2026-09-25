package pi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mr-jones123/sesh/internal/session"
)

func importFixture(t *testing.T, data string) session.Bundle {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestImportNormalizesMessagesToolsAndTree(t *testing.T) {
	bundle := importFixture(t, `{"type":"session","version":3,"id":"pi-1","timestamp":"2026-01-01T00:00:00Z","cwd":"/work/app"}
{"type":"model_change","id":"mc","parentId":null,"timestamp":"2026-01-01T00:00:00Z","provider":"openai","modelId":"gpt"}
{"type":"message","id":"m1","parentId":"mc","timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"Fix it"}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","model":"gpt","content":[{"type":"thinking","thinking":"plan it"},{"type":"toolCall","id":"c1","name":"bash","arguments":{"command":"ls -la"}}]}}
{"type":"message","id":"r1","parentId":"m2","timestamp":"2026-01-01T00:00:03Z","message":{"role":"toolResult","toolCallId":"c1","toolName":"bash","content":[{"type":"text","text":"total 0"}],"isError":false}}
`)
	s := bundle.Session
	if s.ID != "pi-1" || s.Workspace != "/work/app" {
		t.Fatalf("metadata = %#v", s)
	}
	if len(bundle.RawRecords) != 5 {
		t.Fatalf("raw records = %d, want 5", len(bundle.RawRecords))
	}
	if len(s.Events) != 4 {
		t.Fatalf("events = %#v, want 4", s.Events)
	}

	user, thinking, call, result := s.Events[0], s.Events[1], s.Events[2], s.Events[3]
	if user.Type != session.EventMessage || user.Text != "Fix it" || user.ParentID != "" {
		t.Fatalf("user = %#v", user)
	}
	if thinking.Type != session.EventReasoning || thinking.Text != "plan it" || thinking.ParentID != user.ID {
		t.Fatalf("thinking = %#v", thinking)
	}
	if call.Type != session.EventToolCall || string(call.Call.Args) != `{"command":"ls -la"}` || call.ParentID != thinking.ID {
		t.Fatalf("call = %#v args=%s", call, call.Call.Args)
	}
	if result.Type != session.EventToolResult || result.Role != session.RoleTool ||
		result.Result.CallID != "c1" || result.Result.Output != "total 0" || result.ParentID != call.ID {
		t.Fatalf("result = %#v", result)
	}
}

func TestImportRequiresSessionHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"message","id":"m1"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Import(context.Background(), path); err == nil {
		t.Fatal("Import accepted a transcript without a session header")
	}
}
