package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/session"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// chain links events in order, as importers do for linear transcripts.
func chain(events ...session.Event) []session.Event {
	for i := range events {
		events[i].ID = string(rune('a' + i))
		if i > 0 {
			events[i].ParentID = events[i-1].ID
		}
		events[i].CreatedAt = t0.Add(time.Duration(i) * time.Second)
	}
	return events
}

func call(id, name, args string) session.Event {
	return session.Event{Type: session.EventToolCall, Role: session.RoleAssistant, Call: &session.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}}
}

func result(id, output string) session.Event {
	return session.Event{Type: session.EventToolResult, Role: session.RoleTool, Result: &session.ToolResult{CallID: id, Output: output}}
}

func text(role session.Role, value string) session.Event {
	return session.Event{Type: session.EventMessage, Role: role, Text: value}
}

// outEntry is one decoded line of exported JSONL.
type outEntry map[string]any

func (e outEntry) message() map[string]any {
	m, _ := e["message"].(map[string]any)
	return m
}

func export(t *testing.T, source session.Session, opts harness.ExportOptions) []outEntry {
	t.Helper()
	var out bytes.Buffer
	if err := New().Export(context.Background(), source, opts, &out); err != nil {
		t.Fatal(err)
	}
	var entries []outEntry
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var e outEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		entries = append(entries, e)
	}
	return entries
}

// roles summarizes entries as "role:blocktypes" for readable assertions.
func roles(entries []outEntry) []string {
	var got []string
	for _, e := range entries[1:] {
		if e["type"] != "message" {
			got = append(got, e["type"].(string))
			continue
		}
		m := e.message()
		var kinds []string
		for _, b := range m["content"].([]any) {
			kinds = append(kinds, b.(map[string]any)["type"].(string))
		}
		got = append(got, m["role"].(string)+":"+strings.Join(kinds, ","))
	}
	return got
}

func TestExportTranslatesClaudeSession(t *testing.T) {
	source := session.Session{ID: "c1", Title: "Saved source title", Harness: "claude", Workspace: "/work/app", CreatedAt: t0, Events: chain(
		text(session.RoleUser, "Fix it"),
		session.Event{Type: session.EventReasoning, Role: session.RoleAssistant, Text: "check tests", Model: "claude-x"},
		call("t1", "Bash", `{"command":"go test ./...","description":"Run tests"}`),
		result("t1", "FAIL"),
		call("t2", "WebSearch", `{"query":"go flaky test"}`),
		result("t2", "3 results"),
		text(session.RoleAssistant, "Fixed."),
	)}

	entries := export(t, source, harness.ExportOptions{Model: "openai-codex/gpt-x", Workspace: "/other", SessionID: "new-pi", CreatedAt: t0})

	want := []string{"user:text", "assistant:thinking,toolCall", "toolResult:text", "assistant:text", "assistant:text", "model_change"}
	if got := roles(entries); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	if h := entries[0]; h["type"] != "session" || h["version"] != 3.0 || h["id"] != "new-pi" ||
		h["cwd"] != "/other" || h["title"] != "Saved source title" {
		t.Fatalf("header = %v", h)
	}
	// Every entry chains to the one before it, so Pi's leaf path covers all.
	for i := 1; i < len(entries); i++ {
		parent := entries[i]["parentId"]
		if (i == 1 && parent != nil) || (i > 1 && parent != entries[i-1]["id"]) {
			t.Fatalf("entry %d parentId = %v", i, parent)
		}
	}

	callBlock := entries[2].message()["content"].([]any)[1].(map[string]any)
	if callBlock["name"] != "bash" || callBlock["arguments"].(map[string]any)["command"] != "go test ./..." {
		t.Fatalf("tool call = %v", callBlock)
	}
	if r := entries[3].message(); r["toolCallId"] != "t1" || r["toolName"] != "bash" {
		t.Fatalf("tool result = %v", r)
	}
	webSearch := entries[4].message()["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(webSearch, "WebSearch") || !strings.Contains(webSearch, "3 results") {
		t.Fatalf("unmapped call text = %q", webSearch)
	}
	if m := entries[2].message(); m["provider"] != importedProvider || m["model"] != "claude-x" {
		t.Fatalf("assistant model = %v/%v", m["provider"], m["model"])
	}
	if mc := entries[len(entries)-1]; mc["provider"] != "openai-codex" || mc["modelId"] != "gpt-x" {
		t.Fatalf("model_change = %v", mc)
	}
}

func TestExportRejectsMalformedModel(t *testing.T) {
	err := New().Export(context.Background(), session.Session{Harness: "claude"}, harness.ExportOptions{Model: "gpt-x"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Export accepted a model without a provider")
	}
}

func TestInstallPath(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "/pi")
	created := time.Date(2026, 9, 26, 3, 30, 0, 0, time.UTC)
	got, err := New().InstallPath(
		session.Session{Workspace: "/work/original"},
		harness.ExportOptions{Workspace: "/work/app", SessionID: "new-pi", CreatedAt: created},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "/pi/sessions/--work-app--/2026-09-26T03-30-00-000Z_new-pi.jsonl"
	if got != want {
		t.Fatalf("InstallPath = %q, want %q", got, want)
	}
}
