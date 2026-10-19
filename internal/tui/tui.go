// Package tui provides the interactive local-session handoff picker.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"

	"github.com/mr-jones123/sesh/internal/handoff"
)

// Options selects the sessions initially shown by Run.
type Options struct {
	Dir     string
	All     bool
	Harness string
}

// Run opens the session picker. It returns nil when the user cancels and a
// result only after the user confirms and the native target session is written.
// The caller is responsible for launching the target after Run restores the terminal.
func Run(ctx context.Context, opts Options, stdin io.Reader, stdout, stderr io.Writer) (*handoff.Result, error) {
	_ = stderr
	operationCtx, cancelOperations := context.WithCancel(ctx)
	defer cancelOperations()

	initial := newModel(operationCtx, cancelOperations, opts)
	program := tea.NewProgram(
		initial,
		tea.WithContext(ctx),
		tea.WithInput(stdin),
		tea.WithOutput(stdout),
	)
	finalModel, err := program.Run()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, tea.ErrInterrupted) {
			return nil, nil
		}
		return nil, err
	}
	final, ok := finalModel.(model)
	if !ok {
		return nil, fmt.Errorf("unexpected TUI model %T", finalModel)
	}
	if final.runErr != nil {
		return nil, final.runErr
	}
	if final.result != nil {
		return final.result, nil
	}
	return nil, nil
}
