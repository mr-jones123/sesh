// Package redact removes secrets and personal paths from session bundles.
//
// Redaction is deterministic: a fixed, ordered list of regular expressions
// runs over every string in the bundle, so the same input always produces the
// same output. Go's regexp package (RE2) matches in linear time, so a huge
// tool output cannot make a pattern backtrack forever.
package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/mr-jones123/sesh/internal/session"
)

// Rule replaces what Pattern matches with Replacement. When Group is set,
// only that capture group is replaced and the rest of the match is kept, so
// "API_KEY=abc123" becomes "API_KEY=[REDACTED:env-secret]".
type Rule struct {
	Name        string
	Pattern     *regexp.Regexp
	Group       int
	Replacement string
	// Needles are literals, one of which every match contains. Strings
	// without any are skipped before the regex runs: most text, and all
	// base64 image data, holds none of them.
	Needles []string
	// Skip reports a match that looks like a secret but is not one;
	// s[start:end] is the text that would be replaced.
	Skip func(s string, start, end int) bool
}

// Finding is one replaced match. Preview never contains a whole secret.
type Finding struct {
	Rule    string
	Where   string
	Preview string
}

// Rules runs in order: specific token formats first, so a GitHub token is
// reported as github-token rather than by the generic env-secret rule.
var Rules = []Rule{
	secret("private-key", `-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`, "PRIVATE KEY-----"),
	token("anthropic-key", `sk-ant-[A-Za-z0-9_-]{20,}`, "sk-ant-"),
	token("openai-key", `sk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,}`, "sk-"),
	token("github-token", `gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,}`, "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"),
	token("aws-access-key", `(?:AKIA|ASIA)[0-9A-Z]{16}`, "AKIA", "ASIA"),
	token("google-api-key", `AIza[0-9A-Za-z_-]{35}`, "AIza"),
	token("slack-token", `xox[abposr]-[A-Za-z0-9-]{10,}`, "xox"),
	token("stripe-key", `[rs]k_(?:live|test)_[0-9A-Za-z]{16,}`, "k_live_", "k_test_"),
	token("huggingface-token", `hf_[A-Za-z0-9]{30,}`, "hf_"),
	token("npm-token", `npm_[A-Za-z0-9]{36}`, "npm_"),
	secret("jwt", `\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`, "eyJ"),
	value("bearer-token", `(?i)\bbearer\s+([A-Za-z0-9._~+/-]{20,}=*)`, "earer", "EARER"),
	value("url-password", `://[^/\s:@"'\\]+:([^/\s@"'\\]+)@`, "://"),
	// Upper-case names only: env vars and .env files, not code like
	// `token = getToken()`. Values starting with $ are references, not secrets.
	value("env-secret", `\b[A-Z0-9_]*(?:SECRET|TOKEN|PASSWORD|PASSWD|API_KEY|APIKEY|ACCESS_KEY|PRIVATE_KEY)[A-Z0-9_]*\s*[=:]\s*["']?([^\s"'\\,;\[$<{][^\s"'\\,;]{7,})`,
		"SECRET", "TOKEN", "PASSWORD", "PASSWD", "API_KEY", "APIKEY", "ACCESS_KEY", "PRIVATE_KEY"),
	{
		Name:    "email",
		Pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}\b`),
		Needles: []string{"@"},
		// git@github.com is an SSH remote, icon@2x.png a file name,
		// .test/.example/.invalid/.localhost are reserved for examples, and
		// in https://user:pass@host.com the "pass@host.com" is a URL.
		Skip: func(s string, start, end int) bool {
			if start > 0 && s[start-1] == ':' && !strings.HasSuffix(s[:start], "mailto:") {
				return true
			}
			local, domain, _ := strings.Cut(strings.ToLower(s[start:end]), "@")
			if local == "git" || domain == "example.com" || strings.HasSuffix(domain, ".noreply.github.com") {
				return true
			}
			for _, suffix := range []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".test", ".example", ".invalid", ".localhost", ".local"} {
				if strings.HasSuffix(domain, suffix) {
					return true
				}
			}
			return false
		},
		Replacement: "[REDACTED:email]",
	},
	// /Users/<name>, /home/<name> and C:\Users\<name> become ~, which hides
	// the user name and keeps the rest of the path readable.
	{
		Name:        "home-path",
		Pattern:     regexp.MustCompile(`(?:/Users|/home)/[^/\s"'\\:]+|[A-Za-z]:\\Users\\[^\\\s"':]+`),
		Needles:     []string{"/Users/", "/home/", `:\Users\`},
		Replacement: "~",
	},
	// Claude and Pi name session directories after the workspace with / as
	// -, so /Users/xy/app becomes -Users-xy-app. The name becomes ~ there.
	{
		Name:        "home-path",
		Pattern:     regexp.MustCompile(`(?:^|[/\s"'-])-(?:Users|home)-([^-/\s"'\\]+)`),
		Group:       1,
		Needles:     []string{"-Users-", "-home-"},
		Replacement: "~",
	},
}

func secret(name, pattern string, needles ...string) Rule {
	return Rule{Name: name, Pattern: regexp.MustCompile(pattern), Replacement: "[REDACTED:" + name + "]", Needles: needles}
}

// token matches a key format that must not continue a longer token. RE2 has
// no lookbehind, so the character before it is matched outside group 1.
// Without this, "sk-" inside a long base64url signature reads as an OpenAI key.
func token(name, pattern string, needles ...string) Rule {
	return value(name, `(?:^|[^A-Za-z0-9_-])(`+pattern+`)`, needles...)
}

// value replaces group 1 unless it is a placeholder.
func value(name, pattern string, needles ...string) Rule {
	rule := secret(name, pattern, needles...)
	rule.Group = 1
	rule.Skip = func(s string, start, end int) bool { return placeholder(s[start:end]) }
	return rule
}

var (
	constName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	digits    = regexp.MustCompile(`^[0-9]+$`)
)

// placeholder reports values that describe a secret instead of holding one:
// "your-api-key", "sk-ant-...", "XXXX", or a reference to where the secret
// lives, such as DEFAULT_TOKEN, getToken(), process.env.TOKEN or a Terraform
// module output.
func placeholder(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"your", "xxx", "...", "…", "example", "placeholder", "changeme", "redacted", "<", "("} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, prefix := range []string{"process.env", "import.meta.env", "os.environ", "module.", "var.", "local.", "data."} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return constName.MatchString(value) || digits.MatchString(value)
}

// Redactor applies rules and collects what it replaced.
type Redactor struct {
	Rules    []Rule
	Findings []Finding
}

func New() *Redactor { return &Redactor{Rules: Rules} }

// Text redacts one plain string. where labels the findings.
func (r *Redactor) Text(s, where string) string {
	for _, rule := range r.Rules {
		s = r.apply(rule, s, where)
	}
	return s
}

func (r *Redactor) apply(rule Rule, s, where string) string {
	if !containsAny(s, rule.Needles) {
		return s
	}
	matches := rule.Pattern.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		start, end := m[2*rule.Group], m[2*rule.Group+1]
		if start < 0 || (rule.Skip != nil && rule.Skip(s, start, end)) {
			continue
		}
		out.WriteString(s[last:start])
		out.WriteString(rule.Replacement)
		last = end
		r.Findings = append(r.Findings, Finding{Rule: rule.Name, Where: where, Preview: preview(rule, s[start:end])})
	}
	if last == 0 {
		return s
	}
	out.WriteString(s[last:])
	return out.String()
}

func containsAny(s string, needles []string) bool {
	if len(needles) == 0 {
		return true
	}
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// preview shows paths whole (they are the user's own) and at most the
// first four characters of a secret.
func preview(rule Rule, match string) string {
	if rule.Name == "home-path" {
		return match
	}
	if len(match) <= 8 {
		return strings.Repeat("*", len(match))
	}
	return match[:4] + "…"
}

// JSON redacts the string literals of a JSON document. See mapStrings.
func (r *Redactor) JSON(raw []byte, where string) ([]byte, error) {
	return mapStrings(raw, func(s string) string { return r.Text(s, where) }, where)
}

// mapStrings passes every string literal of a JSON document through f and
// leaves every other byte alone. Unchanged strings keep their exact original
// bytes, and a changed string is re-encoded, so the result is always valid
// JSON. f sees the decoded text, so escapes like \n or \u003c cannot hide a
// secret from it.
func mapStrings(raw []byte, f func(string) string, where string) ([]byte, error) {
	var out []byte // nil until the first change
	last := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		end := i + 1
		for end < len(raw) && raw[end] != '"' {
			if raw[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(raw) {
			return nil, fmt.Errorf("%s: unterminated string", where)
		}
		literal := raw[i : end+1]
		var text string
		if err := json.Unmarshal(literal, &text); err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
		if changed := f(text); changed != text {
			out = append(out, raw[last:i]...)
			out = append(out, encodeString(changed)...)
			last = end + 1
		}
		i = end
	}
	if out == nil {
		return raw, nil
	}
	return append(out, raw[last:]...), nil
}

func encodeString(s string) []byte {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(s) // a string always encodes
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// Bundle redacts every string a bundle carries, in place: session metadata,
// the event timeline, and the raw source lines. It marks the bundle Redacted.
func (r *Redactor) Bundle(b *session.Bundle) error {
	b.Redacted = true
	b.Source.Path = r.Text(b.Source.Path, "source path")
	b.Source.Repository = r.Text(b.Source.Repository, "source repository")
	b.Session.Title = r.Text(b.Session.Title, "session title")
	b.Session.Workspace = r.Text(b.Session.Workspace, "session workspace")

	for i := range b.Session.Events {
		event := &b.Session.Events[i]
		where := fmt.Sprintf("event %s", event.ID)
		event.Text = r.Text(event.Text, where)
		if event.Call != nil && len(event.Call.Args) > 0 {
			args, err := r.JSON(event.Call.Args, where)
			if err != nil {
				return err
			}
			event.Call.Args = args
		}
		if event.Result != nil {
			event.Result.Output = r.Text(event.Result.Output, where)
		}
	}

	for i := range b.RawRecords {
		line := &b.RawRecords[i]
		record, err := r.JSON([]byte(line.Record), fmt.Sprintf("raw line %d", line.Line))
		if err != nil {
			return err
		}
		line.Record = string(record)
	}
	return nil
}

// homeRef is a ~ that starts a path: after the start of the text, a space,
// a quote, =, : or (, and before a path separator, a space, a quote, ) or
// the end. "~5 minutes" is not a path.
var homeRef = regexp.MustCompile(`(^|[\s"'=:(])~([/\\\s"')]|$)`)

// ExpandHome undoes the home-path rule for a redacted bundle continued on
// this machine: a ~ that starts a path becomes home, in the workspace and
// in the tool-call arguments the target harness replays. Results and
// messages keep ~; they are history, not paths anything opens.
func ExpandHome(b *session.Bundle, home string) error {
	replacement := "${1}" + strings.ReplaceAll(home, "$", "$$") + "${2}"
	expand := func(s string) string { return homeRef.ReplaceAllString(s, replacement) }

	b.Session.Workspace = expand(b.Session.Workspace)
	for i := range b.Session.Events {
		call := b.Session.Events[i].Call
		if call == nil || len(call.Args) == 0 {
			continue
		}
		args, err := mapStrings(call.Args, expand, "event "+b.Session.Events[i].ID)
		if err != nil {
			return err
		}
		call.Args = args
	}
	return nil
}
