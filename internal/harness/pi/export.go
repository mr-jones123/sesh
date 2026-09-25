package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/session"
	"github.com/mr-jones123/sesh/internal/tools"
	"github.com/mr-jones123/sesh/internal/turns"
)

// Imported assistant messages name this provider so Pi never treats them as
// its own model's output: Pi then turns foreign reasoning into plain text and
// drops provider signatures before sending history to the resumed model.
const importedProvider = "sesh"

// Export writes bundle as a Pi session (format version 3). A Pi bundle with no
// overrides is copied back byte-for-byte; every other bundle is rebuilt from
// its turns along the active branch.
func (Adapter) Export(ctx context.Context, bundle session.Bundle, opts harness.ExportOptions, w io.Writer) error {
	provider, modelID, err := splitModel(opts.Model)
	if err != nil {
		return err
	}
	out := bufio.NewWriter(w)
	if bundle.Session.Harness == "pi" && opts.Model == "" && opts.Workspace == "" && len(bundle.RawRecords) > 0 {
		for _, raw := range bundle.RawRecords {
			if _, err := out.WriteString(raw.Record + "\n"); err != nil {
				return err
			}
		}
		return out.Flush()
	}

	e := &writer{enc: json.NewEncoder(out), source: bundle.Session.Harness, start: bundle.Session.CreatedAt}
	e.enc.SetEscapeHTML(false)
	workspace := opts.Workspace
	if workspace == "" {
		workspace = bundle.Session.Workspace
	}
	e.write(header{
		Type:      "session",
		Version:   3,
		ID:        fmt.Sprintf("sesh-%s-%s", bundle.Session.Harness, bundle.Session.ID),
		Timestamp: timestamp(e.start),
		CWD:       workspace,
	})
	for _, turn := range turns.Build(bundle.Session, supported) {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.turn(turn)
	}
	if provider != "" {
		// Pi resumes with the last model named on the branch, so this goes last.
		e.write(modelChangeEntry{entryHead: e.nextHead("model_change", e.start), Provider: provider, ModelID: modelID})
	}
	if e.err != nil {
		return e.err
	}
	return out.Flush()
}

func splitModel(model string) (provider, id string, err error) {
	if model == "" {
		return "", "", nil
	}
	provider, id, ok := strings.Cut(model, "/")
	if !ok || provider == "" || id == "" {
		return "", "", fmt.Errorf("model %q must be provider/model-id", model)
	}
	return provider, id, nil
}

func supported(action tools.Action) bool {
	_, _, ok := piTool(action)
	return ok
}

// writer emits Pi entries, each chained to the previous one.
type writer struct {
	enc    *json.Encoder
	err    error
	source string // source harness, named in text-rendered calls
	start  time.Time
	nextID int
	parent *string
}

// turn writes one turn. An assistant turn becomes the assistant message, a
// toolResult per answered call, then one text message for calls Pi has no
// tool for. Calls left unanswered in the source get Pi's own
// "No result provided" when the session is resumed.
func (e *writer) turn(t turns.Turn) {
	switch t.Kind {
	case turns.User:
		e.writeMessage(t.At, userMessage{Role: "user", Content: []outBlock{{Type: "text", Text: t.Text}}, Timestamp: t.At.UnixMilli()})
	case turns.Summary:
		text := "The conversation before this point was summarized:\n\n" + t.Text
		e.writeMessage(t.At, userMessage{Role: "user", Content: []outBlock{{Type: "text", Text: text}}, Timestamp: t.At.UnixMilli()})
	case turns.Assistant:
		msg := e.assistant(t.At, t.Model)
		names := map[string]string{} // call ID -> Pi tool name
		for _, b := range t.Blocks {
			switch b.Kind {
			case turns.Text:
				msg.Content = append(msg.Content, outBlock{Type: "text", Text: b.Text})
			case turns.Reasoning:
				msg.Content = append(msg.Content, outBlock{Type: "thinking", Thinking: b.Text})
			case turns.Call:
				name, args, _ := piTool(b.Action) // supported() admitted only mappable calls
				names[b.CallID] = name
				msg.Content = append(msg.Content, outBlock{Type: "toolCall", ID: b.CallID, Name: name, Arguments: args})
				msg.StopReason = "toolUse"
			}
		}
		if len(msg.Content) > 0 {
			e.writeMessage(t.At, msg)
		}
		for _, r := range t.Results {
			e.writeMessage(r.At, toolResultMessage{
				Role:       "toolResult",
				ToolCallID: r.CallID,
				ToolName:   names[r.CallID],
				Content:    []outBlock{{Type: "text", Text: r.Output}},
				IsError:    r.IsError,
				Timestamp:  r.At.UnixMilli(),
			})
		}
		if len(t.Unmapped) > 0 {
			at := t.Unmapped[0].At
			text := e.assistant(at, "")
			text.Content = []outBlock{{Type: "text", Text: turns.UnmappedText(e.source, t.Unmapped)}}
			e.writeMessage(at, text)
		}
	}
}

func (e *writer) assistant(at time.Time, model string) *assistantMessage {
	if model == "" {
		model = e.source
	}
	return &assistantMessage{
		Role:       "assistant",
		API:        importedProvider,
		Provider:   importedProvider,
		Model:      model,
		StopReason: "stop",
		Timestamp:  at.UnixMilli(),
	}
}

// piTool renders an action as the matching Pi built-in tool.
func piTool(action tools.Action) (name string, args json.RawMessage, ok bool) {
	var value any
	switch action.Kind {
	case tools.KindExec:
		name, value = "bash", struct {
			Command string `json:"command"`
		}{action.Command}
	case tools.KindRead:
		name, value = "read", struct {
			Path   string `json:"path"`
			Offset int    `json:"offset,omitempty"`
			Limit  int    `json:"limit,omitempty"`
		}{action.Path, action.Offset, action.Limit}
	case tools.KindWrite:
		name, value = "write", struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}{action.Path, action.Content}
	case tools.KindEdit:
		type edit struct {
			OldText string `json:"oldText"`
			NewText string `json:"newText"`
		}
		edits := make([]edit, len(action.Edits))
		for i, e := range action.Edits {
			edits[i] = edit{e.Old, e.New}
		}
		name, value = "edit", struct {
			Path  string `json:"path"`
			Edits []edit `json:"edits"`
		}{action.Path, edits}
	default:
		return "", nil, false
	}
	args, err := json.Marshal(value)
	return name, args, err == nil
}

func (e *writer) writeMessage(at time.Time, message any) {
	e.write(messageEntry{entryHead: e.nextHead("message", at), Message: message})
}

// nextHead allocates the next entry, chained to the previous one. Entry IDs
// are sequential, so converting the same bundle twice gives the same file.
func (e *writer) nextHead(kind string, at time.Time) entryHead {
	e.nextID++
	id := fmt.Sprintf("%08x", e.nextID)
	head := entryHead{Type: kind, ID: id, ParentID: e.parent, Timestamp: timestamp(at)}
	e.parent = &id
	return head
}

// write encodes one JSONL line. The first error sticks and later writes are
// skipped, so callers check e.err once at the end.
func (e *writer) write(value any) {
	if e.err != nil {
		return
	}
	if err := e.enc.Encode(value); err != nil {
		e.err = fmt.Errorf("write pi session: %w", err)
	}
}

func timestamp(at time.Time) string {
	return at.UTC().Format("2006-01-02T15:04:05.000Z")
}

type header struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	CWD       string `json:"cwd"`
}

type entryHead struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	ParentID  *string `json:"parentId"` // null for the first entry
	Timestamp string  `json:"timestamp"`
}

// Embedded entryHead fields are flattened into the same JSON object.
type messageEntry struct {
	entryHead
	Message any `json:"message"`
}

type modelChangeEntry struct {
	entryHead
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

type outBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type userMessage struct {
	Role      string     `json:"role"`
	Content   []outBlock `json:"content"`
	Timestamp int64      `json:"timestamp"`
}

type assistantMessage struct {
	Role       string     `json:"role"`
	Content    []outBlock `json:"content"`
	API        string     `json:"api"`
	Provider   string     `json:"provider"`
	Model      string     `json:"model"`
	Usage      usage      `json:"usage"`
	StopReason string     `json:"stopReason"`
	Timestamp  int64      `json:"timestamp"`
}

type toolResultMessage struct {
	Role       string     `json:"role"`
	ToolCallID string     `json:"toolCallId"`
	ToolName   string     `json:"toolName"`
	Content    []outBlock `json:"content"`
	IsError    bool       `json:"isError"`
	Timestamp  int64      `json:"timestamp"`
}

// usage is zero: imported turns cost nothing in this session.
type usage struct {
	Input       int  `json:"input"`
	Output      int  `json:"output"`
	CacheRead   int  `json:"cacheRead"`
	CacheWrite  int  `json:"cacheWrite"`
	TotalTokens int  `json:"totalTokens"`
	Cost        cost `json:"cost"`
}

type cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}
