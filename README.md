# sesh

Portable import and export for AI coding sessions.

## Install

Requires Go 1.26.6 or newer (older Go toolchains since 1.21 download it automatically).

```sh
go install github.com/mr-jones123/sesh/cmd/sesh@latest
sesh help
```

Or build from a clone with `go build ./cmd/sesh`.

## Try it

```sh
sesh export --harness claude path/to/session.jsonl
sesh import path/to/session.jsonl.sesh.json
sesh convert --target pi --model openai-codex/gpt-5.6-sol path/to/session.jsonl.sesh.json
pi --session path/to/session.jsonl.pi.jsonl
sesh convert --target codex --install path/to/session.jsonl.sesh.json
codex resume <printed session id>
sesh convert --target claude --install path/to/session.jsonl.sesh.json
claude --resume <printed session id>   # from the session's workspace
```

`export` converts a Pi, Claude Code, or Codex JSONL transcript into a portable `.sesh.json` bundle, with secrets and personal paths redacted (see [Redaction](#redaction)). The harness can be detected from the path, or selected explicitly with `--harness`. The source transcript is never modified.

A bundle (format version 2) holds two views of the session:

- `session.events`: the harness-neutral timeline of messages, reasoning, tool calls, tool results, and compaction summaries. Each tool result links to its call by ID, and `parent_id` links events into a tree.
- `raw_records`: every source line. With `--no-redact` they are byte-for-byte; redacted, only the replaced text inside JSON strings differs.

Harness bookkeeping records (Claude attachments, Codex `event_msg`, Pi model changes, ...) stay in `raw_records` only. Images are not yet extracted into the timeline. Version 1 bundles must be re-exported.

`import` validates and summarizes a local bundle.

## Hand off to another harness

`convert` writes a bundle as a native session for a target harness, so the conversation continues there. Targets: Pi, Codex, and Claude Code, from any of the three sources.

| Neutral action | Pi | Codex | Claude |
|---|---|---|---|
| shell | `bash` | `exec_command` | `Bash` |
| read | `read` | text (no read tool) | `Read` |
| write | `write` | `apply_patch` (Add File) | `Write` |
| edit | `edit` | `apply_patch` (Update File) | `Edit`, one per replacement |
| anything else (web search, MCP, subagents, ...) | text with its result | text with its result | text with its result |

- Only the active branch is written; abandoned rewinds are dropped.
- Reasoning from another model is dropped for Codex and Claude (their reasoning is encrypted or signed per provider). Pi converts foreign reasoning to text itself.
- Calls the source never answered (interrupted runs) get an explicit "no result recorded" result, since Codex and Claude reject unanswered calls.
- Relative file paths become absolute under the workspace for Claude, whose file tools require absolute paths.
- `--workspace dir` records a different working directory; Pi requires it to exist, and Claude files the session under it. `--model provider/id` sets the model Pi resumes with.
- Recorded tool calls are history: they are never run.
- An unredacted (`--no-redact`) Pi bundle converted back to Pi with no overrides is copied byte-for-byte.
- In a redacted bundle, `~` that starts a path in the workspace or a tool call becomes the home directory of whoever runs `convert`, so a session continues on another machine with the same layout under its home.

Pi opens any file with `pi --session <file>`, so nothing is installed. Codex and Claude resume sessions only by ID from their own directories, so `--install` writes the file there after a confirmation prompt (`--yes` skips it):

- Codex: `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl` (default `~/.codex`). Codex registers the session itself on first resume.
- Claude: `$CLAUDE_CONFIG_DIR/projects/<workspace, non-alphanumerics as ->/<id>.jsonl` (default `~/.claude`). Run `claude --resume <id>` from that workspace.

Each conversion gets a new session ID and never overwrites an existing session.

## Redaction

Session transcripts contain prompts, source code, tool arguments, command output, local paths, and anything an agent read, including API keys and `.env` files. `export` replaces these in both the event timeline and the raw source lines, and prints how many it replaced per rule:

- API keys and tokens with a known format: OpenAI, Anthropic, GitHub, AWS, Google, Slack, Stripe, Hugging Face, npm, JWTs, bearer tokens, PEM private keys
- passwords in URLs (`postgres://user:[REDACTED:url-password]@host`) and upper-case `*_TOKEN=`, `*_SECRET=`, `*_PASSWORD=`, `*_API_KEY=` values
- email addresses
- home directories: `/Users/<name>`, `/home/<name>`, `C:\Users\<name>` and encoded session directories like `-Users-<name>-` become `~`

```text
$ sesh export session.jsonl
exported 18 events to session.jsonl.sesh.json
redacted 89 matches
  home-path          78
  email              11
```

The rules are fixed regular expressions, so the same transcript always gives the same bundle. Placeholders such as `your-api-key` and references such as `process.env.API_KEY` are kept. Only JSON string contents change; every other byte of a raw line stays as it was. The bundle is marked `"redacted": true`, and `sesh import` shows it.

Known formats only: a key without a known prefix, a lower-case `password: hunter2`, a user name typed in prose, or text inside an image gets through. Review a bundle before sharing it.

`sesh export --no-redact` keeps everything, for a byte-exact bundle you do not share.

## License

[MIT](LICENSE)
