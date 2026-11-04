# Workflows

Exact call pairs. Prefer MCP when the tools exist; the CLI column is the
fallback and the human path.

## Orientation

| Goal | MCP | CLI |
|---|---|---|
| Resolve project, team, area | `docs_get_context` | `companion context --json` |
| List docs | `docs_list` (`scope`, `cursor`, `limit`) | `companion ls [DIR] [--pending]` |
| Search | `docs_search` (`query`, `scope`, `limit`) | `companion search "<query>" [--all]` |
| Read one doc | `docs_read` (`path`, `rev`, `pr`) | `companion cat <path> [--rev R] [--pr N]` |
| Guidance for a task | `docs_get_guidance` (`task`, `language`, `max_results`) | `companion guidance "<task>" --lang <lang>` |
| History | `docs_history` | `companion log <doc>` |
| Diff | `docs_diff` | `companion diff <doc>` |
| Refresh mirror | `docs_pull` | `companion pull` |
| Validate code refs | `docs_validate_code_refs` | `companion validate-code-refs --all` |
| Stale refs | `docs_stale` | `companion stale --all` |

## Changeset lifecycle

```text
(none) --change start--> draft --sync--> draft PR --submit--> ready (frozen)
                                   ^                              |
                                   +--------- reopen -------------+
ready --human merge--> merged --cleanup--> (none)
draft --abandon--> closed
```

| Step | MCP | CLI |
|---|---|---|
| Start | `docs_change_start` (`title`, `area`) | `companion change start "<topic>" [--area …]` |
| Join a shared PR | `docs_change_join` (`pr`) | `companion change join <pr>` |
| Create a doc | `docs_write` (`path`, `content`, `base_rev`) | `companion new <path> --template <kind>` |
| Edit a doc | `docs_patch` (`path`, `edits` or `unified_diff`, `base_rev`) | `companion edit <path>` (opens `$EDITOR`) |
| Move/rename | `docs_move` (`path`, `new_path`, `base_rev`) | `companion move <path> <new-path>` |
| Delete | `docs_delete` (`path`, `base_rev`) | — |
| Record an ADR | `docs_record_decision` | `companion adr new "<title>"` |
| Propose a pattern | `docs_propose_pattern` | `companion new <area>/patterns/<slug>.md --template pattern` |
| Sync / draft PR | `docs_change_sync` | `companion change sync` |
| Status | `docs_change_status` | `companion status` |
| Submit | `docs_change_submit` | `companion change submit -m "<summary>"` |
| Reopen after review | `docs_change_reopen` | `companion change reopen` |
| Review comments | `docs_review_comments` (`pr`) | `companion change …` + GitHub |

## Write specifics

- `base_rev` is the blob hash returned by `docs_read`. It is required for
  existing files, `docs_move`, and `docs_delete`.
- A stale write returns `base_rev_required_or_stale` with the latest content
  and hash. Re-read, re-apply, retry.
- `docs_write` and `docs_patch` reject `org/` and `teams/` paths. Those areas
  go through `docs_record_decision` / `docs_propose_pattern`.
- `docs_change_sync` only creates a **draft** PR. `docs_change_submit` marks it
  ready and freezes it.
- There is no merge, abandon, or cleanup MCP tool. Do not look for one.

## Failure routing

| Symptom | Do |
|---|---|
| “store is not initialized” | Stop; ask the user to run `companion init --remote <url>` |
| “GitHub token unavailable” | Reads still work; writes can be local, but PR steps need auth. Report it. |
| Mirror older than 24h | `docs_pull` |
| Open PR exists for the area | Ask before joining; never silently join. |
| `lint_failed` on submit | Fix the docs, re-sync, submit again. |
| `code_refs_failed` on submit | Fix the reference or the doc; do not skip unless asked. |
| Branch behind `main` | CLI: `companion change rebase`, then continue. There is no MCP rebase tool. |
| Ready PR refuses sync | `docs_change_reopen`, then patch and submit. |
