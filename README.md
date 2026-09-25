# sesh

Portable import and export for AI coding sessions.

## Try it

```sh
go run ./cmd/sesh help
go run ./cmd/sesh export --harness pi path/to/session.jsonl
go run ./cmd/sesh import path/to/session.jsonl.sesh.json
```

`export` converts a Pi, Claude Code, or Codex JSONL transcript into a portable `.sesh.json` bundle. The harness can be detected from the path, or selected explicitly with `--harness`.

A bundle (format version 2) holds two views of the session:

- `session.events`: the harness-neutral timeline of messages, reasoning, tool calls, tool results, and compaction summaries. Each tool result links to its call by ID, and `parent_id` links events into a tree.
- `raw_records`: every source line, byte-for-byte, for lossless same-harness export.

Harness bookkeeping records (Claude attachments, Codex `event_msg`, Pi model changes, ...) stay in `raw_records` only. Images are not yet extracted into the timeline. Version 1 bundles must be re-exported.

`import` validates and summarizes a local bundle. It does not execute tools or resume the conversation in a harness yet.
