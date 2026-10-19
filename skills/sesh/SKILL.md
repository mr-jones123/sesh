---
name: sesh
description: Hand a local AI coding session between Claude Code, Codex, Pi, and OMP. Use when the user wants another agent to continue a conversation in the same project, find a named session across projects, or create a target-native session without a portable export.
license: MIT
compatibility: Requires a sesh build with the handoff command (Go 1.26.6+ to install) and at least one supported harness. Local, unredacted handoffs only.
metadata:
  author: mr-jones123
  source: https://github.com/mr-jones123/sesh
  version: "0.3.0-dev"
---

# sesh

sesh reads a native Claude Code, Codex, Pi, or OMP transcript, normalizes it in memory, and writes a new native session for the target harness. There is no `.sesh.json` bundle or redaction step.

## Check the CLI

```sh
sesh help handoff
```

An older installed CLI or skill may still use export/convert. Install the updated checkout with `go install ./cmd/sesh` from its repository. Published releases use `go install github.com/mr-jones123/sesh/cmd/sesh@latest`, but an unreleased cutover will not be available through an older tag. Never claim the new commands are installed without checking them.

## Select a session

```sh
sesh list                                  # current project, newest first
sesh list --harness claude                  # narrow to the source harness
sesh list --all                             # every project, including workspaces
sesh list --dir /path/to/project -n 0        # another project's complete list
```

The list contains full IDs, update times, and saved/generated session names (first prompt only when untitled). The TUI has colored harness badges and can search names as well as prompts. Use the available list and context to select the session; do not ask the user to find a UUID manually. Explicit IDs and unique prefixes are resolved across all projects. Ambiguous prefixes fail safely.

For automatic selection, `handoff --from <harness>` takes that harness's newest readable session in the current project. **Newest is not a guarantee that it is the current conversation** when multiple terminals/agents are active. Prefer an exact known session or check the list when identity matters.

## Write the handoff without launching another agent

Always use `--no-run` from an agent or script:

```sh
sesh handoff <session-id> --to codex --no-run
sesh handoff --from claude --to pi --no-run
sesh handoff /path/to/transcript.jsonl --from pi --to claude --no-run
sesh handoff --dir /path/to/project --from codex --to pi --no-run
sesh handoff <session-id> --to omp --no-run
```

A handoff request authorizes creating a new native session, not overwriting an existing one. sesh preserves the source, generates a new identity, and removes partial output on export failure. There are no `--install`, `--output`, export, import, or convert steps.

Report the written path, workspace, and printed resume command. Let the user run that command in a terminal. Do not start the TUI or launch a nested interactive agent from a tool call.

| Target | Resume shape |
|---|---|
| Claude Code | `claude --resume <new-id>` from the printed workspace |
| Codex | `codex resume <new-id>` from the printed workspace |
| Pi | `pi --session <written-file>` from the printed workspace |
| OMP | `omp --resume <written-file>` from the printed workspace |

Use the actual command printed by sesh rather than reconstructing shell quoting.

## Workspace and model overrides

Selecting a session globally preserves its original workspace. It does not move the conversation to the directory where sesh was invoked.

- `--dir` chooses which project's newest source to select; do not combine it with an explicit ID/path.
- `--workspace /existing/path` deliberately changes the destination workspace. Use it for a moved or missing project directory.
- `--model provider/model-id` sets the model Pi resumes with. Other targets reject this option.
- Native stores honor `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, and `PI_CODING_AGENT_DIR` for Pi. OMP defaults to `~/.omp/agent`; `SESH_OMP_AGENT_DIR` overrides sesh's OMP root independently (it is not an OMP-native environment variable). Native OMP output includes its fixed 256-byte title slot.

## Rules

- **History is unredacted.** Keys, paths, email addresses, source code, and tool output may carry over. These native files are not safe public-sharing artifacts.
- **Provider boundary:** on resume, the target may send the imported history to its configured model provider. Make that clear when the user is changing providers or asks about privacy.
- **Tool calls are history.** Never rerun recorded tools merely because their calls appear in a transcript.
- Only the active branch carries over. Foreign reasoning is dropped for Codex/Claude; unsupported tools become text with their results. Images and harness bookkeeping are not archival copies.
- The source remains untouched. If launch fails later, retain the newly written session and use its printed resume command after fixing the target.
- Old `.sesh.json` files are not handoff inputs. Find the original native transcript; never present an old redacted bundle as a complete native history.

For a human in a terminal, bare `sesh` opens the picker, `sesh --all` includes all projects, and `sesh --dir <project>` browses another project. Browsing never writes; `y` on the final confirmation creates the session and resumes the target after terminal restoration.
