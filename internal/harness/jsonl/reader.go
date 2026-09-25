package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Read opens a JSONL file and calls fn once for every non-empty line.
func Read(ctx context.Context, path string, fn func(int, map[string]any) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	defer file.Close()

	return ReadReader(ctx, file, fn)
}

func ReadReader(ctx context.Context, reader io.Reader, fn func(int, map[string]any) error) error {
	scanner := bufio.NewScanner(reader)
	// Tool output can be large. The default Scanner limit is only 64 KiB.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	line := 0
	for scanner.Scan() {
		line++
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(scanner.Bytes()) == 0 {
			continue
		}

		var value map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			return fmt.Errorf("line %d: invalid JSON: %w", line, err)
		}
		if err := fn(line, value); err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read transcript: %w", err)
	}
	return nil
}

func String(value any) string {
	text, _ := value.(string)
	return text
}

func Map(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func Slice(value any) []any {
	result, _ := value.([]any)
	return result
}
