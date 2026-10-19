package omp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/session"
)

func TestEncodeTitleSlotIsExactly256BytesAndUTF8Safe(t *testing.T) {
	original := strings.Repeat("編<&\"\n", 40) + "🚀"
	encoded, err := encodeTitleSlot(original, testTime)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != titleSlotSize || encoded[titleSlotSize-1] != '\n' {
		t.Fatalf("slot length/terminator = %d/%q", len(encoded), encoded[len(encoded)-1])
	}
	if !json.Valid(encoded[:titleSlotSize-1]) {
		t.Fatalf("slot is not valid JSON: %q", encoded)
	}
	var slot titleSlot
	if err := json.Unmarshal(encoded[:titleSlotSize-1], &slot); err != nil {
		t.Fatal(err)
	}
	if slot.Type != "title" || slot.Version != 1 || slot.Source != "auto" || slot.UpdatedAt != "2026-10-19T01:02:03.456Z" {
		t.Fatalf("slot metadata = %#v", slot)
	}
	if !utf8.ValidString(slot.Title) || !strings.HasPrefix(original, slot.Title) || len(slot.Title) >= len(original) {
		t.Fatalf("truncated title = %q", slot.Title)
	}
	if strings.Trim(slot.Pad, " ") != "" {
		t.Fatalf("pad contains non-space bytes: %q", slot.Pad)
	}
}

func TestExportRoundTripsNativeTitleConversationAndToolsWithoutTouchingSource(t *testing.T) {
	sourcePath := writeOMPFixture(t, "Unicode 🚀 title", `{"type":"session","version":3,"id":"original","timestamp":"2026-10-19T01:02:03.456Z","cwd":"/work/app","title":"old"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-10-19T01:02:04.000Z","message":{"role":"user","content":"Fix it"}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-10-19T01:02:05.000Z","message":{"role":"assistant","model":"gpt","content":[{"type":"toolCall","id":"call-1","name":"bash","arguments":{"command":"printf ok"}}]}}
{"type":"message","id":"r1","parentId":"m2","timestamp":"2026-10-19T01:02:06.000Z","message":{"role":"toolResult","toolCallId":"call-1","toolName":"bash","content":[{"type":"text","text":"ok"}],"isError":false}}
{"type":"message","id":"m3","parentId":"r1","timestamp":"2026-10-19T01:02:07.000Z","message":{"role":"assistant","model":"gpt","content":[{"type":"text","text":"Done"}]}}
`)
	before, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	source, err := New().Import(context.Background(), sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	opts := harness.ExportOptions{
		Model:     "openai-codex/gpt-x",
		Workspace: "/work/target",
		SessionID: "01a151aa-3333-7333-8333-333333333333",
		CreatedAt: testTime,
	}
	var out bytes.Buffer
	if err := New().Export(context.Background(), source, opts, &out); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("export changed the source transcript")
	}
	if out.Len() <= titleSlotSize || out.Bytes()[titleSlotSize-1] != '\n' || out.Bytes()[titleSlotSize] != '{' {
		t.Fatalf("native record boundary is invalid around byte %d", titleSlotSize)
	}

	targetPath := filepath.Join(t.TempDir(), "target.jsonl")
	if err := os.WriteFile(targetPath, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := New().Import(context.Background(), targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Harness != "omp" || got.ID != opts.SessionID || got.Workspace != opts.Workspace || got.Title != "Unicode 🚀 title" {
		t.Fatalf("round-trip metadata = %#v", got)
	}
	var prompt, answer, command, result string
	for _, event := range got.Events {
		switch event.Type {
		case session.EventMessage:
			if event.Role == session.RoleUser {
				prompt = event.Text
			} else if event.Role == session.RoleAssistant {
				answer = event.Text
			}
		case session.EventToolCall:
			if event.Call != nil {
				command = string(event.Call.Args)
			}
		case session.EventToolResult:
			if event.Result != nil {
				result = event.Result.Output
			}
		}
	}
	if prompt != "Fix it" || answer != "Done" || !strings.Contains(command, "printf ok") || result != "ok" {
		t.Fatalf("round-trip history prompt=%q answer=%q command=%q result=%q events=%#v", prompt, answer, command, result, got.Events)
	}
}

func TestInstallPathUsesNativeShardTimestampAndFreshID(t *testing.T) {
	home := t.TempDir()
	store := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(agentDirOverride, store)
	opts := harness.ExportOptions{
		Workspace: filepath.Join(home, "work", "app"),
		SessionID: "01a151aa-4444-7444-8444-444444444444",
		CreatedAt: testTime,
	}
	got, err := New().InstallPath(session.Session{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(store, "sessions", "-work-app", "2026-10-19T01-02-03-456Z_"+opts.SessionID+".jsonl")
	if got != want {
		t.Fatalf("InstallPath() = %q, want %q", got, want)
	}
	if _, err := New().InstallPath(session.Session{Workspace: opts.Workspace}, harness.ExportOptions{CreatedAt: testTime}); err == nil {
		t.Fatal("InstallPath accepted a missing fresh session ID")
	}
	if err := New().Export(context.Background(), session.Session{}, harness.ExportOptions{SessionID: opts.SessionID}, &bytes.Buffer{}); err == nil {
		t.Fatal("Export accepted a missing creation time")
	}
}

func TestExportKeepsIncompatibleEditsAndRangedReadsAsText(t *testing.T) {
	source := session.Session{ID: "source", Harness: "pi", Workspace: "/work", CreatedAt: testTime, Events: []session.Event{
		{ID: "u", Type: session.EventMessage, Role: session.RoleUser, Text: "Review the recorded changes"},
		{ID: "c1", ParentID: "u", Type: session.EventToolCall, Call: &session.ToolCall{ID: "edit-call", Name: "edit", Args: json.RawMessage(`{"path":"main.go","edits":[{"oldText":"before","newText":"after"}]}`)}},
		{ID: "r1", ParentID: "c1", Type: session.EventToolResult, Result: &session.ToolResult{CallID: "edit-call", Output: "recorded edit completed"}},
		{ID: "c2", ParentID: "r1", Type: session.EventToolCall, Call: &session.ToolCall{ID: "read-call", Name: "read", Args: json.RawMessage(`{"path":"main.go","offset":8,"limit":3}`)}},
		{ID: "r2", ParentID: "c2", Type: session.EventToolResult, Result: &session.ToolResult{CallID: "read-call", Output: "recorded selected lines"}},
	}}
	var out bytes.Buffer
	if err := New().Export(context.Background(), source, harness.ExportOptions{SessionID: "target", CreatedAt: testTime}, &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "target.jsonl")
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, event := range target.Events {
		if event.Type == session.EventToolCall {
			t.Fatal("emitted Pi-shaped edit or ranged-read arguments as an OMP native call")
		}
		text.WriteString(event.Text)
	}
	for _, preserved := range []string{"oldText", "before", "after", "offset", "recorded edit completed", "recorded selected lines"} {
		if !strings.Contains(text.String(), preserved) {
			t.Fatalf("text fallback lost %q", preserved)
		}
	}
}
