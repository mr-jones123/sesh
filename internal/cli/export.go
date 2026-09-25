package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mr-jones123/sesh/internal/importer"
	"github.com/mr-jones123/sesh/internal/session"
)

func runExport(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	harnessName := flags.String("harness", "", "source harness: pi, claude, or codex")
	output := flags.String("output", "", "output .sesh.json path")
	flags.Usage = func() { printExportUsage(flags.Output()) }

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("export expects exactly one source path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	sourcePath, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}

	adapter, err := importer.Detect(sourcePath)
	if *harnessName != "" {
		adapter, err = importer.Find(*harnessName)
	}
	if err != nil {
		return err
	}

	result, err := adapter.Import(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("import %s session: %w", adapter.Name(), err)
	}
	bundle := session.Bundle{
		FormatVersion: session.CurrentFormatVersion,
		Source:        session.Source{Path: sourcePath},
		Session:       result,
		RawRecords:    result.RawRecords,
	}

	outputPath := *output
	if outputPath == "" {
		outputPath = sourcePath + ".sesh.json"
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output %q: %w", outputPath, err)
	}
	if err := session.EncodeBundle(file, bundle); err != nil {
		_ = file.Close()
		return fmt.Errorf("write bundle: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}

	fmt.Fprintf(stdout, "exported %d events to %s\n", len(result.Events), outputPath)
	return nil
}

func printExportUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: sesh export [options] <session.jsonl>

Converts a local harness transcript into a portable .sesh.json bundle.

Options:
  -harness name  pi, claude, or codex; auto-detected when omitted
  -output path   output path; defaults to <source>.sesh.json
`)
}
