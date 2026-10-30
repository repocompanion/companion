# Document model

## Areas

```text
docs-store/
├── org/                          organization-wide guidance
│   ├── adr/
│   ├── patterns/
│   └── standards/
├── teams/<team>/                 team guidance
└── projects/<owner>/<repo>/      one project
    ├── README.md
    ├── architecture.md
    ├── adr/
    ├── flows/
    ├── runbooks/
    ├── patterns/
    └── standards/
```

Resolution is project → team → org. A project doc with `overrides:` replaces
the named document rather than competing with it.

## Canonical project layout

| Path | Kind | Purpose |
|---|---|---|
| `README.md` | `project-doc` | project overview and documentation map |
| `architecture.md` | `project-doc` | components, data flow, dependencies, trade-offs |
| `adr/YYYY-MM-DD-<slug>.md` | `adr` | one file per decision |
| `flows/<slug>.md` | `flow` | one file per runtime/business flow |
| `runbooks/<slug>.md` | `runbook` | one file per operational procedure |
| `patterns/<slug>.md` | `pattern` | reusable implementation patterns |
| `standards/<slug>.md` | `standard` | rules and conventions |

## Required frontmatter

Every `.md` document needs YAML frontmatter with:

`id`, `kind`, `title`, `status`, `owners`, `last_reviewed`

```yaml
---
id: refund-flow
kind: flow
title: Refund flow
status: approved
topics: [refunds, payments]
languages: [go]
related: [payment-capture-flow]
reference_impls:
  - repo: acme/api
    path: internal/refunds/refund.go
    symbol: Refund
    kind: function
owners: [payments]
last_reviewed: 2026-08-31
---
```

Optional keys: `superseded_by`, `overrides`, `topics`, `languages`, `related`,
`reference_impls`.

- `kind`: `project-doc` | `adr` | `flow` | `runbook` | `pattern` | `standard`
- `status`: `proposed` | `approved` | `deprecated` | `superseded`
- `related`: document IDs, not paths.
- `reference_impls[]`: `repo`, `path`, optional `symbol`, `kind`, `ref`,
  `commit`, `fingerprint`. Code references are validated against the current
  checkout.

## Templates

`companion new <path> --template <kind>` supports
`adr | pattern | flow | runbook | standard | readme | architecture`.
`companion adr new "<title>"` creates the dated ADR filename for you.

## Rules the validator enforces

- Every Markdown document has frontmatter; no flat aggregate files.
- ADRs are one dated file per decision under `adr/`.
- Flows, runbooks, patterns, and standards live in their own directories.
- Project roots contain only `README.md` and `architecture.md`.
- No absolute paths, traversal, `.git`, or symlink escapes.
- Doc IDs are unique within a scope; `lint` also checks links and overrides.

If the validator rejects a path, read `docs_get_context.structure` (or
`companion context --json`) — it returns the expected layout, required
frontmatter, and rules. Do not work around a rejected path by inventing another
one.
