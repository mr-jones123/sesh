package harness

import (
	"slices"
	"strings"
	"unicode"
)

// injectedPromptPrefixes identify user-role messages written by a harness or
// sesh rather than typed by the user.
var injectedPromptPrefixes = []string{
	"<",
	"# AGENTS.md instructions",
	"[Request interrupted",
	"Caveat: The messages below",
	"The conversation before this point was summarized",
	"[Imported by sesh",
}

const codexRequest = "## My request for Codex:"

// Prompt returns one display line for a genuine user message, or an empty
// string for injected context and empty messages.
func Prompt(text string) string {
	text = strings.TrimSpace(text)
	if _, request, ok := strings.Cut(text, codexRequest); ok {
		text = strings.TrimSpace(request)
	}
	if text == "" || slices.ContainsFunc(injectedPromptPrefixes, func(prefix string) bool {
		return strings.HasPrefix(text, prefix)
	}) {
		return ""
	}
	return strings.Join(strings.FieldsFunc(text, unicode.IsSpace), " ")
}
