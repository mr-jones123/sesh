package session

import (
	"encoding/json"
	"time"
)

// Session is the harness-neutral conversation used only while handing a
// local transcript directly to another harness.

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
