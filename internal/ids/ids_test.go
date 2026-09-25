package ids

import (
	"regexp"
	"testing"
	"time"
)

func TestNewV7(t *testing.T) {
	at := time.UnixMilli(0x01a0da10abcd)
	id := NewV7(at)
	if !regexp.MustCompile(`^01a0da10-abcd-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Fatalf("NewV7 = %s, want time prefix, version 7, RFC variant", id)
	}
	if NewV7(at) == id {
		t.Fatal("NewV7 returned the same ID twice")
	}
}
