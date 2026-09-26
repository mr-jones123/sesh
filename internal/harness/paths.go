package harness

import "path/filepath"

// SameDirs is dir and, when it differs, dir with symlinks resolved. Harnesses
// record either (/tmp is /private/tmp on macOS), so a lookup by directory
// tries both.
func SameDirs(dir string) []string {
	dirs := []string{dir}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil && resolved != dir {
		dirs = append(dirs, resolved)
	}
	return dirs
}
