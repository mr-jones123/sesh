package harness

import (
	"os"
	"time"
)

func ParseTime(value any) time.Time {
	switch value := value.(type) {
	case string:
		parsed, _ := time.Parse(time.RFC3339Nano, value)
		return parsed
	case float64:
		return time.UnixMilli(int64(value))
	default:
		return time.Time{}
	}
}

// FileTime is the creation time for a transcript that records none: the
// file's modification time, so importing the same file twice gives the same
// bundle. time.Now() would differ on every run.
func FileTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
