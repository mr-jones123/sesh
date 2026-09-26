package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mr-jones123/sesh/internal/catalog"
	"github.com/mr-jones123/sesh/internal/redact"
	"github.com/mr-jones123/sesh/internal/registry"
	"github.com/mr-jones123/sesh/internal/session"
)

func runExport(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	harnessName := flags.String("harness", "", "source harness: pi, claude, or codex")
	output := flags.String("output", "", "output .sesh.json path")
	noRedact := flags.Bool("no-redact", false, "keep secrets and paths; raw records stay byte-exact")
	last := flags.Bool("last", false, "export the newest session recorded for the current directory")
	flags.Usage = func() { printExportUsage(flags.Output()) }

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *last != (flags.NArg() == 0) || flags.NArg() > 1 {
		flags.Usage()
		return fmt.Errorf("export expects one session path or ID, or -last")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	source, err := findSource(ctx, flags.Arg(0), *last, *harnessName)
	if err != nil {
		return err
	}
	sourcePath := source.Path
	adapter, err := registry.Find(source.Harness)
	if err != nil {
		return err
	}
	if source.ID != "" {
		fmt.Fprintf(stdout, "session %s (%s)", source.ID, source.Harness)
		if source.Prompt != "" {
			fmt.Fprintf(stdout, ": %s", truncate(source.Prompt, 60))
		}
		fmt.Fprintln(stdout)
	}

	bundle, err := adapter.Import(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("import %s session: %w", adapter.Name(), err)
	}
	bundle.FormatVersion = session.CurrentFormatVersion
	bundle.Source = session.Source{Path: sourcePath}
	var findings []redact.Finding
	if !*noRedact {
		redactor := redact.New()
		if err := redactor.Bundle(&bundle); err != nil {
			return fmt.Errorf("redact %s session: %w", adapter.Name(), err)
		}
		findings = redactor.Findings
	}

	outputPath := *output
	switch {
	case outputPath != "":
	case source.ID != "":
		// Found by ID or -last: the transcript sits in the harness's own
		// directory, so the bundle goes to the current one.
		outputPath = source.Harness + "-" + shortID(source.ID) + ".sesh.json"
	default:
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

	fmt.Fprintf(stdout, "exported %d events to %s\n", len(bundle.Session.Events), outputPath)
	if !*noRedact {
		printFindings(stdout, findings)
	}
	return nil
}

// findSource resolves export's argument: a transcript path, a session ID or
// ID prefix, or with last the newest session for the current directory. A
// source found by path has no ID set.
func findSource(ctx context.Context, arg string, last bool, harnessName string) (catalog.Entry, error) {
	if last {
		dir, err := os.Getwd()
		if err != nil {
			return catalog.Entry{}, fmt.Errorf("find current directory: %w", err)
		}
		entries, err := catalog.List(ctx, catalog.Query{Dir: dir, Harness: harnessName})
		if err != nil {
			return catalog.Entry{}, err
		}
		for _, e := range entries {
			if e.Err == nil {
				return e, nil
			}
		}
		return catalog.Entry{}, fmt.Errorf("no readable sessions recorded for %s; see sesh list -all", displayPath(dir))
	}

	path, err := filepath.Abs(arg)
	if err != nil {
		return catalog.Entry{}, fmt.Errorf("resolve source path: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		name := harnessName
		if name == "" {
			adapter, err := registry.Detect(path)
			if err != nil {
				return catalog.Entry{}, err
			}
			name = adapter.Name()
		}
		return catalog.Entry{Harness: name, Path: path}, nil
	}
	if strings.ContainsAny(arg, `/\`) || strings.HasSuffix(arg, ".jsonl") {
		return catalog.Entry{}, fmt.Errorf("source %q: no such file", arg)
	}
	return catalog.Find(arg, harnessName)
}

// printFindings prints how many replacements each rule made, most first.
func printFindings(w io.Writer, findings []redact.Finding) {
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Rule]++
	}
	rules := make([]string, 0, len(counts))
	for rule := range counts {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if counts[rules[i]] != counts[rules[j]] {
			return counts[rules[i]] > counts[rules[j]]
		}
		return rules[i] < rules[j]
	})
	fmt.Fprintf(w, "redacted %d matches\n", len(findings))
	for _, rule := range rules {
		fmt.Fprintf(w, "  %-18s %d\n", rule, counts[rule])
	}
}

func printExportUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: sesh export [options] <session.jsonl | session ID>
       sesh export [options] -last

Converts a local harness transcript into a portable .sesh.json bundle.

The session can be a transcript path, a session ID or its first characters
as shown by sesh list, or -last for the newest session recorded for the
current directory. A session found by ID or -last is written to
<harness>-<id>.sesh.json in the current directory.

Secrets, email addresses and home-directory paths are replaced in the bundle
by default, in both the event timeline and the raw source lines: API keys and
tokens with a known format, URL passwords, upper-case *_TOKEN/*_SECRET/
*_PASSWORD/*_API_KEY values, emails, and /Users/<name>, /home/<name> or /root (as ~).
The rules are fixed regular expressions, so the same transcript always gives
the same bundle. They catch known formats only: review a bundle before
sharing it. The source transcript is never modified.

Options:
  -harness name  pi, claude, or codex; auto-detected from a path, and narrows
                 an ID or -last to one harness
  -last          export the newest session recorded for the current directory
  -output path   output path; defaults to <source>.sesh.json for a path
  -no-redact     keep everything; raw records stay byte-exact
`)
}
