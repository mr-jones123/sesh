---
name: sesh
description: Hand an AI coding session from one agent harness to another (Claude Code, Codex, Pi) and export sessions as redacted, portable bundles with the sesh CLI. Use when the user wants to continue the current conversation in a different agent, move or share a session with another developer or machine, convert a Claude Code, Codex, or Pi transcript, or strip API keys, emails, and home paths from a session before sharing it.
license: MIT
compatibility: Requires the sesh CLI v0.2.0+ (Go 1.26.6+ to install) and at least one of Claude Code, Codex, or Pi. Reads session files from ~/.claude, ~/.codex, and ~/.pi.
metadata:
  author: mr-jones123
  source: https://github.com/mr-jones123/sesh
  version: "0.2.0"
---

# sesh

sesh turns a harness's native session file into a portable `.sesh.json` bundle, then writes that bundle as a native session for another harness so the conversation continues there with its history: messages, tool calls with their results, and the working directory.

```text
Claude / Codex / Pi JSONL ──sesh export──► .sesh.json (redacted) ──sesh convert──► Claude / Codex / Pi session
```

## Setup

Check that the CLI is installed:

```sh
sesh version
```

If it is missing, or `sesh list` is not a command, install or update it (needs Go 1.26.6+; older Go since 1.21 downloads the toolchain):

```sh
go install github.com/mr-jones123/sesh/cmd/sesh@latest
```

## Pick the session

Sessions are named by long UUIDs; never ask the user for one. `sesh list` shows what each harness recorded for the current directory, newest first, with the first message the user typed:

```text
$ sesh list
HARNESS  UPDATED          ID        EVENTS  FIRST MESSAGE
claude   2 min ago        c3894fc5  18      We are building a small notes web app…
codex    today 15:52      01a0de68  16      You are picking up this project from…
pi       yesterday 18:04  019fd2e2  52      add deleting a note…
```

- **The session you are running in** is the newest one for your own harness: `sesh export -last -harness claude` (or `codex`, `pi`). Always pass `-harness`: without it, `-last` takes the newest session of any harness, which may be another agent working in the same directory.
- **Another session:** find it with `sesh list` (`-all` for every directory, `-harness` to narrow, `-n 0` for all rows) and pass its ID, or the first characters of it, to `sesh export`. If more than one could be meant, show the user the rows and ask.

## Export

```sh
sesh export -last -harness claude             # the current Claude Code session → claude-<id>.sesh.json
sesh export 01a0de68                          # by ID or ID prefix from sesh list → <harness>-<id>.sesh.json
sesh export -output handoff.sesh.json -last -harness codex
sesh export <session.jsonl>                   # by path → <session.jsonl>.sesh.json
```

A session found by ID or `-last` is written to the current directory. Give a path with `-output` if the current directory is a repository the bundle should not be committed to.

Export redacts by default and prints what it replaced, per rule:

- API keys and tokens with a known format (OpenAI, Anthropic, GitHub, AWS, Google, Slack, Stripe, Hugging Face, npm), JWTs, bearer tokens, PEM private keys → `[REDACTED:<rule>]`
- passwords in URLs, and upper-case `*_TOKEN=`, `*_SECRET=`, `*_PASSWORD=`, `*_API_KEY=` values
- email addresses
- home directories (`/Users/<name>`, `/home/<name>`, `/root`, `C:\Users\<name>`) → `~`

Report the counts to the user. The source session file is never modified.

`sesh import <bundle>` validates a bundle and shows its harness, event count, and whether it is redacted.

## Convert and resume

| Target | Convert | Resume |
|---|---|---|
| Pi | `sesh convert --target pi <bundle>` → `<bundle>.pi.jsonl` | `pi --session <file>` |
| Codex | `sesh convert --target codex --install <bundle>` | `codex resume <printed id>` |
| Claude Code | `sesh convert --target claude --install <bundle>` | `claude --resume <printed id>`, run from the printed workspace |

- Pi opens any file, so it needs no install. Codex and Claude resume only by ID from their own directories, so `--install` writes the session there. It asks for confirmation first, gives the session a new ID, and never overwrites an existing session.
- `--workspace <dir>` sets the working directory the target session uses. Use it when the recorded directory does not exist on this machine (Pi refuses a missing directory).
- `--model provider/id` sets the model Pi resumes with.
- In a redacted bundle, `~` at the start of a path becomes the current user's home, so a session moves between machines with the same layout under the home directory.

After resuming, the agent sees the earlier conversation as history. Give it a task that follows on from that history, for example "You are picking up this project from the previous agent. Continue with the next step."

## Example: hand the current Claude Code session to Codex

```sh
sesh export -last -harness claude -output /tmp/handoff.sesh.json
sesh convert --target codex --install /tmp/handoff.sesh.json   # confirm the prompt
codex resume <printed id>
```

## Rules

- **Ask before `--install`.** It writes into the target harness's session directory. Pass `--yes` only when the user has already agreed to the install.
- **Never share a `--no-redact` bundle.** `--no-redact` keeps every source line byte-exact, secrets included. Use it only for a local copy that stays on the machine.
- **Redaction catches known formats only.** A key with no known prefix, a lower-case `password: …`, a name typed in prose, or text inside an image gets through. Before a bundle leaves the machine, tell the user to review it and point out anything that still looks sensitive.
- **Recorded tool calls are history.** Neither sesh nor you should rerun them just because they appear in a converted session.
- **What does not carry over:** reasoning from another model is dropped when converting to Codex or Claude (Pi keeps it as text); tools with no equivalent (web search, MCP, subagents) arrive as text with their results.
