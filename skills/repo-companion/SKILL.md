---
name: repo-companion
description: >-
  Find, use, and update an organization's reviewed engineering documentation with
  Repo Companion. Use when the user asks what guidance, patterns, standards, or
  ADRs apply to a coding task; how this project does something; to document a
  flow, module, decision, or pattern; to address review on a docs PR; or when the
  companion CLI, MCP server, or docs store is mentioned. The docs live in one
  central Git store, not in the code repository, and every change becomes a pull
  request a human merges. Requires a configured Repo Companion store; if none
  exists, stop and say so.
license: Apache-2.0
---

# Repo Companion

Repo Companion keeps project docs, patterns, ADRs, and standards in one
Git-backed store. Agents read it through MCP or the `companion` CLI. Agents
write through a **changeset** — a branch, a worktree, and a draft PR. `main` is
read-only and a human merges.

## Before the first call

Use the surface that is actually available:

1. **MCP** — if tools named `docs_get_context` and `docs_get_guidance` are
   available (clients may namespace them, for example
   `mcp__companion__docs_get_context`), use them. They are the primary path:
   they enforce validation and support writes.
2. **CLI** — otherwise run `command -v companion`. Add `--json` when you need to
   parse output.
3. **Neither** — stop and tell the user. Do not invent docs and do not write
   documentation into the code repository.

If MCP is missing but the CLI exists, reads work. Writes are weaker: prefer
asking the user to connect `companion mcp`, and use the CLI fallback below only
when they agree.

Then locate yourself:

- `docs_get_context` / `companion context` resolves the project, team, area, and
  languages, and reports missing required docs and open PRs.
- If the store is not initialized, `companion init --remote …` is a human setup
  decision. Ask; never run it silently.
- If the mirror is more than a day old, `docs_pull` / `companion pull`.

## Choose the lane

| The user wants | Go to |
|---|---|
| “What guidance applies to this task?” | Read: guidance |
| “What do we have on X?” / “Where is the doc for X?” | Read: context, list, search, read |
| “Document this flow / module” / “Update the docs” | Write: changeset |
| “Record this decision” / “Make this a pattern” | Write: decision or pattern |
| “Address the review on the docs PR” | Write: review loop |

Read even when the user asks for a change. Writing without reading the
surrounding doc is how a store rots.

## Read lane

1. **Guidance for a task** — `docs_get_guidance` with the task and optional
   language; CLI: `companion guidance "<task>" --lang <lang>`. Read the top
   results, follow their paths, and cite the canonical path and status. Do not
   paste whole documents.
2. **Orientation** — `docs_get_context`, then `docs_list`
   (`scope: project|team|org|all`) or `docs_search` for a topic.
3. **Full content** — `docs_read`; CLI: `companion cat <path>`. Keep the
   returned `base_rev` if an edit may follow.
4. **Why or when** — `docs_history` / `docs_diff` for versions and diffs.

Precedence is project → team → org. Say so when two docs disagree.

## Write lane

Prefer MCP. The sequence is:

1. `docs_get_context` — check required docs, open PRs, and active changesets.
   If an open shared PR covers this work, ask before `docs_change_join`.
2. `docs_change_start` with a short title, or let the first write auto-create
   one. One area per changeset.
3. Read the current file with `docs_read` and keep its `base_rev`.
4. `docs_write` for a new document; `docs_patch` for an existing one. Prefer
   targeted edits over whole-file replacement. Both require a canonical path
   and valid frontmatter; existing files require `base_rev`.
5. `docs_change_sync` — commit, push, create or update the **draft** PR.
6. `docs_change_status` — confirm PR, CI, and review state.
7. `docs_change_submit` — lint, validate code references, rebase, mark ready,
   freeze. If preflight fails, fix the docs. Do not use the human-only
   `--skip-lint` / `--skip-code-refs` unless the user explicitly asks.
8. Stop. A human merges.

`org/` and `teams/` are special: `docs_write` rejects them. Use
`docs_record_decision` for an ADR and `docs_propose_pattern` for a pattern.

After review: `docs_review_comments` → `docs_change_reopen` if the changeset is
frozen → `docs_patch` → `docs_change_submit`.

### If only the CLI is available

Reads work normally. For a write: `companion change start "<topic>"`, create the
file with `companion new <path> --template <kind>`, edit it in the worktree
shown by `companion status --json` (`changesets[].dir`), then
`companion change sync`. Sync still scans for secrets before committing and
`change submit` lints — but you lose per-write `base_rev` checks, so re-read the
file immediately before editing. If you cannot edit the worktree safely, hand
the edit to the user.

## Hard rules

- Never edit `~/.companion/main`, and never add docs to the code repository.
- Never merge, force-push, or rewrite a ready PR. There is no merge tool, and
  `change sync` refuses a frozen (ready) changeset.
- On `base_rev_required_or_stale`, re-read the document and redo the edit.
  Never fall back to a blind overwrite.
- Keep each changeset scoped to one area and one topic; do not fold unrelated
  docs into someone else’s PR.
- Do not put secrets, credentials, or private repository content in a doc. The
  secret scan is a heuristic, not permission.
- Do not invent paths. Use the templates and the layout in
  `references/document-model.md`.
- If the user did not ask for a docs change, do not create a changeset. Offer.

## Verify before you claim done

- `docs_change_status` / `companion status` — no dirty files, correct PR state.
- `companion lint` on the changed docs (submit runs it already).
- `docs_validate_code_refs` / `companion validate-code-refs` when references
  changed.
- Report the doc paths and the PR number. Never claim a merge.

## References

- `references/workflows.md` — MCP/CLI call pairs and the changeset state machine.
- `references/document-model.md` — areas, canonical paths, frontmatter, templates.
