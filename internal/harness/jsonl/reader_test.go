package jsonl

import (
	"context"
	"strings"
	"testing"
)

func TestReadReaderKeepsExactLinesAndNumbers(t *testing.T) {
	input := "\n{\"type\": \"one\",  \"a\": \"<b>\"}\n{\"type\":\"two\"}\n"
	var lines []Line
	err := ReadReader(context.Background(), strings.NewReader(input), func(line Line) error {
		lines = append(lines, line)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	// Lines are retained after later scans, so each must own its bytes.
	if got := string(lines[0].Raw); got != `{"type": "one",  "a": "<b>"}` || lines[0].Number != 2 {
		t.Fatalf("line 1 = %d %q", lines[0].Number, got)
	}
	if got := string(lines[1].Raw); got != `{"type":"two"}` || lines[1].Number != 3 {
		t.Fatalf("line 2 = %d %q", lines[1].Number, got)
	}
}

func TestReadReaderRejectsInvalidJSON(t *testing.T) {
	err := ReadReader(context.Background(), strings.NewReader("{\"ok\":true}\n{broken\n"), func(Line) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error = %v, want line 2 invalid JSON", err)
	}
}
