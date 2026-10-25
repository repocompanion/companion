# Roadmap

This roadmap describes direction, not a commitment or a release date. The
project is pre-1.0; priorities can change as real usage and security reports
come in. Discussion and well-scoped proposals are welcome in
[GitHub Discussions](https://github.com/repocompanion/companion/discussions).

## Now — make the current workflow trustworthy

- Finish the public-launch hardening: configuration boundaries, path safety,
  secret detection, exit contracts, and clear failure modes.
- Keep the install, upgrade, and first-run path boring: prebuilt binaries,
  `go install`, empty-store bootstrap, `doctor --strict`.
- Make the documentation set useful on its own: quickstart, store setup, FAQ,
  troubleshooting, comparison, and architecture.
- Grow regression coverage around the CLI boundary, config precedence, parser
  edge cases, and MCP tool contracts.
- Add release trust: reproducible artifacts, checksums, signed checksums, SBOM,
  and vulnerability scanning.

## Next — reduce maintenance and surprise

- Split `internal/changeset` into focused units and introduce a shared
  application-service layer so CLI and MCP behavior cannot drift.
- Add read-size limits and cancellation/deadlines to every network and Git
  operation; make Ctrl-C work consistently.
- Broaden MCP protocol compatibility and add contract tests for older clients.
- Add fuzz targets for frontmatter, path resolution, remote parsing, cursors,
  and slug generation.
- Improve GitHub Enterprise support and document its configuration.
- Add a public example docs store and a short recorded demo of the agent loop.

## Later — deepen the knowledge layer

- SQLite FTS/vector investigation for larger stores, if the in-memory BM25
  index becomes a measured bottleneck.
- Richer symbol providers beyond Go and ctags, if the validation model can stay
  understandable.
- Better drift and ownership signals, with clear advisory-versus-blocking
  semantics.
- Documentation-store CI templates and migration helpers for common systems.

## Explicit non-goals

- A daemon, always-on service, or background polling agent.
- A web UI or live document co-editing.
- Automatic merge, cleanup, or abandonment by an agent.
- Storing project docs inside every service repository; the central store is
  the design.
- A general-purpose wiki or knowledge base. Repo Companion is a Git-backed
  documentation workflow for coding agents and their humans.
- GitLab support in the 0.1 line. The code currently targets GitHub.

## How to influence the roadmap

Open a discussion describing the problem, the current workaround, and what
success looks like. Small, tested pull requests that fit an existing item are
easier to accept than a broad rewrite. Security issues always take priority over
new features.
