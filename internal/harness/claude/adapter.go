package claude

import (
	"context"
	"encoding/json"
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

// record is one Claude Code JSONL line. Only user and assistant records carry
// conversation; other types (attachment, last-prompt, file-history-*, ...)
// stay in RawRecords.
type record struct {
	Type             string   `json:"type"`
	UUID             string   `json:"uuid"`
	ParentUUID       string   `json:"parentUuid"`
	SessionID        string   `json:"sessionId"`
	CWD              string   `json:"cwd"`
	Timestamp        string   `json:"timestamp"`
	IsMeta           bool     `json:"isMeta"`
	IsSidechain      bool     `json:"isSidechain"`
	IsCompactSummary bool     `json:"isCompactSummary"`
	Message          *message `json:"message"`
}

type message struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"` // string or []block
}

type block struct {
	Type      string          `json:"type"` // text | thinking | tool_use | tool_result | image
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"` // tool_result: string or []block
	IsError   bool            `json:"is_error"`
}

func (Adapter) Import(ctx context.Context, path string) (session.Bundle, error) {
	bundle := session.Bundle{Session: session.Session{Harness: "claude"}}
	s := &bundle.Session
	lastEvent := map[string]string{} // record uuid -> last event it produced
	callNames := map[string]string{} // tool_use id -> tool name
	// The first conversation record decides which thread this file holds:
	// main sessions skip inline subagent (sidechain) records, while a
	// subagents/agent-*.jsonl file is entirely sidechain and keeps them.
	threadKnown, sidechainThread := false, false

	err := jsonl.Read(ctx, path, func(line jsonl.Line) error {
		bundle.RawRecords = append(bundle.RawRecords, session.RawLine{Line: line.Number, Record: string(line.Raw)})

		var r record
		if err := json.Unmarshal(line.Raw, &r); err != nil {
			return fmt.Errorf("decode record: %w", err)
		}
		if s.ID == "" {
			s.ID = r.SessionID
		}
		if s.Workspace == "" {
			s.Workspace = r.CWD
		}
		if r.UUID == "" {
			return nil // Session-level metadata, not part of the event tree.
		}

		base := session.Event{
			ID:        r.UUID,
			ParentID:  lastEvent[r.ParentUUID],
			CreatedAt: harness.ParseTime(r.Timestamp),
			RawLine:   line.Number,
		}
		var events []session.Event
		conversation := (r.Type == "user" || r.Type == "assistant") && r.Message != nil
		if conversation && !threadKnown {
			threadKnown, sidechainThread = true, r.IsSidechain
		}
		// Meta records are injected context, not something the user typed.
		if conversation && !r.IsMeta && r.IsSidechain == sidechainThread {
			if s.CreatedAt.IsZero() {
				s.CreatedAt = base.CreatedAt
			}
			var err error
			if events, err = messageEvents(base, r, callNames); err != nil {
				return err
			}
		}
		if len(events) > 0 {
			lastEvent[r.UUID] = events[len(events)-1].ID
		} else {
			lastEvent[r.UUID] = base.ParentID
		}
		s.Events = append(s.Events, events...)
		return nil
	})
	if err != nil {
		return session.Bundle{}, err
	}
	if s.ID == "" {
		return session.Bundle{}, fmt.Errorf("session id not found")
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	return bundle, nil
}

func messageEvents(base session.Event, r record, callNames map[string]string) ([]session.Event, error) {
	blocks, err := decodeBlocks(r.Message.Content)
	if err != nil {
		return nil, err
	}
	if r.IsCompactSummary {
		base.Type, base.Text = session.EventSummary, joinText(blocks)
		return []session.Event{base}, nil
	}

	base.Role, base.Model = session.Role(r.Message.Role), r.Message.Model
	events := make([]session.Event, 0, len(blocks))
	for i, b := range blocks {
		event := base
		event.ID = fmt.Sprintf("%s-%d", base.ID, i)
		if len(events) > 0 {
			event.ParentID = events[len(events)-1].ID
		}
		switch b.Type {
		case "text":
			if b.Text == "" {
				continue
			}
			event.Type, event.Text = session.EventMessage, b.Text
		case "thinking":
			if b.Thinking == "" {
				continue
			}
			event.Type, event.Text = session.EventReasoning, b.Thinking
		case "tool_use":
			callNames[b.ID] = b.Name
			event.Type = session.EventToolCall
			event.Call = &session.ToolCall{ID: b.ID, Name: b.Name, Args: b.Input}
		case "tool_result":
			output, err := decodeBlocks(b.Content)
			if err != nil {
				return nil, fmt.Errorf("tool result %s: %w", b.ToolUseID, err)
			}
			event.Type, event.Role = session.EventToolResult, session.RoleTool
			event.Result = &session.ToolResult{CallID: b.ToolUseID, Name: callNames[b.ToolUseID], Output: joinText(output), IsError: b.IsError}
		default:
			continue // Images are kept in RawRecords until assets are supported.
		}
		events = append(events, event)
	}
	return events, nil
}

// decodeBlocks accepts Claude's two content shapes: a plain string or a block list.
func decodeBlocks(raw json.RawMessage) ([]block, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []block{{Type: "text", Text: text}}, nil
	}
	var blocks []block
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("decode content: %w", err)
	}
	return blocks, nil
}

func joinText(blocks []block) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
