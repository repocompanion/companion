# Versioning and compatibility

Repo Companion is pre-1.0. The project follows semantic versioning with the
usual pre-1.0 caveat: a minor release may contain a breaking change when the
design needs it. Patch releases are intended to be safe upgrades.

## What counts as a public surface

The following are treated as user-facing and should not change casually:

- CLI command names, flags, and stable machine-readable output (`--json`);
- MCP tool names, descriptions, and input schemas;
- user and repository config keys;
- the documented docs-store layout (`org/`, `teams/`, `projects/`);
- the `Companion-*` trailers and managed-block markers written to PRs and
  `AGENTS.md`;
- release artifact names and checksums.

These are implementation details and may change at any time:

- `state.json` (documented as a rebuildable cache);
- internal Go packages;
- worktree paths and branch naming, as long as existing changesets remain
  usable;
- cache locations under `~/.companion/cache/`;
- log wording and non-JSON human output.

## Deprecation

When practical:

1. announce a deprecation in the changelog and, where possible, the CLI help or
   MCP tool description;
2. keep the old behavior for at least one minor release;
3. remove it in a later release with a migration note.

Security fixes and cases where maintaining the old behavior is unsafe may be
exceptions. They will be called out in the changelog and release notes.

## Store and configuration compatibility

The store is Git, so documents and history survive companion upgrades. The
local cache and `state.json` are not authoritative and can be rebuilt with
`companion doctor --repair` or by recreating changesets.

Config files are read strictly: unknown keys are rejected so a typo cannot
silently change policy. If a future release adds keys, older binaries will
reject them; use the version documented for that store until you upgrade.

## Release process

- Releases are cut from `main` with a `vX.Y.Z` tag.
- The release workflow runs `go vet` and `go test` before publishing.
- GoReleaser produces Linux, macOS, and Windows archives for amd64 and arm64,
  plus `checksums.txt`.
- Release notes are generated from conventional commits and should be reviewed
  by a maintainer before the tag is pushed.
- Pre-1.0 versions are supported on a best-effort basis only; see
  [SECURITY.md](SECURITY.md).
