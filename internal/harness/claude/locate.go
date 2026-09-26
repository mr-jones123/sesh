package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mr-jones123/sesh/internal/harness"
)

// claudeHome is $CLAUDE_CONFIG_DIR, or ~/.claude.
func claudeHome() (string, error) {
	if home := os.Getenv("CLAUDE_CONFIG_DIR"); home != "" {
		return home, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find claude home: %w", err)
	}
	return filepath.Join(userHome, ".claude"), nil
}

// Sessions lists projects/<project>/<id>.jsonl. The project directory is
// named after the session's cwd, so dir picks it directly. Subagent
// transcripts live one level deeper and are not sessions of their own.
func (Adapter) Sessions(dir string) ([]harness.SessionFile, error) {
	home, err := claudeHome()
	if err != nil {
		return nil, err
	}
	projects := []string{"*"}
	if dir != "" {
		projects = projects[:0]
		for _, d := range harness.SameDirs(dir) {
			projects = append(projects, projectSlug(d))
		}
	}
	var files []harness.SessionFile
	for _, project := range projects {
		paths, err := filepath.Glob(filepath.Join(home, "projects", project, "*.jsonl"))
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			files = append(files, harness.SessionFile{Path: path, ID: strings.TrimSuffix(filepath.Base(path), ".jsonl")})
		}
	}
	return files, nil
}
