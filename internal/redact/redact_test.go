package redact

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-jones123/sesh/internal/session"
)

// Fake keys are built at run time so the source never holds a string that
// secret scanners (including GitHub push protection) would flag.
var (
	openAIKey = "sk-proj-" + strings.Repeat("a1B2", 12)
	githubPAT = "ghp_" + strings.Repeat("Z9y8", 9)
)

func TestJSONRewritesOnlyChangedStrings(t *testing.T) {
	raw := `{"b": 1,  "a": "<tag> & 1.50", "cmd": "export OPENAI_API_KEY=` + openAIKey + `"}`
	got, err := New().JSON([]byte(raw), "test")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"b": 1,  "a": "<tag> & 1.50", "cmd": "export OPENAI_API_KEY=[REDACTED:openai-key]"}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestJSONMatchesDecodedText(t *testing.T) {
	// A JSON writer may escape any character; the key must still be found.
	escaped := strings.Replace(githubPAT, "_", `\u005f`, 1)
	got, err := New().JSON([]byte(`{"output":"token: `+escaped+`\n"}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if want := "token: [REDACTED:github-token]\n"; decoded["output"] != want {
		t.Fatalf("got %q, want %q", decoded["output"], want)
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"key inside a longer token is not a key", "sig-" + openAIKey, "sig-" + openAIKey},
		{"adjacent keys are both replaced", openAIKey + "\n" + openAIKey, "[REDACTED:openai-key]\n[REDACTED:openai-key]"},
		{"env name is kept", "GITHUB_TOKEN=" + githubPAT, "GITHUB_TOKEN=[REDACTED:github-token]"},
		{"env value", "DB_PASSWORD=hunter22hunter", "DB_PASSWORD=[REDACTED:env-secret]"},
		{"env placeholder", "OPENAI_API_KEY=your-api-key-here", "OPENAI_API_KEY=your-api-key-here"},
		{"env reference to where the secret lives", "API_TOKEN: process.env.API_TOKEN", "API_TOKEN: process.env.API_TOKEN"},
		{"env reference to a constant", "MAX_TOKENS = DEFAULT_MAX_TOKENS", "MAX_TOKENS = DEFAULT_MAX_TOKENS"},
		{"url password", "postgres://app:hunter22@db:5432/app", "postgres://app:[REDACTED:url-password]@db:5432/app"},
		{"url password reference", "https://USER:ACCESS_KEY@hub.example.com", "https://USER:ACCESS_KEY@hub.example.com"},
		{"email", "mail jane.doe@acme.io today", "mail [REDACTED:email] today"},
		{"ssh remote is not an email", "git@github.com:acme/app.git", "git@github.com:acme/app.git"},
		{"home path", "/Users/alice/src/app/main.go", "~/src/app/main.go"},
		{"linux home path", "cd /home/alice && ls", "cd ~ && ls"},
		{"windows home path", `C:\Users\alice\src`, `~\src`},
		{"encoded session directory", "~/.claude/projects/-Users-alice-src-app/1.jsonl", "~/.claude/projects/-Users-~-src-app/1.jsonl"},
		{"pi session directory", "sessions/--Users-alice-src-app--/1.jsonl", "sessions/--Users-~-src-app--/1.jsonl"},
		{"words with -home- are not paths", "my-home-page", "my-home-page"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := New().Text(test.in, "test"); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestBundleRedactsEveryView(t *testing.T) {
	bundle := session.Bundle{
		Session: session.Session{
			ID:        "s1",
			Workspace: "/Users/alice/src/app",
			Events: []session.Event{
				{ID: "e1", Text: "use " + openAIKey},
				{ID: "e2", Call: &session.ToolCall{ID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"echo ` + githubPAT + `"}`)}},
				{ID: "e3", Result: &session.ToolResult{CallID: "c1", Output: githubPAT}},
			},
		},
		RawRecords: []session.RawLine{{Line: 1, Record: `{"text":"use ` + openAIKey + `"}`}},
	}
	r := New()
	if err := r.Bundle(&bundle); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{openAIKey, githubPAT, "alice"} {
		if strings.Contains(string(encoded), leaked) {
			t.Errorf("bundle still contains %q", leaked)
		}
	}
	if len(r.Findings) != 5 {
		t.Errorf("got %d findings, want 5: %+v", len(r.Findings), r.Findings)
	}
}

func TestExpandHomeRestoresPathsOnly(t *testing.T) {
	bundle := session.Bundle{Session: session.Session{
		ID:        "s1",
		Workspace: "~/src/app",
		Events: []session.Event{{ID: "e1", Call: &session.ToolCall{ID: "c1", Name: "bash",
			Args: json.RawMessage(`{"command":"cd ~ && cat ~/src/app/go.mod","note":"took ~5 minutes"}`)}}},
	}}
	if err := ExpandHome(&bundle, "/home/bob"); err != nil {
		t.Fatal(err)
	}
	if bundle.Session.Workspace != "/home/bob/src/app" {
		t.Errorf("workspace = %q", bundle.Session.Workspace)
	}
	want := `{"command":"cd /home/bob && cat /home/bob/src/app/go.mod","note":"took ~5 minutes"}`
	if got := string(bundle.Session.Events[0].Call.Args); got != want {
		t.Errorf("args = %s\nwant   %s", got, want)
	}
}
