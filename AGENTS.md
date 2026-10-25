## Documentation

Before implementing infrastructure, integration or cross-cutting code (queues,
databases, migrations, observability, APIs, auth), call the
`docs_get_guidance` MCP tool. Before writing or refining documentation, call
`docs_get_context`. If the mirror is stale, call `docs_pull`.

Never write docs into the code repo. All doc changes go through these tools
and become PRs.

