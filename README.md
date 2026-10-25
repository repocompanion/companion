# Repo Companion

[![CI](https://github.com/repocompanion/companion/actions/workflows/ci.yml/badge.svg)](https://github.com/repocompanion/companion/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/repocompanion/companion?include_prereleases)](https://github.com/repocompanion/companion/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/repocompanion/companion)](go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

**Repo Companion** is a Git-backed documentation store for humans and coding
agents. It keeps project docs, organization patterns, decisions, and standards
in one central repository. Every change is made in a local changeset worktree
and published as a GitHub pull request.

The companion CLI and MCP server let an agent ask *“what guidance applies to
this task?”* and find the relevant docs without guessing paths. It can then
propose documentation changes through the same reviewed PR workflow humans use.
Agents never merge.

> **Status: v0.1, pre-1.0.** The core workflow is usable, but interfaces may
> still change between minor releases. See [VERSIONING.md](VERSIONING.md) and
> the [roadmap](ROADMAP.md).

## Try it in 60 seconds

The offline demo creates a throwaway docs store, a small Go repository, and a
changeset. It needs no GitHub account, token, or network access.

```bash
git clone https://github.com/repocompanion/companion.git
cd companion
make build
bash docs/demo-cli-only.sh
```

The script starts and ends by checking its own output: worktree, overlay read,
pushed branch, and changeset commit must all exist. See
[docs/DEMO_CLI.md](docs/DEMO_CLI.md) for the walkthrough.

## Install

### Prebuilt binaries

Download the archive for your OS and architecture from
[Releases](https://github.com/repocompanion/companion/releases/latest), then
place `companion` somewhere on `PATH`. Each archive contains `LICENSE`,
`NOTICE`, `THIRD_PARTY_NOTICES.md`, and `SECURITY.md`; the release also publishes
`checksums.txt`.

```bash
# macOS/Linux example; use the latest version and correct architecture
tar -xzf companion_*_darwin_arm64.tar.gz
sudo mv companion /usr/local/bin/
companion version
```

Verify a download against `checksums.txt` if your release includes it:

```bash
shasum -a 256 -c checksums.txt
```

### Go install

```bash
go install github.com/repocompanion/companion/cmd/companion@latest
companion version
```

Go 1.27.3 or newer is required. `companion version` reports the module version
for `go install` builds and the release version for downloaded archives.

### Build from source

```bash
git clone https://github.com/repocompanion/companion.git
cd companion
make build        # ./companion
make test
```

Optional tooling: **Universal Ctags** adds non-Go symbol indexing. Go code works
without it. The first line of `ctags --version` must say `Universal Ctags`; the
BSD `ctags` bundled with macOS is not used.

```bash
# macOS
brew install universal-ctags
# Ubuntu/Debian
sudo apt install universal-ctags
# Windows
winget install UniversalCtags.Ctags
```

## Quick start with GitHub

### 1. Create the docs store

Create a private GitHub repository for the central store, for example
`your-org/docs-store`. It can be empty: `companion init` creates the initial
`main` commit with `.companion/registry.yaml` if the repository has no commits.
If your organization requires a different default branch, create `main` first.

The recommended repository layout and branch-protection settings are in
[docs/STORE.md](docs/STORE.md).

### 2. Authenticate

```bash
gh auth login
```

Git uses your normal SSH key or credential helper. PR operations use a token
from `COMPANION_FORGE_TOKEN`, `forge.token`, or `gh auth token`. See
[SECURITY.md](SECURITY.md) for the token model.

### 3. Initialize and ask for context

```bash
companion init --remote git@github.com:your-org/docs-store.git
companion context
companion guidance "implement an SQS consumer in Go" --lang go
companion doctor --strict
```

`companion context` shows the resolved project, team, languages, and the source
of each value. `companion guidance` ranks patterns, ADRs, and standards and
attaches validated `reference_impls` where the code repository is available.

### 4. Make a documentation change

```bash
companion change start "refund flow"
companion new flows/refund.md --template flow
companion edit flows/refund.md
companion change sync        # commit, push, create/update a draft PR
companion change submit -m "document the refund flow"
```

`submit` lints the changed docs, validates code references, rebases on `main` if
needed, marks the PR ready, and freezes the changeset. A human merges under
branch protection. To continue after review:

```bash
companion change reopen
# edit and sync
companion change submit
```

## Connect an agent

The MCP server runs over stdio. No port is opened and no daemon is started.

**Claude Code:**

```bash
claude mcp add --scope user companion -- companion mcp
```

**Cursor** (`~/.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "companion": {
      "command": "companion",
      "args": ["mcp"]
    }
  }
}
```

`companion init` also prints a short documentation instruction block for a
repository’s `CLAUDE.md`, `AGENTS.md`, or `.cursor/rules`. For non-MCP clients,
use the exports:

```bash
companion export agents-md --output AGENTS.md
companion export llms-txt --output llms.txt
companion export context --format markdown --output context.md
```

## What it does

- **One central docs store** with `org/`, `teams/`, and
  `projects/<owner>/<repo>/` areas.
- **Read-only local mirror** at `~/.companion/main`, with an active changeset
  overlay so pending additions, edits, and deletions are visible.
- **Explicit changesets**: a branch, a worktree, and a draft PR. No background
  sync, polling, or auto-push.
- **Guidance for agents**: task-based ranking across patterns, ADRs, and
  standards, with project → team → org precedence and explicit overrides.
- **Local BM25 search** with field weighting, snippets, line numbers, and
  matched terms.
- **Bounded context** for agents, with pagination and visible warnings when open
  PRs cannot be checked.
- **Code references** in document frontmatter, validated against the current
  repository with the native Go provider or optional ctags.
- **Exports** for agents without MCP: a managed `AGENTS.md` block, `llms.txt`,
  and a JSON/Markdown context bundle.
- **Legacy import** that scans a Markdown tree, reports ambiguous files, and
  applies only a resolved mapping.
- **MCP tools** for context, guidance, docs, changesets, reviews, code maps, and
  pull requests.
- **Safety controls**: canonical paths, frontmatter validation, symlink
  rejection, `base_rev` guards, secret scanning, per-area locks, and no
  agent merge.

## Importing existing documentation

Start with a dry run. The importer never guesses ambiguous files:

```bash
# Inspect the proposed mapping without writing anything.
companion import --from ./docs --project acme/api --report migration.json

# Resolve ambiguous files with a path mapping, then apply.
companion import --from ./docs --project acme/api --map mapping.json --apply
```

Ambiguous entries, aggregate files, and collisions are marked `unresolved` or
`collision`. `--apply` requires a report with no unresolved entries, writes the
documents into one changeset, and opens or updates a draft PR. Import copies
files; preserving history with `git mv` is not implemented yet.

## How it works

```text
MCP client / CLI ──► runtime.Session ──► changeset.Manager
                                              │
                    ┌─────────────────────────┼─────────────────────┐
                    ▼                         ▼                     ▼
             ~/.companion/main        changeset worktree     GitHub PR
             (read-only mirror)       (~/.companion/changes)  (forge API)
```

The docs store is cloned bare, `main` is checked out read-only, and each
changeset gets its own worktree. The local `state.json` is a rebuildable cache.
Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the package map, read/write
paths, and safety boundaries.

## Documentation

- [Store setup](docs/STORE.md) — repository layout, branch protection,
  CODEOWNERS, registry, and docs CI.
- [Architecture](docs/ARCHITECTURE.md) — packages, data flow, storage layout.
- [FAQ](docs/FAQ.md) — setup, authentication, Windows, and design questions.
- [Troubleshooting](docs/TROUBLESHOOTING.md) — common errors and recovery.
- [Comparison](docs/COMPARISON.md) — versus `AGENTS.md`, Backstage, Confluence,
  and docs-as-code.
- [CLI demo](docs/DEMO_CLI.md) — the offline walkthrough in detail.
- [Versioning](VERSIONING.md), [roadmap](ROADMAP.md), and
  [changelog](CHANGELOG.md).

## Commands

| Command | Purpose |
|---|---|
| `companion init --remote <url>` | Clone the store and create the local mirror; seed an empty store |
| `companion pull` | Fetch `main` and fast-forward the mirror |
| `companion context` / `project` | Show resolved project, team, languages, and provenance |
| `companion status` | Mirror, changesets, dirty files, and PR state |
| `companion ls [DIR] [--pending]` | List docs, optionally with active changeset state |
| `companion cat <doc> [--rev R] [--pr N]` | Print a document from main, a revision, or a PR |
| `companion search <query> [--all]` | Search the active overlay |
| `companion guidance "<task>" [--lang go]` | Ranked patterns, ADRs, and standards with code context |
| `companion change` (`start`, `sync`, `submit`, `reopen`, `rebase`, `cleanup`, `abandon`, `join`, `adopt`) | Manage the changeset lifecycle |
| `companion new <doc> --template ...` | Create a document from a canonical template |
| `companion adr new "<title>"` | Create a date-stamped ADR |
| `companion edit\|move\|restore` | Edit, rename with history, or restore a document |
| `companion log\|diff\|blame` | Inspect versions, diffs, and authorship |
| `companion export agents-md\|llms-txt\|context` | Render context for non-MCP agents |
| `companion import --from <dir>` | Inspect or apply a legacy Markdown migration |
| `companion lint [--store DIR]` | Local subset of store CI |
| `companion validate-code-refs [--all]` | Validate reference paths, symbols, and drift |
| `companion stale [--all]` | Show documents with stale code references |
| `companion code map\|find` | Inspect the current repository’s symbols |
| `companion doctor [--strict] [--repair]` | Check git, store, auth, mirror, state, and providers |
| `companion mcp` | Run the MCP server on stdio |
| `companion version` | Print the build version |

Run `companion <command> --help` for flags and examples.

## MCP tools

Read: `docs_get_context`, `docs_get_guidance`, `docs_list`, `docs_search`,
`docs_read`, `docs_history`, `docs_diff`, `docs_pull`,
`docs_validate_code_refs`, `docs_stale`, `docs_code_map`, `docs_code_find`.

Changes: `docs_change_start`, `docs_change_join`, `docs_write`, `docs_patch`,
`docs_move`, `docs_delete`, `docs_record_decision`, `docs_propose_pattern`,
`docs_scaffold_project`, `docs_set_context`, `docs_change_sync`,
`docs_change_submit`, `docs_change_reopen`, `docs_change_status`,
`docs_review_comments`.

`docs_write` and `docs_patch` require canonical paths and frontmatter;
`docs_delete` and edits to existing files require `base_rev`. Security-sensitive
user configuration cannot be changed through MCP. There is no merge, abandon, or
cleanup tool: a human merges the PR.

## Safety model

- `main` is read-only locally and protected on GitHub.
- All writes happen in a changeset worktree, are committed immediately, and are
  scanned for likely secrets before the commit.
- Document paths reject absolute paths, traversal, `.git`, and symlink escapes.
- `base_rev` prevents overwriting a newer edit; failed writes roll back.
- Per-area file locks serialize Git work between concurrent CLI/MCP calls.
- `change submit` freezes a changeset until `change reopen`; `change sync`
  refuses to push a ready PR.
- Unpushed work is never silently deleted; cleanup/abandon require `--archive`
  or `--force`.
- No daemon, service, polling loop, telemetry, or local network port.
- The secret scan is a heuristic, not a guarantee.

## Status and limitations

- **GitHub only** for the PR workflow; GitLab is not implemented.
- **One central store**; project docs are not stored in service repositories.
- **No web UI, live co-editing, semantic/vector search, or background daemon.**
- **MCP roots** are supported on protocol `2025-11-25`. Newer clients that do not
  provide roots fall back to the process working directory.
- **Agents can never merge.** Branch protection remains the source of truth.
- **Code references** are strongest for Go. Non-Go symbols use optional
  Universal Ctags; without it they are reported as unverified. Cross-repo symbol
  resolution is out of scope, and drift warnings are advisory.
- **Import copies files**; it does not yet preserve Git history with `git mv`.
- **Pre-1.0:** a minor release may change a surface. See
  [VERSIONING.md](VERSIONING.md).

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) and the
[Code of Conduct](CODE_OF_CONDUCT.md). For questions, use
[Discussions](https://github.com/repocompanion/companion/discussions); for
reproducible bugs, use the issue templates.

**Security:** do not open a public issue. Use
[SECURITY.md](SECURITY.md).

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE). Third-party
notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
