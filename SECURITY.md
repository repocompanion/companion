# Security policy

Repo Companion runs on a developer machine, reads project documentation, talks
to GitHub, and can write documents through pull requests. It handles GitHub
tokens and may read private repositories, so security reports are welcome and
will be taken seriously.

## Supported versions

| Version | Security fixes |
|---|---|
| `0.1.x` | Yes |
| `< 0.1` | No |

This project is pre-1.0. Fixes are made on `main` and released as a new patch
or minor version. There is no long-term-support branch yet.

## Reporting a vulnerability

**Do not open a public issue for a security problem.**

Use GitHub's private vulnerability reporting:

<https://github.com/repocompanion/companion/security/advisories/new>

Please include:

- the companion version (`companion version`) and operating system;
- the command or MCP tool involved;
- a minimal reproduction, including whether a GitHub token, a docs store, or a
  malicious document is required;
- the impact you believe is possible;
- any suggested fix or mitigation.

We aim to acknowledge reports within three business days and to keep you
updated while a fix is developed. This is a best-effort, maintainer-run project;
there is no bug bounty or contractual response time.

## In scope

- path traversal, symlink escape, or writes outside the companion worktree;
- GitHub token or credential disclosure in output, logs, URLs, or errors;
- an MCP tool performing an action reserved for a human (merge, abandon,
  cleanup, or changing security-sensitive configuration);
- command injection or unsafe execution of repository-controlled content;
- a remote or config value that redirects requests or credentials to an
  unintended host;
- bypasses of the documented write, `base_rev`, PR-state, or secret-scan
  guardrails;
- release artifact, checksum, or build-pipeline compromise.

## Out of scope

- problems that require an already compromised local account or a malicious
  editor, `git`, `gh`, `ctags`, or Go toolchain binary;
- GitHub platform vulnerabilities, or repository settings such as branch
  protection;
- the content of documentation itself, including prompt-injection text that
  only results in a normal reviewable documentation PR;
- the secret scanner missing a credential format. The scanner is a heuristic,
  not a guarantee. A missing pattern is useful as a hardening report, but it is
  not treated as a vulnerability by itself;
- denial of service caused by a deliberately enormous local docs repository,
  unless it crosses a documented trust boundary.

If you are unsure, report privately and let the maintainers triage it.

## Security model

- **Local-first.** Companion is a CLI and a stdio MCP server. It does not open a
  network port, run a daemon, or send telemetry.
- **Read-only main.** The local mirror of `main` is made read-only; edits happen
  in changeset worktrees and become branches and pull requests.
- **Agents cannot merge.** MCP tools can propose and update documentation
  changes, but there is no merge, abandon, or cleanup tool. A human merges
  under GitHub branch protection.
- **Tokens are local.** A token is read from `COMPANION_FORGE_TOKEN`, the user
  config, or `gh auth token`. User config and state are written with restrictive
  permissions. Tokens are not passed on a command line.
- **Credentials are not echoed.** Remote URLs are redacted in error output and
  diagnostics.
- **Docs content is untrusted input.** Agents must treat document text, PR
  titles, and review comments as data, not instructions. Security-sensitive
  configuration cannot be changed through MCP.
- **The secret scan is best effort.** It covers common token/key formats and is
  run before commits. It can miss formats and can produce false positives.

## Safe harbor

If you make a good-faith effort to follow this policy, do not access data that
is not yours, do not degrade other users' service, and give us reasonable time
to fix the issue before disclosure, we will not pursue legal action or report
you to a platform for the research itself. Coordinate public disclosure with
the maintainers.
