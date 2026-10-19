package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/mr-jones123/sesh/internal/handoff"
	"github.com/mr-jones123/sesh/internal/registry"
	"github.com/mr-jones123/sesh/internal/tui"
)

func interactive(stdin io.Reader, stdout io.Writer) bool {
	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	return inOK && outOK && term.IsTerminal(in.Fd()) && term.IsTerminal(out.Fd())
}

func runPicker(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("sesh", flag.ContinueOnError)
	flags.SetOutput(stderr)
	all := flags.Bool("all", false, "browse sessions across all projects")
	dir := flags.String("dir", "", "project to browse instead of the current directory")
	from := flags.String("from", "", "show only claude, codex, pi, or omp")
	flags.Usage = func() { printRootUsage(flags.Output()) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments; use sesh handoff <ID> --to <harness>")
	}
	if *all && *dir != "" {
		return fmt.Errorf("--all and --dir are mutually exclusive")
	}
	if *from != "" {
		if _, err := registry.Find(*from); err != nil {
			return err
		}
	}
	if !interactive(stdin, stdout) {
		if len(args) == 0 {
			printRootUsage(stdout)
			return nil
		}
		return fmt.Errorf("the session picker requires a terminal; use sesh list or sesh handoff --no-run")
	}
	workspace, err := listDir(*dir)
	if err != nil {
		return err
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return fmt.Errorf("project directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project %q is not a directory", workspace)
	}
	result, err := tui.Run(ctx, tui.Options{Dir: workspace, All: *all, Harness: *from}, stdin, stdout, stderr)
	if err != nil || result == nil {
		return err
	}
	// Run has left the alternate screen and restored terminal input before launch.
	printHandoffResult(stdout, *result)
	return handoff.Launch(ctx, *result, stdin, stdout, stderr)
}
