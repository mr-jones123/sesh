package omp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/harness/pi"
	"github.com/mr-jones123/sesh/internal/session"
	"github.com/mr-jones123/sesh/internal/tools"
)

const titleSlotSize = 256

type titleSlot struct {
	Type      string `json:"type"`
	Version   int    `json:"v"`
	Title     string `json:"title"`
	Source    string `json:"source"`
	UpdatedAt string `json:"updatedAt"`
	Pad       string `json:"pad"`
}

// Export writes OMP's authoritative fixed-size title slot, followed by the
// shared Pi v3 session header and event grammar.
func (Adapter) Export(ctx context.Context, source session.Session, opts harness.ExportOptions, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateOptions(opts); err != nil {
		return err
	}
	if err := validateModel(opts.Model); err != nil {
		return err
	}
	slot, err := encodeTitleSlot(source.Title, opts.CreatedAt)
	if err != nil {
		return err
	}
	written, err := w.Write(slot)
	if err != nil {
		return fmt.Errorf("write omp title slot: %w", err)
	}
	if written != len(slot) {
		return fmt.Errorf("write omp title slot: %w", io.ErrShortWrite)
	}
	if err := pi.ExportCompatible(ctx, source, opts, w, supported); err != nil {
		return fmt.Errorf("write omp session: %w", err)
	}
	return nil
}

func supported(action tools.Action) bool {
	switch action.Kind {
	case tools.KindExec:
		return true
	case tools.KindWrite:
		return !strings.ContainsAny(action.Path, ":?#")
	case tools.KindRead:
		return action.Offset == 0 && action.Limit == 0 && !strings.ContainsAny(action.Path, ":?#")
	case tools.KindEdit, tools.KindOther:
		return false
	default:
		return false
	}
}

// InstallPath places a fresh transcript in OMP's native project shard.
func (Adapter) InstallPath(source session.Session, opts harness.ExportOptions) (string, error) {
	if err := validateOptions(opts); err != nil {
		return "", err
	}
	workspace := opts.Workspace
	if workspace == "" {
		workspace = source.Workspace
	}
	if !filepath.IsAbs(workspace) {
		return "", fmt.Errorf("omp sessions need an absolute workspace; got %q (use --workspace)", workspace)
	}
	project, err := sessionDirName(workspace)
	if err != nil {
		return "", err
	}
	root, err := agentDir()
	if err != nil {
		return "", err
	}
	stamp := strings.NewReplacer(":", "-", ".", "-").Replace(opts.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"))
	name := stamp + "_" + opts.SessionID + ".jsonl"
	return filepath.Join(root, "sessions", project, name), nil
}

func validateOptions(opts harness.ExportOptions) error {
	if opts.SessionID == "" || opts.CreatedAt.IsZero() {
		return fmt.Errorf("omp export needs a fresh session ID and creation time")
	}
	return nil
}

func validateModel(model string) error {
	if model == "" {
		return nil
	}
	provider, id, found := strings.Cut(model, "/")
	if !found || provider == "" || id == "" {
		return fmt.Errorf("model %q must be provider/model-id", model)
	}
	return nil
}

func encodeTitleSlot(title string, updatedAt time.Time) ([]byte, error) {
	slot := titleSlot{
		Type:      "title",
		Version:   1,
		Source:    "auto",
		UpdatedAt: updatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
	empty, err := json.Marshal(slot)
	if err != nil {
		return nil, fmt.Errorf("encode omp title slot: %w", err)
	}
	// Empty already includes the title's two JSON quotes. Every accepted title
	// byte must leave room for the pad field and the final physical newline.
	budget := titleSlotSize - 1 - len(empty)
	if budget < 0 {
		return nil, fmt.Errorf("omp title slot metadata exceeds %d bytes", titleSlotSize)
	}
	slot.Title = truncateJSONText(title, budget)
	encoded, err := json.Marshal(slot)
	if err != nil {
		return nil, fmt.Errorf("encode omp title slot: %w", err)
	}
	pad := titleSlotSize - 1 - len(encoded)
	if pad < 0 {
		return nil, fmt.Errorf("omp title slot exceeds %d bytes", titleSlotSize)
	}
	slot.Pad = strings.Repeat(" ", pad)
	encoded, err = json.Marshal(slot)
	if err != nil {
		return nil, fmt.Errorf("encode omp title slot padding: %w", err)
	}
	if len(encoded) != titleSlotSize-1 {
		return nil, fmt.Errorf("omp title slot is %d bytes, want %d", len(encoded)+1, titleSlotSize)
	}
	return append(encoded, '\n'), nil
}

// truncateJSONText returns the longest UTF-8 prefix whose JSON encoding adds
// at most budget bytes over an empty string. encoding/json escapes HTML and
// control runes, so byte accounting follows that exact representation.
func truncateJSONText(value string, budget int) string {
	used, end := 0, 0
	for end < len(value) {
		r, size := utf8.DecodeRuneInString(value[end:])
		encoded := jsonRuneBytes(r)
		if used+encoded > budget {
			break
		}
		used += encoded
		end += size
	}
	return value[:end]
}

func jsonRuneBytes(r rune) int {
	switch r {
	case '\b', '\f', '\n', '\r', '\t', '\\', '"':
		return 2
	case '<', '>', '&', '\u2028', '\u2029':
		return 6
	}
	if r < 0x20 {
		return 6
	}
	return utf8.RuneLen(r)
}
