# Repo Companion CLI-only demo

A self-contained walkthrough for people who only want the CLI. No MCP client, no
GitHub token and no network access are required.

The runnable script is [demo-cli-only.sh](demo-cli-only.sh). This page explains
what it does, what to expect and how to verify it.

---

## Quick start

```bash
git clone https://github.com/repocompanion/companion.git
cd companion
bash docs/demo-cli-only.sh
```

Optional overrides:

```bash
DEMO=/tmp/my-companion-demo \
COMPANION_BIN=/absolute/path/to/companion \
bash docs/demo-cli-only.sh
```

The script:

- Creates `$DEMO/seed-store` and pushes it to a local bare repo,
  `$DEMO/docs-store.git`.
- Creates a small Go repository at `$DEMO/acme-api` with a `.companion.yaml`.
- Builds `companion` from this checkout if `COMPANION_BIN` is not executable.
- Uses an isolated `COMPANION_HOME` and `COMPANION_CONFIG` under `$DEMO`.
- Leaves the demo in place at the end for inspection.

Default `DEMO` is `/tmp/companion-cli-demo`.

---

## What it demonstrates

| # | Command | What to look for |
|---:|---|---|
| 1 | `companion init --remote <store>` | Store, mirror and config paths; `ssh: ok`; GitHub-token warning (expected offline) |
| 2 | `companion context` | Project `acme/api`, team `payments`, languages `go`, area `projects/acme/api`, each with its source |
| 3 | `companion guidance "implement an SQS consumer in Go" --lang go` | Ranked `SQS consumer` pattern, a `ref:` line and a validated `code:` line |
| 4 | `companion validate-code-refs --all` | `1 valid paths, 0 missing`, symbol/path valid and drift fresh |
| 5 | `companion search "dead-letter" --all` | Hit in `org/patterns/messaging/sqs.md` |
| 6 | `companion change start "refund flow"` | Changeset ID, branch and worktree; `PR - (none)` |
| 7 | `companion new flows/refund.md --title "Refund flow"` | `created ... in changeset ...` |
| 8 | `companion cat flows/refund.md` | Frontmatter and flow template, read through the active changeset overlay |
| 9 | `companion status` | Active changeset marked `*`; `DIRTY 0` |
| 10 | `companion lint --store <worktree>` | `ok: 3 documents, no issues` |
| 11 | `companion change sync` | Files list and `pushed` branch; warning that no GitHub token is available |
| 12 | `companion log flows/refund.md --wip` | `v1 ... "docs(api): refund flow"` |
| 13 | `companion export agents-md` | Managed `<!-- companion:begin -->` block with docs and missing required docs |

The script fails at its self-check step if the worktree, overlay read, pushed
branch or changeset commit is missing.

---

## Manual sequence

Use this instead of the script if you are recording and want to pause between
steps, or if you want to run the commands yourself.

### 1. Set up the demo environment

Run the setup half of the script manually:

```bash
bash docs/demo-cli-only.sh
```

It performs the whole demo. To run the manual sequence, reuse the same paths
that the script prints at the end:

```bash
export DEMO=/tmp/companion-cli-demo
export COMPANION_HOME="$DEMO/companion-home"
export COMPANION_CONFIG="$DEMO/companion-config.yaml"
export COMPANION_BIN=/absolute/path/to/companion
cd "$DEMO/acme-api"
```

### 2. Context and guidance

```bash
"$COMPANION_BIN" context
"$COMPANION_BIN" guidance "implement an SQS consumer in Go" --lang go
"$COMPANION_BIN" validate-code-refs --all
"$COMPANION_BIN" search "dead-letter" --all
```

Expected:

- The project resolves to `acme/api`, team `payments`, area
  `projects/acme/api`.
- Guidance returns `SQS consumer` with a reference to
  `internal/consumer/consumer.go` and `path=valid symbol=valid`.
- Search with `--all` finds the org pattern; without `--all`, search is scoped
  to the project area.

### 3. Author in a changeset

```bash
"$COMPANION_BIN" change start "refund flow"
"$COMPANION_BIN" new flows/refund.md --title "Refund flow"
"$COMPANION_BIN" cat flows/refund.md
"$COMPANION_BIN" status
```

Expected:

- `change start` creates a changeset, branch and worktree.
- `new` creates the flow template in that worktree.
- `cat` prints it through the overlay.
- `status` marks the changeset active with `DIRTY 0`.

For a video, this is a good place to open the file in an editor:

```bash
EDITOR="nano" "$COMPANION_BIN" edit flows/refund.md
```

Add a sentence under `## Trigger`, save, and run `companion status` again to show
the dirty worktree.

### 4. Lint and push

The normal `lint` command reads the main mirror. To lint the changeset worktree:

```bash
WORKTREE="$("$COMPANION_BIN" status --json | jq -r '.changesets[0].dir')"
"$COMPANION_BIN" lint --store "$WORKTREE"
```

Expected: `ok: 3 documents, no issues`.

```bash
"$COMPANION_BIN" change sync
"$COMPANION_BIN" log flows/refund.md --wip
```

Expected:

- `sync` pushes the branch to the local store.
- Without a GitHub token, it prints:
  `warning branch pushed, but no GitHub token is available; draft PR was not created/updated`.
- `main` is unchanged; the changeset branch exists in `$DEMO/docs-store.git`.
- `log --wip` shows the local commit.

### 5. Export

```bash
"$COMPANION_BIN" export agents-md
```

Expected: a managed block beginning with `<!-- companion:begin -->`, listing the
project docs, common tasks and required docs. Add `--output AGENTS.md` to write
it into the code repository.

---

## How to test the script

### Automated

```bash
set -e
bash docs/demo-cli-only.sh
test $? -eq 0
```

A successful run ends with:

```text
OK: worktree, overlay read, pushed branch and changeset commit verified
...
CLI demo complete.
```

### Inspect the result

```bash
export DEMO=/tmp/companion-cli-demo

git --git-dir="$DEMO/docs-store.git" for-each-ref \
  --format='%(refname)' refs/heads/docs

find "$DEMO/companion-home/changes" -name refund.md

"$COMPANION_BIN" status
```

Expected:

- At least one branch under `refs/heads/docs/`.
- A `refund.md` in the active changeset worktree.
- `companion status` shows the changeset with `PR - (none)`.

### Reset

```bash
rm -rf "${DEMO:-/tmp/companion-cli-demo}"
```

The script also prints the exact reset command at the end.

---

## Enabling real PRs later

The script deliberately stops after pushing the branch. To turn the same flow
into a real GitHub draft PR:

```bash
gh auth login
"$COMPANION_BIN" init --remote git@github.com:<org>/docs-store.git
"$COMPANION_BIN" change sync
```

`change sync` creates or updates the draft PR. `change submit` lints, rebases,
marks the PR ready and freezes the changeset. The GitHub setup and agent
connection are covered in the [README](../README.md#connect-an-agent); the CLI
commands are identical.

---

## Notes and limitations

- The script is intentionally offline: the docs store is a local bare repo, so
  companion's GitHub client is disabled and PR creation is skipped.
- The `init` command prints an MCP setup snippet. A CLI-only user can ignore it.
- `companion lint` without `--store` lints the main mirror, not the active
  changeset. The script passes the worktree path explicitly.
- Guidance provider output may be `go` or `ctags` depending on whether
  Universal Ctags is installed.
- Changeset IDs, scores, hashes, usernames and timestamps vary on each run.
- The script uses `jq` only in the optional manual lint step; the script itself
  does not require it.
