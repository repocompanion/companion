# Contributing to Repo Companion

Thanks for taking the time to improve Repo Companion. This project is
pre-1.0, so feedback on the design, documentation, and developer experience is
as useful as code.

By participating, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

- Read [README.md](README.md), [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md), and
  [SECURITY.md](SECURITY.md).
- For a bug, open an issue with the bug template after checking
  `companion doctor --json`.
- For a larger change, open a discussion or an issue first. It is easier to
  agree on the shape of a refactor before it is written.
- **Never report a vulnerability in a public issue.** Use the private process
  in [SECURITY.md](SECURITY.md).

## Requirements

- Go 1.27.3 or newer
- Git 2.30 or newer
- Optional: Universal Ctags for non-Go symbol indexing

## Build and test

```bash
git clone https://github.com/repocompanion/companion.git
cd companion

make build          # produce ./companion
make test           # go test ./...
make vet            # go vet ./...
make fmt            # gofmt
```

The full test suite uses local bare Git repositories and a fake GitHub API
server. It does not need GitHub credentials or network access. Integration
tests are the slowest part; run a package directly when iterating:

```bash
go test ./internal/changeset -run TestName -count=1
```

## What a change should include

- **Tests.** Bug fixes need a regression test. New behavior needs coverage at
  the level where the behavior lives, not only through the CLI.
- **Formatting and vet.** `gofmt -l cmd internal` must be empty and
  `go vet ./...` must pass.
- **Documentation.** Update the README, `docs/`, or command help when the
  behavior or setup changes. Public documentation lives in Markdown and goes
  through normal pull requests.
- **Compatibility notes.** Call out changes to MCP tool names or input schemas,
  config keys, the store layout, or `state.json`. See [VERSIONING.md](VERSIONING.md).
- **No secrets.** Do not commit tokens, credentials, or private store content.
  The local scanner is a safety net, not a substitute for checking.

## Commit messages

Use conventional-style subjects so the release notes remain useful:

```text
feat(mcp): add a bounded guidance response
fix(store): reject a symlinked registry file
docs: add the store setup guide
test(config): cover invalid org_write values
```

Keep the subject under about 72 characters. Explain the user-visible reason in
the body, not just the mechanics.

### Developer Certificate of Origin

New contributions should be signed off under the
[Developer Certificate of Origin 1.1](https://developercertificate.org/):

```bash
git commit -s -m "fix(store): reject a symlinked registry file"
```

The sign-off certifies that you have the right to submit the contribution under
this project's license. Commits made before this policy was added are not
retroactively required to have a sign-off.

## Pull requests

A good pull request:

1. explains the problem and the chosen approach;
2. includes tests and documentation;
3. keeps unrelated refactors out of the same change;
4. passes `make fmt test vet`;
5. notes any compatibility or security impact;
6. is small enough to review in one sitting when possible.

Maintainers may ask for a change to be split or deferred. Review is best effort.
There is no guaranteed response time.

## Project shape

- `cmd/companion` — process entry point.
- `internal/cli` — Cobra commands and output.
- `internal/mcpserver` — MCP stdio server, tools, resources, and prompts.
- `internal/changeset` — branches, worktrees, sync, submit, and writes.
- `internal/store` — the bare store, read-only mirror, documents, and paths.
- `internal/forge` — GitHub REST/GraphQL operations.
- `internal/config` — layered context and configuration.
- `internal/guidance`, `internal/search`, `internal/codeindex` — knowledge and
  code-reference features.
- `docs/` — public user and architecture documentation.

Internal planning notes are intentionally not part of the public tree. Use the
issue tracker and discussions for design proposals.

## Release process

Maintainers release from a clean `main` with a `vX.Y.Z` tag. GoReleaser builds
the supported targets, publishes checksums, and attaches the archives to the
GitHub release. The release workflow re-runs `go vet` and `go test` before
publishing.
