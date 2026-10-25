# Frequently asked questions

## What problem does Repo Companion solve?

Coding agents are good at reading code and bad at guessing where the surrounding
knowledge lives. Repo Companion gives humans and agents one place to ask for
project docs, organization patterns, decisions, and standards before changing a
service. Every documentation change is a normal Git branch and GitHub pull
request.

## Where are the docs stored?

In one central Git repository, usually called `your-org/docs-store`. The local
CLI keeps a read-only mirror of `main` under `~/.companion` and creates a
worktree per changeset. Project docs are not stored in each service repository.

## Does it replace `AGENTS.md`?

No. It complements repository-local agent files. `companion export agents-md`
maintains a managed block inside an existing `AGENTS.md` and leaves human-written
content outside it untouched. Use the MCP tools when the client supports MCP,
and the export when it does not.

## Do I need GitHub?

For the full workflow, yes: Git transport is SSH or HTTPS and pull requests use
the GitHub API. GitLab is not implemented in the 0.1 line. The CLI demo runs
fully offline against a local bare repository.

## How does authentication work?

- Git operations use your normal SSH key or credential helper.
- PR operations use a token from `COMPANION_FORGE_TOKEN`, `forge.token`, or
  `gh auth token`.
- A private docs store normally needs a token with repository access (`repo`
  for classic tokens, or equivalent fine-grained permissions). Follow your
  organization's least-privilege policy.
- Tokens are stored in a `0600` user config if you choose `forge.token`; using
  the environment or `gh` is usually preferable.

## Can an agent merge, delete, or abandon a changeset?

No. MCP tools can create and update documentation pull requests. Merge,
abandon, and cleanup are human CLI actions, and branch protection remains the
authority. An agent also cannot change security-sensitive user configuration
through MCP.

## Does companion send telemetry or open a port?

No. It is a CLI and a stdio MCP server. It does not run a daemon, listen on a
network port, or send telemetry. It talks to GitHub for the operations you
invoke, and code indexing can invoke the local Go toolchain or `ctags`.

## What does `companion init` do with an empty repository?

If the remote has no commits, `init` creates an initial `main` commit containing
`.companion/registry.yaml`, pushes it, and then creates the local mirror. If the
remote already has a different default branch, create `main` first or use a
store that follows the documented layout.

## Why did my `.companion.yaml` or user config stop being accepted?

Configuration is validated and decoded strictly. Unknown keys, an unsupported
`forge.type`, an invalid `mcp.org_write` value, a non-loopback `http` API URL, or
a non-canonical project are errors rather than silent fallbacks. Fix the
reported key or run `companion config set` for a known key.

## How do I reset local state?

The authoritative data is in the docs store. The local cache can be rebuilt:

```bash
companion doctor --repair
```

To start completely fresh, remove `~/.companion` (or the directory set by
`COMPANION_HOME`) after confirming there are no local changesets you still need.
Unpushed work is protected by archive/force flags on cleanup, but read the
output before deleting anything.

## Is the secret scanner a guarantee?

No. It is a best-effort safety net that covers common credential formats before
commits. It can miss formats and can flag legitimate examples. Do not rely on it
as your only secret control; use your organization's secret scanning and review
practices too.

## Does it work on Windows?

The release targets include Windows amd64 and arm64, and CI runs the test suite
on Windows. Some tests skip symlink creation when the runner cannot create
symlinks. Use a recent Git for Windows and a terminal that supports the editor
you configure.

## Why is there no web UI, daemon, or live collaboration?

The design deliberately keeps Git and pull requests as the source of truth and
avoids another always-on service. The trade-off is explicit sync (`change sync`,
`change submit`) rather than background behavior. See the
[roadmap](../ROADMAP.md) for what is intentionally out of scope.

## How do I get help?

Start with [SUPPORT.md](../SUPPORT.md), then use GitHub Discussions or the issue
templates. Report security problems privately through
[SECURITY.md](../SECURITY.md).
