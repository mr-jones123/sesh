---
name: sesh
description: Hand an AI coding session from one agent harness to another (Claude Code, Codex, Pi) and export sessions as redacted, portable bundles with the sesh CLI. Use when the user wants to continue the current conversation in a different agent, move or share a session with another developer or machine, convert a Claude Code, Codex, or Pi transcript, or strip API keys, emails, and home paths from a session before sharing it.
license: MIT
compatibility: Requires the sesh CLI (Go 1.26.6+ to install) and at least one of Claude Code, Codex, or Pi. Reads session files from ~/.claude, ~/.codex, and ~/.pi.
metadata:
  author: mr-jones123
  source: https://github.com/mr-jones123/sesh
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

If it is missing, install it (needs Go 1.26.6+; older Go since 1.21 downloads the toolchain):

```sh
go install github.com/mr-jones123/sesh/cmd/sesh@latest
```

## Find the session file

Each harness stores sessions in its own directory. To hand off the session you are running in, pick the newest file for the current directory.

| Harness | Session files |
|---|---|
| Claude Code | `~/.claude/projects/<cwd with every non-alphanumeric as ->/<id>.jsonl` |
| Codex | `~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl`; the first line records the `cwd` |
| Pi | `~/.pi/agent/sessions/--<cwd without its leading /, with / as ->--/<time>_<id>.jsonl` |

`$CLAUDE_CONFIG_DIR` and `$CODEX_HOME` replace `~/.claude` and `~/.codex` when set.

```sh
# Claude Code: newest session for the current directory
ls -t ~/.claude/projects/$(pwd | sed 's/[^A-Za-z0-9]/-/g')/*.jsonl | head -1

# Codex: newest rollout started in the current directory
for f in $(ls -t ~/.codex/sessions/*/*/*/rollout-*.jsonl); do
  head -1 "$f" | grep -q "\"cwd\":\"$PWD\"" && { echo "$f"; break; }
done

# Pi: newest session for the current directory
ls -t ~/.pi/agent/sessions/--$(pwd | sed 's#^/##; s#/#-#g')--/*.jsonl | head -1
```

If several sessions match, or the user means an older one, list the candidates with their modification times and ask which one.

## Export

```sh
sesh export <session.jsonl>                 # writes <session.jsonl>.sesh.json
sesh export -output handoff.sesh.json <session.jsonl>
sesh export --harness pi <file.jsonl>       # needed when the file is outside the harness's own directory
```

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
f=$(ls -t ~/.claude/projects/$(pwd | sed 's/[^A-Za-z0-9]/-/g')/*.jsonl | head -1)
sesh export -output /tmp/handoff.sesh.json "$f"
sesh convert --target codex --install /tmp/handoff.sesh.json   # confirm the prompt
codex resume <printed id>
```

## Rules

- **Ask before `--install`.** It writes into the target harness's session directory. Pass `--yes` only when the user has already agreed to the install.
- **Never share a `--no-redact` bundle.** `--no-redact` keeps every source line byte-exact, secrets included. Use it only for a local copy that stays on the machine.
- **Redaction catches known formats only.** A key with no known prefix, a lower-case `password: …`, a name typed in prose, or text inside an image gets through. Before a bundle leaves the machine, tell the user to review it and point out anything that still looks sensitive.
- **Recorded tool calls are history.** Neither sesh nor you should rerun them just because they appear in a converted session.
- **What does not carry over:** reasoning from another model is dropped when converting to Codex or Claude (Pi keeps it as text); tools with no equivalent (web search, MCP, subagents) arrive as text with their results.
