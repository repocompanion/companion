# Agent instructions

This is the public Repo Companion repository. Read
[CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md) before making a
change.

## Workflow

- Use Go 1.25+ and Git 2.30+.
- Run `make fmt`, `make vet`, and `go test ./...` before proposing a change.
- Keep changes small and focused. Add a regression test for every bug fix and
  tests for new behavior.
- Use conventional commit subjects (`fix(store): ...`, `feat(mcp): ...`).
- A human contributor should sign off with `git commit -s`; if you are an agent
  preparing a patch for review, say so in the pull request instead of inventing
  an identity.

## Documentation

- Public documentation lives in Markdown: `README.md` and `docs/`.
- This repository uses normal Markdown pull requests for its own docs. It does
  not require the companion docs MCP tools to edit them.
- Do not add internal planning notes, roadmaps that are not meant for users, or
  scratch files. Local planning material is kept in the ignored `.internal/`
  directory.
- Update the README, `docs/`, or command help when behavior or setup changes.

## Safety

- Never add tokens, credentials, private repository content, or personal data.
- The local secret scanner is a heuristic, not permission to commit secrets.
- Do not weaken path containment, `base_rev` checks, PR freezing, branch
  protection assumptions, or agent permission boundaries.
- Report vulnerabilities privately using [SECURITY.md](SECURITY.md). Do not put
  exploit details in a public issue or pull request.
