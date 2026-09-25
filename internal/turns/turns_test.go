package turns

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mr-jones123/sesh/internal/session"
	"github.com/mr-jones123/sesh/internal/tools"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// chain links events in order, as importers do for linear transcripts.
func chain(events ...session.Event) []session.Event {
	for i := range events {
		events[i].ID = fmt.Sprintf("e%d", i)
		if i > 0 {
			events[i].ParentID = events[i-1].ID
		}
		events[i].CreatedAt = t0.Add(time.Duration(i) * time.Second)
	}
	return events
}

func call(id, name, args string) session.Event {
	return session.Event{Type: session.EventToolCall, Role: session.RoleAssistant, Call: &session.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}}
}

func result(id, output string) session.Event {
	return session.Event{Type: session.EventToolResult, Role: session.RoleTool, Result: &session.ToolResult{CallID: id, Output: output}}
}

func text(role session.Role, value string) session.Event {
	return session.Event{Type: session.EventMessage, Role: role, Text: value}
}

func all(tools.Action) bool { return true }

// shape summarizes turns as "user", "summary", or
// "assistant[blocks|results|unmapped]" for readable assertions.
func shape(turns []Turn) string {
	var parts []string
	for _, t := range turns {
		switch t.Kind {
		case User:
			parts = append(parts, "user")
		case Summary:
			parts = append(parts, "summary")
		case Assistant:
			var blocks, results, unmapped []string
			for _, b := range t.Blocks {
				switch b.Kind {
				case Text:
					blocks = append(blocks, "text")
				case Reasoning:
					blocks = append(blocks, "reasoning")
				case Call:
					blocks = append(blocks, "call:"+b.CallID)
				}
			}
			for _, r := range t.Results {
				results = append(results, r.CallID)
			}
			for _, u := range t.Unmapped {
				unmapped = append(unmapped, u.Name)
			}
			parts = append(parts, fmt.Sprintf("assistant[%s|%s|%s]", strings.Join(blocks, ","), strings.Join(results, ","), strings.Join(unmapped, ",")))
		}
	}
	return strings.Join(parts, " ")
}

func build(harness string, supports func(tools.Action) bool, events ...session.Event) []Turn {
	return Build(session.Session{Harness: harness, CreatedAt: t0, Events: chain(events...)}, supports)
}

func TestBuildGroupsInterleavedParallelCalls(t *testing.T) {
	// Claude streams parallel calls and results interleaved; targets need
	// one assistant turn with every call, then every result.
	got := build("claude", all,
		call("a", "Bash", `{"command":"a"}`),
		call("b", "Bash", `{"command":"b"}`),
		result("a", "A"),
		call("c", "Bash", `{"command":"c"}`),
		result("b", "B"),
		result("c", "C"),
		text(session.RoleAssistant, "all done"),
	)
	if want := "assistant[call:a,call:b,call:c|a,b,c|] assistant[text||]"; shape(got) != want {
		t.Fatalf("turns = %s, want %s", shape(got), want)
	}
}

func TestBuildDefersUnmappedCallsWithTheirResults(t *testing.T) {
	got := build("claude", all,
		text(session.RoleUser, "go"),
		call("web", "WebSearch", `{"query":"q"}`),
		call("sh", "Bash", `{"command":"ls"}`),
		result("web", "found it"),
		result("sh", "a.go"),
		text(session.RoleAssistant, "Done."),
	)
	if want := "user assistant[call:sh|sh|WebSearch] assistant[text||]"; shape(got) != want {
		t.Fatalf("turns = %s, want %s", shape(got), want)
	}
	if u := got[1].Unmapped[0]; !u.Done || u.Output != "found it" || u.Args != `{"query":"q"}` {
		t.Fatalf("unmapped = %#v", u)
	}
}

func TestBuildTreatsUnsupportedActionsAsUnmapped(t *testing.T) {
	// A target without a read tool (Codex) keeps reads as text.
	noRead := func(a tools.Action) bool { return a.Kind != tools.KindRead }
	got := build("claude", noRead,
		call("r", "Read", `{"file_path":"a.go"}`),
		result("r", "package main"),
	)
	if want := "assistant[||Read]"; shape(got) != want {
		t.Fatalf("turns = %s, want %s", shape(got), want)
	}
}

func TestBuildKeepsLateResultsAsText(t *testing.T) {
	// A result arriving after its turn closed must not become an unpaired
	// tool result in the next turn.
	got := build("claude", all,
		call("a", "Bash", `{"command":"a"}`),
		text(session.RoleUser, "stop"),
		result("a", "late"),
	)
	if want := "assistant[call:a||] user assistant[||]"; shape(got) != want {
		t.Fatalf("turns = %s, want %s", shape(got), want)
	}
	if u := got[2].Unmapped[0]; u.Output != "late" || !u.Done {
		t.Fatalf("late result = %#v", u)
	}
}

func TestBuildFollowsActiveBranch(t *testing.T) {
	events := chain(text(session.RoleUser, "first"), text(session.RoleAssistant, "abandoned"), text(session.RoleAssistant, "kept"))
	events[2].ParentID = events[0].ID // the user rewound and the assistant answered again
	got := Build(session.Session{Harness: "claude", CreatedAt: t0, Events: events}, all)
	if len(got) != 2 || got[1].Blocks[0].Text != "kept" {
		t.Fatalf("turns = %s", shape(got))
	}
}

func TestUnmappedText(t *testing.T) {
	got := UnmappedText("claude", []Unmapped{
		{Name: "WebSearch", Args: `{"query":"q"}`, Output: "hit", Done: true},
		{Name: "Agent", Args: `{}`, Output: "boom", IsError: true, Done: true},
		{Name: "Monitor", Args: `{}`},
	})
	for _, want := range []string{"[Tool call from claude: WebSearch]\n{\"query\":\"q\"}\n\nResult:\nhit", "Result (error):\nboom", "(no result recorded)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("text = %q, missing %q", got, want)
		}
	}
}
