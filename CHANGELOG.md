# Changelog

All notable changes to sesh are listed here. Versions follow [Semantic Versioning](https://semver.org): until 1.0.0, a minor version may change commands and flags.

## [0.3.0] - 2026-10-21

### Added
- A Bubble Tea v2 session picker as the default terminal experience: project/all-project scope, search, session details, target selection, and explicit confirmation before writing.
- `sesh --all`, `--dir`, and `--from` for scoped interactive browsing.
- `sesh handoff [ID|path] --to claude|codex|pi|omp`: translate native history in memory, write a new target-native session, and resume in the recorded workspace.
- `handoff --no-run` for agents/scripts, plus `--workspace` and Pi-only `--model` overrides. Flags may appear after the session ID/path.
- Native Pi store installation, new session identities for every target, private exclusive writes, partial-file cleanup, and retained-session recovery instructions when launching fails.
- Saved/generated session headings, searchable names, distinct Claude/Codex/Pi/OMP color badges, higher-contrast metadata, spaced rows, and theme-aware selection colors.
- Native OMP discovery and handoff, including authoritative first-line titles, compact home-relative project paths, and valid fixed-width title slots. `SESH_OMP_AGENT_DIR` selects sesh's OMP root separately from Pi.

### Changed
- Session discovery reads metadata and the first genuine prompt rather than importing the entire event history. List output omits event counts and shows full, selectable IDs.
- The CLI and TUI share the same handoff service and native translators. The TUI restores terminal input/output before launching a target and prevents confirmation when details do not fit.
- Repository agent-skill instructions use local `handoff --no-run` rather than portable exports.
- Native name discovery follows Claude manual/generated titles, Codex's session index, Pi saved-name records, and OMP title slots without importing full histories. Untitled sessions retain a first-prompt fallback.
- OMP-specific edits, ranged reads, selectors, archive members, and unsupported tool options are preserved as text where their native semantics cannot be translated faithfully.

### Removed
- Portable `.sesh.json` bundles, raw-record archives, bundle codecs, and redaction. Local handoffs preserve sensitive content; resuming a target may send history to its configured provider.
- The `export`, `import`, `convert`, and JSON `inspect` commands. Existing files are not deleted or migrated; use the original native transcript as the source.
- Pi's byte-exact raw-copy path; same-harness handoffs now rebuild a new native session.

## [0.2.0] - 2026-09-27

### Added
- `sesh list`: the sessions Claude Code, Codex, and Pi recorded for the current directory, newest first, with the first message typed in each. `-all` lists every directory, `-dir` another one, `-harness` narrows to one harness, `-n` sets how many rows.
- `sesh export -last`: export the newest session recorded for the current directory, optionally of one harness (`-harness`).
- `sesh export <ID>`: export by session ID or a unique ID prefix, as shown by `sesh list`. Sessions found by ID or `-last` are written to `<harness>-<id>.sesh.json` in the current directory.
- `sesh version` reports the module version recorded by `go install …@<version>`, or a pseudo-version for builds from a checkout.

### Changed
- The agent skill uses `sesh list` and `sesh export -last -harness <name>` instead of shell commands to find the current session.

## [0.1.0] - 2026-09-26

First public release.

### Added
- `sesh export`: Claude Code, Codex, and Pi transcripts to a portable `.sesh.json` bundle (format version 2) with the neutral event timeline and every source line.
- `sesh import`: validate and summarize a bundle.
- `sesh convert --target pi|codex|claude`: write a bundle as a native session for another harness; `-install` places Codex and Claude sessions where they resume by ID.
- Redaction on by default in `export`: known API key and token formats, URL passwords, upper-case env secrets, emails, and home directories (`/Users/<name>`, `/home/<name>`, `/root`) as `~`. `-no-redact` keeps a byte-exact bundle; `convert` turns `~` back into the current user's home.
- Agent skill in `skills/sesh`, installable with `npx skills add mr-jones123/sesh --skill sesh`.

[0.3.0]: https://github.com/mr-jones123/sesh/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/mr-jones123/sesh/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/mr-jones123/sesh/releases/tag/v0.1.0
