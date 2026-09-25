package session

import "time"

const CurrentFormatVersion = 1

type Session struct {
	ID         string    `json:"id"`
	Title      string    `json:"title,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	Harness    string    `json:"harness"`
	Workspace  string    `json:"workspace,omitempty"`
	Events     []Event   `json:"events"`
	RawRecords []RawLine `json:"-"`
}

type Event struct {
	ID        string         `json:"id,omitempty"`
	ParentID  string         `json:"parent_id,omitempty"`
	Type      EventType      `json:"type"`
	Role      string         `json:"role,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	Content   string         `json:"content,omitempty"`
	Tool      *ToolEvent     `json:"tool,omitempty"`
	Raw       map[string]any `json:"raw,omitempty"`
}

type EventType string

const (
	EventMessage    EventType = "message"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventSummary    EventType = "summary"
)

type ToolEvent struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
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

type RawLine struct {
	Line   int            `json:"line"`
	Record map[string]any `json:"record"`
}
