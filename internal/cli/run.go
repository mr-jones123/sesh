// Package cli translates command-line arguments into application commands.
package cli

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
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
		fmt.Fprintln(stdout, version())
		return nil
	case "inspect":
		return runInspect(ctx, args[1:], stdout, stderr)
	case "export":
		return runExport(ctx, args[1:], stdout, stderr)
	case "list":
		return runList(ctx, args[1:], stdout, stderr)
	case "import":
		return runImport(ctx, args[1:], stdout, stderr)
	case "convert":
		return runConvert(ctx, args[1:], stdin, stdout, stderr)
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
	case "list":
		printListUsage(stdout)
	case "import":
		printImportUsage(stdout)
	case "convert":
		printConvertUsage(stdout)
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
  list      List the sessions recorded for this directory
  export    Convert a harness transcript to a .sesh.json bundle
  import    Read and validate a .sesh.json bundle
  convert   Write a bundle as another harness's native session
  inspect   Check that a file contains valid JSON
  version   Print the build version
  help      Show command help

Run "sesh help <command>" for command-specific help.
`)
}
