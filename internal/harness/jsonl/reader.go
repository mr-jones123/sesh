package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Line is one non-empty JSONL line. Raw is only valid for the duration of
// the callback; callers that retain it must copy it.
type Line struct {
	Number int
	Raw    json.RawMessage
}

// Read opens a JSONL file and calls fn once for every non-empty line.
func Read(ctx context.Context, path string, fn func(Line) error) error {
	return ReadUntil(ctx, path, func(line Line) (bool, error) {
		return false, fn(line)
	})
}

// ReadUntil opens a JSONL file and stops after fn reports that it has enough
// data. JSON and scanner errors encountered before that point are returned.
func ReadUntil(ctx context.Context, path string, fn func(Line) (bool, error)) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	defer file.Close()

	return ReadReaderUntil(ctx, file, fn)
}

func ReadReader(ctx context.Context, reader io.Reader, fn func(Line) error) error {
	return ReadReaderUntil(ctx, reader, func(line Line) (bool, error) {
		return false, fn(line)
	})
}

func ReadReaderUntil(ctx context.Context, reader io.Reader, fn func(Line) (bool, error)) error {
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
		stop, err := fn(Line{Number: number, Raw: json.RawMessage(bytes)})
		if err != nil {
			return fmt.Errorf("line %d: %w", number, err)
		}
		if stop {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read transcript: %w", err)
			}
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read transcript: %w", err)
	}
	return nil
}
