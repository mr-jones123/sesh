package omp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mr-jones123/sesh/internal/harness"
)

func TestSessionDirNameUsesCompactHomeRelativeAndAbsoluteShards(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	inside, err := sessionDirName(filepath.Join(home, "Programming", "Self-Projects", "sesh"))
	if err != nil {
		t.Fatal(err)
	}
	if inside != "-Programming-Self-Projects-sesh" {
		t.Fatalf("home-relative shard = %q", inside)
	}
	root, err := sessionDirName(home)
	if err != nil {
		t.Fatal(err)
	}
	if root != "-" {
		t.Fatalf("home shard = %q", root)
	}
	outside, err := sessionDirName("/private/tmp/work")
	if err != nil {
		t.Fatal(err)
	}
	if outside != "--private-tmp-work--" {
		t.Fatalf("absolute shard = %q", outside)
	}
}

func TestAgentDirIgnoresPiNativeOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(t.TempDir(), "pi-agent"))
	t.Setenv(agentDirOverride, "")

	got, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".omp", "agent")
	if got != want {
		t.Fatalf("agentDir() = %q, want %q", got, want)
	}
}

func TestSessionsDiscoversProjectAndAllWithNativeIDs(t *testing.T) {
	home := t.TempDir()
	store := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(agentDirOverride, store)
	project := filepath.Join(home, "work", "app")
	other := "/private/tmp/other"

	create := func(workspace, name string) string {
		t.Helper()
		shard, err := sessionDirName(workspace)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(store, "sessions", shard, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	projectPath := create(project, "2026-10-19T01-02-03-456Z_01a151aa-1111-7111-8111-111111111111.jsonl")
	otherPath := create(other, "2026-10-19T01-02-04-456Z_01a151aa-2222-7222-8222-222222222222.jsonl")

	got, err := New().Sessions(project)
	if err != nil {
		t.Fatal(err)
	}
	wantProject := []harness.SessionFile{{Path: projectPath, ID: "01a151aa-1111-7111-8111-111111111111"}}
	if len(got) != 1 || got[0] != wantProject[0] {
		t.Fatalf("project sessions = %#v, want %#v", got, wantProject)
	}

	all, err := New().Sessions("")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]string, len(all))
	for _, file := range all {
		seen[file.Path] = file.ID
	}
	if seen[projectPath] != "01a151aa-1111-7111-8111-111111111111" || seen[otherPath] != "01a151aa-2222-7222-8222-222222222222" || len(seen) != 2 {
		t.Fatalf("all sessions = %#v", all)
	}
}
