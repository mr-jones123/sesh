package codex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/mr-jones123/sesh/internal/harness/jsonl"
)

// sessionTitles loads Codex's optional name sidecar once. Entries are
// append-only, so the last record for an ID is the current saved name.
func sessionTitles(home string) (map[string]string, error) {
	var titles map[string]string
	err := jsonl.ScanMetadata(context.Background(), filepath.Join(home, "session_index.jsonl"), func(line jsonl.MetadataLine) error {
		id, ok := jsonl.StringField(line.Prefix, "id")
		if !ok || id == "" {
			return nil
		}
		title, _ := jsonl.StringField(line.Prefix, "thread_name")
		if titles == nil {
			titles = make(map[string]string)
		}
		titles[id] = strings.TrimSpace(title)
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session index: %w", err)
	}
	return titles, nil
}
