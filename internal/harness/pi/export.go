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
)

// Imported assistant messages name this provider so Pi never treats them as
// its own model's output: Pi then turns foreign reasoning into plain text and
// drops provider signatures before sending history to the resumed model.
const importedProvider = "sesh"

// Export writes bundle as a Pi session (format version 3). A Pi bundle with no
// overrides is copied back byte-for-byte; every other bundle is rebuilt from
// its neutral events along the active branch.
func (Adapter) Export(ctx context.Context, bundle session.Bundle, opts harness.ExportOptions, w io.Writer) error {
	provider, modelID, err := splitModel(opts.Model)
	if err != nil {
		return err
	}
	out := bufio.NewWriter(w)
	if bundle.Session.Harness == "pi" && opts == (harness.ExportOptions{}) && len(bundle.RawRecords) > 0 {
		for _, raw := range bundle.RawRecords {
			if _, err := out.WriteString(raw.Record + "\n"); err != nil {
				return err
			}
		}
		return out.Flush()
	}

	e := &exporter{
		enc:       json.NewEncoder(out),
		source:    bundle.Session,
		mapped:    map[string]string{},
		unmapped:  map[string]*deferredCall{},
		pending:   map[string]bool{},
		startedAt: bundle.Session.CreatedAt,
	}
	e.enc.SetEscapeHTML(false)

	workspace := opts.Workspace
	if workspace == "" {
		workspace = bundle.Session.Workspace
	}
	e.write(header{
		Type:      "session",
		Version:   3,
		ID:        fmt.Sprintf("sesh-%s-%s", bundle.Session.Harness, bundle.Session.ID),
		Timestamp: timestamp(e.startedAt),
		CWD:       workspace,
	})
	for _, event := range activeBranch(bundle.Session.Events) {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.add(event)
	}
	e.flushTurn()
	if provider != "" {
		// Pi resumes with the last model named on the branch, so this goes last.
		e.write(modelChangeEntry{entryHead: e.nextHead("model_change", e.startedAt), Provider: provider, ModelID: modelID})
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

// activeBranch returns the path from the root to the last event. Branches
// the source abandoned (rewinds, edits) are left out, as Pi would.
func activeBranch(events []session.Event) []session.Event {
	if len(events) == 0 {
		return nil
	}
	index := make(map[string]int, len(events))
	for i, event := range events {
		index[event.ID] = i
	}
	var path []session.Event
	// Walk parent links from the last event; the length check stops a
	// malformed cycle.
	for i := len(events) - 1; len(path) < len(events); {
		path = append(path, events[i])
		parent, ok := index[events[i].ParentID]
		if !ok {
			break
		}
		i = parent
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path
}

// exporter turns neutral events into Pi entries. Pi pairs each assistant
// tool call with a tool result that must follow the message directly, but
// Claude streams parallel calls and results interleaved (call A, call B,
// result A, call C, ...). So a turn is buffered: assistant events merge into
// one message while any of its calls is unanswered, results are held until
// the turn ends, and calls without a Pi equivalent are written as text after
// the results.
type exporter struct {
	enc       *json.Encoder
	err       error
	source    session.Session
	startedAt time.Time
	nextID    int
	parent    *string

	assistant *assistantMessage // open assistant message of the turn
	results   []timedResult     // results for its tool calls, in arrival order
	deferred  []*deferredCall   // calls written as text after the results
	pending   map[string]bool   // calls of the turn still waiting for a result
	sawResult bool              // the turn has entered its tool-result phase

	mapped   map[string]string        // call ID -> Pi tool name
	unmapped map[string]*deferredCall // call ID -> call written as text
}

type timedResult struct {
	at      time.Time
	message toolResultMessage
}

type deferredCall struct {
	name, args, output string
	isError, done      bool
	at                 time.Time
}

func (e *exporter) add(event session.Event) {
	at := event.CreatedAt
	if at.IsZero() {
		at = e.startedAt
	}
	switch event.Type {
	case session.EventMessage:
		switch event.Role {
		case session.RoleUser:
			e.flushTurn()
			e.writeMessage(at, userMessage{Role: "user", Content: []outBlock{{Type: "text", Text: event.Text}}, Timestamp: at.UnixMilli()})
		case session.RoleAssistant:
			msg := e.assistantEvent(event, at)
			msg.Content = append(msg.Content, outBlock{Type: "text", Text: event.Text})
		}
		// System context (Codex developer messages, Pi system prompts) belongs
		// to the source harness; Pi supplies its own.
	case session.EventReasoning:
		msg := e.assistantEvent(event, at)
		msg.Content = append(msg.Content, outBlock{Type: "thinking", Thinking: event.Text})
	case session.EventToolCall:
		e.addCall(event, at)
	case session.EventToolResult:
		e.addResult(event, at)
	case session.EventSummary:
		e.flushTurn()
		text := "The conversation before this point was summarized:\n\n" + event.Text
		e.writeMessage(at, userMessage{Role: "user", Content: []outBlock{{Type: "text", Text: text}}, Timestamp: at.UnixMilli()})
	}
}

// assistantEvent returns the message an assistant event belongs to. Output
// after all of a turn's results starts a new turn; output while calls are
// still unanswered is part of the same streamed response.
func (e *exporter) assistantEvent(event session.Event, at time.Time) *assistantMessage {
	if e.sawResult && len(e.pending) == 0 {
		e.flushTurn()
	}
	if e.assistant == nil {
		model := event.Model
		if model == "" {
			model = e.source.Harness
		}
		e.assistant = &assistantMessage{
			Role:       "assistant",
			API:        importedProvider,
			Provider:   importedProvider,
			Model:      model,
			StopReason: "stop",
			Timestamp:  at.UnixMilli(),
			at:         at,
		}
	}
	return e.assistant
}

func (e *exporter) addCall(event session.Event, at time.Time) {
	call := event.Call
	msg := e.assistantEvent(event, at)
	e.pending[call.ID] = true
	name, args, ok := piTool(tools.Parse(e.source.Harness, call.Name, call.Args))
	if !ok {
		d := &deferredCall{name: call.Name, args: string(call.Args), at: at}
		e.unmapped[call.ID] = d
		e.deferred = append(e.deferred, d)
		return
	}
	e.mapped[call.ID] = name
	msg.Content = append(msg.Content, outBlock{Type: "toolCall", ID: call.ID, Name: name, Arguments: args})
	msg.StopReason = "toolUse"
}

func (e *exporter) addResult(event session.Event, at time.Time) {
	result := event.Result
	e.sawResult = true
	delete(e.pending, result.CallID)
	if name, ok := e.mapped[result.CallID]; ok {
		e.results = append(e.results, timedResult{at: at, message: toolResultMessage{
			Role:       "toolResult",
			ToolCallID: result.CallID,
			ToolName:   name,
			Content:    []outBlock{{Type: "text", Text: result.Output}},
			IsError:    result.IsError,
			Timestamp:  at.UnixMilli(),
		}})
		return
	}
	d, ok := e.unmapped[result.CallID]
	if !ok { // a result whose call is not on the exported branch
		d = &deferredCall{name: result.Name, at: at}
		e.deferred = append(e.deferred, d)
	}
	d.output, d.isError, d.done = result.Output, result.IsError, true
}

// flushTurn writes the open turn: the assistant message, its tool results,
// then text for calls Pi has no tool for. Calls still pending were never
// answered in the source; Pi fills those in as "No result provided".
func (e *exporter) flushTurn() {
	if e.assistant != nil && len(e.assistant.Content) > 0 {
		e.writeMessage(e.assistant.at, e.assistant)
	}
	for _, r := range e.results {
		e.writeMessage(r.at, r.message)
	}
	if len(e.deferred) > 0 {
		e.writeDeferred()
	}
	e.assistant, e.results, e.deferred, e.sawResult = nil, nil, nil, false
	clear(e.pending)
}

func (e *exporter) writeDeferred() {
	var text strings.Builder
	for i, d := range e.deferred {
		if i > 0 {
			text.WriteString("\n\n")
		}
		fmt.Fprintf(&text, "[Tool call from %s: %s]\n%s", e.source.Harness, d.name, d.args)
		switch {
		case !d.done:
			text.WriteString("\n\n(no result recorded)")
		case d.isError:
			fmt.Fprintf(&text, "\n\nResult (error):\n%s", d.output)
		default:
			fmt.Fprintf(&text, "\n\nResult:\n%s", d.output)
		}
	}
	at := e.deferred[0].at
	e.writeMessage(at, &assistantMessage{
		Role:       "assistant",
		Content:    []outBlock{{Type: "text", Text: text.String()}},
		API:        importedProvider,
		Provider:   importedProvider,
		Model:      e.source.Harness,
		StopReason: "stop",
		Timestamp:  at.UnixMilli(),
	})
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

func (e *exporter) writeMessage(at time.Time, message any) {
	e.write(messageEntry{entryHead: e.nextHead("message", at), Message: message})
}

// nextHead allocates the next entry, chained to the previous one. Entry IDs
// are sequential, so converting the same bundle twice gives the same file.
func (e *exporter) nextHead(kind string, at time.Time) entryHead {
	e.nextID++
	id := fmt.Sprintf("%08x", e.nextID)
	head := entryHead{Type: kind, ID: id, ParentID: e.parent, Timestamp: timestamp(at)}
	e.parent = &id
	return head
}

// write encodes one JSONL line. The first error sticks and later writes are
// skipped, so callers check e.err once at the end.
func (e *exporter) write(value any) {
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
	at         time.Time
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
