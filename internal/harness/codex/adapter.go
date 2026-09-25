package codex

import (
	"context"
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

func (Adapter) Import(ctx context.Context, path string) (session.Session, error) {
	result := session.Session{Harness: "codex"}
	err := jsonl.Read(ctx, path, func(line int, value map[string]any) error {
		result.RawRecords = append(result.RawRecords, session.RawLine{Line: line, Record: value})
		kind := jsonl.String(value["type"])
		payload := jsonl.Map(value["payload"])
		if result.CreatedAt.IsZero() {
			result.CreatedAt = harness.ParseTime(value["timestamp"])
		}

		switch kind {
		case "session_meta":
			meta := payload
			if nested := jsonl.Map(payload["meta"]); nested != nil {
				meta = nested
			}
			result.ID = first(meta, "session_id", "id")
			result.Workspace = jsonl.String(meta["cwd"])
		case "response_item":
			return responseItem(value, payload, &result)
		case "event_msg":
			return eventMessage(value, payload, &result)
		}
		return nil
	})
	if err != nil {
		return session.Session{}, err
	}
	if result.ID == "" {
		return session.Session{}, fmt.Errorf("session metadata not found")
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now()
	}
	return result, nil
}

func responseItem(line, payload map[string]any, result *session.Session) error {
	typeName := jsonl.String(payload["type"])
	created := harness.ParseTime(line["timestamp"])
	event := session.Event{ID: jsonl.String(line["timestamp"]), CreatedAt: created, Raw: line}
	switch typeName {
	case "message":
		event.Type = session.EventMessage
		event.Role = jsonl.String(payload["role"])
		event.Content = text(payload["content"])
	case "function_call", "custom_tool_call":
		event.Type = session.EventToolCall
		event.Tool = &session.ToolEvent{ID: first(payload, "call_id", "id"), Name: first(payload, "name", "function"), Arguments: stringify(payload["arguments"])}
	case "function_call_output", "custom_tool_call_output":
		event.Type = session.EventToolResult
		event.Tool = &session.ToolEvent{ID: first(payload, "call_id", "id"), Output: stringify(payload["output"])}
	default:
		return nil
	}
	result.Events = append(result.Events, event)
	return nil
}

func eventMessage(line, payload map[string]any, result *session.Session) error {
	messageType := jsonl.String(payload["type"])
	if messageType != "user_message" && messageType != "agent_message" {
		return nil
	}
	result.Events = append(result.Events, session.Event{ID: jsonl.String(line["timestamp"]), CreatedAt: harness.ParseTime(line["timestamp"]), Type: session.EventMessage, Role: strings.TrimSuffix(messageType, "_message"), Content: jsonl.String(payload["message"]), Raw: line})
	return nil
}

func first(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := jsonl.String(value[key]); text != "" {
			return text
		}
	}
	return ""
}
func text(value any) string {
	if text := jsonl.String(value); text != "" {
		return text
	}
	var parts []string
	for _, item := range jsonl.Slice(value) {
		block := jsonl.Map(item)
		if text := jsonl.String(block["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}
func stringify(value any) string { return fmt.Sprintf("%v", value) }
