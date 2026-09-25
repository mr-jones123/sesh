package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Line is one non-empty JSONL line, byte-for-byte as it appeared on disk.
type Line struct {
	Number int
	Raw    json.RawMessage
}

// Read opens a JSONL file and calls fn once for every non-empty line.
func Read(ctx context.Context, path string, fn func(Line) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	defer file.Close()

	return ReadReader(ctx, file, fn)
}

func ReadReader(ctx context.Context, reader io.Reader, fn func(Line) error) error {
	scanner := bufio.NewScanner(reader)
	// Tool output can be large. The default Scanner limit is only 64 KiB.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	number := 0
	for scanner.Scan() {
		number++
		if err := ctx.Err(); err != nil {
			return err
		}
		bytes := scanner.Bytes()
		if len(bytes) == 0 {
			continue
		}
		if !json.Valid(bytes) {
			return fmt.Errorf("line %d: invalid JSON", number)
		}
		// Scanner overwrites this buffer on the next Scan, so keep a copy.
		raw := make(json.RawMessage, len(bytes))
		copy(raw, bytes)
		if err := fn(Line{Number: number, Raw: raw}); err != nil {
			return fmt.Errorf("line %d: %w", number, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read transcript: %w", err)
	}
	return nil
}
