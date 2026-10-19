package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/mr-jones123/sesh/internal/catalog"
	"github.com/mr-jones123/sesh/internal/handoff"
)

type listFunc func(context.Context, catalog.Query) ([]catalog.Entry, error)
type createFunc func(context.Context, handoff.Request) (handoff.Result, error)

type listMsg struct {
	generation uint64
	entries    []catalog.Entry
	err        error
}

type createMsg struct {
	result handoff.Result
	err    error
}

func loadSessions(ctx context.Context, generation uint64, query catalog.Query, list listFunc) tea.Cmd {
	return func() tea.Msg {
		entries, err := list(ctx, query)
		return listMsg{generation: generation, entries: entries, err: err}
	}
}

func createSession(ctx context.Context, request handoff.Request, create createFunc) tea.Cmd {
	return func() tea.Msg {
		result, err := create(ctx, request)
		return createMsg{result: result, err: err}
	}
}
