package jsonl

import (
	"context"
	"strings"
	"testing"
)

func TestReadReaderPassesExactLinesAndNumbers(t *testing.T) {
	input := "\n{\"type\": \"one\",  \"a\": \"<b>\"}\n{\"type\":\"two\"}\n"
	var records []string
	var numbers []int
	err := ReadReader(context.Background(), strings.NewReader(input), func(line Line) error {
		records = append(records, string(line.Raw))
		numbers = append(numbers, line.Number)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("lines = %d, want 2", len(records))
	}
	if records[0] != `{"type": "one",  "a": "<b>"}` || numbers[0] != 2 {
		t.Fatalf("line 1 = %d %q", numbers[0], records[0])
	}
	if records[1] != `{"type":"two"}` || numbers[1] != 3 {
		t.Fatalf("line 2 = %d %q", numbers[1], records[1])
	}
}

func TestReadReaderRejectsInvalidJSON(t *testing.T) {
	err := ReadReader(context.Background(), strings.NewReader("{\"ok\":true}\n{broken\n"), func(Line) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error = %v, want line 2 invalid JSON", err)
	}
}

func TestReadReaderUntilStopsWithoutReadingLaterLines(t *testing.T) {
	input := "{\"type\":\"one\"}\n{broken after stop\n"
	var calls int
	err := ReadReaderUntil(context.Background(), strings.NewReader(input), func(Line) (bool, error) {
		calls++
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestReadReaderUntilReportsScannerErrorBeforeStop(t *testing.T) {
	oversized := `{"text":"` + strings.Repeat("x", 16*1024*1024) + `"}` + "\n"
	err := ReadReaderUntil(context.Background(), strings.NewReader(oversized), func(Line) (bool, error) {
		return true, nil
	})
	if err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("error = %v, want scanner token-too-long error", err)
	}
}
