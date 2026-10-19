// Package cli translates command-line arguments into application commands.
package cli

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

// Version is the release this binary was built from. It is empty unless set
// at build time with:
//
//	go build -ldflags "-X github.com/mr-jones123/sesh/internal/cli.Version=v0.2.0" ./cmd/sesh
//
// Otherwise the version Go recorded in the binary is used: the tag for
// `go install github.com/mr-jones123/sesh/cmd/sesh@v0.2.0`, or a
// pseudo-version for a build from a git checkout.
var Version string

func version() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// Run is the CLI entry point. Arguments and output streams are passed in
// instead of read globally, which keeps command parsing easy to test.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(args) == 0 {
		return runPicker(ctx, args, stdin, stdout, stderr)
	}

	switch args[0] {
	case "help", "-h", "--help":
		return runHelp(args[1:], stdout)
	case "version", "-v", "--version":
		if len(args) != 1 {
			return fmt.Errorf("version does not accept arguments")
		}
		fmt.Fprintln(stdout, version())
		return nil
	case "handoff":
		return runHandoff(ctx, args[1:], stdin, stdout, stderr)
	case "list":
		return runList(ctx, args[1:], stdout, stderr)
	default:
		if strings.HasPrefix(args[0], "-") {
			return runPicker(ctx, args, stdin, stdout, stderr)
		}
		printRootUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runHelp(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		printRootUsage(stdout)
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("help accepts at most one command")
	}

	switch args[0] {
	case "list":
		printListUsage(stdout)
	case "handoff":
		printHandoffUsage(stdout)
	case "version":
		fmt.Fprintln(stdout, "Usage: sesh version")
	default:
		return fmt.Errorf("no help topic for %q", args[0])
	}

	return nil
}

func printRootUsage(w io.Writer) {
	fmt.Fprint(w, `sesh hands local AI coding sessions between Claude Code, Codex, Pi, and OMP.

Usage:
  sesh                         Browse this project's sessions
  sesh --all                   Browse sessions across all projects
  sesh --dir path              Browse another project
  sesh --from claude|codex|pi|omp   Filter the session picker
  sesh handoff [ID|path] --to claude|codex|pi|omp [options]

Commands:
  list      List sessions (use --all for every project)
  handoff   Write a native session and resume the target agent
  version   Print the build version
  help      Show command help

The picker requires a terminal. For agents/scripts, use handoff --no-run.
History is local and unredacted; this is not a sanitized sharing tool.
Run "sesh help handoff" or "sesh help list" for options.
`)
}
