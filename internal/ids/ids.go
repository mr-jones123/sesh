// Package ids generates session identifiers.
package ids

import (
	"crypto/rand"
	"fmt"
	"time"
)

// NewV7 returns a version 7 UUID for t: 48 bits of Unix milliseconds, then
// random bits. Codex names its own threads this way, and the time prefix
// keeps IDs sortable by creation. Claude accepts any valid UUID.
func NewV7(t time.Time) string {
	var b [16]byte
	_, _ = rand.Read(b[6:]) // crypto/rand.Read never returns an error
	ms := uint64(t.UnixMilli())
	for i := range 6 {
		b[i] = byte(ms >> (40 - 8*i))
	}
	b[6] = b[6]&0x0f | 0x70 // version 7
	b[8] = b[8]&0x3f | 0x80 // RFC 9562 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// CallID makes a tool-call ID safe for every target: Anthropic accepts only
// [A-Za-z0-9_-] up to 64 characters, and Pi records OpenAI calls as
// "call_x|fc_y". Other characters become '_'. The mapping is deterministic,
// so a call and its result stay paired.
func CallID(id string) string {
	safe := []byte(id)
	for i, c := range safe {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			safe[i] = '_'
		}
	}
	if len(safe) > 64 {
		safe = safe[:64]
	}
	return string(safe)
}
