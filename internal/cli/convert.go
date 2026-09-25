package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mr-jones123/sesh/internal/harness"
	"github.com/mr-jones123/sesh/internal/registry"
	"github.com/mr-jones123/sesh/internal/session"
)

func runConvert(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("convert", flag.ContinueOnError)
	flags.SetOutput(stderr)
	target := flags.String("target", "", "target harness: pi")
	model := flags.String("model", "", "provider/model-id the target resumes with")
	workspace := flags.String("workspace", "", "working directory recorded in the target session")
	output := flags.String("output", "", "output .jsonl path")
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
	if err := ctx.Err(); err != nil {
		return err
	}
	exporter, err := registry.FindExporter(*target)
	if err != nil {
		return err
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

	outputPath := *output
	if outputPath == "" {
		outputPath = strings.TrimSuffix(bundlePath, ".sesh.json") + "." + exporter.Name() + ".jsonl"
	}
	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output %q: %w", outputPath, err)
	}
	opts := harness.ExportOptions{Model: *model, Workspace: *workspace}
	if err := exporter.Export(ctx, bundle, opts, out); err != nil {
		_ = out.Close()
		_ = os.Remove(outputPath)
		return fmt.Errorf("convert to %s: %w", exporter.Name(), err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}

	fmt.Fprintf(stdout, "wrote %s session to %s\n", exporter.Name(), outputPath)
	fmt.Fprintf(stdout, "open it with: pi --session %s\n", outputPath)
	return nil
}

func printConvertUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: sesh convert --target pi [options] <session.sesh.json>

Writes a bundle as a native session for the target harness. The file is
written only to the output path; nothing is installed into the harness's
own session directory. Recorded tool calls are history and are not run.

Tools the target has (shell, read, write, edit) become real tool calls;
other tools are kept as text. A Pi bundle with no overrides is copied
back byte-for-byte.

Options:
  -target name      target harness: pi
  -model p/id       provider/model-id to resume with, e.g. openai-codex/gpt-5.6-sol
  -workspace dir    working directory to record; defaults to the source's
  -output path      output path; defaults to <bundle>.<target>.jsonl
`)
}
