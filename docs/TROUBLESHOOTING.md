# Troubleshooting

Start with:

```bash
companion version
companion doctor --json
```

`doctor` is read-only unless you pass `--repair`. Use `--strict` in setup
scripts when a failed check should return a non-zero exit code. Optional
`ctags` is not counted as a strict failure.

## `companion store is not initialized`

Run the setup once:

```bash
companion init --remote git@github.com:<org>/docs-store.git
```

If the repository is empty, `init` creates the first `main` commit containing
`.companion/registry.yaml` and pushes it. If the remote has a different default
branch, create `main` first or point at a store that uses `main`.

## `GitHub token unavailable`

Read-only docs work without a token. PR creation, status, review comments, and
`change submit` need one.

```bash
gh auth login
# or
export COMPANION_FORGE_TOKEN="<token>"
companion doctor
```

Prefer the environment or `gh` over writing a token into config. The token needs
repository access to the docs store; use the narrowest permissions your
organization allows.

## `SSH check failed`

The companion store uses your normal Git remote. Check that the URL is correct
and that SSH works outside companion:

```bash
ssh -T git@github.com
git ls-remote <store-remote> main
```

If you use HTTPS with a credential helper, that is supported for Git; the forge
token is separate from Git authentication.

## `store has no "main" branch`

The remote exists but its default branch is not `main`, or it has no commits.
For an empty repository, `companion init` now seeds it. For `master`/other
branches, create `main` in the store first; the CLI intentionally uses `main`.

## `multiple changesets are active`

Multiple worktrees are open and companion will not guess which one to use. Pass
a changeset ID:

```bash
companion change list
companion change sync cs-abc123
```

## `pending` / exit code 2

A pending result means local commits were kept but a remote step failed: fetch,
push, PR refresh, or PR creation. Nothing was lost. Fix the network/auth issue
and run the same command again. With `--json`, the result remains valid JSON so
scripts can inspect it.

## A changeset is frozen

`change submit` marks the PR ready and freezes the local changeset. To add more
commits after review:

```bash
companion change reopen
# edit and sync
companion change submit
```

## The main mirror is dirty

Direct edits to `~/.companion/main` are not part of the workflow. Move them into
a changeset:

```bash
companion change adopt
```

Then inspect and sync the resulting changeset.

## `ctags` is missing or not Universal Ctags

Go code works without ctags. For non-Go symbol validation install Universal
Ctags and check the first line of `ctags --version`; the BSD `ctags` bundled with
macOS does not provide JSON output.

## `state.json` is corrupt or worktrees look wrong

The state file is a cache. Rebuild it from the real worktrees:

```bash
companion doctor --repair
companion status
```

The original file may be preserved as `state.json.corrupt` for diagnosis.

## The docs mirror is stale

```bash
companion pull
```

`pull` fetches `main` and fast-forwards the read-only mirror. Re-run exports and
guidance after pulling.

## An editor does not open

Set `VISUAL` or `EDITOR`:

```bash
EDITOR="code --wait" companion edit flows/refund.md
```

The value is split into a command and arguments; shell operators are not
interpreted. On Windows, the default is `notepad`.

## Windows and symlinks

Symlink tests are skipped on Windows runners that cannot create symlinks. The
production code rejects symlinked document paths on every platform. If a store
contains a tracked symlink, remove it or open the file outside companion; the
tool will refuse to edit through it.

## Reporting a problem

Include the `companion version`, OS, install method, sanitized `doctor --json`,
and a minimal reproduction. Remove tokens, private URLs, hostnames, and document
content. Security issues go through [SECURITY.md](../SECURITY.md).
