package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const metadataPrefixLimit = 64 * 1024

// MetadataLine is the bounded beginning of one non-empty JSONL line. Prefix
// may be truncated; metadata records are expected to fit within the limit.
type MetadataLine struct {
	Number    int
	Prefix    []byte
	Truncated bool
}

// ScanMetadata reads every line without validating or retaining full records.
// Large conversation and tool records are discarded after a bounded prefix so
// callers can find metadata appended anywhere in a transcript cheaply.
func ScanMetadata(ctx context.Context, path string, fn func(MetadataLine) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, metadataPrefixLimit)
	for number := 1; ; number++ {
		prefix, truncated, err := readPrefix(reader)
		if len(prefix) > 0 {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if callbackErr := fn(MetadataLine{Number: number, Prefix: prefix, Truncated: truncated}); callbackErr != nil {
				return fmt.Errorf("line %d: %w", number, callbackErr)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read transcript: %w", err)
		}
	}
}

func readPrefix(reader *bufio.Reader) ([]byte, bool, error) {
	var prefix []byte
	truncated := false
	for {
		part, isPrefix, err := reader.ReadLine()
		remaining := metadataPrefixLimit - len(prefix)
		if remaining > 0 {
			prefix = append(prefix, part[:min(remaining, len(part))]...)
		}
		if len(part) > remaining || isPrefix && len(prefix) == metadataPrefixLimit {
			truncated = true
		}
		if err != nil {
			return prefix, truncated, err
		}
		if !isPrefix {
			return prefix, truncated, nil
		}
	}
}

// StringField returns a top-level JSON string field from a line prefix. It
// deliberately does not validate or decode the rest of the JSON object.
func StringField(raw []byte, name string) (string, bool) {
	depth := 0
	for i := 0; i < len(raw); {
		switch raw[i] {
		case '{', '[':
			depth++
			i++
		case '}', ']':
			depth--
			i++
		case '"':
			end, ok := stringEnd(raw, i)
			if !ok {
				return "", false
			}
			if depth != 1 {
				i = end
				continue
			}
			keyMatches := equalASCII(raw[i+1:end-1], name)
			j := skipSpace(raw, end)
			if j >= len(raw) || raw[j] != ':' {
				i = end
				continue
			}
			j = skipSpace(raw, j+1)
			if !keyMatches || j >= len(raw) || raw[j] != '"' {
				i = end
				continue
			}
			valueEnd, ok := stringEnd(raw, j)
			if !ok {
				return "", false
			}
			var value string
			err := json.Unmarshal(raw[j:valueEnd], &value)
			if err != nil {
				return "", false
			}
			return value, true
		default:
			i++
		}
	}
	return "", false
}

func equalASCII(raw []byte, value string) bool {
	if len(raw) != len(value) {
		return false
	}
	for i := range raw {
		if raw[i] != value[i] {
			return false
		}
	}
	return true
}

func stringEnd(raw []byte, start int) (int, bool) {
	escaped := false
	for i := start + 1; i < len(raw); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch raw[i] {
		case '\\':
			escaped = true
		case '"':
			return i + 1, true
		}
	}
	return 0, false
}

func skipSpace(raw []byte, start int) int {
	for start < len(raw) {
		switch raw[start] {
		case ' ', '\t', '\r', '\n':
			start++
		default:
			return start
		}
	}
	return start
}
