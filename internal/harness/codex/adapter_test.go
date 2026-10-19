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

	s, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "codex-1" || s.Workspace != "/work/app" {
		t.Fatalf("session = %#v", s)
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

func TestSummarizeUsesTypedPromptAfterInjectedContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	data := `{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"codex-1","cwd":"/work/app"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions\nignore"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"files\n\n## My request for Codex:\nFix the bug"}]}}
{invalid after prompt
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "codex-1" || got.Workspace != "/work/app" || got.Prompt != "Fix the bug" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestSummarizeWithoutPromptReturnsMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	data := `{"type":"session_meta","payload":{"id":"codex-empty","cwd":"/work/empty"}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "codex-empty" || got.Workspace != "/work/empty" || got.Prompt != "" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestSessionIndexTitlesUseFilenameIDsAndRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	const id = "01a1213b-323c-7861-9339-e3abf9f712f6"
	path := filepath.Join(home, "sessions", "2026", "10", "09", "rollout-2026-10-09T00-00-00-"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"session_meta","payload":{"id":"different-payload-id","cwd":"/work/app"}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(home, "session_index.jsonl")
	if err := os.WriteFile(index, []byte(
		`{"id":"`+id+`","thread_name":"Initial name","updated_at":"2026-10-09T00:00:00Z"}`+"\n"+
			`{"id":"`+id+`","thread_name":"Saved rename","updated_at":"2026-10-09T00:01:00Z"}`+"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := New().Sessions("")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].ID != id || files[0].Title != "Saved rename" {
		t.Fatalf("files = %#v", files)
	}

	file, err := os.OpenFile(index, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"id":"` + id + `","thread_name":"Newest rename"}` + "\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	refreshed, err := New().Sessions("")
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed) != 1 || refreshed[0].Title != "Newest rename" {
		t.Fatalf("refreshed files = %#v", refreshed)
	}

	imported, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID != "different-payload-id" || imported.Title != "" {
		t.Fatalf("imported mismatched metadata = %#v", imported)
	}
	if err := os.WriteFile(path, []byte(`{"type":"session_meta","payload":{"id":"`+id+`","cwd":"/work/app"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	imported, err = New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Title != "Newest rename" {
		t.Fatalf("import title = %q", imported.Title)
	}
	summary, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Title != "Newest rename" {
		t.Fatalf("summary title = %q", summary.Title)
	}
}

func TestSessionIndexMissingFallsBackToInlineMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	const id = "01a1213b-323c-7861-9339-e3abf9f712f6"
	path := filepath.Join(home, "sessions", "2026", "10", "09", "rollout-2026-10-09T00-00-00-"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"/work/app","thread_name":"Inline title"}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := New().Sessions("")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Title != "" {
		t.Fatalf("files without index = %#v", files)
	}
	summary, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Title != "Inline title" || imported.Title != "Inline title" {
		t.Fatalf("summary title = %q, import title = %q", summary.Title, imported.Title)
	}
}
