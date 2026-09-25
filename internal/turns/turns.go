// Package turns groups a session's active branch into turns every target
// harness can render. Pi, Claude, and Codex all require each assistant tool
// call to be answered before the conversation moves on, but sources record
// calls and results in different orders: Claude streams parallel calls and
// results interleaved (call A, call B, result A, call C, ...). Build buffers
// each turn until all of its calls are answered.
package turns

import (
	"fmt"
	"strings"
	"time"

	"github.com/mr-jones123/sesh/internal/session"
	"github.com/mr-jones123/sesh/internal/tools"
)

type Kind int

const (
	User      Kind = iota // Text is what the user typed
	Assistant             // Blocks, Results, Unmapped
	Summary               // Text is a compaction summary
)

type Turn struct {
	Kind     Kind
	At       time.Time
	Text     string     // User, Summary
	Model    string     // Assistant: source model, may be empty
	Blocks   []Block    // Assistant: text, reasoning, and supported calls in order
	Results  []Result   // Assistant: results for the calls in Blocks, in arrival order
	Unmapped []Unmapped // Assistant: calls the target has no tool for
}

type BlockKind int

const (
	Text BlockKind = iota
	Reasoning
	Call
)

type Block struct {
	Kind   BlockKind
	Text   string       // Text, Reasoning
	CallID string       // Call
	Action tools.Action // Call
}

type Result struct {
	CallID  string
	Output  string
	IsError bool
	At      time.Time
}

// Unmapped is a call the target cannot express as one of its own tools. It
// is rendered as text after the turn's results, so it never sits between a
// tool call and its result.
type Unmapped struct {
	Name    string // source tool name
	Args    string // source arguments as JSON
	Output  string
	IsError bool
	Done    bool // a result was recorded
	At      time.Time
}

// Build groups the active branch of s into turns. supports reports whether
// the target has a tool for an action; other calls become Unmapped.
func Build(s session.Session, supports func(tools.Action) bool) []Turn {
	b := &builder{
		harness:  s.Harness,
		start:    s.CreatedAt,
		supports: supports,
		pending:  map[string]bool{},
		inTurn:   map[string]bool{},
		unmapped: map[string]*Unmapped{},
	}
	for _, event := range ActiveBranch(s.Events) {
		b.add(event)
	}
	b.flush()
	return b.turns
}

// ActiveBranch returns the path from the root to the last event. Branches
// the source abandoned (rewinds, edits) are left out.
func ActiveBranch(events []session.Event) []session.Event {
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

// UnmappedText renders calls the target has no tool for as one message.
func UnmappedText(harness string, calls []Unmapped) string {
	var text strings.Builder
	for i, call := range calls {
		if i > 0 {
			text.WriteString("\n\n")
		}
		fmt.Fprintf(&text, "[Tool call from %s: %s]\n%s", harness, call.Name, call.Args)
		switch {
		case !call.Done:
			text.WriteString("\n\n(no result recorded)")
		case call.IsError:
			fmt.Fprintf(&text, "\n\nResult (error):\n%s", call.Output)
		default:
			fmt.Fprintf(&text, "\n\nResult:\n%s", call.Output)
		}
	}
	return text.String()
}

type builder struct {
	harness  string
	start    time.Time
	supports func(tools.Action) bool
	turns    []Turn

	open      *Turn                // assistant turn being built
	deferred  []*Unmapped          // its unmapped calls, filled in as results arrive
	pending   map[string]bool      // its calls still waiting for a result
	inTurn    map[string]bool      // its supported calls
	sawResult bool                 // it has entered its tool-result phase
	unmapped  map[string]*Unmapped // its unmapped calls by call ID
}

func (b *builder) add(event session.Event) {
	at := event.CreatedAt
	if at.IsZero() {
		at = b.start
	}
	switch event.Type {
	case session.EventMessage:
		switch event.Role {
		case session.RoleUser:
			b.flush()
			b.turns = append(b.turns, Turn{Kind: User, At: at, Text: event.Text})
		case session.RoleAssistant:
			turn := b.assistant(event, at)
			turn.Blocks = append(turn.Blocks, Block{Kind: Text, Text: event.Text})
		}
		// System context (Codex developer messages, Pi system prompts)
		// belongs to the source harness; each target supplies its own.
	case session.EventReasoning:
		turn := b.assistant(event, at)
		turn.Blocks = append(turn.Blocks, Block{Kind: Reasoning, Text: event.Text})
	case session.EventToolCall:
		b.call(event, at)
	case session.EventToolResult:
		b.result(event, at)
	case session.EventSummary:
		b.flush()
		b.turns = append(b.turns, Turn{Kind: Summary, At: at, Text: event.Text})
	}
}

// assistant returns the turn an assistant event belongs to. Output after
// all of a turn's results starts a new turn; output while calls are still
// unanswered is part of the same streamed response.
func (b *builder) assistant(event session.Event, at time.Time) *Turn {
	if b.sawResult && len(b.pending) == 0 {
		b.flush()
	}
	if b.open == nil {
		b.open = &Turn{Kind: Assistant, At: at, Model: event.Model}
	}
	return b.open
}

func (b *builder) call(event session.Event, at time.Time) {
	call := event.Call
	turn := b.assistant(event, at)
	b.pending[call.ID] = true
	action := tools.Parse(b.harness, call.Name, call.Args)
	if action.Kind == tools.KindOther || !b.supports(action) {
		u := &Unmapped{Name: call.Name, Args: string(call.Args), At: at}
		b.unmapped[call.ID] = u
		b.deferred = append(b.deferred, u)
		return
	}
	b.inTurn[call.ID] = true
	turn.Blocks = append(turn.Blocks, Block{Kind: Call, CallID: call.ID, Action: action})
}

func (b *builder) result(event session.Event, at time.Time) {
	result := event.Result
	b.sawResult = true
	delete(b.pending, result.CallID)
	if b.inTurn[result.CallID] {
		b.open.Results = append(b.open.Results, Result{CallID: result.CallID, Output: result.Output, IsError: result.IsError, At: at})
		return
	}
	u, ok := b.unmapped[result.CallID]
	if !ok {
		// The call is off the active branch or in an already closed turn;
		// keep the output as text rather than as an unpaired result.
		if b.open == nil {
			b.open = &Turn{Kind: Assistant, At: at}
		}
		u = &Unmapped{Name: result.Name, At: at}
		b.deferred = append(b.deferred, u)
	}
	u.Output, u.IsError, u.Done = result.Output, result.IsError, true
}

// flush closes the open assistant turn. Calls still pending were never
// answered in the source.
func (b *builder) flush() {
	if b.open != nil {
		for _, u := range b.deferred {
			b.open.Unmapped = append(b.open.Unmapped, *u)
		}
		if len(b.open.Blocks) > 0 || len(b.open.Unmapped) > 0 {
			b.turns = append(b.turns, *b.open)
		}
	}
	b.open, b.deferred, b.sawResult = nil, nil, false
	clear(b.pending)
	clear(b.inTurn)
	clear(b.unmapped)
}
