package harness

import (
	"context"
	"io"
	"time"

	"github.com/mr-jones123/sesh/internal/session"
)

// Adapter reads one harness's native local transcript.
type Adapter interface {
	Name() string
	Detect(path string) bool
	Import(context.Context, string) (session.Session, error)
	Summarize(context.Context, string) (Summary, error)
}

// Summary is the metadata catalog needs without importing a transcript's
// complete event history.
type Summary struct {
	ID        string
	Title     string
	Workspace string
	Prompt    string
}

// Exporter writes a session as one harness's native transcript.
type Exporter interface {
	Name() string
	Export(context.Context, session.Session, ExportOptions, io.Writer) error
}

// ExportOptions override source values in the written transcript. Zero
// values keep the source where the target supports that.
type ExportOptions struct {
	// Model is "provider/model-id", the model the target resumes with.
	Model string
	// Workspace is the working directory recorded in the target transcript.
	Workspace string
	// SessionID and CreatedAt name the new target session.
	SessionID string
	CreatedAt time.Time
}

// Installer chooses the target harness's native session path.
type Installer interface {
	InstallPath(session.Session, ExportOptions) (string, error)
}

// SessionFile is one transcript a harness recorded on this machine.
type SessionFile struct {
	Path  string
	ID    string // session ID from the file name
	Title string // native saved name, when discovery provides it in bulk
}

// Locator is an adapter that knows where its harness stores sessions.
type Locator interface {
	// Sessions lists the harness's transcripts. With dir set, it lists only
	// sessions whose storage location or header says they started in dir;
	// callers confirm against the summarized workspace.
	Sessions(dir string) ([]SessionFile, error)
}
