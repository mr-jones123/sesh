// Package handoff writes local session history into another harness's native store.
package handoff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mr-jones123/sesh/internal/catalog"
	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/ids"
	"github.com/mr-jones123/sesh/internal/registry"
	"github.com/mr-jones123/sesh/internal/session"
)

type Request struct {
	Source    catalog.Entry
	Target    string
	Workspace string
	Model     string
}

type Result struct {
	Path      string
	SessionID string
	Workspace string
	Command   []string
}

// Create preserves the source and writes a new native session. It never launches an agent.
func Create(ctx context.Context, req Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if req.Source.Err != nil {
		return Result{}, fmt.Errorf("source session: %w", req.Source.Err)
	}
	exporter, err := registry.FindExporter(req.Target)
	if err != nil {
		return Result{}, err
	}
	if req.Model != "" && req.Target != "pi" {
		return Result{}, fmt.Errorf("--model is supported only for Pi")
	}
	adapter, err := registry.Find(req.Source.Harness)
	if err != nil {
		return Result{}, err
	}
	s, err := adapter.Import(ctx, req.Source.Path)
	if err != nil {
		return Result{}, fmt.Errorf("read %s session: %w", adapter.Name(), err)
	}
	if len(s.Events) == 0 {
		return Result{}, fmt.Errorf("source session has no conversation to hand off")
	}
	workspace := req.Workspace
	if workspace == "" {
		workspace = s.Workspace
	}
	if workspace == "" {
		return Result{}, fmt.Errorf("source has no workspace; pass --workspace")
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return Result{}, fmt.Errorf("resolve workspace: %w", err)
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return Result{}, fmt.Errorf("workspace unavailable; pass --workspace: %w", err)
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return Result{}, fmt.Errorf("workspace: %w", err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("workspace %q is not a directory", workspace)
	}
	created := time.Now()
	opts := harness.ExportOptions{Workspace: workspace, Model: req.Model, SessionID: ids.NewV7(created), CreatedAt: created}
	installer, ok := exporter.(harness.Installer)
	if !ok {
		return Result{}, fmt.Errorf("%s has no native session location", req.Target)
	}
	path, err := installer.InstallPath(s, opts)
	if err != nil {
		return Result{}, fmt.Errorf("locate target session: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return Result{}, fmt.Errorf("resolve target path: %w", err)
	}
	result := Result{Path: path, SessionID: opts.SessionID, Workspace: workspace}
	switch req.Target {
	case "claude":
		result.Command = []string{"claude", "--resume", opts.SessionID}
	case "codex":
		result.Command = []string{"codex", "resume", opts.SessionID}
	case "pi":
		result.Command = []string{"pi", "--session", path}
	case "omp":
		result.Command = []string{"omp", "--resume", path}
	default:
		return Result{}, fmt.Errorf("unsupported target %q", req.Target)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Result{}, fmt.Errorf("create target directory: %w", err)
	}
	if err := writeNative(ctx, exporter, s, opts, path); err != nil {
		return Result{}, err
	}
	return result, nil
}

func writeNative(ctx context.Context, exporter harness.Exporter, s session.Session, opts harness.ExportOptions, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create native session: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = out.Close()
			_ = os.Remove(path)
		}
	}()
	if err := exporter.Export(ctx, s, opts, out); err != nil {
		return fmt.Errorf("write %s session: %w", exporter.Name(), err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close native session: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	complete = true
	return nil
}
