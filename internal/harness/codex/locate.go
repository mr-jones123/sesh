package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mr-jones123/sesh/internal/harness"
)

// codexHome is $CODEX_HOME, or ~/.codex.
func codexHome() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find codex home: %w", err)
	}
	return filepath.Join(userHome, ".codex"), nil
}

// Sessions lists sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl. Codex files
// rollouts by date, not directory, so with dir set each rollout's first line
// (its session_meta record) is read for the cwd it started in.
func (Adapter) Sessions(dir string) ([]harness.SessionFile, error) {
	home, err := codexHome()
	if err != nil {
		return nil, err
	}
	paths, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return nil, err
	}
	var dirs []string
	if dir != "" {
		dirs = harness.SameDirs(dir)
	}
	var files []harness.SessionFile
	for _, path := range paths {
		if dirs != nil && !slices.Contains(dirs, startDir(path)) {
			continue
		}
		files = append(files, harness.SessionFile{Path: path, ID: rolloutID(path)})
	}
	return files, nil
}

// rolloutID is the UUID that ends a rollout file name.
func rolloutID(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	const uuidLen = 36
	if len(name) < uuidLen {
		return name
	}
	return name[len(name)-uuidLen:]
}

// startDir is the cwd in a rollout's session_meta line, or "" if the file
// does not start with one.
func startDir(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return ""
	}
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			CWD string `json:"cwd"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &meta) != nil || meta.Type != "session_meta" {
		return ""
	}
	return meta.Payload.CWD
}
