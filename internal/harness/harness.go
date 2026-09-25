package harness

import (
	"context"

	"github.com/mr-jones123/sesh/internal/session"
)

// Adapter imports one harness's local transcript into a sesh bundle: the
// neutral session plus the verbatim source lines.
type Adapter interface {
	Name() string
	Detect(path string) bool
	Import(context.Context, string) (session.Bundle, error)
}
