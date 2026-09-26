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

	"github.com/mr-jones123/sesh/internal/catalog"
)

func runList(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	all := flags.Bool("all", false, "list sessions from every directory")
	dir := flags.String("dir", "", "directory to list instead of the current one")
	harnessName := flags.String("harness", "", "only pi, claude, or codex")
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
		fmt.Fprintln(table, "HARNESS\tUPDATED\tID\tEVENTS\tWORKSPACE\tFIRST MESSAGE")
	} else {
		fmt.Fprintln(table, "HARNESS\tUPDATED\tID\tEVENTS\tFIRST MESSAGE")
	}
	for _, e := range shown {
		prompt := truncate(e.Prompt, 60)
		if e.Err != nil {
			prompt = "(could not read: " + truncate(e.Err.Error(), 50) + ")"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%d\t", e.Harness, ago(e.Updated, now), shortID(e.ID), e.Events)
		if *all {
			fmt.Fprintf(table, "%s\t", displayPath(e.Workspace))
		}
		fmt.Fprintln(table, prompt)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if len(shown) < len(entries) {
		fmt.Fprintf(stdout, "… %d older; use -n 0 to show all\n", len(entries)-len(shown))
	}
	fmt.Fprintln(stdout, "\nexport one with: sesh export <ID>   (or sesh export -last for the newest)")
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

// shortID is enough of an ID to pass to sesh export.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
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

func printListUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: sesh list [options]

Lists the sessions Claude Code, Codex, and Pi recorded for the current
directory, newest first, with the first message you typed in each. Pass an
ID (or its first characters) to sesh export.

Sessions are read from ~/.claude (or $CLAUDE_CONFIG_DIR), ~/.codex (or
$CODEX_HOME), and ~/.pi/agent (or $PI_CODING_AGENT_DIR).

Options:
  -all           list sessions from every directory, with their workspace
  -dir path      list another directory instead of the current one
  -harness name  only pi, claude, or codex
  -n count       show at most count sessions (default 20; 0 for all)
`)
}
