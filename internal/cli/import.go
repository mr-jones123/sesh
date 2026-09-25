package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mr-jones123/sesh/internal/session"
)

func runImport(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	printJSON := flags.Bool("json", false, "print the complete bundle as JSON")
	flags.Usage = func() { printImportUsage(flags.Output()) }

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("import expects exactly one .sesh.json path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	file, err := os.Open(flags.Arg(0))
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()

	bundle, err := session.DecodeBundle(file)
	if err != nil {
		return fmt.Errorf("import %q: %w", flags.Arg(0), err)
	}
	if *printJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(bundle)
	}

	fmt.Fprintf(stdout, "session %s\n", bundle.Session.ID)
	fmt.Fprintf(stdout, "harness: %s\n", bundle.Session.Harness)
	fmt.Fprintf(stdout, "events: %d\n", len(bundle.Session.Events))
	fmt.Fprintf(stdout, "raw records: %d\n", len(bundle.RawRecords))
	if bundle.Source.Path != "" {
		fmt.Fprintf(stdout, "source: %s\n", bundle.Source.Path)
	}
	return nil
}

func printImportUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: sesh import [options] <session.sesh.json>

Reads and validates a portable sesh bundle locally.

Options:
  -json   print the complete bundle as JSON
`)
}
