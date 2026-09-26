package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/ids"
	"github.com/mr-jones123/sesh/internal/registry"
	"github.com/mr-jones123/sesh/internal/session"
)

func runConvert(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("convert", flag.ContinueOnError)
	flags.SetOutput(stderr)
	target := flags.String("target", "", "target harness: pi, codex, claude")
	model := flags.String("model", "", "provider/model-id the target resumes with (pi)")
	workspace := flags.String("workspace", "", "working directory recorded in the target session")
	output := flags.String("output", "", "output .jsonl path")
	install := flags.Bool("install", false, "place the session in the target harness's session directory")
	yes := flags.Bool("yes", false, "with -install, skip the confirmation prompt")
	flags.Usage = func() { printConvertUsage(flags.Output()) }

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("convert expects exactly one .sesh.json path")
	}
	if *target == "" {
		flags.Usage()
		return fmt.Errorf("convert requires --target")
	}
	if *install && *output != "" {
		return fmt.Errorf("-install and -output are mutually exclusive")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	exporter, err := registry.FindExporter(*target)
	if err != nil {
		return err
	}
	installer, installable := exporter.(harness.Installer)
	if *install && !installable {
		return fmt.Errorf("%s opens session files directly; convert without -install and pass the file to %s", exporter.Name(), exporter.Name())
	}

	bundlePath := flags.Arg(0)
	file, err := os.Open(bundlePath)
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	bundle, err := session.DecodeBundle(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("convert %q: %w", bundlePath, err)
	}
	if len(bundle.Session.Events) == 0 {
		return fmt.Errorf("convert %q: the session has no conversation to convert", bundlePath)
	}

	opts := harness.ExportOptions{Model: *model, Workspace: *workspace}
	if installable {
		opts.CreatedAt = time.Now()
		opts.SessionID = ids.NewV7(opts.CreatedAt)
	}

	outputPath := *output
	switch {
	case *install:
		if outputPath, err = installer.InstallPath(bundle, opts); err != nil {
			return err
		}
		if !*yes && !confirm(stdin, stdout, fmt.Sprintf("install %s session to %s? [y/N] ", exporter.Name(), outputPath)) {
			return fmt.Errorf("install cancelled")
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
			return fmt.Errorf("create session directory: %w", err)
		}
	case outputPath == "":
		outputPath = strings.TrimSuffix(bundlePath, ".sesh.json") + "." + exporter.Name() + ".jsonl"
	}
	if err := writeExport(ctx, exporter, bundle, opts, outputPath, *install); err != nil {
		return err
	}

	switch {
	case *install:
		fmt.Fprintf(stdout, "installed %s session %s at %s\n", exporter.Name(), opts.SessionID, outputPath)
		fmt.Fprintf(stdout, "resume it with: %s\n", installer.ResumeCommand(bundle, opts))
	case installable:
		fmt.Fprintf(stdout, "wrote %s session %s to %s\n", exporter.Name(), opts.SessionID, outputPath)
		fmt.Fprintf(stdout, "%s resumes sessions only from its own directory; rerun with -install to place it there\n", exporter.Name())
	default:
		fmt.Fprintf(stdout, "wrote %s session to %s\n", exporter.Name(), outputPath)
		fmt.Fprintf(stdout, "open it with: pi --session %s\n", outputPath)
	}
	return nil
}

// writeExport writes the converted session to path. An installed session
// never replaces an existing file; a failed export leaves no partial file.
func writeExport(ctx context.Context, exporter harness.Exporter, bundle session.Bundle, opts harness.ExportOptions, path string, exclusive bool) error {
	mode := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive {
		mode = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	out, err := os.OpenFile(path, mode, 0o600)
	if err != nil {
		return fmt.Errorf("create output %q: %w", path, err)
	}
	if err := exporter.Export(ctx, bundle, opts, out); err != nil {
		_ = out.Close()
		_ = os.Remove(path)
		return fmt.Errorf("convert to %s: %w", exporter.Name(), err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	return nil
}

// confirm asks a yes/no question; anything but "y" or "yes" is no.
func confirm(stdin io.Reader, stdout io.Writer, question string) bool {
	fmt.Fprint(stdout, question)
	if stdin == nil {
		return false
	}
	answer, _ := bufio.NewReader(stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func printConvertUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: sesh convert --target pi|codex|claude [options] <session.sesh.json>

Writes a bundle as a native session for the target harness. Recorded tool
calls are history and are never run.

Shell, write, and edit calls (and reads, where the target has a read tool)
become the target's own tools; other tools are kept as text. Reasoning from
another model is dropped for Codex and Claude; Pi converts it to text
itself. A Pi bundle converted to Pi with no overrides is copied back
byte-for-byte.

Pi opens any session file (pi --session <file>). Codex and Claude resume
sessions only by ID from their own directories, so use -install to place
the file there: $CODEX_HOME (default ~/.codex) or $CLAUDE_CONFIG_DIR
(default ~/.claude, filed under the workspace). An existing session is
never overwritten.

Options:
  -target name      target harness: pi, codex, claude
  -model p/id       pi only: provider/model-id to resume with
  -workspace dir    working directory to record; defaults to the source's
  -output path      output path; defaults to <bundle>.<target>.jsonl
  -install          place the session in the target's session directory
  -yes              with -install, skip the confirmation prompt
`)
}
