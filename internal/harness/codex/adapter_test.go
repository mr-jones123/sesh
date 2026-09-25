package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mr-jones123/sesh/internal/session"
)

func TestImportNormalizesRollout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	data := `{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"codex-1","cwd":"/work/app"}}
{"timestamp":"2026-01-01T00:00:00Z","type":"turn_context","payload":{"model":"gpt-x"}}
{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"<permissions>"}]}}
{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Fix it"}]}}
{"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"Fix it"}}
{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"Check tests"}],"encrypted_content":"opaque"}}
{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"go test ./...\"}","call_id":"call-1"}}
{"timestamp":"2026-01-01T00:00:03Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":[{"type":"input_text","text":"ok"}]}}
{"timestamp":"2026-01-01T00:00:04Z","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch","call_id":"call-2"}}
{"timestamp":"2026-01-01T00:00:05Z","type":"response_item","payload":{"type":"function_call","name":"view_image","arguments":{"path":"a.png"},"call_id":"call-3"}}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	bundle, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	s := bundle.Session
	if s.ID != "codex-1" || s.Workspace != "/work/app" || len(bundle.RawRecords) != 10 {
		t.Fatalf("session = %#v, raw = %d", s, len(bundle.RawRecords))
	}
	// event_msg duplicates the user message and is skipped.
	if len(s.Events) != 7 {
		t.Fatalf("events = %#v, want 7", s.Events)
	}

	developer, user, reasoning, call, result, patch, image := s.Events[0], s.Events[1], s.Events[2], s.Events[3], s.Events[4], s.Events[5], s.Events[6]
	if developer.Role != session.RoleSystem || user.Role != session.RoleUser || user.ParentID != developer.ID {
		t.Fatalf("developer = %#v, user = %#v", developer, user)
	}
	if reasoning.Type != session.EventReasoning || reasoning.Text != "Check tests" || reasoning.Model != "gpt-x" {
		t.Fatalf("reasoning = %#v", reasoning)
	}
	if call.Call.Name != "exec_command" || string(call.Call.Args) != `{"cmd":"go test ./..."}` {
		t.Fatalf("call = %#v args=%s", call.Call, call.Call.Args)
	}
	if *result.Result != (session.ToolResult{CallID: "call-1", Name: "exec_command", Output: "ok"}) || result.ParentID != call.ID {
		t.Fatalf("result = %#v", result.Result)
	}
	// Free-text custom tool input is stored as a JSON string, keeping Args valid JSON.
	if string(patch.Call.Args) != `"*** Begin Patch"` {
		t.Fatalf("patch args = %s", patch.Call.Args)
	}
	// Some rollouts send arguments as an object rather than JSON text.
	if string(image.Call.Args) != `{"path":"a.png"}` {
		t.Fatalf("object args = %s", image.Call.Args)
	}
}
