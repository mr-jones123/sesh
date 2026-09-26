// Package catalog finds the sessions every supported harness has recorded
// on this machine, so a session can be picked by directory, recency, or ID
// instead of by its file path.
package catalog

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/registry"
	"github.com/mr-jones123/sesh/internal/session"
)

// Entry is one recorded session.
type Entry struct {
	Harness   string
	Path      string
	ID        string
	Workspace string
	Updated   time.Time // the transcript's modification time
	Prompt    string    // the first message the user typed, on one line
	Events    int
	Err       error // set when the transcript could not be imported
}

// Query selects sessions. Zero values select everything.
type Query struct {
	Dir     string // only sessions started in this directory
	Harness string // only this harness
}

// List returns the sessions matching q, most recently updated first. Each
// candidate is imported to read its workspace and first prompt.
func List(ctx context.Context, q Query) ([]Entry, error) {
	files, err := locate(q)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, len(files))
	var wg sync.WaitGroup
	next := make(chan int)
	for range min(8, len(files)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				entries[i] = read(ctx, files[i])
			}
		}()
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if q.Dir != "" {
		// A storage location is only a hint (Claude's project names are
		// lossy), so keep sessions whose recorded workspace is the directory.
		dirs := harness.SameDirs(q.Dir)
		entries = slices.DeleteFunc(entries, func(e Entry) bool {
			return e.Err == nil && e.Workspace != "" && !slices.Contains(dirs, e.Workspace)
		})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Updated.After(entries[j].Updated) })
	return entries, nil
}

// Find resolves a session ID or unique ID prefix across every directory. It
// matches file names, so nothing is imported until the match is known.
func Find(id, harnessName string) (Entry, error) {
	files, err := locate(Query{Harness: harnessName})
	if err != nil {
		return Entry{}, err
	}
	var matches []located
	for _, f := range files {
		if strings.HasPrefix(f.ID, id) {
			matches = append(matches, f)
		}
	}
	switch len(matches) {
	case 0:
		return Entry{}, fmt.Errorf("no session with ID %q; run sesh list -all", id)
	case 1:
		return Entry{Harness: matches[0].harness, Path: matches[0].Path, ID: matches[0].ID}, nil
	}
	const shown = 10
	names := make([]string, 0, shown+1)
	for _, m := range matches[:min(shown, len(matches))] {
		names = append(names, m.harness+" "+m.ID)
	}
	if len(matches) > shown {
		names = append(names, fmt.Sprintf("… %d more", len(matches)-shown))
	}
	return Entry{}, fmt.Errorf("ID %q matches %d sessions; use more characters:\n  %s", id, len(matches), strings.Join(names, "\n  "))
}

type located struct {
	harness.SessionFile
	harness string
}

func locate(q Query) ([]located, error) {
	var files []located
	for _, adapter := range registry.Adapters() {
		locator, ok := adapter.(harness.Locator)
		if !ok || (q.Harness != "" && adapter.Name() != q.Harness) {
			continue
		}
		found, err := locator.Sessions(q.Dir)
		if err != nil {
			return nil, fmt.Errorf("find %s sessions: %w", adapter.Name(), err)
		}
		for _, f := range found {
			files = append(files, located{SessionFile: f, harness: adapter.Name()})
		}
	}
	return files, nil
}

func read(ctx context.Context, f located) Entry {
	entry := Entry{Harness: f.harness, Path: f.Path, ID: f.ID}
	if info, err := os.Stat(f.Path); err == nil {
		entry.Updated = info.ModTime()
	}
	adapter, err := registry.Find(f.harness)
	if err != nil {
		entry.Err = err
		return entry
	}
	bundle, err := adapter.Import(ctx, f.Path)
	if err != nil {
		entry.Err = err
		return entry
	}
	entry.Workspace = bundle.Session.Workspace
	entry.Events = len(bundle.Session.Events)
	entry.Prompt = FirstPrompt(bundle.Session.Events)
	return entry
}

// injected are prefixes of user messages the harness or sesh wrote, not the
// user: Codex's environment and AGENTS.md context, Claude's slash-command
// and interruption records, skill bodies, and sesh's own markers.
var injected = []string{
	"<",
	"# AGENTS.md instructions",
	"[Request interrupted",
	"Caveat: The messages below",
	"The conversation before this point was summarized",
	"[Imported by sesh",
}

// codexRequest heads the typed text in a Codex message that mentions files.
const codexRequest = "## My request for Codex:"

// FirstPrompt is the first user message that the user typed, on one line.
func FirstPrompt(events []session.Event) string {
	for _, e := range events {
		if e.Type != session.EventMessage || e.Role != session.RoleUser {
			continue
		}
		text := strings.TrimSpace(e.Text)
		if _, request, ok := strings.Cut(text, codexRequest); ok {
			text = strings.TrimSpace(request)
		}
		if text == "" || slices.ContainsFunc(injected, func(p string) bool { return strings.HasPrefix(text, p) }) {
			continue
		}
		return strings.Join(strings.FieldsFunc(text, unicode.IsSpace), " ")
	}
	return ""
}
