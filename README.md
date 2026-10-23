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
- **Non-MCP exports**: generate a managed `AGENTS.md` block, `llms.txt` discovery file, or a JSON/Markdown context bundle from the same overlay and BM25 search.
- **Legacy import**: scan a Markdown tree, review a migration report, and apply resolved documents into one changeset with `docsync import`.
- **Code context**: validate `reference_impls` paths/symbols, resolve symbols with the Go provider and optional ctags/LSP, detect advisory drift, and expose `code map`, `code find` and `stale` commands plus MCP tools.
- **Changeset worktrees** at `~/.docsync/changes/<area>--<topic>/`.
- **Explicit sync**: `change sync` commits, integrates the remote branch and
  pushes; `change submit` lints the changed docs, rebases on `main`, marks the
  PR ready and freezes the changeset. There is no daemon, polling or auto-push.
- **Shared PRs**: collaborators join an open PR with `change join`; `submit`
  freezes it until `change reopen`.
- **GitHub integration** through SSH for Git and a token from
  `DOCSYNC_FORGE_TOKEN`, `forge.token`, or `gh auth token` for PRs.
- **MCP server** on stdio with context, guidance, read/history, changeset and
  review-comment tools, plus roots, document/template resources and cursor
  pagination.
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

## Optional tooling for code context

Go code works without external tooling. These tools add broader validation:

### Universal Ctags

```bash
# macOS
brew install universal-ctags

# Ubuntu/Debian
sudo apt install universal-ctags

# Windows
winget install UniversalCtags.Ctags

# Verify
ctags --version
```

The first line must say `Universal Ctags`. The BSD `ctags` bundled with macOS
does not provide JSON output and is not used.

### SCIP index

SCIP is a durable external index format. Generate an index with your language's
SCIP indexer, then point docsync at it:

```bash
export DOCSYNC_SCIP_INDEX=/absolute/path/to/index.scip
# or place index.scip at the repository root
```

When present, SCIP is used for durable definitions and references before LSP or
ctags. It is not generated automatically. LSIF is not implemented yet.

### LSP language servers

```bash
# Go
go install golang.org/x/tools/gopls@latest

# TypeScript/JavaScript
npm install -g typescript-language-server typescript

# Python
npm install -g pyright
# or: pipx install python-lsp-server

# PHP
npm install -g intelephense
# or install phpactor and put it on PATH
```

For Java, install Eclipse JDT LS and put `jdtls` on `PATH`. For Kotlin, install
`kotlin-language-server`. For Rust, install `rust-analyzer`.

The LSP provider is live and current-repo-only. It is not used as a durable
code-map index and does not run a background daemon.

Verify what is available:

```bash
docsync doctor
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
| `docsync export agents-md` | Managed AGENTS.md block for coding agents |
| `docsync export llms-txt` | Compact llms.txt discovery file |
| `docsync export context` | Bounded JSON/Markdown context bundle |
| `docsync import --from <dir>` | Dry-run or apply a legacy Markdown migration |
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
| `docsync validate-code-refs [--all]` | Validate reference_impls paths, symbols and drift |
| `docsync stale [--all]` | Show documents with stale code references |
| `docsync code map [--path DIR]` | Index available code symbols in the current repo |
| `docsync code find <symbol>` | Resolve a code symbol |
| `docsync code refs <symbol>` | Find LSP references to a symbol |
| `docsync config get\|set\|init-repo` | Manage user and repo config |
| `docsync registry add-repo <repo> --team <t>` | Update the org registry via PR |
| `docsync doctor [--online]` | Check git, store, auth, mirror and code providers |
| `docsync mcp` | Run the MCP server on stdio |

## Exports for non-MCP agents

```bash
docsync export agents-md --output AGENTS.md
docsync export llms-txt --output llms.txt
docsync export context --format markdown --output context.md
```

Exports use the active changeset overlay, so pending docs are visible with their
source/state. They include a snapshot warning, required-doc status, documentation
groups and top BM25 guidance for common tasks.

`agents-md` writes a managed `<!-- docsync:begin -->` / `<!-- docsync:end -->`
block and preserves human content outside it. It refuses to overwrite a file
without that block unless `--force` is passed. Without `--output`, all export
commands write to stdout.

## Importing existing documentation

```bash
# Inspect the proposed mapping without writing anything.
docsync import --from ./docs --project acme/api --report migration.json

# Resolve ambiguous files with a path mapping.
docsync import --from ./docs --project acme/api --map mapping.json --apply
```

The importer detects README/architecture docs, ADRs, flows, runbooks, patterns
and standards from frontmatter, paths and headings. Ambiguous entries,
aggregate files and collisions are marked `unresolved` or `collision` rather
than guessed. `--apply` requires a report with no unresolved entries, writes
the documents into one changeset and opens/updates a draft PR.

The current implementation copies external files. `git mv`/history preservation
and aggregate-section mappings are deferred; see `CAVEATS.md`.

## MCP roots, resources and pagination

- `docs_list`, `docs_search` and `docs_get_context` accept `limit`/`max_docs`
  and an opaque `cursor`; responses include `next_cursor` when another page
  exists.
- `docsync://docs/{+path}` resources return an overlay-aware document.
- `docsync://templates/{kind}` resources return canonical skeletons.
- Tool annotations distinguish read-only, destructive and idempotent tools.
- MCP roots are used for project discovery when the client provides them. The
  current implementation negotiates protocol `2025-11-25` so the roots API
  remains available during its deprecation window; newer protocol versions fall
  back to cwd until multi-round-trip roots support is added.

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
`docs_read`, `docs_history`, `docs_diff`, `docs_pull`,
`docs_validate_code_refs`, `docs_stale`, `docs_code_map`, `docs_code_find`,
`docs_code_refs`.

Changes: `docs_change_start`, `docs_change_join`, `docs_write`, `docs_patch`,
`docs_move`, `docs_delete`, `docs_record_decision`, `docs_propose_pattern`, `docs_scaffold_project`,
`docs_set_context`, `docs_change_sync`, `docs_change_submit`,
`docs_change_reopen`, `docs_change_status`, `docs_review_comments`.

Agents can never merge. `docs_change_submit` lints changed documents and keeps
the PR as a draft on errors; only a human can use `docsync change submit
--skip-lint`. A human merges the PR under GitHub branch protection.

## Code context

Code references are declared in document frontmatter:

```yaml
reference_impls:
  - repo: your-org/payments-api
    path: internal/consumer/consumer.go
    symbol: Consumer.Run        # optional
    kind: method                # optional
    ref: main
    fingerprint: sha256:...     # optional
```

Path validation works for every language. Symbol validation is provider-based:

- native Go provider: `go/packages` / `go/ast`;
- optional `universal-ctags`: syntax symbols for many languages;
- optional LSP: live definitions for the current repo;
- otherwise: `git grep`, reported as `unverified`.

Current-repo references are validated. References to another repo are reported
as `external` and are not symbol-checked. Drift is advisory.

### CLI

```bash
# Validate all reference_impls in the current project.
docsync validate-code-refs

# Validate one document.
docsync validate-code-refs --path projects/acme/api/patterns/sqs.md

# Validate the whole store as JSON.
docsync validate-code-refs --all --json

# Show only stale references.
docsync stale
docsync stale --all --json

# Inspect the current repo's symbol map.
docsync code map --path internal/consumer

# Resolve a specific symbol.
docsync code find Consumer.Run --path internal/consumer

# Find references to a symbol through LSP.
docsync code refs Consumer.Run --path internal/consumer --include-declaration

# Show available providers.
docsync doctor
```

`code map` uses durable providers (Go, ctags and optional SCIP). LSP is used
for live resolution by `code find`/`code refs` and validation, not to build the
durable map.

Code indexes are cached under:

```text
~/.docsync/cache/code/<repo>/<commit>/
```

### MCP

The same operations are available as read-only tools:

| Tool | Purpose |
|---|---|
| `docs_validate_code_refs` | Validate paths, symbols, providers and drift |
| `docs_stale` | Return documents with stale references |
| `docs_code_map` | Return bounded Go/ctags symbols for the current repo |
| `docs_code_find` | Resolve a symbol live through available providers |
| `docs_code_refs` | Find references through an available LSP provider |

Examples:

```text
docs_validate_code_refs { "scope": "project" }
docs_validate_code_refs { "path": "projects/acme/api/patterns/sqs.md" }
docs_stale { "scope": "project" }
docs_code_map { "path": "internal/consumer", "kind": "method", "limit": 100 }
docs_code_find { "symbol": "Consumer.Run", "path": "internal/consumer" }
docs_code_refs { "symbol": "Consumer.Run", "path": "internal/consumer", "include_declaration": true }
```

Reference validation results include:

```text
path_status    valid | missing | external | unverified | invalid
symbol_status  valid | missing | unsupported | external | unverified
drift_status   fresh | stale | unknown
provider       go | ctags | lsp-<language> | git-grep
```

Drift is currently advisory. It appears in validation, context and guidance
warnings and in `docs_stale`, but does not block submit.

### Provider limits

- Go definitions/symbols: native.
- ctags: syntax symbols only.
- LSP: definitions and references when a server is installed.
- SCIP: durable definitions/references when `DOCSYNC_SCIP_INDEX` or
  `index.scip` is available.
- LSIF: not implemented.
- Cross-repo symbol resolution: intentionally out of scope.
- Go indexes must match the current checkout; non-HEAD symbol validation is
  pending.

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
- MCP roots are supported on protocol `2025-11-25`; protocol `2026-07-28+`
  falls back to cwd because roots were replaced by multi-round-trip requests.
- The current MCP roots implementation is intentionally lazy and cache-aware;
  it does not yet use the newer multi-round-trip roots input request.
- Image/binary additions are a human CLI/editor action, not an MCP write.
- Import copies files; it does not yet preserve Git history with `git mv`.
- `docsync import` is CLI-only; no MCP import tool scans arbitrary local paths.
- Code-reference validation is strongest for Go. Non-Go syntax symbols use
  `universal-ctags`; live definitions use an installed LSP server. Without either,
  they use `git grep` and are marked `unverified`.
- The native Go provider is primary; `gopls` is an optional semantic fallback.
- LSP supports definitions and references.
- SCIP is supported through a prebuilt index; SCIP generation is external and
  LSIF is not implemented yet.
- Code-reference validation is not yet part of `docsync lint` or submit preflight.
- Go indexing currently covers packages, functions, methods, types and basic
  route calls; jobs/queues, migrations, config and tests are pending.
- ctags is syntax-only and language coverage depends on the installed
  Universal Ctags build.
- Drift warnings are advisory; submit preflight does not yet block or warn.
- Code-change drift uses the referenced path’s latest Git date.
- `docs_stale` only covers the current repository.
- Fingerprints are compared for Go, ctags and SCIP symbols when available;
  unsupported providers and path-only references may report drift as `unknown`.
- SCIP index freshness is not verified against the current checkout.

See `PLAN.md` for the full design and roadmap.
