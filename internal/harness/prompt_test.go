package harness

import "testing"

func TestPromptExcludesInjectedMessagesAndExtractsCodexRequest(t *testing.T) {
	for _, injected := range []string{
		"<environment_context>cwd</environment_context>",
		"# AGENTS.md instructions for app",
		"[Request interrupted by user]",
		"Caveat: The messages below were generated",
		"The conversation before this point was summarized",
		"[Imported by sesh: context]",
	} {
		if got := Prompt(injected); got != "" {
			t.Errorf("Prompt(%q) = %q, want empty", injected, got)
		}
	}

	if got := Prompt("attached files\n\n## My request for Codex:\n  Fix\n the bug "); got != "Fix the bug" {
		t.Fatalf("Prompt(Codex request) = %q", got)
	}
}
