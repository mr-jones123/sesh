# sesh

Portable import and export for AI coding sessions.

## Try it

```sh
go run ./cmd/sesh help
go run ./cmd/sesh export --harness claude path/to/session.jsonl
go run ./cmd/sesh import path/to/session.jsonl.sesh.json
go run ./cmd/sesh convert --target pi --model openai-codex/gpt-5.6-sol path/to/session.jsonl.sesh.json
pi --session path/to/session.jsonl.pi.jsonl
go run ./cmd/sesh convert --target codex --install path/to/session.jsonl.sesh.json
codex resume <printed session id>
```

`export` converts a Pi, Claude Code, or Codex JSONL transcript into a portable `.sesh.json` bundle. The harness can be detected from the path, or selected explicitly with `--harness`.

A bundle (format version 2) holds two views of the session:

- `session.events`: the harness-neutral timeline of messages, reasoning, tool calls, tool results, and compaction summaries. Each tool result links to its call by ID, and `parent_id` links events into a tree.
- `raw_records`: every source line, byte-for-byte, for lossless same-harness export.

Harness bookkeeping records (Claude attachments, Codex `event_msg`, Pi model changes, ...) stay in `raw_records` only. Images are not yet extracted into the timeline. Version 1 bundles must be re-exported.

`import` validates and summarizes a local bundle.

## Hand off to another harness

`convert` writes a bundle as a native session for a target harness, so the conversation continues there. Targets: Pi and Codex.

- Shell, write, and edit calls become the target's own tools: Claude `Bash` → Pi `bash` / Codex `exec_command`, Claude `Edit` → Pi `edit` / Codex `apply_patch`. Reads become Pi `read`; Codex has no read tool, so reads stay text there. Other tools (web search, MCP, subagents) are kept as text with their results.
- Only the active branch is written; abandoned rewinds are dropped.
- Reasoning from another model is dropped for Codex (its reasoning is encrypted per provider). Pi converts foreign reasoning to text itself.
- `--workspace dir` records a different working directory; Pi requires it to exist. `--model provider/id` sets the model Pi resumes with.
- Recorded tool calls are history: they are never run.
- A Pi bundle converted back to Pi with no overrides is copied byte-for-byte.

Pi opens any file with `pi --session <file>`, so nothing is installed. Codex resumes sessions only by ID from its own directory, so `--install` writes the rollout to `$CODEX_HOME/sessions/YYYY/MM/DD/` (default `~/.codex`) after a confirmation prompt (`--yes` skips it). Each conversion gets a new session ID and never overwrites an existing session. Codex registers the session itself on first resume.
