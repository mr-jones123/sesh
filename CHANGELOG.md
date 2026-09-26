# Changelog

All notable changes to sesh are listed here. Versions follow [Semantic Versioning](https://semver.org): until 1.0.0, a minor version may change commands, flags, or the bundle format.

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

[0.2.0]: https://github.com/mr-jones123/sesh/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/mr-jones123/sesh/releases/tag/v0.1.0
