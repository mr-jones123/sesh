# sesh

Portable import and export for AI coding sessions.

## Try it

```sh
go run ./cmd/sesh help
go run ./cmd/sesh export --harness claude path/to/session.jsonl
go run ./cmd/sesh import path/to/session.jsonl.sesh.json
go run ./cmd/sesh convert --target pi --model openai-codex/gpt-5.6-sol path/to/session.jsonl.sesh.json
pi --session path/to/session.jsonl.pi.jsonl
```

`export` converts a Pi, Claude Code, or Codex JSONL transcript into a portable `.sesh.json` bundle. The harness can be detected from the path, or selected explicitly with `--harness`.

A bundle (format version 2) holds two views of the session:

- `session.events`: the harness-neutral timeline of messages, reasoning, tool calls, tool results, and compaction summaries. Each tool result links to its call by ID, and `parent_id` links events into a tree.
- `raw_records`: every source line, byte-for-byte, for lossless same-harness export.

Harness bookkeeping records (Claude attachments, Codex `event_msg`, Pi model changes, ...) stay in `raw_records` only. Images are not yet extracted into the timeline. Version 1 bundles must be re-exported.

`import` validates and summarizes a local bundle.

## Hand off to another harness

`convert` writes a bundle as a native session for a target harness, so the conversation continues there. Pi is the only target so far.

- Shell, read, write, and edit calls become the target's own tools, for example Claude `Bash` → Pi `bash`, Codex `apply_patch` → Pi `edit`. Other tools (web search, MCP, subagents) are kept as text with their results.
- Only the active branch is written; abandoned rewinds are dropped.
- `--model provider/id` sets the model Pi resumes with; without it Pi uses its default. `--workspace dir` records a different working directory, which Pi requires to exist.
- The file goes only to the output path. Nothing is installed into `~/.pi`, and recorded tool calls are history: they are never run.
- A Pi bundle converted back to Pi with no overrides is copied byte-for-byte.
