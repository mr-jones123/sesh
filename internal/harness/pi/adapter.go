package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

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

// entry is one Pi JSONL line. Fields a given entry type does not use stay empty.
type entry struct {
	Type      string   `json:"type"`
	ID        string   `json:"id"`
	ParentID  string   `json:"parentId"`
	Timestamp string   `json:"timestamp"`
	CWD       string   `json:"cwd"`     // session header
	Summary   string   `json:"summary"` // compaction, branch_summary
	Message   *message `json:"message"` // message
}

type message struct {
	Role       string          `json:"role"`    // user | assistant | system | toolResult
	Content    json.RawMessage `json:"content"` // string or []block
	Model      string          `json:"model"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	IsError    bool            `json:"isError"`
}

type block struct {
	Type      string          `json:"type"` // text | thinking | toolCall | image
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (Adapter) Import(ctx context.Context, path string) (session.Bundle, error) {
	bundle := session.Bundle{Session: session.Session{Harness: "pi"}}
	s := &bundle.Session
	// One Pi entry with N blocks becomes N events, but later entries point at
	// the entry ID. Remember which event ended each entry.
	lastEvent := map[string]string{}

	err := jsonl.Read(ctx, path, func(line jsonl.Line) error {
		bundle.RawRecords = append(bundle.RawRecords, session.RawLine{Line: line.Number, Record: string(line.Raw)})

		var e entry
		if err := json.Unmarshal(line.Raw, &e); err != nil {
			return fmt.Errorf("decode entry: %w", err)
		}
		if len(bundle.RawRecords) == 1 {
			if e.Type != "session" {
				return fmt.Errorf("expected session header, found %q", e.Type)
			}
			s.ID, s.Workspace = e.ID, e.CWD
			s.CreatedAt = harness.ParseTime(e.Timestamp)
			return nil
		}

		base := session.Event{
			ID:        e.ID,
			ParentID:  lastEvent[e.ParentID],
			CreatedAt: harness.ParseTime(e.Timestamp),
			RawLine:   line.Number,
		}
		var events []session.Event
		switch e.Type {
		case "message":
			if e.Message != nil {
				var err error
				if events, err = messageEvents(base, *e.Message); err != nil {
					return err
				}
			}
		case "compaction", "branch_summary":
			base.Type, base.Text = session.EventSummary, e.Summary
			events = []session.Event{base}
		}
		// Entries outside the timeline (model_change, custom, ...) stay in
		// RawRecords; their children attach to the nearest timeline ancestor.
		if len(events) > 0 {
			lastEvent[e.ID] = events[len(events)-1].ID
		} else {
			lastEvent[e.ID] = base.ParentID
		}
		s.Events = append(s.Events, events...)
		return nil
	})
	if err != nil {
		return session.Bundle{}, err
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = harness.FileTime(path)
	}
	return bundle, nil
}

func messageEvents(base session.Event, m message) ([]session.Event, error) {
	blocks, err := decodeContent(m.Content)
	if err != nil {
		return nil, err
	}

	if m.Role == "toolResult" {
		base.Type, base.Role = session.EventToolResult, session.RoleTool
		base.Result = &session.ToolResult{CallID: m.ToolCallID, Name: m.ToolName, Output: joinText(blocks), IsError: m.IsError}
		return []session.Event{base}, nil
	}

	base.Role, base.Model = session.Role(m.Role), m.Model
	events := make([]session.Event, 0, len(blocks))
	for i, b := range blocks {
		event := base
		event.ID = fmt.Sprintf("%s-%d", base.ID, i)
		if len(events) > 0 {
			event.ParentID = events[len(events)-1].ID // chain blocks in order
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
		case "toolCall":
			event.Type = session.EventToolCall
			event.Call = &session.ToolCall{ID: b.ID, Name: b.Name, Args: b.Arguments}
		default:
			continue // Images are kept in RawRecords until assets are supported.
		}
		events = append(events, event)
	}
	return events, nil
}

// decodeContent accepts Pi's two content shapes: a plain string or a block list.
func decodeContent(raw json.RawMessage) ([]block, error) {
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
