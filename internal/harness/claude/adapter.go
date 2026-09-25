package claude

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/harness/jsonl"
	"github.com/mr-jones123/sesh/internal/session"
)

type Adapter struct{}

func New() Adapter           { return Adapter{} }
func (Adapter) Name() string { return "claude" }
func (Adapter) Detect(path string) bool {
	return strings.HasSuffix(path, ".jsonl") && strings.Contains(filepath.ToSlash(path), "/.claude/projects/")
}

func (Adapter) Import(ctx context.Context, path string) (session.Session, error) {
	result := session.Session{Harness: "claude"}
	err := jsonl.Read(ctx, path, func(line int, value map[string]any) error {
		result.RawRecords = append(result.RawRecords, session.RawLine{Line: line, Record: value})
		typeName := jsonl.String(value["type"])
		if typeName != "user" && typeName != "assistant" {
			return nil
		}
		message := jsonl.Map(value["message"])
		if message == nil {
			return nil
		}
		if result.ID == "" {
			result.ID = jsonl.String(value["sessionId"])
		}
		if result.Workspace == "" {
			result.Workspace = jsonl.String(value["cwd"])
		}
		if result.CreatedAt.IsZero() {
			result.CreatedAt = parseTime(value["timestamp"])
		}

		created := parseTime(value["timestamp"])
		for index, blockValue := range blocks(message["content"]) {
			block := jsonl.Map(blockValue)
			event := session.Event{ID: fmt.Sprintf("%s-%d", jsonl.String(value["uuid"]), index), CreatedAt: created, Role: jsonl.String(message["role"]), Raw: value}
			switch jsonl.String(block["type"]) {
			case "text", "thinking":
				event.Type = session.EventMessage
				event.Content = jsonl.String(block["text"])
			case "tool_use":
				event.Type = session.EventToolCall
				event.Tool = &session.ToolEvent{ID: jsonl.String(block["id"]), Name: jsonl.String(block["name"]), Arguments: stringify(block["input"])}
			case "tool_result":
				event.Type = session.EventToolResult
				event.Tool = &session.ToolEvent{ID: jsonl.String(block["tool_use_id"]), Output: content(block["content"]), IsError: block["is_error"] == true}
			default:
				continue
			}
			result.Events = append(result.Events, event)
		}
		return nil
	})
	if err != nil {
		return session.Session{}, err
	}
	if result.ID == "" {
		return session.Session{}, fmt.Errorf("session id not found")
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now()
	}
	return result, nil
}

func blocks(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return []any{map[string]any{"type": "text", "text": jsonl.String(value)}}
}

func content(value any) string {
	if text := jsonl.String(value); text != "" {
		return text
	}
	return stringify(value)
}
func stringify(value any) string    { return fmt.Sprintf("%v", value) }
func parseTime(value any) time.Time { return harness.ParseTime(value) }
