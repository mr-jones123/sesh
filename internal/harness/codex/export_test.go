package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/session"
	"github.com/mr-jones123/sesh/internal/tools"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func chain(events ...session.Event) []session.Event {
	for i := range events {
		events[i].ID = fmt.Sprintf("e%d", i)
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

var opts = harness.ExportOptions{SessionID: "01a0da10-0000-7000-8000-000000000001", CreatedAt: t0}

type rolloutLine struct {
	Type    string         `json:"type"`
	Ordinal int            `json:"ordinal"`
	Payload map[string]any `json:"payload"`
}

func export(t *testing.T, s session.Session) []rolloutLine {
	t.Helper()
	var out bytes.Buffer
	if err := New().Export(context.Background(), session.Bundle{Session: s}, opts, &out); err != nil {
		t.Fatal(err)
	}
	var lines []rolloutLine
	for _, raw := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var l rolloutLine
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("line %q: %v", raw, err)
		}
		lines = append(lines, l)
	}
	return lines
}

// items lists response items as "type" or "message:role" — what the model
// receives on resume.
func items(lines []rolloutLine) []string {
	var got []string
	for _, l := range lines {
		if l.Type != "response_item" {
			continue
		}
		kind := l.Payload["type"].(string)
		if kind == "message" {
			kind += ":" + l.Payload["role"].(string)
		}
		got = append(got, kind)
	}
	return got
}

func TestExportTranslatesClaudeSession(t *testing.T) {
	lines := export(t, session.Session{ID: "c1", Harness: "claude", Workspace: "/work/app", CreatedAt: t0, Events: chain(
		text(session.RoleUser, "Fix it"),
		session.Event{Type: session.EventReasoning, Role: session.RoleAssistant, Text: "private thoughts"},
		call("toolu_1", "Bash", `{"command":"go test ./..."}`),
		result("toolu_1", "FAIL"),
		call("toolu_2", "Edit", `{"file_path":"a.go","old_string":"x := 1\n","new_string":"x := 2\n"}`),
		result("toolu_2", "ok"),
		call("toolu_3", "Read", `{"file_path":"a.go"}`),
		result("toolu_3", "package main"),
		text(session.RoleAssistant, "Fixed."),
	)})

	want := "message:user function_call function_call_output custom_tool_call custom_tool_call_output message:assistant message:assistant"
	if got := strings.Join(items(lines), " "); got != want {
		t.Fatalf("response items = %s, want %s", got, want)
	}
	for i, l := range lines {
		if l.Ordinal != i {
			t.Fatalf("line %d ordinal = %d", i, l.Ordinal)
		}
	}
	if meta := lines[0]; meta.Type != "session_meta" || meta.Payload["id"] != opts.SessionID || meta.Payload["cwd"] != "/work/app" {
		t.Fatalf("session_meta = %v", meta)
	}
	if lines[1].Payload["type"] != "task_started" || lines[len(lines)-1].Payload["type"] != "task_complete" {
		t.Fatalf("rollout is not wrapped in one task: first %v, last %v", lines[1].Payload, lines[len(lines)-1].Payload)
	}

	var exec, patch map[string]any
	var readText string
	for _, l := range lines {
		switch l.Payload["type"] {
		case "function_call":
			exec = l.Payload
		case "custom_tool_call":
			patch = l.Payload
		case "message":
			if content := l.Payload["content"].([]any)[0].(map[string]any)["text"].(string); strings.Contains(content, "Read") {
				readText = content
			}
		}
		if strings.Contains(fmt.Sprint(l.Payload), "private thoughts") {
			t.Fatal("reasoning from the source model was written")
		}
	}
	if exec["name"] != "exec_command" || exec["arguments"] != `{"cmd":"go test ./..."}` || exec["call_id"] != "toolu_1" {
		t.Fatalf("exec call = %v", exec)
	}
	// The patch must mean the same edit when read back as Codex input.
	back := tools.Parse("codex", "apply_patch", json.RawMessage(mustJSON(patch["input"])))
	if back.Kind != tools.KindEdit || back.Path != "a.go" || back.Edits[0] != (tools.Edit{Old: "x := 1", New: "x := 2"}) {
		t.Fatalf("patch %q parses back as %#v", patch["input"], back)
	}
	// Codex has no read tool, so the read stays text with its result.
	if !strings.Contains(readText, "package main") {
		t.Fatalf("read call text = %q", readText)
	}
}

func TestExportAnswersInterruptedCalls(t *testing.T) {
	// The source was interrupted before the call returned.
	lines := export(t, session.Session{ID: "p1", Harness: "pi", CreatedAt: t0, Events: chain(
		call("c1", "bash", `{"command":"sleep 100"}`),
		text(session.RoleUser, "stop"),
	)})
	want := "function_call function_call_output message:user"
	if got := strings.Join(items(lines), " "); got != want {
		t.Fatalf("response items = %s, want %s", got, want)
	}
}

func TestExportSanitizesCallIDs(t *testing.T) {
	lines := export(t, session.Session{ID: "p1", Harness: "pi", CreatedAt: t0, Events: chain(
		call("call_a|fc_b", "bash", `{"command":"ls"}`),
		result("call_a|fc_b", "a.go"),
	)})
	var ids []any
	for _, l := range lines {
		if id, ok := l.Payload["call_id"]; ok {
			ids = append(ids, id)
		}
	}
	if len(ids) != 2 || ids[0] != "call_a_fc_b" || ids[1] != ids[0] {
		t.Fatalf("call ids = %v", ids)
	}
}

func TestExportRequiresSessionID(t *testing.T) {
	err := New().Export(context.Background(), session.Bundle{}, harness.ExportOptions{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Export accepted options without a session ID")
	}
}

func TestInstallPath(t *testing.T) {
	t.Setenv("CODEX_HOME", "/codex")
	created := time.Date(2026, 9, 26, 3, 30, 0, 0, time.Local)
	got, err := New().InstallPath(session.Bundle{}, harness.ExportOptions{SessionID: "abc", CreatedAt: created})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/codex", "sessions", "2026", "09", "26", "rollout-2026-09-26T03-30-00-abc.jsonl"); got != want {
		t.Fatalf("InstallPath = %s, want %s", got, want)
	}
}

func mustJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
