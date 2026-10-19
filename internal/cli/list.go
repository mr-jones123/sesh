package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mr-jones123/sesh/internal/catalog"
	"github.com/mr-jones123/sesh/internal/registry"
)

func runList(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	all := flags.Bool("all", false, "list sessions from every directory")
	dir := flags.String("dir", "", "directory to list instead of the current one")
	harnessName := flags.String("harness", "", "only pi, claude, codex, or omp")
	limit := flags.Int("n", 20, "show at most this many sessions; 0 for all")
	flags.Usage = func() { printListUsage(flags.Output()) }

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return fmt.Errorf("list does not take arguments")
	}
	if *all && *dir != "" {
		return fmt.Errorf("-all and -dir are mutually exclusive")
	}
	if *harnessName != "" {
		if _, err := registry.Find(*harnessName); err != nil {
			return err
		}
	}
	if *limit < 0 {
		return fmt.Errorf("-n must be non-negative")
	}

	query := catalog.Query{Harness: *harnessName}
	if !*all {
		var err error
		if query.Dir, err = listDir(*dir); err != nil {
			return err
		}
	}
	entries, err := catalog.List(ctx, query)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		where := "any directory"
		if query.Dir != "" {
			where = displayPath(query.Dir)
		}
		fmt.Fprintf(stdout, "no sessions for %s\n", where)
		return nil
	}

	shown := entries
	if *limit > 0 && len(shown) > *limit {
		shown = shown[:*limit]
	}
	now := time.Now()
	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if *all {
		fmt.Fprintln(table, "HARNESS\tUPDATED\tID\tWORKSPACE\tSESSION")
	} else {
		fmt.Fprintln(table, "HARNESS\tUPDATED\tID\tSESSION")
	}
	for _, e := range shown {
		name := e.Title
		if strings.TrimSpace(name) == "" {
			name = e.Prompt
		}
		name = truncate(safeText(name), 60)
		if e.Err != nil {
			name = "(could not read: " + truncate(e.Err.Error(), 50) + ")"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t", e.Harness, ago(e.Updated, now), safeText(e.ID))
		if *all {
			fmt.Fprintf(table, "%s\t", safeText(displayPath(e.Workspace)))
		}
		fmt.Fprintln(table, safeText(name))
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if len(shown) < len(entries) {
		fmt.Fprintf(stdout, "… %d older; use -n 0 to show all\n", len(entries)-len(shown))
	}
	fmt.Fprintln(stdout, "\nhandoff with: sesh handoff <ID> --to claude|codex|pi|omp")
	return nil
}

func listDir(dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("find current directory: %w", err)
		}
		return wd, nil
	}
	return filepath.Abs(dir)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// displayPath writes the home directory as ~.
func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}

// ago says when t was, relative to now, the way a person would.
func ago(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	}
	t = t.Local()
	y, m, day := now.Local().Date()
	today := time.Date(y, m, day, 0, 0, 0, 0, time.Local)
	switch {
	case !t.Before(today):
		return "today " + t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -1)):
		return "yesterday " + t.Format("15:04")
	case t.Year() == y:
		return t.Format("Jan 2 15:04")
	}
	return t.Format("2006-01-02")
}

func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f || r >= 0x80 && r <= 0x9f {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

func printListUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: sesh list [options]

Lists the sessions Claude Code, Codex, Pi, and OMP recorded for the current
directory, newest first, with saved session names (first prompt if untitled).
Pass an ID or unique prefix to sesh handoff <ID> --to <harness>.

Stores: ~/.claude ($CLAUDE_CONFIG_DIR), ~/.codex ($CODEX_HOME),
~/.pi/agent ($PI_CODING_AGENT_DIR), ~/.omp/agent ($SESH_OMP_AGENT_DIR).

Options:
  -all           list sessions from every directory, with their workspace
  -dir path      list another directory instead of the current one
  -harness name  only pi, claude, codex, or omp
  -n count       show at most count sessions (default 20; 0 for all)
`)
}
