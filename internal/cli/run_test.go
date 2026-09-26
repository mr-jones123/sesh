package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mr-jones123/sesh/internal/session"
)

func TestRunWithoutArgumentsPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := Run(context.Background(), nil, nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("stdout = %q, want root usage", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty output", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := Run(context.Background(), []string{"unknown"}, nil, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("Run() error = %v, want unknown command error", err)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %q, want root usage", stderr.String())
	}
}

func TestRunInspect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, []byte(`{"title":"hello"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(context.Background(), []string{"inspect", path}, nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("Run() error = %v; stderr = %q", err, stderr.String())
	}
	if want := path + ": valid JSON\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunInspectPrintsNormalizedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, []byte(`{"count":12345678901234567890}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"inspect", "-json", path}, nil, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "12345678901234567890") {
		t.Fatalf("stdout = %q, want number preserved", stdout.String())
	}
}

func TestRunInspectRejectsMultipleJSONValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, []byte(`{} {}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Run(context.Background(), []string{"inspect", path}, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "more than one JSON value") {
		t.Fatalf("Run() error = %v, want multiple value error", err)
	}
}

func TestRunExportAndImport(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "pi.jsonl")
	output := filepath.Join(directory, "session.sesh.json")
	transcript := `{"type":"session","version":3,"id":"demo","timestamp":"2026-01-01T00:00:00Z","cwd":"/tmp/app"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"hello"}}
`
	if err := os.WriteFile(source, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}

	var exported bytes.Buffer
	if err := Run(context.Background(), []string{"export", "--harness", "pi", "-output", output, source}, nil, &exported, &bytes.Buffer{}); err != nil {
		t.Fatalf("export error = %v", err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("output file: %v", err)
	}

	var imported bytes.Buffer
	if err := Run(context.Background(), []string{"import", output}, nil, &imported, &bytes.Buffer{}); err != nil {
		t.Fatalf("import error = %v", err)
	}
	if !strings.Contains(imported.String(), "session demo") || !strings.Contains(imported.String(), "raw records: 2") {
		t.Fatalf("import output = %q", imported.String())
	}
}

// exportClaudeFixture writes a two-message Claude transcript and exports it,
// returning the directory and bundle path.
func exportClaudeFixture(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	source := filepath.Join(directory, "claude.jsonl")
	bundlePath := filepath.Join(directory, "claude.sesh.json")
	transcript := `{"type":"user","uuid":"u1","sessionId":"c1","cwd":"/work","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"hello"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"c1","timestamp":"2026-01-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}
`
	if err := os.WriteFile(source, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"export", "--harness", "claude", "-output", bundlePath, source}, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("export error = %v", err)
	}
	return directory, bundlePath
}

func TestRunConvertClaudeBundleToPi(t *testing.T) {
	directory, bundlePath := exportClaudeFixture(t)

	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"convert", "--target", "pi", "--model", "openai/gpt-x", bundlePath}, nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("convert error = %v", err)
	}
	output := filepath.Join(directory, "claude.pi.jsonl")
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// header, user, assistant, model_change
	if len(lines) != 4 || !strings.Contains(lines[0], `"type":"session"`) || !strings.Contains(lines[3], `"modelId":"gpt-x"`) {
		t.Fatalf("pi session = %s", data)
	}
	if !strings.Contains(stdout.String(), "pi --session "+output) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunConvertInstallsCodexSessionAfterConfirmation(t *testing.T) {
	_, bundlePath := exportClaudeFixture(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	sessions := func() []string {
		found, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*.jsonl"))
		return found
	}

	err := Run(context.Background(), []string{"convert", "--target", "codex", "--install", bundlePath}, strings.NewReader("n\n"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "cancelled") || len(sessions()) != 0 {
		t.Fatalf("declined install: error = %v, sessions = %v", err, sessions())
	}

	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"convert", "--target", "codex", "--install", bundlePath}, strings.NewReader("y\n"), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("install error = %v", err)
	}
	installed := sessions()
	if len(installed) != 1 {
		t.Fatalf("sessions = %v, want one", installed)
	}
	data, err := os.ReadFile(installed[0])
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(string(data), "\n", 2)[0]
	// The ID in the file name must match session_meta, since Codex finds
	// sessions by the name.
	id := strings.TrimSuffix(filepath.Base(installed[0]), ".jsonl")
	id = id[len(id)-36:]
	if !strings.Contains(first, `"type":"session_meta"`) || !strings.Contains(first, `"id":"`+id+`"`) {
		t.Fatalf("installed %s starts with %s", installed[0], first)
	}
	if !strings.Contains(stdout.String(), "codex resume "+id) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunConvertRejectsInstallForPi(t *testing.T) {
	_, bundlePath := exportClaudeFixture(t)
	err := Run(context.Background(), []string{"convert", "--target", "pi", "--install", "--yes", bundlePath}, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "opens session files directly") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunConvertRejectsUnknownTarget(t *testing.T) {
	err := Run(context.Background(), []string{"convert", "--target", "vim", "x.sesh.json"}, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unsupported target") {
		t.Fatalf("error = %v", err)
	}
}

// piTranscript has a user message holding key and a bash call under
// /Users/alice, so both secret and home-path redaction apply.
func piTranscript(key string) string {
	return `{"type":"session","version":3,"id":"demo","timestamp":"2026-01-01T00:00:00Z","cwd":"/Users/alice/app"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"use ` + key + `"}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"t1","name":"bash","arguments":{"command":"cat /Users/alice/app/go.mod"}}]}}
`
}

func TestRunExportRedactsByDefault(t *testing.T) {
	key := "sk-proj-" + strings.Repeat("a1B2", 12) // built at run time for secret scanners
	directory := t.TempDir()
	source := filepath.Join(directory, "pi.jsonl")
	transcript := piTranscript(key)
	if err := os.WriteFile(source, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"export", "--harness", "pi", source}, nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("export error = %v", err)
	}
	redacted, err := os.ReadFile(source + ".sesh.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{key, "alice"} {
		if strings.Contains(string(redacted), leaked) {
			t.Errorf("bundle still contains %q", leaked)
		}
	}
	if !strings.Contains(string(redacted), `"redacted": true`) || !strings.Contains(stdout.String(), "openai-key") {
		t.Errorf("stdout = %q, want a redacted bundle and its findings", stdout.String())
	}
	if got, _ := os.ReadFile(source); string(got) != transcript {
		t.Error("export modified the source transcript")
	}

	exact := filepath.Join(directory, "exact.sesh.json")
	if err := Run(context.Background(), []string{"export", "--harness", "pi", "-no-redact", "-output", exact, source}, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("export -no-redact error = %v", err)
	}
	file, err := os.Open(exact)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	bundle, err := session.DecodeBundle(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(transcript, "\n"), "\n")
	if bundle.Redacted || len(bundle.RawRecords) != len(lines) {
		t.Fatalf("redacted = %v, raw records = %d", bundle.Redacted, len(bundle.RawRecords))
	}
	for i, raw := range bundle.RawRecords {
		if raw.Record != lines[i] {
			t.Errorf("raw line %d = %s, want the source line unchanged", i+1, raw.Record)
		}
	}
}

func TestRunConvertExpandsHomeInRedactedBundle(t *testing.T) {
	directory := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	source := filepath.Join(directory, "pi.jsonl")
	if err := os.WriteFile(source, []byte(piTranscript("hello")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"export", "--harness", "pi", source}, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("export error = %v", err)
	}
	output := filepath.Join(directory, "out.pi.jsonl")
	if err := Run(context.Background(), []string{"convert", "--target", "pi", "-output", output, source + ".sesh.json"}, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("convert error = %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	// The ~ written by redaction continues under this user's home, in the
	// session's working directory and in the replayed tool call.
	for _, want := range []string{`"cwd":"` + home + `/app"`, `cat ` + home + `/app/go.mod`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("pi session missing %s:\n%s", want, data)
		}
	}
}

func TestRunExportFindsSessionByIDAndLast(t *testing.T) {
	pi := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", pi)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	work, _ := filepath.EvalSymlinks(t.TempDir())
	t.Chdir(work)
	dir := filepath.Join(pi, "sessions", "--"+strings.ReplaceAll(strings.TrimPrefix(work, "/"), "/", "-")+"--")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := strings.Replace(piTranscript("hello"), "/Users/alice/app", work, 1)
	if err := os.WriteFile(filepath.Join(dir, "2026-01-01T00-00-00-000Z_01a0de9d-aaaa.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"export", "01a0de"}, {"export", "-last"}} {
		_ = os.Remove("pi-01a0de9d.sesh.json")
		var stdout bytes.Buffer
		if err := Run(context.Background(), args, nil, &stdout, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		// Found by ID or -last, the bundle lands in the current directory,
		// not next to the transcript in Pi's session directory.
		if _, err := os.Stat(filepath.Join(work, "pi-01a0de9d.sesh.json")); err != nil {
			t.Errorf("%v: %v; stdout = %q", args, err, stdout.String())
		}
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
