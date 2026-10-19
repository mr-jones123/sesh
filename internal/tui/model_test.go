package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mr-jones123/sesh/internal/catalog"
	"github.com/mr-jones123/sesh/internal/handoff"
)

func TestScopeChangeRejectsStaleListResult(t *testing.T) {
	m := testModel(t)
	m = update(t, m, listMsg{
		generation: 1,
		entries:    []catalog.Entry{{ID: "project", Harness: "claude"}},
	})

	var command tea.Cmd
	m, command = updateCommand(t, m, specialKey(tea.KeyTab))
	if command == nil {
		t.Fatal("scope change did not request a new catalog listing")
	}
	if m.scope != scopeAll || !m.loading || m.generation != 2 {
		t.Fatalf("scope change state = scope %v, loading %v, generation %d", m.scope, m.loading, m.generation)
	}

	m = update(t, m, listMsg{
		generation: 1,
		entries:    []catalog.Entry{{ID: "stale", Harness: "codex"}},
	})
	if !m.loading {
		t.Fatal("stale result ended the current loading state")
	}
	if len(m.entries) != 0 {
		t.Fatalf("stale result replaced entries: %#v", m.entries)
	}
}

func TestSearchTreatsQJKAsText(t *testing.T) {
	m := testModel(t)
	m = update(t, m, listMsg{
		generation: 1,
		entries: []catalog.Entry{
			{ID: "one", Harness: "claude", Prompt: "qjk one"},
			{ID: "two", Harness: "codex", Prompt: "qjk two"},
		},
	})
	m = update(t, m, textKey("/"))
	for _, character := range []string{"q", "j", "k"} {
		var command tea.Cmd
		m, command = updateCommand(t, m, textKey(character))
		if command != nil {
			t.Fatalf("search key %q unexpectedly returned a command", character)
		}
	}
	if !m.searching || m.cancelled {
		t.Fatalf("search state = searching %v, cancelled %v", m.searching, m.cancelled)
	}
	if m.search != "qjk" {
		t.Fatalf("search = %q, want qjk", m.search)
	}
	if len(m.visible) != 2 {
		t.Fatalf("visible sessions = %d, want 2", len(m.visible))
	}
}

func TestSavedSessionNameHeadsTheRowAndIsSearchable(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 120, 36
	m = update(t, m, listMsg{
		generation: 1,
		entries: []catalog.Entry{
			{ID: "one", Harness: "claude", Title: "Investigate flaky workers", Prompt: "raw opening prompt"},
			{ID: "two", Harness: "omp", Title: "Polish session picker", Prompt: "raw opening prompt"},
		},
	})
	content := m.View().Content
	titleAt := strings.Index(content, "Investigate flaky workers")
	promptAt := strings.Index(content, "raw opening prompt")
	if titleAt < 0 || promptAt < 0 || titleAt >= promptAt {
		t.Fatal("saved name did not precede the opening-prompt preview")
	}
	m = update(t, m, textKey("/"))
	m = update(t, m, textKey("POLISH"))
	if len(m.visible) != 1 || m.visible[0].ID != "two" {
		t.Fatalf("saved-name search selected %+v", m.visible)
	}
}

func TestUnnamedSessionFallsBackToOpeningPrompt(t *testing.T) {
	entry := catalog.Entry{Title: " \t", Prompt: "Continue the parser migration"}
	if got := sessionName(entry); got != entry.Prompt {
		t.Fatalf("untitled heading = %q, want its opening prompt", got)
	}
	entry.Title = "Native generated name"
	if got := sessionName(entry); got != entry.Title {
		t.Fatalf("named heading = %q, want persisted name", got)
	}
}

func TestConfirmationDoesNotCreateUntilY(t *testing.T) {
	m := testModel(t)
	m = update(t, m, listMsg{
		generation: 1,
		entries: []catalog.Entry{{
			ID:        "session-id",
			Harness:   "claude",
			Path:      "/sessions/source.jsonl",
			Workspace: "/work/project",
		}},
	})
	calls := 0
	m.create = func(_ context.Context, request handoff.Request) (handoff.Result, error) {
		calls++
		return handoff.Result{Path: "/sessions/target.jsonl", Workspace: request.Workspace}, nil
	}

	var command tea.Cmd
	m, command = updateCommand(t, m, specialKey(tea.KeyEnter))
	if command != nil || m.screen != screenTarget || calls != 0 {
		t.Fatalf("session choice wrote early: screen %v, command %v, calls %d", m.screen, command != nil, calls)
	}
	m, command = updateCommand(t, m, specialKey(tea.KeyEnter))
	if command != nil || m.screen != screenConfirm || calls != 0 {
		t.Fatalf("target choice wrote early: screen %v, command %v, calls %d", m.screen, command != nil, calls)
	}
	m, command = updateCommand(t, m, specialKey(tea.KeyEnter))
	if command != nil || m.screen != screenConfirm || calls != 0 {
		t.Fatalf("Enter confirmed unexpectedly: screen %v, command %v, calls %d", m.screen, command != nil, calls)
	}
	m, command = updateCommand(t, m, textKey("y"))
	if command == nil || m.screen != screenCreating || calls != 0 {
		t.Fatalf("explicit confirmation state = screen %v, command %v, calls %d", m.screen, command != nil, calls)
	}
	message := command()
	if calls != 1 {
		t.Fatalf("create calls = %d, want 1", calls)
	}
	if _, ok := message.(createMsg); !ok {
		t.Fatalf("create command returned %T", message)
	}
}
func TestConfirmationRequiresVisibleDetails(t *testing.T) {
	m := testModel(t)
	m.screen = screenConfirm
	m.hasSelected = true
	m.selected = catalog.Entry{Harness: "claude", ID: "session-id", Workspace: "/work/project"}
	m.width = 31
	m.height = 24
	calls := 0
	m.create = func(context.Context, handoff.Request) (handoff.Result, error) {
		calls++
		return handoff.Result{}, nil
	}

	m, command := updateCommand(t, m, textKey("y"))
	if command != nil || calls != 0 || m.screen != screenConfirm {
		t.Fatalf("hidden confirmation created session: screen %v, command %v, calls %d", m.screen, command != nil, calls)
	}
}

func TestEscapeClearsRetainedFilterBeforeQuitting(t *testing.T) {
	m := testModel(t)
	m.entries = []catalog.Entry{
		{ID: "one", Prompt: "first"},
		{ID: "two", Prompt: "second"},
	}
	m.search = "first"
	m.rebuildVisible()

	m = update(t, m, specialKey(tea.KeyEscape))
	if m.cancelled || m.search != "" || len(m.visible) != 2 {
		t.Fatalf("Escape state = cancelled %v, search %q, visible %d", m.cancelled, m.search, len(m.visible))
	}
}

func TestCancelDuringCreateWaitsAndDoesNotReturnLaunchableResult(t *testing.T) {
	m := testModel(t)
	m.screen = screenCreating

	var command tea.Cmd
	m, command = updateCommand(t, m, textKey("q"))
	if command != nil || !m.createCancelPending {
		t.Fatalf("create cancellation = command %v, pending %v", command != nil, m.createCancelPending)
	}

	result := handoff.Result{
		Path:      "/sessions/target.jsonl",
		Workspace: "/work/project",
		Command:   []string{"pi", "--session", "/sessions/target.jsonl"},
	}
	m, command = updateCommand(t, m, createMsg{result: result})
	if command == nil {
		t.Fatal("completed cancellation did not quit")
	}
	if m.result != nil || m.runErr == nil {
		t.Fatalf("completed cancellation returned result %v, error %v", m.result != nil, m.runErr)
	}
	for _, recovery := range []string{result.Path, "pi"} {
		if !strings.Contains(m.runErr.Error(), recovery) {
			t.Fatalf("cancellation error %q omitted recovery value %q", m.runErr, recovery)
		}
	}
}

func TestSafeTextRemovesTerminalControls(t *testing.T) {
	got := safeText("prompt\x1b]52;c;secret\a\nnext\u009b31m\u202e")
	for _, forbidden := range []string{"\x1b", "\a", "\n", "\u009b", "\u202e"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("safeText(%q) retained control %q", got, forbidden)
		}
	}
}

func testModel(t *testing.T) model {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := newModel(ctx, cancel, Options{Dir: "/work/project"})
	m.list = func(context.Context, catalog.Query) ([]catalog.Entry, error) { return nil, nil }
	return m
}

func update(t *testing.T, m model, message tea.Msg) model {
	t.Helper()
	updated, _ := m.Update(message)
	result, ok := updated.(model)
	if !ok {
		t.Fatalf("updated model has type %T", updated)
	}
	return result
}

func updateCommand(t *testing.T, m model, message tea.Msg) (model, tea.Cmd) {
	t.Helper()
	updated, command := m.Update(message)
	result, ok := updated.(model)
	if !ok {
		t.Fatalf("updated model has type %T", updated)
	}
	return result, command
}

func textKey(text string) tea.KeyPressMsg {
	runes := []rune(text)
	return tea.KeyPressMsg(tea.Key{Text: text, Code: runes[0]})
}

func specialKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}
