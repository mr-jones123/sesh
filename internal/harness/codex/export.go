package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/ids"
	"github.com/mr-jones123/sesh/internal/session"
	"github.com/mr-jones123/sesh/internal/tools"
	"github.com/mr-jones123/sesh/internal/turns"
)

// importTurn is the single Codex turn wrapping imported history, as Codex's
// own external-session importer does.
const importTurn = "sesh-import-turn-1"

// Export writes bundle as a Codex rollout. The rollout's response items are
// what Codex sends the model on resume; the event_msg items are what its UI
// shows. Shell calls become exec_command and file writes/edits become
// apply_patch; reads (Codex has no read tool) and other tools are kept as
// text. Reasoning from the source model is dropped: Codex reasoning is
// encrypted per provider and cannot be forged.
func (Adapter) Export(ctx context.Context, bundle session.Bundle, opts harness.ExportOptions, w io.Writer) error {
	if opts.SessionID == "" || opts.CreatedAt.IsZero() {
		return fmt.Errorf("codex export needs a session ID and creation time")
	}
	workspace := opts.Workspace
	if workspace == "" {
		workspace = bundle.Session.Workspace
	}
	out := bufio.NewWriter(w)
	e := &writer{enc: json.NewEncoder(out), source: bundle.Session.Harness, thread: opts.SessionID}
	e.enc.SetEscapeHTML(false)

	e.line(opts.CreatedAt, "session_meta", sessionMetaOut{
		ID:            opts.SessionID,
		Timestamp:     timestamp(opts.CreatedAt),
		CWD:           workspace,
		Originator:    "sesh",
		CLIVersion:    "sesh",
		Source:        "cli",
		ModelProvider: "openai",
	})
	e.line(opts.CreatedAt, "event_msg", taskEvent{Type: "task_started", TurnID: importTurn})
	var last string
	for _, turn := range turns.Build(bundle.Session, supported) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if text := e.turn(turn); text != "" {
			last = text
		}
	}
	e.line(opts.CreatedAt, "event_msg", taskEvent{Type: "task_complete", TurnID: importTurn, LastAgentMessage: last})
	if e.err != nil {
		return e.err
	}
	return out.Flush()
}

// InstallPath places the rollout where Codex looks for it: Codex finds a
// session by the ID in the file name and registers it on first resume.
func (Adapter) InstallPath(_ session.Bundle, opts harness.ExportOptions) (string, error) {
	home, err := codexHome()
	if err != nil {
		return "", err
	}
	local := opts.CreatedAt.Local()
	name := fmt.Sprintf("rollout-%s-%s.jsonl", local.Format("2006-01-02T15-04-05"), opts.SessionID)
	return filepath.Join(home, "sessions", local.Format("2006"), local.Format("01"), local.Format("02"), name), nil
}

func (Adapter) ResumeCommand(bundle session.Bundle, opts harness.ExportOptions) string {
	workspace := opts.Workspace
	if workspace == "" {
		workspace = bundle.Session.Workspace
	}
	return fmt.Sprintf("cd %s && codex resume %s", tools.ShellQuote(workspace), opts.SessionID)
}

func supported(action tools.Action) bool {
	switch action.Kind {
	case tools.KindExec, tools.KindWrite, tools.KindEdit:
		return true
	}
	return false
}

type writer struct {
	enc     *json.Encoder
	err     error
	source  string // source harness, named in text-rendered calls
	thread  string
	ordinal int
	items   int
}

// turn writes one turn and returns the last assistant text it contained.
func (e *writer) turn(t turns.Turn) string {
	switch t.Kind {
	case turns.User:
		e.userMessage(t.At, t.Text)
	case turns.Summary:
		e.userMessage(t.At, "The conversation before this point was summarized:\n\n"+t.Text)
	case turns.Assistant:
		var last string
		kinds := map[string]tools.Kind{} // call ID -> kind, to pick the output record type
		for _, b := range t.Blocks {
			switch b.Kind {
			case turns.Text:
				e.agentMessage(t.At, b.Text)
				last = b.Text
			case turns.Call:
				kinds[b.CallID] = b.Action.Kind
				e.call(t.At, ids.CallID(b.CallID), b.Action)
			}
			// Reasoning is dropped; see Export.
		}
		for _, r := range t.Results {
			output := r.Output
			if r.IsError {
				output = "Error: " + output
			}
			e.output(r.At, kinds, r.CallID, output)
			delete(kinds, r.CallID)
		}
		// Codex rejects a call without an output, so calls the source never
		// answered (interrupted runs) get an explicit one.
		for _, b := range t.Blocks {
			if _, unanswered := kinds[b.CallID]; b.Kind == turns.Call && unanswered {
				e.output(t.At, kinds, b.CallID, "aborted: no result was recorded for this call")
			}
		}
		if len(t.Unmapped) > 0 {
			text := turns.UnmappedText(e.source, t.Unmapped)
			e.agentMessage(t.Unmapped[0].At, text)
			last = text
		}
		return last
	}
	return ""
}

func (e *writer) output(at time.Time, kinds map[string]tools.Kind, callID, output string) {
	record := "function_call_output"
	if kinds[callID] != tools.KindExec {
		record = "custom_tool_call_output"
	}
	e.line(at, "response_item", callOutputOut{Type: record, CallID: ids.CallID(callID), Output: output})
}

func (e *writer) call(at time.Time, id string, action tools.Action) {
	if action.Kind == tools.KindExec {
		args, _ := json.Marshal(struct {
			Cmd string `json:"cmd"`
		}{action.Command}) // marshaling a string field cannot fail
		e.line(at, "response_item", functionCallOut{Type: "function_call", Name: "exec_command", Arguments: string(args), CallID: id})
		return
	}
	e.line(at, "response_item", customCallOut{Type: "custom_tool_call", Status: "completed", CallID: id, Name: "apply_patch", Input: patch(action)})
}

// patch renders a write or edit as the Codex apply_patch format, the inverse
// of tools.parsePatch.
func patch(action tools.Action) string {
	var b strings.Builder
	b.WriteString("*** Begin Patch\n")
	if action.Kind == tools.KindWrite {
		fmt.Fprintf(&b, "*** Add File: %s\n", action.Path)
		for _, line := range lines(action.Content) {
			b.WriteString("+" + line + "\n")
		}
	} else {
		fmt.Fprintf(&b, "*** Update File: %s\n", action.Path)
		for _, edit := range action.Edits {
			b.WriteString("@@\n")
			for _, line := range lines(edit.Old) {
				b.WriteString("-" + line + "\n")
			}
			for _, line := range lines(edit.New) {
				b.WriteString("+" + line + "\n")
			}
		}
	}
	b.WriteString("*** End Patch")
	return b.String()
}

func lines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// userMessage and agentMessage write the response item the model sees and
// the events the Codex UI shows: the CLI replays legacy-history sessions
// from user_message/agent_message events (verified with codex 0.156.1),
// while Codex's own importer writes item_completed items, so both are kept.
func (e *writer) userMessage(at time.Time, text string) {
	e.line(at, "response_item", messageOut{Type: "message", Role: "user", Content: []contentOut{{Type: "input_text", Text: text}}})
	e.line(at, "event_msg", legacyMessageOut{Type: "user_message", Message: text, Images: json.RawMessage("[]")})
	e.item(at, itemOut{Type: "UserMessage", Content: []itemText{{Type: "text", Text: text, TextElements: json.RawMessage("[]")}}})
}

func (e *writer) agentMessage(at time.Time, text string) {
	e.line(at, "response_item", messageOut{Type: "message", Role: "assistant", Content: []contentOut{{Type: "output_text", Text: text}}})
	e.line(at, "event_msg", legacyMessageOut{Type: "agent_message", Message: text})
	e.item(at, itemOut{Type: "AgentMessage", Content: []itemText{{Type: "Text", Text: text}}})
}

func (e *writer) item(at time.Time, item itemOut) {
	e.items++
	item.ID = fmt.Sprintf("sesh-item-%d", e.items)
	e.line(at, "event_msg", itemCompletedOut{Type: "item_completed", ThreadID: e.thread, TurnID: importTurn, Item: item, CompletedAtMS: at.UnixMilli()})
}

// line writes one rollout line. The first error sticks; Export checks it.
func (e *writer) line(at time.Time, kind string, payload any) {
	if e.err != nil {
		return
	}
	err := e.enc.Encode(lineOut{Timestamp: timestamp(at), Ordinal: e.ordinal, Type: kind, Payload: payload})
	e.ordinal++
	if err != nil {
		e.err = fmt.Errorf("write codex rollout: %w", err)
	}
}

func timestamp(at time.Time) string {
	return at.UTC().Format("2006-01-02T15:04:05.000Z")
}

type lineOut struct {
	Timestamp string `json:"timestamp"`
	Ordinal   int    `json:"ordinal"`
	Type      string `json:"type"`
	Payload   any    `json:"payload"`
}

type sessionMetaOut struct {
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	CWD           string `json:"cwd"`
	Originator    string `json:"originator"`
	CLIVersion    string `json:"cli_version"`
	Source        string `json:"source"`
	ModelProvider string `json:"model_provider"`
}

type legacyMessageOut struct {
	Type    string          `json:"type"`
	Message string          `json:"message"`
	Images  json.RawMessage `json:"images,omitempty"` // user messages only
}

type taskEvent struct {
	Type             string `json:"type"`
	TurnID           string `json:"turn_id"`
	LastAgentMessage string `json:"last_agent_message,omitempty"`
}

type messageOut struct {
	Type    string       `json:"type"`
	Role    string       `json:"role"`
	Content []contentOut `json:"content"`
}

type contentOut struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type functionCallOut struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON text, as Codex records it
	CallID    string `json:"call_id"`
}

type customCallOut struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Input  string `json:"input"`
}

type callOutputOut struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type itemCompletedOut struct {
	Type          string  `json:"type"`
	ThreadID      string  `json:"thread_id"`
	TurnID        string  `json:"turn_id"`
	Item          itemOut `json:"item"`
	CompletedAtMS int64   `json:"completed_at_ms"`
}

type itemOut struct {
	Type    string     `json:"type"`
	ID      string     `json:"id"`
	Content []itemText `json:"content"`
}

type itemText struct {
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	TextElements json.RawMessage `json:"text_elements,omitempty"` // user messages only
}
