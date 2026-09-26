package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mr-jones123/sesh/internal/harness"
)

// agentDir is $PI_CODING_AGENT_DIR, or ~/.pi/agent.
func agentDir() (string, error) {
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find pi agent dir: %w", err)
	}
	return filepath.Join(home, ".pi", "agent"), nil
}

// sessionDirName encodes a cwd as Pi names its session directory:
// "--" + the path without its leading separator, with / \ and : as "-" + "--".
func sessionDirName(cwd string) string {
	trimmed := strings.TrimLeft(cwd, `/\`)
	return "--" + strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(trimmed) + "--"
}

// Sessions lists sessions/--<cwd>--/<time>_<id>.jsonl.
func (Adapter) Sessions(dir string) ([]harness.SessionFile, error) {
	root, err := agentDir()
	if err != nil {
		return nil, err
	}
	names := []string{"*"}
	if dir != "" {
		names = names[:0]
		for _, d := range harness.SameDirs(dir) {
			names = append(names, sessionDirName(d))
		}
	}
	var files []harness.SessionFile
	for _, name := range names {
		paths, err := filepath.Glob(filepath.Join(root, "sessions", name, "*.jsonl"))
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			_, id, found := strings.Cut(base, "_")
			if !found {
				id = base
			}
			files = append(files, harness.SessionFile{Path: path, ID: id})
		}
	}
	return files, nil
}
