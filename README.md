# sesh

Portable import and export for AI coding sessions.

## Try it

```sh
go run ./cmd/sesh help
go run ./cmd/sesh export --harness pi path/to/session.jsonl
go run ./cmd/sesh import path/to/session.jsonl.sesh.json
```

`export` converts a Pi, Claude Code, or Codex JSONL transcript into a portable `.sesh.json` bundle. The harness can be detected from the path, or selected explicitly with `--harness`.

`import` validates and summarizes a local bundle. It does not execute tools or resume the conversation in a harness yet.
