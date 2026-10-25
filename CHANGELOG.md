# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[semantic versioning](https://semver.org/) with the pre-1.0 caveats in
[VERSIONING.md](VERSIONING.md).

## [Unreleased]

### Security

- Restrict `docs_set_context` to non-sensitive context fields. MCP clients can
  no longer change the docs-store remote, forge token, or forge API URL.
- Contain tracked symlinks in `companion edit`, config/registry reads, and
  imports.
- Broaden secret detection and scan UTF-16 content. The scanner remains a
  best-effort heuristic.
- Reject non-GitHub forge hosts unless a matching GitHub Enterprise API URL is
  configured; use the real host in exported links.
- Redact credentials from git errors and remote diagnostics.
- Require private (`0700`) companion state directories.
- Disable interactive git prompts in MCP mode and bound `gh auth token`.

### Added

- `companion version` and a real MCP server version, including a
  `debug.ReadBuildInfo` fallback for `go install`.
- `doctor --strict` for setup scripts.
- Bootstrap of an empty docs store during `companion init`.
- Exit code `2` for pending publishes, with JSON output preserved.
- Public launch documentation and community health files.

### Changed

- Raise the minimum supported Go version to 1.27.3 and update
  `golang.org/x/tools` to 0.52.0.
- Stabilize the Windows test suite with explicit LF rules and a Windows-safe
  MCP root URI; run govulncheck with a patched stable toolchain.

### Fixed

- Parse frontmatter with whole-line fences; a `---` inside a block scalar no
  longer truncates a document, and unclosed frontmatter is an error.
- Report search line numbers relative to the file.
- Fail closed when changed-file listing or the submit lint gate cannot run.
- Validate configuration on load instead of silently accepting unknown keys,
  invalid policies, cleartext non-loopback API URLs, and malformed repository
  projects.
- Propagate repository and registry load errors instead of falling back.
- Avoid requesting MCP roots from clients that did not advertise the capability.
- Make the managed `AGENTS.md` export stable for a given docs commit.
- Keep optional `ctags` out of `doctor --strict` failures.
- Reject Windows drive paths when parsing hosted remotes.

## [0.1.0] - 2026-10-25

### Added

- Initial public prototype: central Git-backed docs store, changeset
  worktrees, GitHub pull requests, guidance, BM25 search, context exports,
  legacy import, code-reference validation, and an MCP stdio server.
