package handoff

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mr-jones123/sesh/internal/catalog"
	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/registry"
	"github.com/mr-jones123/sesh/internal/session"
)

func sourceFixture(t *testing.T) (catalog.Entry, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi"))
	t.Setenv("SESH_OMP_AGENT_DIR", filepath.Join(root, "omp"))
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "source.jsonl")
	content := "keep /Users/alice/private and alice@company.com unchanged"
	rows := []map[string]any{
		{"type": "user", "uuid": "u1", "sessionId": "original", "cwd": workspace, "timestamp": "2026-01-01T00:00:00Z", "message": map[string]any{"role": "user", "content": content}},
		{"type": "assistant", "uuid": "a1", "parentUuid": "u1", "sessionId": "original", "timestamp": "2026-01-01T00:00:01Z", "message": map[string]any{"role": "assistant", "content": []map[string]any{{"type": "text", "text": "continuing"}}}},
	}
	var raw bytes.Buffer
	for _, row := range rows {
		if err := json.NewEncoder(&raw).Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, raw.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return catalog.Entry{Harness: "claude", Path: path}, content
}

func TestCreatePreservesHistoryAndWorkspaceInEachNativeStore(t *testing.T) {
	for _, target := range []string{"claude", "codex", "pi", "omp"} {
		t.Run(target, func(t *testing.T) {
			source, content := sourceFixture(t)
			before, _ := os.ReadFile(source.Path)
			result, err := Create(context.Background(), Request{Source: source, Target: target})
			if err != nil {
				t.Fatal(err)
			}
			adapter, err := registry.Find(target)
			if err != nil {
				t.Fatal(err)
			}
			s, err := adapter.Import(context.Background(), result.Path)
			if err != nil {
				t.Fatal(err)
			}
			if s.ID != result.SessionID || s.ID == "original" || s.Workspace != result.Workspace {
				t.Fatalf("native identity/workspace mismatch: %+v, result %+v", s, result)
			}
			if len(s.Events) != 2 || s.Events[0].Text != content || s.Events[1].Text != "continuing" {
				t.Fatalf("history changed: %+v", s.Events)
			}
			after, _ := os.ReadFile(source.Path)
			if !bytes.Equal(before, after) {
				t.Fatal("source was modified")
			}
			info, err := os.Stat(result.Path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("private file permissions: %v, %v", info, err)
			}
			second, err := Create(context.Background(), Request{Source: source, Target: target})
			if err != nil {
				t.Fatal(err)
			}
			if second.Path == result.Path || second.SessionID == result.SessionID {
				t.Fatal("repeat handoff reused the target identity")
			}
		})
	}
}

func TestCreateRequiresAvailableWorkspaceOrExplicitOverride(t *testing.T) {
	source, _ := sourceFixture(t)
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := Create(context.Background(), Request{Source: source, Target: "pi", Workspace: missing}); err == nil {
		t.Fatal("accepted a missing workspace")
	}
	override, _ := filepath.EvalSymlinks(t.TempDir())
	result, err := Create(context.Background(), Request{Source: source, Target: "pi", Workspace: override})
	if err != nil {
		t.Fatal(err)
	}
	adapter, _ := registry.Find("pi")
	s, err := adapter.Import(context.Background(), result.Path)
	if err != nil || s.Workspace != override {
		t.Fatalf("override did not reach native session: %+v, %v", s, err)
	}
}

type brokenExporter struct{}

func (brokenExporter) Name() string { return "broken" }
func (brokenExporter) Export(_ context.Context, _ session.Session, _ harness.ExportOptions, w io.Writer) error {
	if _, err := io.WriteString(w, "partial history"); err != nil {
		return err
	}
	return errors.New("export interrupted")
}

func TestNativeWriteNeverOverwritesAndRemovesFailedOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("existing session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeNative(context.Background(), brokenExporter{}, session.Session{}, harness.ExportOptions{}, path); err == nil {
		t.Fatal("overwrote an existing session")
	}
	if raw, _ := os.ReadFile(path); string(raw) != "existing session" {
		t.Fatal("existing output was changed or removed")
	}
	partial := filepath.Join(t.TempDir(), "partial.jsonl")
	if err := writeNative(context.Background(), brokenExporter{}, session.Session{}, harness.ExportOptions{}, partial); err == nil {
		t.Fatal("ignored export failure")
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("failed export left a partial session: %v", err)
	}
}

func TestMalformedModelLeavesNoNativeFile(t *testing.T) {
	source, _ := sourceFixture(t)
	if _, err := Create(context.Background(), Request{Source: source, Target: "pi", Model: "invalid"}); err == nil {
		t.Fatal("accepted an invalid model")
	}
	if err := filepath.WalkDir(os.Getenv("PI_CODING_AGENT_DIR"), func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".jsonl") {
			t.Errorf("failed handoff left native output: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
