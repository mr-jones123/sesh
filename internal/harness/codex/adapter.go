package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/harness/jsonl"
	"github.com/mr-jones123/sesh/internal/session"
)

type Adapter struct{}

func New() Adapter           { return Adapter{} }
func (Adapter) Name() string { return "codex" }
func (Adapter) Detect(path string) bool {
	return strings.HasSuffix(path, ".jsonl") && strings.Contains(strings.ReplaceAll(path, "\\\\", "/"), "/.codex/")
}

// line is one Codex rollout line. The payload shape depends on Type.
type line struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"` // session_meta | turn_context | response_item | compacted | event_msg | ...
	Payload   json.RawMessage `json:"payload"`
}

type sessionMeta struct {
	ID        string       `json:"id"`
	SessionID string       `json:"session_id"`
	CWD       string       `json:"cwd"`
	Meta      *sessionMeta `json:"meta"` // older rollouts nest the metadata
}

type turnContext struct {
	Model string `json:"model"`
}

type compacted struct {
	Message string `json:"message"`
}

// item is a response_item payload: a message, reasoning, tool call, or tool output.
type item struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`      // message
	Content   []contentItem   `json:"content"`   // message
	Summary   []contentItem   `json:"summary"`   // reasoning
	Name      string          `json:"name"`      // function_call, custom_tool_call
	CallID    string          `json:"call_id"`   // calls and outputs
	Arguments json.RawMessage `json:"arguments"` // function_call: JSON text in a string, or an object
	Input     json.RawMessage `json:"input"`     // custom_tool_call: free text, e.g. a patch
	Output    json.RawMessage `json:"output"`    // string or []contentItem
}

type contentItem struct {
	Type string `json:"type"` // input_text | output_text | summary_text | input_image
	Text string `json:"text"`
}

// Codex event_msg records mirror response items for its UI; they are skipped
// so each message appears once.
func (Adapter) Import(ctx context.Context, path string) (session.Bundle, error) {
	bundle := session.Bundle{Session: session.Session{Harness: "codex"}}
	s := &bundle.Session
	previous := ""                   // Codex rollouts are linear: each event follows the last.
	model := ""                      // Set by turn_context, applies to later assistant items.
	callNames := map[string]string{} // call_id -> tool name

	err := jsonl.Read(ctx, path, func(raw jsonl.Line) error {
		bundle.RawRecords = append(bundle.RawRecords, session.RawLine{Line: raw.Number, Record: string(raw.Raw)})

		var l line
		if err := json.Unmarshal(raw.Raw, &l); err != nil {
			return fmt.Errorf("decode line: %w", err)
		}
		created := harness.ParseTime(l.Timestamp)
		if s.CreatedAt.IsZero() {
			s.CreatedAt = created
		}

		event := session.Event{
			ID:        fmt.Sprintf("line-%d", raw.Number),
			ParentID:  previous,
			CreatedAt: created,
			RawLine:   raw.Number,
		}
		keep := false
		switch l.Type {
		case "session_meta":
			var meta sessionMeta
			if err := json.Unmarshal(l.Payload, &meta); err != nil {
				return fmt.Errorf("decode session_meta: %w", err)
			}
			if meta.Meta != nil {
				meta = *meta.Meta
			}
			s.ID = firstNonEmpty(meta.SessionID, meta.ID)
			s.Workspace = meta.CWD
		case "turn_context":
			var turn turnContext
			if err := json.Unmarshal(l.Payload, &turn); err != nil {
				return fmt.Errorf("decode turn_context: %w", err)
			}
			model = turn.Model
		case "compacted":
			var c compacted
			if err := json.Unmarshal(l.Payload, &c); err != nil {
				return fmt.Errorf("decode compacted: %w", err)
			}
			event.Type, event.Text = session.EventSummary, c.Message
			keep = c.Message != ""
		case "response_item":
			var it item
			if err := json.Unmarshal(l.Payload, &it); err != nil {
				return fmt.Errorf("decode response_item: %w", err)
			}
			var err error
			if keep, err = responseItem(&event, it, model, callNames); err != nil {
				return err
			}
		}
		if keep {
			s.Events = append(s.Events, event)
			previous = event.ID
		}
		return nil
	})
	if err != nil {
		return session.Bundle{}, err
	}
	if s.ID == "" {
		return session.Bundle{}, fmt.Errorf("session metadata not found")
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	return bundle, nil
}

// responseItem fills event from it and reports whether the event belongs in
// the timeline.
func responseItem(event *session.Event, it item, model string, callNames map[string]string) (bool, error) {
	switch it.Type {
	case "message":
		event.Type = session.EventMessage
		event.Role = role(it.Role)
		event.Text = joinText(it.Content)
		if event.Role == session.RoleAssistant {
			event.Model = model
		}
		return event.Text != "", nil
	case "reasoning":
		// encrypted_content only works with the original provider; keep the readable summary.
		event.Type, event.Role, event.Model = session.EventReasoning, session.RoleAssistant, model
		event.Text = joinText(it.Summary)
		return event.Text != "", nil
	case "function_call", "custom_tool_call":
		args := it.Arguments
		if it.Type == "custom_tool_call" {
			args = it.Input
		}
		callNames[it.CallID] = it.Name
		event.Type, event.Role, event.Model = session.EventToolCall, session.RoleAssistant, model
		event.Call = &session.ToolCall{ID: it.CallID, Name: it.Name, Args: argsJSON(args)}
		return true, nil
	case "function_call_output", "custom_tool_call_output":
		output, err := outputText(it.Output)
		if err != nil {
			return false, fmt.Errorf("tool output %s: %w", it.CallID, err)
		}
		event.Type, event.Role = session.EventToolResult, session.RoleTool
		event.Result = &session.ToolResult{CallID: it.CallID, Name: callNames[it.CallID], Output: output}
		return true, nil
	}
	return false, nil
}

// role maps Codex roles onto sesh roles. "developer" carries injected
// instructions, which sesh treats as system context.
func role(value string) session.Role {
	if value == "developer" {
		return session.RoleSystem
	}
	return session.Role(value)
}

// argsJSON normalizes call arguments to one JSON value. Codex usually sends
// function_call arguments as JSON text inside a string, sometimes as a plain
// object; custom tools (apply_patch, exec) send free text, kept as a string.
func argsJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return raw // already an object or array
	}
	if json.Valid([]byte(text)) {
		return json.RawMessage(text)
	}
	return raw
}

// outputText accepts both output shapes: a plain string or a content list.
func outputText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var items []contentItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", fmt.Errorf("decode output: %w", err)
	}
	return joinText(items), nil
}

func joinText(items []contentItem) string {
	var parts []string
	for _, item := range items {
		if item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
