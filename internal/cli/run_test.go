package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWithoutArgumentsPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := Run(context.Background(), nil, &stdout, &stderr)
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

	err := Run(context.Background(), []string{"unknown"}, &stdout, &stderr)
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
	err := Run(context.Background(), []string{"inspect", path}, &stdout, &stderr)
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
	err := Run(context.Background(), []string{"inspect", "-json", path}, &stdout, &bytes.Buffer{})
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

	err := Run(context.Background(), []string{"inspect", path}, &bytes.Buffer{}, &bytes.Buffer{})
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
	if err := Run(context.Background(), []string{"export", "--harness", "pi", "-output", output, source}, &exported, &bytes.Buffer{}); err != nil {
		t.Fatalf("export error = %v", err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("output file: %v", err)
	}

	var imported bytes.Buffer
	if err := Run(context.Background(), []string{"import", output}, &imported, &bytes.Buffer{}); err != nil {
		t.Fatalf("import error = %v", err)
	}
	if !strings.Contains(imported.String(), "session demo") || !strings.Contains(imported.String(), "raw records: 2") {
		t.Fatalf("import output = %q", imported.String())
	}
}

func TestRunConvertClaudeBundleToPi(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "claude.jsonl")
	bundlePath := filepath.Join(directory, "claude.sesh.json")
	transcript := `{"type":"user","uuid":"u1","sessionId":"c1","cwd":"/work","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"hello"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"c1","timestamp":"2026-01-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}
`
	if err := os.WriteFile(source, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"export", "--harness", "claude", "-output", bundlePath, source}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("export error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"convert", "--target", "pi", "--model", "openai/gpt-x", bundlePath}, &stdout, &bytes.Buffer{}); err != nil {
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

func TestRunConvertRejectsUnknownTarget(t *testing.T) {
	err := Run(context.Background(), []string{"convert", "--target", "vim", "x.sesh.json"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unsupported target") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Run(ctx, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
}
