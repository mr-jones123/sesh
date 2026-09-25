package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func runInspect(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)

	printJSON := flags.Bool("json", false, "print normalized JSON after validation")
	flags.Usage = func() {
		printInspectUsage(flags.Output())
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("inspect expects exactly one file path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	path := flags.Arg(0)
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	defer file.Close()

	value, err := decodeJSON(file)
	if err != nil {
		return fmt.Errorf("inspect %q: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if !*printJSON {
		fmt.Fprintf(stdout, "%s: valid JSON\n", path)
		return nil
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("print JSON: %w", err)
	}
	return nil
}

func decodeJSON(reader io.Reader) (any, error) {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}

	var trailing any
	err := decoder.Decode(&trailing)
	switch {
	case errors.Is(err, io.EOF):
		return value, nil
	case err == nil:
		return nil, fmt.Errorf("file contains more than one JSON value")
	default:
		return nil, fmt.Errorf("decode trailing JSON: %w", err)
	}
}

func printInspectUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: sesh inspect [options] <path>

Checks that a session file contains exactly one valid JSON value.
Schema validation will be added with the session package.

Options:
  -json   print normalized JSON after validation
`)
}
