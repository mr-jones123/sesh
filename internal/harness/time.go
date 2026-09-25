package harness

import "time"

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
