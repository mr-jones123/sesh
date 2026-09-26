package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"time"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/ids"
	"github.com/mr-jones123/sesh/internal/session"
	"github.com/mr-jones123/sesh/internal/tools"
	"github.com/mr-jones123/sesh/internal/turns"
)

// Export writes bundle as a Claude Code session: one JSONL record per
// message, chained by parentUuid. Shell, read, write, and edit calls become
// Bash, Read, Write, and Edit; an edit with several replacements becomes one
// Edit call per replacement, each answered with the recorded result. Other
// tools are kept as text. Reasoning is dropped: Claude thinking blocks carry
// Anthropic signatures that cannot be produced for another model's output.
func (Adapter) Export(ctx context.Context, bundle session.Bundle, opts harness.ExportOptions, w io.Writer) error {
	if opts.SessionID == "" || opts.CreatedAt.IsZero() {
		return fmt.Errorf("claude export needs a session ID and creation time")
	}
	out := bufio.NewWriter(w)
	e := &writer{
		enc:       json.NewEncoder(out),
		source:    bundle.Session.Harness,
		sessionID: opts.SessionID,
		workspace: workspace(bundle, opts),
	}
	e.enc.SetEscapeHTML(false)
	for _, turn := range turns.Build(bundle.Session, supported) {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.turn(turn)
	}
	if e.err != nil {
		return e.err
	}
	return out.Flush()
}

// InstallPath places the session where `claude --resume` looks for it:
// projects/<workspace with every non-alphanumeric replaced by '-'>/<id>.jsonl
// under $CLAUDE_CONFIG_DIR (default ~/.claude).
func (Adapter) InstallPath(bundle session.Bundle, opts harness.ExportOptions) (string, error) {
	home, err := claudeHome()
	if err != nil {
		return "", err
	}
	dir := workspace(bundle, opts)
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("claude sessions need an absolute workspace; got %q (use -workspace)", dir)
	}
	return filepath.Join(home, "projects", projectSlug(dir), opts.SessionID+".jsonl"), nil
}

func (Adapter) ResumeCommand(bundle session.Bundle, opts harness.ExportOptions) string {
	return fmt.Sprintf("cd %s && claude --resume %s", tools.ShellQuote(workspace(bundle, opts)), opts.SessionID)
}

var nonAlphanumeric = regexp.MustCompile(`[^A-Za-z0-9]`)

// projectSlug names Claude's per-project session directory.
func projectSlug(dir string) string {
	return nonAlphanumeric.ReplaceAllString(dir, "-")
}

// workspace is the target working directory. Claude files sessions under
// the resolved path of its cwd (/tmp is /private/tmp on macOS), so an
// existing directory is resolved the same way.
func workspace(bundle session.Bundle, opts harness.ExportOptions) string {
	dir := opts.Workspace
	if dir == "" {
		dir = bundle.Session.Workspace
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

func supported(action tools.Action) bool {
	switch action.Kind {
	case tools.KindExec, tools.KindRead, tools.KindWrite, tools.KindEdit:
		return true
	}
	return false
}

type writer struct {
	enc       *json.Encoder
	err       error
	source    string // source harness, named in text-rendered calls
	sessionID string
	workspace string
	parent    *string
	messages  int
}

func (e *writer) turn(t turns.Turn) {
	switch t.Kind {
	case turns.User:
		e.user(t.At, t.Text)
	case turns.Summary:
		e.user(t.At, "The conversation before this point was summarized:\n\n"+t.Text)
	case turns.Assistant:
		var content []outBlock
		calls := map[string][]string{} // source call ID -> Claude tool_use IDs
		var order []string             // source call IDs in call order
		for _, b := range t.Blocks {
			switch b.Kind {
			case turns.Text:
				content = append(content, outBlock{Type: "text", Text: b.Text})
			case turns.Call:
				uses := e.toolUses(b.CallID, b.Action)
				for _, use := range uses {
					calls[b.CallID] = append(calls[b.CallID], use.ID)
				}
				order = append(order, b.CallID)
				content = append(content, uses...)
			}
			// Reasoning is dropped; see Export.
		}
		if len(content) > 0 {
			stop := "end_turn"
			if len(calls) > 0 {
				stop = "tool_use"
			}
			e.assistant(t.At, t.Model, content, stop)
		}
		if len(calls) > 0 {
			e.toolResults(t, calls, order)
		}
		if len(t.Unmapped) > 0 {
			e.assistant(t.Unmapped[0].At, "", []outBlock{{Type: "text", Text: turns.UnmappedText(e.source, t.Unmapped)}}, "end_turn")
		}
	}
}

// toolUses renders one action as Claude tool_use blocks: one per
// replacement for edits, one otherwise.
func (e *writer) toolUses(callID string, action tools.Action) []outBlock {
	id := ids.CallID(callID)
	use := func(suffix, name string, input any) outBlock {
		data, _ := json.Marshal(input) // plain structs of strings and ints
		return outBlock{Type: "tool_use", ID: splitID(id, suffix), Name: name, Input: data}
	}
	switch action.Kind {
	case tools.KindExec:
		return []outBlock{use("", "Bash", bashInput{Command: action.Command})}
	case tools.KindRead:
		return []outBlock{use("", "Read", readInput{FilePath: e.abs(action.Path), Offset: action.Offset, Limit: action.Limit})}
	case tools.KindWrite:
		return []outBlock{use("", "Write", writeInput{FilePath: e.abs(action.Path), Content: action.Content})}
	}
	uses := make([]outBlock, len(action.Edits))
	for i, edit := range action.Edits {
		suffix := ""
		if i > 0 {
			suffix = fmt.Sprintf("-%d", i+1)
		}
		uses[i] = use(suffix, "Edit", editInput{FilePath: e.abs(action.Path), OldString: edit.Old, NewString: edit.New})
	}
	return uses
}

// toolResults writes one user record answering every tool_use of the turn.
// A split edit repeats its single recorded result; a call the source never
// answered gets an interruption result, since Claude rejects unanswered
// tool_use blocks.
func (e *writer) toolResults(t turns.Turn, calls map[string][]string, order []string) {
	results := map[string]turns.Result{}
	for _, r := range t.Results {
		results[r.CallID] = r
	}
	at := t.At
	var content []outBlock
	for _, callID := range order {
		r, ok := results[callID]
		output, isError := r.Output, r.IsError
		if ok {
			at = r.At
		} else {
			output, isError = "[Request interrupted: no result was recorded for this call]", true
		}
		for _, useID := range calls[callID] {
			content = append(content, outBlock{Type: "tool_result", ToolUseID: useID, Content: output, IsError: &isError})
		}
	}
	e.record(at, "user", messageOut{Role: "user", Content: content})
}

// abs makes a tool path absolute; Claude's file tools require it.
func (e *writer) abs(path string) string {
	if filepath.IsAbs(path) || e.workspace == "" {
		return path
	}
	return filepath.Join(e.workspace, path)
}

// splitID appends suffix to a tool_use ID, keeping it within Claude's 64
// character limit.
func splitID(id, suffix string) string {
	if len(id)+len(suffix) > 64 {
		id = id[:64-len(suffix)]
	}
	return id + suffix
}

func (e *writer) user(at time.Time, text string) {
	content, _ := json.Marshal(text)
	e.record(at, "user", messageOut{Role: "user", Content: json.RawMessage(content)})
}

func (e *writer) assistant(at time.Time, model string, content []outBlock, stop string) {
	if model == "" {
		model = e.source
	}
	e.messages++
	e.record(at, "assistant", messageOut{
		ID:         fmt.Sprintf("msg_sesh_%d", e.messages),
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		Content:    content,
		StopReason: stop,
		Usage:      &usageOut{},
	})
}

// record writes one Claude record chained to the previous one. The first
// error sticks; Export checks it.
func (e *writer) record(at time.Time, kind string, message messageOut) {
	if e.err != nil {
		return
	}
	if e.parent == nil && kind == "assistant" {
		// The Messages API requires the conversation to open with a user
		// turn; some sources start with the assistant.
		e.user(at, "[Imported by sesh: this conversation begins with an assistant message.]")
	}
	id := ids.NewV7(at)
	err := e.enc.Encode(recordOut{
		ParentUUID:  e.parent,
		IsSidechain: false,
		Type:        kind,
		Message:     message,
		UUID:        id,
		Timestamp:   at.UTC().Format("2006-01-02T15:04:05.000Z"),
		UserType:    "external",
		Entrypoint:  "cli",
		CWD:         e.workspace,
		SessionID:   e.sessionID,
	})
	if err != nil {
		e.err = fmt.Errorf("write claude session: %w", err)
		return
	}
	e.parent = &id
}

type recordOut struct {
	ParentUUID  *string    `json:"parentUuid"` // null for the first record
	IsSidechain bool       `json:"isSidechain"`
	Type        string     `json:"type"`
	Message     messageOut `json:"message"`
	UUID        string     `json:"uuid"`
	Timestamp   string     `json:"timestamp"`
	UserType    string     `json:"userType"`
	Entrypoint  string     `json:"entrypoint"`
	CWD         string     `json:"cwd"`
	SessionID   string     `json:"sessionId"`
}

type messageOut struct {
	ID         string    `json:"id,omitempty"`
	Type       string    `json:"type,omitempty"`
	Role       string    `json:"role"`
	Model      string    `json:"model,omitempty"`
	Content    any       `json:"content"` // a string for typed user text, else []outBlock
	StopReason string    `json:"stop_reason,omitempty"`
	Usage      *usageOut `json:"usage,omitempty"`
}

type outBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   *bool           `json:"is_error,omitempty"`
}

// usageOut is zero: imported turns cost nothing in this session.
type usageOut struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type bashInput struct {
	Command string `json:"command"`
}

type readInput struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type writeInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type editInput struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}
