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

	adapter, err := registry.Detect(sourcePath)
	if *harnessName != "" {
		adapter, err = registry.Find(*harnessName)
	}
	if err != nil {
		return err
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

	fmt.Fprintf(stdout, "exported %d events to %s\n", len(bundle.Session.Events), outputPath)
	if !*noRedact {
		printFindings(stdout, findings)
	}
	return nil
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
	fmt.Fprint(w, `Usage: sesh export [options] <session.jsonl>

Converts a local harness transcript into a portable .sesh.json bundle.

Secrets, email addresses and home-directory paths are replaced in the bundle
by default, in both the event timeline and the raw source lines: API keys and
tokens with a known format, URL passwords, upper-case *_TOKEN/*_SECRET/
*_PASSWORD/*_API_KEY values, emails, and /Users/<name> or /home/<name> (as ~).
The rules are fixed regular expressions, so the same transcript always gives
the same bundle. They catch known formats only: review a bundle before
sharing it. The source transcript is never modified.

Options:
  -harness name  pi, claude, or codex; auto-detected when omitted
  -output path   output path; defaults to <source>.sesh.json
  -no-redact     keep everything; raw records stay byte-exact
`)
}
