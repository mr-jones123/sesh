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

	"github.com/mr-jones123/sesh/internal/catalog"
	"github.com/mr-jones123/sesh/internal/handoff"
	"github.com/mr-jones123/sesh/internal/registry"
)

func runHandoff(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("handoff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	from := flags.String("from", "", "source harness: claude, codex, pi, omp")
	to := flags.String("to", "", "target harness: claude, codex, pi, omp")
	dir := flags.String("dir", "", "project to select the newest source session from")
	workspace := flags.String("workspace", "", "override the source workspace")
	model := flags.String("model", "", "Pi model as provider/model-id")
	noRun := flags.Bool("no-run", false, "write the native session without launching the target")
	flags.Usage = func() { printHandoffUsage(flags.Output()) }
	if err := parseInterspersed(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 1 || *to == "" {
		flags.Usage()
		return fmt.Errorf("handoff requires --to and accepts at most one session ID or path")
	}
	if _, err := registry.FindExporter(*to); err != nil {
		return err
	}
	if *from != "" {
		if _, err := registry.Find(*from); err != nil {
			return err
		}
	}
	if !*noRun && !interactive(stdin, stdout) {
		return fmt.Errorf("launching an agent requires a terminal; use --no-run to write the session and print its resume command")
	}
	if flags.NArg() != 0 && *dir != "" {
		return fmt.Errorf("--dir selects the newest project session; omit it when specifying a session ID or path")
	}
	source, err := selectSource(ctx, flags.Arg(0), *from, *dir)
	if err != nil {
		return err
	}
	result, err := handoff.Create(ctx, handoff.Request{Source: source, Target: *to, Workspace: *workspace, Model: *model})
	if err != nil {
		return err
	}
	printHandoffResult(stdout, result)
	if *noRun {
		return nil
	}
	return handoff.Launch(ctx, result, stdin, stdout, stderr)
}

func selectSource(ctx context.Context, arg, from, dir string) (catalog.Entry, error) {
	if arg == "" {
		if from == "" {
			return catalog.Entry{}, fmt.Errorf("pass --from when selecting the newest project session, or choose an ID with sesh list")
		}
		workspace, err := listDir(dir)
		if err != nil {
			return catalog.Entry{}, err
		}
		entries, err := catalog.List(ctx, catalog.Query{Dir: workspace, Harness: from})
		if err != nil {
			return catalog.Entry{}, err
		}
		for _, entry := range entries {
			if entry.Err == nil {
				return entry, nil
			}
		}
		return catalog.Entry{}, fmt.Errorf("no readable %s sessions for %s; use sesh list --all to choose another project", from, workspace)
	}
	path, err := filepath.Abs(arg)
	if err != nil {
		return catalog.Entry{}, err
	}
	info, err := os.Stat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return catalog.Entry{}, fmt.Errorf("source %q is not a regular file", arg)
		}
		if from == "" {
			adapter, err := registry.Detect(path)
			if err != nil {
				return catalog.Entry{}, err
			}
			from = adapter.Name()
		}
		return catalog.Entry{Harness: from, Path: path}, nil
	}
	if !os.IsNotExist(err) {
		return catalog.Entry{}, fmt.Errorf("source %q: %w", arg, err)
	}
	if strings.ContainsAny(arg, `/\`) || strings.HasSuffix(arg, ".jsonl") {
		return catalog.Entry{}, fmt.Errorf("source %q: no such file", arg)
	}
	return catalog.Find(arg, from)
}

// Go's flag parser stops at the first positional argument. Preserve the usual
// `handoff <id> --to codex` spelling without guessing which options take values.
func parseInterspersed(flags *flag.FlagSet, args []string) error {
	var options, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		options = append(options, arg)
		name, _, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		f := flags.Lookup(name)
		if f == nil || hasValue {
			continue
		}
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			options = append(options, args[i])
		}
	}
	return flags.Parse(append(append(options, "--"), positional...))
}

func printHandoffResult(w io.Writer, result handoff.Result) {
	fmt.Fprintf(w, "created native session %s at %s\nworkspace: %s\nresume with: %s\n", result.SessionID, result.Path, result.Workspace, result.ResumeCommand())
}

func printHandoffUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: sesh handoff [session ID | transcript path] --to claude|codex|pi|omp [options]
       sesh handoff --from claude|codex|pi|omp --to claude|codex|pi|omp [options]

Writes a new native session and resumes the target agent. With no ID or path,
selects the newest session of --from in the current project (or --dir).
Explicit IDs are resolved across projects; ambiguous prefixes are rejected.
The source is untouched. History is local and unredacted, not safe for sharing.
The target may send that history to its configured model provider on resume.

Options:
  --from name       source harness; required for newest-project selection
  --to name         target harness (required)
  --dir path        project to select from instead of the current directory
  --workspace path  override the workspace recorded in the target session
  --model p/id      Pi model override
  --no-run          write only; print the resume command (for agents/scripts)

Recorded tool calls are history: sesh never executes them.
`)
}
