<p align="center">
  <img src="docs/logo.png" alt="sesh" width="800">
</p>

# sesh

Hand a local AI coding session between Claude Code, Codex, Pi, and OMP. Pick a conversation in the terminal, choose the next agent, and continue in its original project.

History is normalized in memory and written directly into the target's native session store. There is no portable bundle, redaction pass, server, or project-local sesh database.

## Install once, use from any project

Requires Go 1.26.6 or newer. Install the current checkout, including the handoff/TUI cutover:

```sh
go install ./cmd/sesh
sesh help
```

Go installs into `GOBIN` when configured, otherwise `$(go env GOPATH)/bin` (usually `~/go/bin`). Put that directory on your `PATH`. The same executable works in every project; no installation inside individual repositories is needed.

Published releases can be installed with:

```sh
go install github.com/mr-jones123/sesh/cmd/sesh@latest
```

Version v0.3.0 introduces the native handoff/TUI workflow; older tags use export/convert. `sesh version` reports a release/module version, or `dev` for an unversioned local build.

## Interactive picker

```sh
sesh                          # sessions recorded for this project
sesh --all                    # sessions across all projects
sesh --dir ~/work/app          # browse another project
sesh --from claude            # show one source harness
sesh --from omp               # browse OMP's own session store
```

The Bubble Tea picker uses the harness's saved/generated session name as the heading, falling back to the first user prompt only when a name is missing. Claude has an amber badge, Codex green, Pi blue, and OMP violet. Stronger metadata contrast, spaced rows, and a quiet selection background adapt to dark and light terminals. The detail pane keeps the full name, first prompt, ID, and workspace. All-projects rows also show the project name.

- **Up/Down** or **j/k**: move.
- **/**: search saved names, prompts, IDs, harnesses, and workspaces. While searching, `q`, `j`, and `k` are text.
- **Enter** in search: keep the filter. **Esc**: clear it.
- **Tab** outside search: switch between this project and all projects.
- **Enter** on a session: choose a target; **Enter** again: review the confirmation.
- **y** on confirmation: create the native session, close the picker, and start the target. Repeated Enter never confirms.
- **n/Esc**: go back. **q** outside search or **Ctrl+C**: cancel.

Browsing and target selection do not write files. If the terminal is too small to show the confirmation details, resize it before confirming. A cancellation during creation waits for the native writer to stop; if creation already committed, sesh reports the retained file and resume command without launching it.

The picker requires terminal input and output. Bare `sesh` prints help when used without a terminal.

## Direct handoff

```sh
# Newest Claude session in this project → Codex, then resume.
sesh handoff --from claude --to codex

# An explicit session ID or unique prefix works from any project.
sesh handoff <session-id> --to pi

# Use a native transcript path instead of an ID.
sesh handoff /path/to/transcript.jsonl --from claude --to pi

# Select the newest session in another project, preserving its workspace.
sesh handoff --dir ~/work/app --from codex --to claude

# Write only; print the file location and exact resume command.
sesh handoff <session-id> --to codex --no-run

# Override a missing/moved workspace, or Pi's model.
sesh handoff <session-id> --to pi --workspace ~/work/app --model provider/model-id --no-run
```

Options may come before or after the ID/path. With no ID/path, `--from` is required; sesh selects the newest readable session from that harness in the current project (or `--dir`). Newest is a convenience, not proof that a session is the conversation currently running in another terminal.

Explicit IDs resolve globally. Ambiguous prefixes fail rather than choosing silently. There is no global "pick the newest session anywhere" behavior. `--dir` is for automatic project selection; omit it when supplying an explicit ID/path.

Discovery scope and destination workspace are separate: selecting a session globally still resumes its recorded project. Only `--workspace` changes that location, and the directory must exist. `--model` is supported only for Pi.

Without `--no-run`, direct handoff requires a terminal. Agents and scripts should always use `--no-run`, then give the user the printed resume command.

### Find sessions without the TUI

```sh
sesh list
sesh list --all
sesh list --dir ~/work/app --harness codex
sesh list --all -n 0           # show every session
```

List output includes saved session names and full IDs so nearby UUIDv7 sessions remain individually selectable. A shorter prefix is accepted only when unique. Discovery reads the first genuine prompt and lightweight name metadata, including appended renames, without importing the event timeline. Claude manual titles outrank generated titles; Codex uses its session-name index; Pi reads saved name records; OMP's first title slot outranks a stale header. Full history parsing happens only for the chosen handoff.

## What carries over

Messages, reasoning where supported, tool calls with their results, and the working directory are translated through a harness-neutral in-memory session.

| Action | Pi | Codex | Claude Code | OMP |
|---|---|---|---|---|
| Shell | `bash` | `exec_command` | `Bash` | `bash` |
| Read | `read` | Text with result | `Read` | `read` for plain files; ranged reads as text |
| Write | `write` | `apply_patch` Add File | `Write` | `write` |
| Edit | `edit` | `apply_patch` Update File | `Edit` | Text with result |
| Unsupported tool | Text with result | Text with result | Text with result | Text with result |

- Only the active branch is written; abandoned rewinds are excluded.
- Foreign reasoning is dropped for Codex and Claude because it cannot have the original provider's signatures/encryption. Pi-family targets handle foreign reasoning as text.
- Unanswered calls receive target-appropriate handling; Codex and Claude get explicit missing-result content rather than invalid call/result pairs.
- Claude file-tool paths become absolute under the workspace.
- Recorded tool calls are history. sesh never executes them.
- Images and harness-specific bookkeeping are not preserved as an archival copy.
- Same-harness handoffs also create a new native session; there is no byte-exact raw-copy mode.
- OMP hashline edits, selectors, URLs, archive members, and tool options with no portable equivalent stay as text rather than losing their original arguments or masquerading as another tool's schema.

## Where files go

Each handoff gets a new ID. Source transcripts are untouched, target files are created exclusively with private permissions, and failed exports remove partial output.

| Harness | Native store | Override |
|---|---|---|
| Claude Code | `~/.claude/projects/<encoded-workspace>/<id>.jsonl` | `CLAUDE_CONFIG_DIR` |
| Codex | `~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl` | `CODEX_HOME` |
| Pi | `~/.pi/agent/sessions/<encoded-workspace>/<time>_<id>.jsonl` | `PI_CODING_AGENT_DIR` |
| OMP | `~/.omp/agent/sessions/<encoded-workspace>/<time>_<id>.jsonl` | `SESH_OMP_AGENT_DIR` (sesh-specific) |

`SESH_OMP_AGENT_DIR` changes sesh's OMP store only, not OMP's own configuration. Native Pi and OMP share the `PI_CODING_AGENT_DIR` environment variable; sesh uses the separate OMP override so you can select their roots independently. OMP is resumed with the explicit written path. Its native output includes the fixed 256-byte title slot.

No sesh files are placed in the project. The target starts in the session workspace with normal terminal input/output. If it fails to launch, the written session remains and sesh prints its resume command.

## Privacy

**Handoffs are local and unredacted, not sanitized exports.** Prompts, source code, tool output, credentials, email addresses, and paths in the conversation may carry over. On resume, the target harness may send that history to its configured model provider. Do not publish native session files as though they were safe sharing bundles.

The old `export`, `import`, `convert`, and JSON `inspect` commands, `.sesh.json` schema, and redaction options have been removed. Existing bundles are not inputs to handoff; select the original native transcript instead. Existing files are not automatically migrated or deleted.

## Use from an agent

The repository's [agent skill](skills/sesh/SKILL.md) teaches session selection and `handoff --no-run`. Keep an installed skill aligned with the CLI; older skill versions still reference export/convert.

## Troubleshooting

| Problem | Action |
|---|---|
| `sesh` not found after installation | Add Go's binary directory to `PATH`. |
| Installed CLI still exposes export/convert | Run `go install ./cmd/sesh` from this checkout and check `command -v sesh` for another installation. |
| No sessions in the current project | Use `sesh --all` or `sesh list --all`; project scope follows the workspace recorded by the harness. |
| Ambiguous ID prefix | Use the full ID from `sesh list` or the picker detail pane; `--from` can narrow the source harness. |
| Unknown external transcript format | Pass `--from claude`, `--from codex`, `--from pi`, or `--from omp`. |
| Workspace unavailable | Use an existing directory with `handoff --workspace`; browsing globally alone does not relocate the session. |
| Target executable missing, login expired, or target rejects startup | Fix the target installation/login, then run the printed resume command. The native session is retained. |
| Claude cannot find the resumed session | Run its resume command from the printed workspace. |

## Development

```sh
go run ./cmd/sesh --all
go test ./...
go vet ./...
```

Releases and breaking changes are recorded in [CHANGELOG.md](CHANGELOG.md). Until 1.0.0, a minor version may change commands and flags.

Versions come from Git tags, not commit messages. To release, run tests and vet, update the changelog with the version and date, commit the release notes, then create and push the tag:

```sh
git tag -a v0.3.0 -m "Release v0.3.0"
git push origin main v0.3.0
```

A Conventional Commit such as `feat!:` does not automatically create a release.

## License

[MIT](LICENSE)
