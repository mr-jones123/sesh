package pi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mr-jones123/sesh/internal/session"
)

func importFixture(t *testing.T, data string) session.Session {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestImportNormalizesMessagesToolsAndTree(t *testing.T) {
	s := importFixture(t, `{"type":"session","version":3,"id":"pi-1","timestamp":"2026-01-01T00:00:00Z","cwd":"/work/app"}
{"type":"model_change","id":"mc","parentId":null,"timestamp":"2026-01-01T00:00:00Z","provider":"openai","modelId":"gpt"}
{"type":"message","id":"m1","parentId":"mc","timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"Fix it"}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","model":"gpt","content":[{"type":"thinking","thinking":"plan it"},{"type":"toolCall","id":"c1","name":"bash","arguments":{"command":"ls -la"}}]}}
{"type":"message","id":"r1","parentId":"m2","timestamp":"2026-01-01T00:00:03Z","message":{"role":"toolResult","toolCallId":"c1","toolName":"bash","content":[{"type":"text","text":"total 0"}],"isError":false}}
`)
	if s.ID != "pi-1" || s.Workspace != "/work/app" {
		t.Fatalf("metadata = %#v", s)
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

func TestSummarizeSkipsInjectedPromptAndStopsAfterFirstRealPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"session","version":3,"id":"pi-1","timestamp":"2026-01-01T00:00:00Z","cwd":"/work/app"}
{"type":"message","id":"m1","message":{"role":"user","content":"[Imported by sesh: context]"}}
{"type":"message","id":"m2","message":{"role":"user","content":[{"type":"text","text":"Fix\n it"}]}}
{invalid after prompt
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "pi-1" || got.Workspace != "/work/app" || got.Prompt != "Fix it" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestSummarizeWithoutPromptReturnsMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"session","version":3,"id":"pi-empty","cwd":"/work/empty"}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "pi-empty" || got.Workspace != "/work/empty" || got.Prompt != "" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestNativeTitlePrecedenceAndAppendedRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"session","version":3,"id":"pi-title","cwd":"/work/app","name":"Header name"}
{"type":"session_info","id":"i1","name":"Generated info name"}
{"type":"message","id":"m1","message":{"role":"user","content":"Original prompt"}}
{malformed unrelated tail
{"type":"title","id":"t1","title":"Manual rename","source":"user"}
{"type":"title","id":"t2","title":"Later automatic name","source":"auto"}
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

func TestOMPTitleSlotPrecedesStaleHeader(t *testing.T) {
	prefix := `{"type":"title","v":1,"title":"OMP slot name","source":"auto","updatedAt":"2026-10-19T00:00:00Z","pad":"`
	suffix := `"}` + "\n"
	slot := prefix + strings.Repeat(" ", 256-len(prefix)-len(suffix)) + suffix
	if len(slot) != 256 {
		t.Fatalf("slot length = %d", len(slot))
	}
	data := slot + `{"type":"session","version":3,"id":"omp-1","cwd":"/work/app","title":"Stale header name"}
{"type":"message","id":"m1","message":{"role":"user","content":"Prompt"}}
`
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Title != "OMP slot name" || imported.Title != "OMP slot name" || imported.ID != "omp-1" {
		t.Fatalf("summary = %#v, imported = %#v", summary, imported)
	}
}
