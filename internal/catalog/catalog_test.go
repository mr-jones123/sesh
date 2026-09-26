package catalog

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeHomes points every harness at its own temporary directory.
func fakeHomes(t *testing.T) (claude, codex, pi string) {
	t.Helper()
	root := t.TempDir()
	claude, codex, pi = filepath.Join(root, "claude"), filepath.Join(root, "codex"), filepath.Join(root, "pi")
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", codex)
	t.Setenv("PI_CODING_AGENT_DIR", pi)
	return claude, codex, pi
}

func write(t *testing.T, path, data string, updated time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, updated, updated); err != nil {
		t.Fatal(err)
	}
}

func claudeSession(cwd, id, prompt string) string {
	return `{"type":"user","uuid":"u1","sessionId":"` + id + `","cwd":"` + cwd + `","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"` + prompt + `"}}` + "\n"
}

func codexSession(cwd, id string, prompts ...string) string {
	data := `{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `"}}` + "\n"
	for _, p := range prompts {
		data += `{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + p + `"}]}}` + "\n"
	}
	return data
}

func piSession(cwd, id, prompt string) string {
	return `{"type":"session","version":3,"id":"` + id + `","timestamp":"2026-01-01T00:00:00Z","cwd":"` + cwd + `"}` + "\n" +
		`{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"` + prompt + `"}}` + "\n"
}

var nonAlphanumeric = regexp.MustCompile(`[^A-Za-z0-9]`)

func TestListFindsEachHarnessForTheDirectoryNewestFirst(t *testing.T) {
	claude, codex, pi := fakeHomes(t)
	app, other := t.TempDir(), t.TempDir()
	app, _ = filepath.EvalSymlinks(app) // harnesses record the resolved cwd
	now := time.Now()

	const codexID = "01a0de68-b266-771b-8fd7-f9a39f157f6d"
	write(t, filepath.Join(claude, "projects", nonAlphanumeric.ReplaceAllString(app, "-"), "c1.jsonl"),
		claudeSession(app, "c1", "build the backend"), now.Add(-3*time.Hour))
	write(t, filepath.Join(codex, "sessions", "2026", "01", "01", "rollout-2026-01-01T00-00-00-"+codexID+".jsonl"),
		codexSession(app, codexID, "<environment_context>cwd</environment_context>", "# AGENTS.md instructions for app", "add the frontend"), now.Add(-time.Hour))
	pidir := "--" + strings.ReplaceAll(strings.TrimPrefix(app, "/"), "/", "-") + "--"
	write(t, filepath.Join(pi, "sessions", pidir, "2026-01-01T00-00-00-000Z_p1.jsonl"),
		piSession(app, "p1", `add deleting\nnotes`), now.Add(-2*time.Hour))
	// Another directory's sessions must not appear.
	write(t, filepath.Join(claude, "projects", nonAlphanumeric.ReplaceAllString(other, "-"), "c2.jsonl"),
		claudeSession(other, "c2", "unrelated"), now)
	write(t, filepath.Join(codex, "sessions", "2026", "01", "02", "rollout-2026-01-02T00-00-00-11111111-2222-3333-4444-555555555555.jsonl"),
		codexSession(other, "x", "unrelated"), now)

	entries, err := List(context.Background(), Query{Dir: app})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		if e.Err != nil {
			t.Fatalf("%s %s: %v", e.Harness, e.ID, e.Err)
		}
		got = append(got, e.Harness+" "+e.ID+" "+e.Prompt)
	}
	want := []string{
		"codex " + codexID + " add the frontend", // injected context skipped
		"pi p1 add deleting notes",               // joined onto one line
		"claude c1 build the backend",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}

	all, err := List(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Errorf("List(all) = %d sessions, want 5", len(all))
	}
	onlyPi, err := List(context.Background(), Query{Dir: app, Harness: "pi"})
	if err != nil || len(onlyPi) != 1 || onlyPi[0].Harness != "pi" {
		t.Errorf("List(pi) = %+v, %v", onlyPi, err)
	}
}

func TestFindResolvesIDPrefixes(t *testing.T) {
	claude, _, pi := fakeHomes(t)
	write(t, filepath.Join(claude, "projects", "-a", "3636a042-5fcb.jsonl"), claudeSession("/a", "3636a042-5fcb", "x"), time.Now())
	write(t, filepath.Join(pi, "sessions", "--a--", "2026_36aa0000.jsonl"), piSession("/a", "36aa0000", "x"), time.Now())

	if e, err := Find("3636", ""); err != nil || e.Harness != "claude" || e.ID != "3636a042-5fcb" {
		t.Errorf("Find(3636) = %+v, %v", e, err)
	}
	if e, err := Find("36aa", ""); err != nil || e.Harness != "pi" {
		t.Errorf("Find(36aa) = %+v, %v", e, err)
	}
	if _, err := Find("36", ""); err == nil || !strings.Contains(err.Error(), "matches 2 sessions") {
		t.Errorf("Find(36) error = %v, want an ambiguity error", err)
	}
	if _, err := Find("36", "pi"); err != nil {
		t.Errorf("Find(36, pi) error = %v, want the harness to narrow it", err)
	}
	if _, err := Find("ffff", ""); err == nil {
		t.Error("Find(ffff) found a session")
	}
}
