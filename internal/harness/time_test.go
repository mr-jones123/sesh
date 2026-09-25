package harness

import (
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	if got := ParseTime("2026-01-01T00:00:00Z"); !got.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("ParseTime() = %v", got)
	}
	if got := ParseTime(float64(1000)); !got.Equal(time.UnixMilli(1000)) {
		t.Fatalf("ParseTime() = %v", got)
	}
}
