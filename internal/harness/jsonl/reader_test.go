package jsonl

import (
	"context"
	"strings"
	"testing"
)

func TestReadReader(t *testing.T) {
	count := 0
	err := ReadReader(context.Background(), strings.NewReader("\n{\"type\":\"one\"}\n{\"type\":\"two\"}\n"), func(line int, value map[string]any) error {
		count++
		if line != count+1 {
			t.Fatalf("line = %d, want %d", line, count+1)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}
