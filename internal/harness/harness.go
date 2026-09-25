package harness

import (
	"context"
	"io"

	"github.com/mr-jones123/sesh/internal/session"
)

// Adapter imports one harness's local transcript into a sesh bundle: the
// neutral session plus the verbatim source lines.
type Adapter interface {
	Name() string
	Detect(path string) bool
	Import(context.Context, string) (session.Bundle, error)
}

// Exporter writes a bundle as one harness's native transcript.
type Exporter interface {
	Name() string
	Export(context.Context, session.Bundle, ExportOptions, io.Writer) error
}

// ExportOptions override source values in the written transcript. Zero
// values keep the source.
type ExportOptions struct {
	// Model is "provider/model-id", the model the target resumes with.
	Model string
	// Workspace is the working directory recorded in the target transcript.
	Workspace string
}
