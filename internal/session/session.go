package session

import (
	"encoding/json"
	"time"
)

// CurrentFormatVersion 2 splits tool calls from results and stores raw lines
// verbatim. Version 1 bundles must be re-exported from their source.
const CurrentFormatVersion = 2

type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Harness   string    `json:"harness"`
	Workspace string    `json:"workspace,omitempty"`
	Events    []Event   `json:"events"`
}

type Event struct {
	ID        string      `json:"id"`
	ParentID  string      `json:"parent_id,omitempty"`
	Type      EventType   `json:"type"`
	Role      Role        `json:"role,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	Model     string      `json:"model,omitempty"`
	Text      string      `json:"text,omitempty"`
	Call      *ToolCall   `json:"call,omitempty"`
	Result    *ToolResult `json:"result,omitempty"`
	// RawLine is the source line number in Bundle.RawRecords, not a copy.
	RawLine int `json:"raw_line,omitempty"`
}

type EventType string

const (
	EventMessage    EventType = "message"
	EventReasoning  EventType = "reasoning"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventSummary    EventType = "summary"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type ToolResult struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name,omitempty"`
	Output  string `json:"output,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

type Source struct {
	Path       string `json:"path,omitempty"`
	Repository string `json:"repository,omitempty"`
	CommitSHA  string `json:"commit_sha,omitempty"`
	Dirty      bool   `json:"dirty,omitempty"`
}

type Bundle struct {
	FormatVersion int       `json:"format_version"`
	Source        Source    `json:"source,omitempty"`
	Session       Session   `json:"session"`
	RawRecords    []RawLine `json:"raw_records,omitempty"`
}

// RawLine keeps one source line verbatim. Record is a string, not
// json.RawMessage: encoding/json re-indents and escapes embedded raw JSON,
// while a JSON string decodes back to the exact original bytes.
type RawLine struct {
	Line   int    `json:"line"`
	Record string `json:"record"`
}
