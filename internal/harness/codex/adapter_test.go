package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	data := `{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"codex-1","cwd":"/work/app"}}
{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Fix it"}]}}
{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"agent_message","message":"Done"}}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "codex-1" || len(result.Events) != 2 {
		t.Fatalf("result = %#v", result)
	}
}
