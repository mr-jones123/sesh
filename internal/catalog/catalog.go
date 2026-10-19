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

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/registry"
)

// Entry is one recorded session.
type Entry struct {
	Harness   string
	Path      string
	ID        string
	Title     string
	Workspace string
	Updated   time.Time // the transcript's modification time
	Prompt    string    // the first message the user typed, on one line
	Err       error     // set when the transcript could not be summarized
}

// Query selects sessions. Zero values select everything.
type Query struct {
	Dir     string // only sessions started in this directory
	Harness string // only this harness
}

// List returns the sessions matching q, most recently updated first. Each
// candidate is summarized without importing its event history.
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
		return Entry{}, fmt.Errorf("no session with ID %q; run sesh list --all", id)
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
	adapter harness.Adapter
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
			files = append(files, located{SessionFile: f, harness: adapter.Name(), adapter: adapter})
		}
	}
	return files, nil
}

func read(ctx context.Context, f located) Entry {
	entry := Entry{Harness: f.harness, Path: f.Path, ID: f.ID, Title: f.Title}
	if info, err := os.Stat(f.Path); err == nil {
		entry.Updated = info.ModTime()
	}
	summary, err := f.adapter.Summarize(ctx, f.Path)
	if err != nil {
		entry.Err = err
		return entry
	}
	if entry.ID == "" {
		entry.ID = summary.ID
	}
	if entry.Title == "" {
		entry.Title = summary.Title
	}
	entry.Workspace = summary.Workspace
	entry.Prompt = summary.Prompt
	return entry
}
