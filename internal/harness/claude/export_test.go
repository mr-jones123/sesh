package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/session"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

var opts = harness.ExportOptions{SessionID: "01a0da30-0000-7000-8000-000000000001", CreatedAt: t0}

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

type outRecord struct {
	ParentUUID *string `json:"parentUuid"`
	UUID       string  `json:"uuid"`
	Type       string  `json:"type"`
	SessionID  string  `json:"sessionId"`
	CWD        string  `json:"cwd"`
	Message    struct {
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
	} `json:"message"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   string          `json:"content"`
	IsError   bool            `json:"is_error"`
}

func (r outRecord) blocks() []contentBlock {
	var blocks []contentBlock
	_ = json.Unmarshal(r.Message.Content, &blocks) // typed user text is a string: no blocks
	return blocks
}

func export(t *testing.T, s session.Session, o harness.ExportOptions) (string, []outRecord) {
	t.Helper()
	var out bytes.Buffer
	if err := New().Export(context.Background(), session.Bundle{Session: s}, o, &out); err != nil {
		t.Fatal(err)
	}
	var records []outRecord
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var r outRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		records = append(records, r)
	}
	return out.String(), records
}

// shape lists records as "type:blocktypes" ("user:text" for typed text).
func shape(records []outRecord) string {
	var parts []string
	for _, r := range records {
		blocks := r.blocks()
		if blocks == nil {
			parts = append(parts, r.Type+":text")
			continue
		}
		var kinds []string
		for _, b := range blocks {
			kinds = append(kinds, b.Type)
		}
		parts = append(parts, r.Type+":"+strings.Join(kinds, ","))
	}
	return strings.Join(parts, " ")
}

func TestExportTranslatesCodexSession(t *testing.T) {
	patch, _ := json.Marshal("*** Begin Patch\n*** Update File: src/a.go\n@@\n-x := 1\n+x := 2\n*** End Patch")
	_, records := export(t, session.Session{ID: "x1", Harness: "codex", Workspace: "/work/app", CreatedAt: t0, Events: chain(
		text(session.RoleUser, "Fix it"),
		session.Event{Type: session.EventReasoning, Role: session.RoleAssistant, Text: "private thoughts"},
		call("call_1", "exec_command", `{"cmd":"go test ./..."}`),
		result("call_1", "FAIL"),
		call("call_2", "apply_patch", string(patch)),
		result("call_2", "Success"),
		call("call_3", "view_image", `{"path":"a.png"}`),
		result("call_3", "<image>"),
		text(session.RoleAssistant, "Fixed."),
	)}, opts)

	want := "user:text assistant:tool_use user:tool_result assistant:tool_use user:tool_result assistant:text assistant:text"
	if got := shape(records); got != want {
		t.Fatalf("records = %s, want %s", got, want)
	}
	for i, r := range records {
		if (i == 0 && r.ParentUUID != nil) || (i > 0 && (r.ParentUUID == nil || *r.ParentUUID != records[i-1].UUID)) {
			t.Fatalf("outRecord %d parentUuid = %v", i, r.ParentUUID)
		}
		if r.SessionID != opts.SessionID || r.CWD != "/work/app" {
			t.Fatalf("outRecord %d session = %s cwd = %s", i, r.SessionID, r.CWD)
		}
		if strings.Contains(string(r.Message.Content), "private thoughts") {
			t.Fatal("reasoning from the source model was written")
		}
	}
	bash := records[1].blocks()[0]
	if bash.Name != "Bash" || string(bash.Input) != `{"command":"go test ./..."}` || records[1].Message.StopReason != "tool_use" {
		t.Fatalf("bash = %+v", bash)
	}
	if r := records[2].blocks()[0]; r.ToolUseID != "call_1" || r.Content != "FAIL" {
		t.Fatalf("bash result = %+v", r)
	}
	// Codex patch paths are relative; Claude's Edit needs an absolute path.
	edit := records[3].blocks()[0]
	if edit.Name != "Edit" || string(edit.Input) != `{"file_path":"/work/app/src/a.go","old_string":"x := 1","new_string":"x := 2"}` {
		t.Fatalf("edit = %s %s", edit.Name, edit.Input)
	}
	if unmapped := string(records[5].Message.Content); !strings.Contains(unmapped, "view_image") || !strings.Contains(unmapped, "<image>") {
		t.Fatalf("unmapped call text = %s", unmapped)
	}
}

func TestExportSplitsMultiReplacementEdits(t *testing.T) {
	_, records := export(t, session.Session{ID: "p1", Harness: "pi", Workspace: "/w", CreatedAt: t0, Events: chain(
		text(session.RoleUser, "rename"),
		call("call_a|fc_b", "edit", `{"path":"/w/a.go","edits":[{"oldText":"a","newText":"b"},{"oldText":"c","newText":"d"}]}`),
		result("call_a|fc_b", "Successfully replaced 2 block(s)"),
	)}, opts)

	if got := shape(records); got != "user:text assistant:tool_use,tool_use user:tool_result,tool_result" {
		t.Fatalf("records = %s", got)
	}
	uses, results := records[1].blocks(), records[2].blocks()
	// Pi's "|" is not allowed in Anthropic tool IDs.
	if uses[0].ID != "call_a_fc_b" || uses[1].ID != "call_a_fc_b-2" {
		t.Fatalf("tool_use ids = %s, %s", uses[0].ID, uses[1].ID)
	}
	if string(uses[1].Input) != `{"file_path":"/w/a.go","old_string":"c","new_string":"d"}` {
		t.Fatalf("second edit = %s", uses[1].Input)
	}
	for i, r := range results {
		if r.ToolUseID != uses[i].ID || r.Content != "Successfully replaced 2 block(s)" {
			t.Fatalf("result %d = %+v", i, r)
		}
	}
}

func TestExportAnswersInterruptedCalls(t *testing.T) {
	_, records := export(t, session.Session{ID: "p1", Harness: "pi", CreatedAt: t0, Events: chain(
		text(session.RoleUser, "wait"),
		call("c1", "bash", `{"command":"sleep 100"}`),
		text(session.RoleUser, "stop"),
	)}, opts)
	if got := shape(records); got != "user:text assistant:tool_use user:tool_result user:text" {
		t.Fatalf("records = %s", got)
	}
	if r := records[2].blocks()[0]; r.ToolUseID != "c1" || !r.IsError {
		t.Fatalf("interrupted result = %+v", r)
	}
}

func TestExportReadsBackThroughImporter(t *testing.T) {
	data, _ := export(t, session.Session{ID: "p1", Harness: "pi", Workspace: "/w", CreatedAt: t0, Events: chain(
		text(session.RoleUser, "hi"),
		call("c1", "read", `{"path":"a.go"}`),
		result("c1", "package main"),
		text(session.RoleAssistant, "done"),
	)}, opts)
	path := filepath.Join(t.TempDir(), opts.SessionID+".jsonl")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, err := New().Import(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	events := bundle.Session.Events
	if len(events) != 4 || events[1].Call.Name != "Read" || events[2].Result.CallID != events[1].Call.ID || events[2].Result.Output != "package main" {
		t.Fatalf("re-imported events = %+v", events)
	}
}

func TestInstallPath(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/claude")
	bundle := session.Bundle{Session: session.Session{Workspace: "/no/such/projects i like/Soup-MLX"}}
	got, err := New().InstallPath(bundle, harness.ExportOptions{SessionID: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "/claude/projects/-no-such-projects-i-like-Soup-MLX/abc.jsonl"; got != want {
		t.Fatalf("InstallPath = %s, want %s", got, want)
	}
	if _, err := New().InstallPath(session.Bundle{}, harness.ExportOptions{SessionID: "abc"}); err == nil {
		t.Fatal("InstallPath accepted an empty workspace")
	}
}

func TestExportOpensWithUserMessage(t *testing.T) {
	_, records := export(t, session.Session{ID: "p1", Harness: "pi", CreatedAt: t0, Events: chain(
		text(session.RoleAssistant, "resuming"),
	)}, opts)
	if got := shape(records); got != "user:text assistant:text" {
		t.Fatalf("records = %s", got)
	}
}
