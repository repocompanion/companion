# How Repo Companion compares

Repo Companion is a focused tool, not a replacement for every documentation
platform. The useful question is where your docs need to live for coding agents
to find them.

| Approach | Strength | Where it falls short for agents | Repo Companion's position |
|---|---|---|---|
| **`AGENTS.md` / `CLAUDE.md` in every repo** | Zero setup; lives with the code | Duplicated across repos; organization patterns and decisions drift; no review path across projects | Keeps repo-local instructions, but provides a central store and a managed `AGENTS.md` export |
| **Backstage TechDocs** | Strong catalog, ownership, and portal UX | Requires a portal and build pipeline; docs usually stay per-component; agent access depends on extra integration | Git-native, no portal, docs are pull requests, MCP/CLI reads are local |
| **Confluence / Notion** | Familiar authoring and permissions | Content is outside the code review flow; agents need a separate integration; links and ownership drift | Docs are Markdown in one Git repo with normal PR review and CODEOWNERS |
| **adr-tools / per-repo Markdown** | Excellent ADR discipline | Decisions are scattered by repository; no cross-project guidance or search | Central `adr/`, `patterns/`, `standards/`, and project areas with one search index |
| **Plain docs-as-code monorepo** | Simple and transparent | Agents still need to know where to look; no changeset/PR flow or guidance ranking | Adds changesets, guidance ranking, confirmations, and an MCP server on top of docs-as-code |

## When Repo Companion is a good fit

- You have several services and want one reviewed home for patterns,
  decisions, and runbooks.
- You use coding agents and want them to ask for guidance before writing code.
- You already review changes through GitHub pull requests and want docs to use
  the same path.
- You are comfortable with Markdown, Git, and a small local CLI.

## When it is not a good fit

- You need a rich web editor, databases, or live co-editing.
- You cannot use GitHub for the documentation workflow.
- You want a fully managed cloud service; companion is local-first by design.
- You need semantic/vector search today; current search is local BM25.
- You expect an agent to merge its own documentation changes.

## Migration

The `companion import` command scans a Markdown tree, proposes canonical
destinations, reports ambiguous files, and applies the resolved set into one
changeset. It copies files rather than preserving Git history, and it is a
human CLI action. Start with a dry run:

```bash
companion import --from ./docs --project acme/api --report migration.json
```

See the [README](../README.md#importing-existing-documentation) for the full
workflow.
