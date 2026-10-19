package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mr-jones123/sesh/internal/catalog"
	"github.com/mr-jones123/sesh/internal/harness/codex"
)

func isolatedHomes(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi"))
	t.Setenv("SESH_OMP_AGENT_DIR", filepath.Join(root, "omp"))
	return root
}

func claudeFixture(t *testing.T, workspace, id, text string) string {
	t.Helper()
	slug := regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(workspace, "-")
	path := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", slug, id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	record := map[string]any{"type": "user", "uuid": "u1", "sessionId": id, "cwd": workspace,
		"timestamp": "2026-01-01T00:00:00Z", "message": map[string]any{"role": "user", "content": text}}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func oneCodexSession(t *testing.T) string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(os.Getenv("CODEX_HOME"), func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".jsonl") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("native Codex sessions = %v, want exactly one", found)
	}
	return found[0]
}

func TestHandoffExplicitIDWorksAcrossProjectsAndAcceptsTrailingFlags(t *testing.T) {
	isolatedHomes(t)
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := claudeFixture(t, workspace, "abcd-1234", "continue in the original project")
	before, _ := os.ReadFile(source)
	t.Chdir(t.TempDir())
	if err := Run(context.Background(), []string{"handoff", "abcd", "--to", "codex", "--no-run"}, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	s, err := codex.New().Import(context.Background(), oneCodexSession(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.Workspace != workspace || s.ID == "abcd-1234" || s.Events[0].Text != "continue in the original project" {
		t.Fatalf("native handoff = %+v", s)
	}
	after, _ := os.ReadFile(source)
	if !bytes.Equal(before, after) {
		t.Fatal("handoff modified the source")
	}
}

func TestHandoffNewestSelectionIsProjectScoped(t *testing.T) {
	isolatedHomes(t)
	project, _ := filepath.EvalSymlinks(t.TempDir())
	other, _ := filepath.EvalSymlinks(t.TempDir())
	old := claudeFixture(t, project, "old", "old project session")
	newest := claudeFixture(t, project, "new", "new project session")
	unrelated := claudeFixture(t, other, "other", "unrelated newer session")
	for i, path := range []string{old, newest, unrelated} {
		at := time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := Run(context.Background(), []string{"handoff", "--from", "claude", "--to", "codex", "--dir", project, "--no-run"}, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	s, err := codex.New().Import(context.Background(), oneCodexSession(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.Workspace != project || s.Events[0].Text != "new project session" {
		t.Fatalf("selected unrelated or old session: %+v", s)
	}
}

func TestHandoffRejectsAmbiguousOrUnspecifiedSourceBeforeWriting(t *testing.T) {
	isolatedHomes(t)
	workspace := t.TempDir()
	claudeFixture(t, workspace, "abcd-1", "first")
	claudeFixture(t, workspace, "abcd-2", "second")
	for _, args := range [][]string{
		{"handoff", "abcd", "--to", "codex", "--no-run"},
		{"handoff", "--to", "codex", "--no-run"},
	} {
		if err := Run(context.Background(), args, nil, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("%v unexpectedly succeeded", args)
		}
	}
	if _, err := os.Stat(os.Getenv("CODEX_HOME")); !os.IsNotExist(err) {
		t.Fatalf("invalid selection wrote target storage: %v", err)
	}
}

func TestHandoffNonterminalMustExplicitlyChooseNoRun(t *testing.T) {
	isolatedHomes(t)
	source := claudeFixture(t, t.TempDir(), "source", "hello")
	err := Run(context.Background(), []string{"handoff", source, "--to", "codex"}, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("nonterminal handoff launched or silently wrote a session")
	}
	if _, err := os.Stat(os.Getenv("CODEX_HOME")); !os.IsNotExist(err) {
		t.Fatalf("terminal check happened after writing: %v", err)
	}
}

func TestRunHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, nil, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
}

func TestListedIDsRemainResolvableWithSharedPrefixes(t *testing.T) {
	isolatedHomes(t)
	workspace := t.TempDir()
	for _, id := range []string{"prefix00-first", "prefix00-second"} {
		claudeFixture(t, workspace, id, "continue this session")
	}
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"list", "--all", "--harness", "claude"}, nil, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	resolved := map[string]bool{}
	for _, word := range strings.Fields(output.String()) {
		if !strings.HasPrefix(word, "prefix00") {
			continue
		}
		entry, err := catalog.Find(word, "claude")
		if err != nil {
			t.Fatalf("listed ID %q cannot select its session: %v", word, err)
		}
		resolved[entry.ID] = true
	}
	if !resolved["prefix00-first"] || !resolved["prefix00-second"] {
		t.Fatalf("list did not expose both selectable sessions: %v", resolved)
	}
}
