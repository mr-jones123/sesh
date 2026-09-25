package pi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"session","version":3,"id":"pi-1","timestamp":"2026-01-01T00:00:00Z","cwd":"/work/app"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"Fix it"}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"I will inspect it."},{"type":"toolCall","id":"call-1","name":"read","arguments":{"path":"main.go"}}]}}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "pi-1" || result.Workspace != "/work/app" {
		t.Fatalf("metadata = %#v", result)
	}
	if len(result.Events) != 3 {
		t.Fatalf("events = %d, want 3", len(result.Events))
	}
	if result.Events[2].Type != "tool_call" {
		t.Fatalf("event type = %q", result.Events[2].Type)
	}
}
