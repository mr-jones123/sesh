// Package cli translates command-line arguments into application commands.
package cli

import (
	"context"
	"fmt"
	"io"
)

// Version can be replaced at build time with:
//
//	go build -ldflags "-X github.com/mr-jones123/sesh/internal/cli.Version=v0.1.0" ./cmd/sesh
var Version = "dev"

// Run is the CLI entry point. Arguments and output streams are passed in
// instead of read globally, which keeps command parsing easy to test.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(args) == 0 {
		printRootUsage(stdout)
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		return runHelp(args[1:], stdout)
	case "version", "-v", "--version":
		if len(args) != 1 {
			return fmt.Errorf("version does not accept arguments")
		}
		fmt.Fprintln(stdout, Version)
		return nil
	case "inspect":
		return runInspect(ctx, args[1:], stdout, stderr)
	case "export":
		return runExport(ctx, args[1:], stdout, stderr)
	case "import":
		return runImport(ctx, args[1:], stdout, stderr)
	default:
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
	case "inspect":
		printInspectUsage(stdout)
	case "export":
		printExportUsage(stdout)
	case "import":
		printImportUsage(stdout)
	case "version":
		fmt.Fprintln(stdout, "Usage: sesh version")
	default:
		return fmt.Errorf("no help topic for %q", args[0])
	}

	return nil
}

func printRootUsage(w io.Writer) {
	fmt.Fprint(w, `sesh shares and forks AI coding sessions.

Usage:
  sesh <command> [options]

Commands:
  inspect   Check that a file contains valid JSON
  export    Convert a harness transcript to a .sesh.json bundle
  import    Read and validate a .sesh.json bundle
  version   Print the build version
  help      Show command help

Run "sesh help <command>" for command-specific help.
`)
}
