package omp

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/harness/pi"
	"github.com/mr-jones123/sesh/internal/session"
)

// Adapter reads and writes OMP's Pi-family native transcript grammar.
type Adapter struct{}

func New() Adapter           { return Adapter{} }
func (Adapter) Name() string { return "omp" }

func (Adapter) Detect(path string) bool {
	if !strings.HasSuffix(path, ".jsonl") {
		return false
	}
	if root, err := agentDir(); err == nil && pathWithin(path, filepath.Join(root, "sessions")) {
		return true
	}
	return strings.Contains(filepath.ToSlash(path), "/.omp/agent/sessions/")
}

// Import delegates the shared Pi-family event grammar, while preserving OMP
// as the source identity used by later tool translation and handoff display.
func (Adapter) Import(ctx context.Context, path string) (session.Session, error) {
	s, err := pi.New().Import(ctx, path)
	if err != nil {
		return session.Session{}, err
	}
	s.Harness = "omp"
	return s, nil
}

func (Adapter) Summarize(ctx context.Context, path string) (harness.Summary, error) {
	return pi.New().Summarize(ctx, path)
}
