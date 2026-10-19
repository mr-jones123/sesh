package omp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mr-jones123/sesh/internal/session"
)

var testTime = time.Date(2026, 10, 19, 1, 2, 3, 456000000, time.UTC)

func writeOMPFixture(t *testing.T, title, body string) string {
	t.Helper()
	slot, err := encodeTitleSlot(title, testTime)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, append(slot, body...), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImportAndSummarizeUseAuthoritativeTitleSlot(t *testing.T) {
	path := writeOMPFixture(t, "Authoritative name", `{"type":"session","version":3,"id":"omp-1","timestamp":"2026-10-19T01:02:03.456Z","cwd":"/work/app","title":"stale header"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-10-19T01:02:04.000Z","message":{"role":"user","content":"Fix the parser"}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-10-19T01:02:05.000Z","message":{"role":"assistant","model":"gpt","content":[{"type":"toolCall","id":"c1","name":"bash","arguments":{"command":"go test ./..."}}]}}
{"type":"message","id":"r1","parentId":"m2","timestamp":"2026-10-19T01:02:06.000Z","message":{"role":"toolResult","toolCallId":"c1","toolName":"bash","content":[{"type":"text","text":"ok"}],"isError":false}}
`)

	got, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Harness != "omp" || got.ID != "omp-1" || got.Workspace != "/work/app" || got.Title != "Authoritative name" {
		t.Fatalf("import metadata = %#v", got)
	}
	if len(got.Events) != 3 || got.Events[0].Type != session.EventMessage || got.Events[1].Type != session.EventToolCall || got.Events[2].Type != session.EventToolResult {
		t.Fatalf("import events = %#v", got.Events)
	}
	if got.Events[2].Result == nil || got.Events[2].Result.Output != "ok" {
		t.Fatalf("tool result = %#v", got.Events[2])
	}

	summary, err := New().Summarize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ID != "omp-1" || summary.Workspace != "/work/app" || summary.Title != "Authoritative name" || summary.Prompt != "Fix the parser" {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestDetectUsesOMPOverrideWithoutClaimingPi(t *testing.T) {
	root := t.TempDir()
	t.Setenv(agentDirOverride, root)
	ompPath := filepath.Join(root, "sessions", "-work", "one.jsonl")
	if !New().Detect(ompPath) {
		t.Fatalf("Detect(%q) = false", ompPath)
	}
	if New().Detect(filepath.Join(t.TempDir(), ".pi", "agent", "sessions", "one.jsonl")) {
		t.Fatal("OMP adapter claimed a Pi transcript")
	}
}
