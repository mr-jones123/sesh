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
	Title     string   `json:"title"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
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

func (Adapter) Import(ctx context.Context, path string) (session.Session, error) {
	s := session.Session{Harness: "pi"}
	// One Pi entry with N blocks becomes N events, but later entries point at
	// the entry ID. Remember which event ended each entry.
	lastEvent := map[string]string{}
	headerSeen := false
	var titles nativeTitles

	err := jsonl.Read(ctx, path, func(line jsonl.Line) error {
		var e entry
		if err := json.Unmarshal(line.Raw, &e); err != nil {
			return fmt.Errorf("decode entry: %w", err)
		}
		if !headerSeen {
			if line.Number == 1 && e.Type == "title" {
				titles.set(e.Title, titleSlot)
				return nil
			}
			headerSeen = true
			if e.Type != "session" {
				return fmt.Errorf("expected session header, found %q", e.Type)
			}
			s.ID, s.Workspace = e.ID, e.CWD
			s.CreatedAt = harness.ParseTime(e.Timestamp)
			titles.set(firstNonEmpty(e.Title, e.Name), titleHeader)
			return nil
		}
		titles.observe(e)

		base := session.Event{
			ID:        e.ID,
			ParentID:  lastEvent[e.ParentID],
			CreatedAt: harness.ParseTime(e.Timestamp),
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
		// Entries outside the timeline (model_change, custom, ...) attach
		// their children to the nearest timeline ancestor.
		if len(events) > 0 {
			lastEvent[e.ID] = events[len(events)-1].ID
		} else {
			lastEvent[e.ID] = base.ParentID
		}
		s.Events = append(s.Events, events...)
		return nil
	})
	if err != nil {
		return session.Session{}, err
	}
	s.Title = titles.value
	if !headerSeen || s.ID == "" {
		return session.Session{}, fmt.Errorf("session header not found")
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = harness.FileTime(path)
	}
	return s, nil
}

func (Adapter) Summarize(ctx context.Context, path string) (harness.Summary, error) {
	var summary harness.Summary
	headerSeen := false
	err := jsonl.ReadUntil(ctx, path, func(line jsonl.Line) (bool, error) {
		var e entry
		if err := json.Unmarshal(line.Raw, &e); err != nil {
			return false, fmt.Errorf("decode entry: %w", err)
		}
		if !headerSeen {
			if line.Number == 1 && e.Type == "title" {
				return false, nil
			}
			headerSeen = true
			if e.Type != "session" {
				return false, fmt.Errorf("expected session header, found %q", e.Type)
			}
			summary.ID, summary.Workspace = e.ID, e.CWD
			return false, nil
		}
		if summary.Prompt == "" && e.Type == "message" && e.Message != nil &&
			e.Message.Role == string(session.RoleUser) {
			blocks, err := decodeContent(e.Message.Content)
			if err != nil {
				return false, err
			}
			summary.Prompt = harness.Prompt(joinText(blocks))
		}
		return summary.ID != "" && summary.Workspace != "" && summary.Prompt != "", nil
	})
	if err != nil {
		return harness.Summary{}, err
	}
	scanned, err := scanTitle(ctx, path)
	if err != nil {
		return harness.Summary{}, err
	}
	summary.Title = scanned
	if !headerSeen || summary.ID == "" {
		return harness.Summary{}, fmt.Errorf("session header not found")
	}
	return summary, nil
}

const (
	titleHeader = iota + 1
	titleInfo
	titleGenerated
	titleManual
	titleSlot
)

type nativeTitles struct {
	value    string
	priority int
}

func (t *nativeTitles) set(value string, priority int) {
	if priority < t.priority {
		return
	}
	t.value = strings.TrimSpace(value)
	t.priority = priority
}

func (t *nativeTitles) observe(e entry) {
	switch e.Type {
	case "session_info":
		t.set(e.Name, titleInfo)
	case "title":
		priority := titleGenerated
		switch e.Source {
		case "user", "manual", "custom":
			priority = titleManual
		}
		t.set(e.Title, priority)
	}
}

func scanTitle(ctx context.Context, path string) (string, error) {
	var titles nativeTitles
	err := jsonl.ScanMetadata(ctx, path, func(line jsonl.MetadataLine) error {
		kind, _ := jsonl.StringField(line.Prefix, "type")
		switch kind {
		case "session":
			title, _ := jsonl.StringField(line.Prefix, "title")
			if title == "" {
				title, _ = jsonl.StringField(line.Prefix, "name")
			}
			titles.set(title, titleHeader)
		case "session_info":
			name, _ := jsonl.StringField(line.Prefix, "name")
			titles.set(name, titleInfo)
		case "title":
			title, _ := jsonl.StringField(line.Prefix, "title")
			if line.Number == 1 {
				titles.set(title, titleSlot)
				break
			}
			source, _ := jsonl.StringField(line.Prefix, "source")
			priority := titleGenerated
			switch source {
			case "user", "manual", "custom":
				priority = titleManual
			}
			titles.set(title, priority)
		}
		return nil
	})
	return titles.value, err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
			continue // Images are not represented in the neutral session.
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
