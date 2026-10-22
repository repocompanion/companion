# Setting up the central docs store

`docsync` expects one repository, for example `your-org/docs-store`, with
`main` protected. Project docs are not stored in service repositories.

## 1. Create the repository

Create an empty **private** GitHub repository. Give every engineer read access
and use CODEOWNERS plus branch protection for write control. The docsync Git
transport is SSH; API calls use a token from `gh auth token` or
`DOCSYNC_FORGE_TOKEN`.

Recommended layout:

```text
docs-store/
├── .github/
│   ├── CODEOWNERS
│   └── workflows/docs-ci.yml
├── .docsync/
│   ├── registry.yaml
│   └── templates/            # optional team templates
├── org/
│   ├── adr/
│   ├── patterns/
│   └── standards/
├── teams/
│   └── payments/
└── projects/
    └── your-org/
        └── payments-api/
            ├── README.md
            ├── architecture.md
            ├── adr/
            ├── flows/
            └── runbooks/
```

## 2. Branch protection

On `main`:

- require a pull request before merging;
- require at least one approval;
- require the docs CI check;
- disallow direct pushes, including administrators;
- allow **squash merge only**;
- optionally enable auto-merge for `projects/**` after approval and green CI.

The docsync CLI never merges. `docsync change submit` only marks a PR ready and
freezes the changeset; a human or GitHub auto-merge performs the merge.

## 3. CODEOWNERS

Route review by area. Adjust the team names to your organization:

```text
/org/                            @your-org/architecture
/org/patterns/observability/     @your-org/sre
/teams/payments/                 @your-org/payments-leads
/projects/your-org/payments-api/ @your-org/payments

/.github/                        @your-org/architecture
/.docsync/                       @your-org/architecture
```

`docsync context` reads this file for the reviewer display. GitHub itself
enforces approvals.

MCP writes enforce the canonical layout and frontmatter. If an agent tries to
create `decisions.md`, `operations.md` or a doc without frontmatter, docsync
returns an actionable error and `docs_get_context.structure` shows the expected
paths and required fields.

## 4. Registry

`.docsync/registry.yaml` maps repositories, teams and aliases. It is changed
through a docs PR, for example:

```bash
docsync registry add-repo your-org/payments-api --team payments
```

Example:

```yaml
teams:
  payments:
    name: Payments
    github_team: your-org/payments
    domain: checkout
  platform:
    github_team: your-org/platform
repos:
  your-org/payments-api: { team: payments, domain: checkout }
  your-org/orders-service: { team: orders }
  your-org/checkout-*: { team: checkout }
aliases:
  your-org/payment-api: your-org/payments-api
defaults:
  languages_hint: [go, php, node]
```

## 5. CI

Use `docsync lint --store .` in the store checkout. It checks frontmatter,
duplicate IDs within a scope, broken internal links, `overrides` targets and
likely secrets. Add markdownlint and gitleaks for broader coverage.

```yaml
name: docs-ci
on:
  pull_request:
permissions:
  contents: read
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25.x'
      - run: go install github.com/bolacha/docsync/cmd/docsync@latest
      - run: docsync lint --store .
      - uses: DavidAnson/markdownlint-cli2-action@v19
      - uses: gitleaks/gitleaks-action@v2
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

The plan also calls for validating reference implementations against GitHub.
That requires a token and network access; it is not part of the local lint
subset yet.

## 6. Seed content

Start with a small set of high-value org patterns:

- SQS/queue consumer with retries and DLQ;
- database migration;
- logging, metrics and tracing;
- API error handling.

For each pattern include a skeleton, MUST/SHOULD rules, pitfalls and at least
one reference implementation. Point at `ref: main` rather than a feature branch
SHA.

Then add project docs through changesets:

```bash
docsync init --remote git@github.com:your-org/docs-store.git
docsync --project your-org/payments-api change start "initial project docs"
docsync --project your-org/payments-api new README.md --template readme
docsync --project your-org/payments-api new architecture.md --template architecture
docsync --project your-org/payments-api change submit -m "initial project docs"
```
