package harness

import (
	"context"

	"github.com/mr-jones123/sesh/internal/session"
)

// Adapter imports one harness's local transcript into sesh's neutral model.
type Adapter interface {
	Name() string
	Detect(path string) bool
	Import(context.Context, string) (session.Session, error)
}
