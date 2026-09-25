package pi

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
func (Adapter) Name() string { return "pi" }

func (Adapter) Detect(path string) bool {
	return strings.HasSuffix(path, ".jsonl") && strings.Contains(filepath.ToSlash(path), "/.pi/")
}

func (Adapter) Import(ctx context.Context, path string) (session.Session, error) {
	result := session.Session{Harness: "pi"}
	first := true

	err := jsonl.Read(ctx, path, func(line int, value map[string]any) error {
		result.RawRecords = append(result.RawRecords, session.RawLine{Line: line, Record: value})
		typeName := jsonl.String(value["type"])
		if first {
			if typeName != "session" {
				return fmt.Errorf("expected session header, found %q", typeName)
			}
			result.ID = jsonl.String(value["id"])
			result.CreatedAt = harness.ParseTime(value["timestamp"])
			result.Workspace = jsonl.String(value["cwd"])
			first = false
			return nil
		}

		created := harness.ParseTime(value["timestamp"])
		event := session.Event{ID: jsonl.String(value["id"]), CreatedAt: created, Raw: value}
		switch typeName {
		case "message":
			message := jsonl.Map(value["message"])
			role := jsonl.String(message["role"])
			blocks := jsonl.Slice(message["content"])
			if len(blocks) == 0 {
				blocks = []any{message["content"]}
			}
			for index, blockValue := range blocks {
				block := jsonl.Map(blockValue)
				blockType := jsonl.String(block["type"])
				item := event
				item.ID = fmt.Sprintf("%s-%d", event.ID, index)
				item.Role = role
				if text, ok := blockValue.(string); ok {
					item.Type = session.EventMessage
					item.Content = text
					result.Events = append(result.Events, item)
					continue
				}
				switch blockType {
				case "text", "thinking":
					item.Type = session.EventMessage
					item.Content = jsonl.String(block["text"])
				case "toolCall":
					item.Type = session.EventToolCall
					item.Tool = &session.ToolEvent{ID: jsonl.String(block["id"]), Name: jsonl.String(block["name"]), Arguments: fmt.Sprintf("%v", block["arguments"])}
				case "toolResult":
					item.Type = session.EventToolResult
					item.Tool = &session.ToolEvent{ID: jsonl.String(block["toolCallId"]), Name: jsonl.String(block["toolName"]), Output: contentText(block["content"]), IsError: block["isError"] == true}
				default:
					continue
				}
				result.Events = append(result.Events, item)
			}
		case "compaction", "branch_summary":
			event.Type = session.EventSummary
			event.Content = jsonl.String(value["summary"])
			result.Events = append(result.Events, event)
		default:
			return nil // Preserve unsupported entries in the raw transcript, not the timeline.
		}
		return nil
	})
	if err != nil {
		return session.Session{}, err
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now()
	}
	return result, nil
}

func contentText(value any) string {
	if text, ok := value.(string); ok {
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
