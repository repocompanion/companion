# docsync

`docsync` is a Git-backed document store for humans and coding agents. Project
docs and organization guidance live in one central repository; every change is
made in a local changeset worktree and published through a GitHub pull request.

The primary problem it solves is **agent context and discoverability**: an agent
can ask for the guidance relevant to a coding task before it starts, and can
find the project docs, open PRs and reference implementations without guessing
paths.

This is an early, working implementation of `PLAN.md`.

## Features

- **Central docs store** (`your-org/docs-store`) with `org/`, `teams/` and
  `projects/<owner>/<repo>/` areas.
- **Read-only main mirror** at `~/.docsync/main`; all reads are local and fast.
- **Active changeset overlay** for reads: pending additions/modifications are visible, deleted docs are tombstoned, and results carry `source`/`state`.
- **Bounded context** for agents: `docs_get_context` returns a 200-doc index by default, with `max_docs`/`full` controls and visible warnings when open PRs could not be checked.
- **Idempotent knowledge writes**: repeating a decision, pattern or scaffold request returns the existing path instead of creating a duplicate PR.
- **Changeset worktrees** at `~/.docsync/changes/<area>--<topic>/`.
- **Explicit sync**: `change sync` commits, integrates the remote branch and
  pushes; `change submit` lints the changed docs, rebases on `main`, marks the
  PR ready and freezes the changeset. There is no daemon, polling or auto-push.
- **Shared PRs**: collaborators join an open PR with `change join`; `submit`
  freezes it until `change reopen`.
- **GitHub integration** through SSH for Git and a token from
  `DOCSYNC_FORGE_TOKEN`, `forge.token`, or `gh auth token` for PRs.
- **MCP server** on stdio with context, guidance, read/history, changeset and
  review-comment tools.
- **Layered context resolution** with provenance: flags, environment, repo
  `.docsync.yaml`, user config, org registry, Git/manifests/hostname.
- **BM25 search** with field weighting, snippets, matched terms and score
  explanations. Guidance uses the same ranking and returns snippets; use
  `docs_read`/`docsync cat` for full content.
- **Guidance ranking** for patterns, ADRs and standards with project → team →
  org precedence and explicit `overrides`.
- **Shared validation** in `internal/docrules` for MCP writes, lint, templates
  and submit preflight; `change submit` keeps the PR draft when lint fails.
- **Lint** for frontmatter, canonical layout, broken links, duplicate IDs and likely secrets.

## Build

Requirements: Go 1.25+, Git 2.30+.

```bash
go build -o docsync ./cmd/docsync
./docsync --help
```

Or:

```bash
make build
make test
```

## Quick start

```bash
# 1. Create the central store in GitHub, then initialize locally.
docsync init --remote git@github.com:your-org/docs-store.git

# 2. Authenticate for PRs.
gh auth login

# 3. Read context and guidance.
docsync context
docsync guidance "implement an SQS consumer in Go"

# 4. Start a changeset and author a doc.
docsync change start "refund flow"
docsync new flows/refund.md --template flow
docsync edit flows/refund.md

# 5. Push and open/update the draft PR.
docsync change sync

# 6. When ready for review.
docsync change submit -m "document the refund flow"
```

For a ready PR that receives review comments:

```bash
docsync change reopen
# edit files
docsync change sync
docsync change submit
```

Agent setup (Claude Code):

```bash
claude mcp add --scope user docsync -- docsync mcp
```

Cursor (`~/.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "docsync": {
      "command": "docsync",
      "args": ["mcp"]
    }
  }
}
```

`docsync init` prints a snippet for each code repository's `CLAUDE.md`,
`AGENTS.md` or `.cursor/rules`.

## Commands

| Command | Purpose |
|---|---|
| `docsync init --remote <url>` | Clone the bare store and create the main mirror |
| `docsync pull` | Fetch main and fast-forward the mirror |
| `docsync status` | Mirror, changesets, dirty files and PR states |
| `docsync context` | Resolved identity/project/team/languages with sources |
| `docsync project` | Resolved project, team and area |
| `docsync ls [DIR] [--pending]` | List docs with pending states, optionally with open PRs |
| `docsync cat <doc> [--rev R] [--pr N]` | Print a document; reports changeset deletions |
| `docsync search <query> [--all]` | Search with the active changeset overlay |
| `docsync guidance "<task>" [--lang go]` | Ranked patterns, ADRs, standards |
| `docsync change start "<topic>"` | Start a changeset |
| `docsync change join <PR#>` | Join a shared changeset |
| `docsync change sync [id]` | Commit, integrate, push, create/update draft PR |
| `docsync change submit [id] [--skip-lint]` | Lint, rebase on main, mark ready, freeze |
| `docsync change reopen [id]` | Return a ready PR to draft |
| `docsync change rebase [id]` | Resolve conflicts with the remote branch/main |
| `docsync change cleanup [id]` | Remove a merged/closed changeset |
| `docsync change abandon [id]` | Close the PR and remove the worktree |
| `docsync change adopt` | Move accidental mirror edits into a changeset |
| `docsync new <doc> --template ...` | Create a doc from a template |
| `docsync adr new "<title>"` | Create a date-stamped ADR |
| `docsync edit <doc>` | Open a document in the active worktree |
| `docsync move <doc> <new-path>` | Rename with Git history preserved |
| `docsync log <doc> [--wip]` | Versions, authors, PRs and agent source |
| `docsync diff <doc> [A] [B] [--pr N]` | Diff versions or a PR |
| `docsync blame <doc>` | Line authorship |
| `docsync restore <doc> <version>` | Open a changeset that restores a version |
| `docsync lint [--store DIR]` | Local subset of store CI |
| `docsync config get\|set\|init-repo` | Manage user and repo config |
| `docsync registry add-repo <repo> --team <t>` | Update the org registry via PR |
| `docsync doctor [--online]` | Check git, store, auth and mirror |
| `docsync mcp` | Run the MCP server on stdio |

## MCP document structure

MCP writes are validated. A project uses this canonical layout:

```text
README.md
architecture.md
adr/YYYY-MM-DD-<slug>.md
flows/<slug>.md
runbooks/<slug>.md
patterns/<slug>.md
standards/<slug>.md
```

Every `.md` written through MCP needs frontmatter with `id`, `kind`, `title`,
`status`, `owners` and `last_reviewed`. `docs_get_context` returns this structure
in its `structure` field even when a project has no docs yet. Flat aggregate
files such as `decisions.md` or `operations.md` are rejected; use one ADR per
decision, one flow per flow and one runbook per procedure. `docs_delete` can
remove only `.md`/`.mmd` files and requires `base_rev`; `docs_move` renames
with `git mv` and validates the destination.

## MCP tools

Read: `docs_get_context`, `docs_get_guidance`, `docs_list`, `docs_search`,
`docs_read`, `docs_history`, `docs_diff`, `docs_pull`.

Changes: `docs_change_start`, `docs_change_join`, `docs_write`, `docs_patch`,
`docs_move`, `docs_delete`, `docs_record_decision`, `docs_propose_pattern`, `docs_scaffold_project`,
`docs_set_context`, `docs_change_sync`, `docs_change_submit`,
`docs_change_reopen`, `docs_change_status`, `docs_review_comments`.

Agents can never merge. `docs_change_submit` lints changed documents and keeps
the PR as a draft on errors; only a human can use `docsync change submit
--skip-lint`. A human merges the PR under GitHub branch protection.

## Configuration

Highest priority first:

1. flags / MCP arguments
2. environment (`DOCSYNC_PROJECT`, `DOCSYNC_TEAM`, `DOCSYNC_HOME`, ...)
3. repo `.docsync.yaml`
4. user config (`~/.config/docsync/config.yaml`; `%APPDATA%\docsync\config.yaml`)
5. store registry (`.docsync/registry.yaml`)
6. auto-detection from Git remotes, manifests and hostname
7. built-in defaults

Example user config:

```yaml
remote: git@github.com:your-org/docs-store.git
forge:
  type: github
user:
  default_team: payments
machine: ana-mbp
changesets:
  branch_prefix: docs
  auto_create: true
mcp:
  org_write: propose-only
  max_file_size: 1048576
```

Example repo config:

```yaml
project: your-org/payments-api
team: payments
domain: checkout
languages: [go]
docs:
  required: [README.md, architecture.md, runbooks/]
```

## Safety model

- `main` is read-only locally and protected on GitHub.
- Writes happen only in a changeset worktree; agent writes are committed
  immediately and local secrets are scanned before every commit.
- Paths are checked for escapes (including symlinks).
- Per-area file locks serialize Git work between concurrent CLI/MCP calls.
- MCP `docs_write` / `docs_patch` reject missing frontmatter and non-canonical
  document paths.
- `change sync` refuses to push when a PR is ready.
- `change submit` freezes a changeset until `change reopen`.
- Unpushed local work is never deleted silently; cleanup/abandon require
  `--archive` or `--force`.
- No daemon, service, polling, cloud agent or local network port.

## Development

```bash
make fmt
make test
make vet
```

Integration tests use local bare repositories and a fake GitHub API server; they
do not need GitHub or network access. Real-token PR/GraphQL paths are exercised
manually against a sandbox repository.

## Limitations

- GitHub only; GitLab is not implemented.
- One central store; docs are not stored inside service repositories.
- No web UI, live co-editing, semantic search or background daemon.
- GitHub review/check display is minimal; branch protection remains the source
  of truth.
- MCP project detection uses the process cwd and the `project` argument; MCP
  client roots are not consulted yet.
- Image/binary additions are a human CLI/editor action, not an MCP write.

See `PLAN.md` for the full design and roadmap.
