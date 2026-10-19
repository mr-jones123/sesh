package omp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mr-jones123/sesh/internal/harness"
)

const agentDirOverride = "SESH_OMP_AGENT_DIR"

// agentDir is sesh's OMP-specific override, or OMP's native ~/.omp/agent root.
// It intentionally does not use PI_CODING_AGENT_DIR: native OMP and Pi share
// that variable, while sesh presents them as distinct session stores.
func agentDir() (string, error) {
	if dir := os.Getenv(agentDirOverride); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find omp agent dir: %w", err)
	}
	return filepath.Join(home, ".omp", "agent"), nil
}

// sessionDirName matches OMP's compact home-relative project shards. Paths
// outside the user's home use the Pi-family absolute-path spelling.
func sessionDirName(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home for omp session path: %w", err)
	}
	clean := filepath.Clean(cwd)
	rel, err := filepath.Rel(home, clean)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if rel == "." {
			return "-", nil
		}
		return "-" + pathDashes(rel), nil
	}
	trimmed := strings.TrimLeft(clean, `/\`)
	return "--" + pathDashes(trimmed) + "--", nil
}

func pathDashes(path string) string {
	return strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(path)
}

// Sessions lists OMP's native sessions/<project>/<time>_<id>.jsonl files.
func (Adapter) Sessions(dir string) ([]harness.SessionFile, error) {
	root, err := agentDir()
	if err != nil {
		return nil, err
	}
	projects := []string{"*"}
	if dir != "" {
		projects = projects[:0]
		seen := make(map[string]struct{})
		for _, candidate := range harness.SameDirs(dir) {
			project, err := sessionDirName(candidate)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[project]; ok {
				continue
			}
			seen[project] = struct{}{}
			projects = append(projects, project)
		}
	}

	var files []harness.SessionFile
	seenPaths := make(map[string]struct{})
	for _, project := range projects {
		paths, err := filepath.Glob(filepath.Join(root, "sessions", project, "*.jsonl"))
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if _, ok := seenPaths[path]; ok {
				continue
			}
			seenPaths[path] = struct{}{}
			files = append(files, harness.SessionFile{Path: path, ID: sessionID(path)})
		}
	}
	return files, nil
}

func sessionID(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if _, id, found := strings.Cut(name, "_"); found {
		return id
	}
	return name
}

func pathWithin(path, root string) bool {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absoluteRoot, absolutePath)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
