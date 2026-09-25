package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"user","uuid":"u1","sessionId":"claude-1","cwd":"/work/app","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"Fix it"}}
{"type":"assistant","uuid":"a1","sessionId":"claude-1","timestamp":"2026-01-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"go test ./..."}}]}}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "claude-1" || len(result.Events) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Events[1].Tool.Name != "Bash" {
		t.Fatalf("tool = %#v", result.Events[1].Tool)
	}
}
