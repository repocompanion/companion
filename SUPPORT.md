# Support

Repo Companion is maintained by a small team. Support is best effort and there
is no guaranteed response time or commercial support offering.

## Where to ask

- **Questions, setup help, and design discussion:** use
  [GitHub Discussions](https://github.com/repocompanion/companion/discussions).
- **Reproducible bugs and feature requests:** open an issue with the matching
  template.
- **Security vulnerabilities or conduct concerns:** report privately using
  [SECURITY.md](SECURITY.md). Do not open a public issue.

## Before opening an issue

1. Read the [README](README.md), [store setup](docs/STORE.md), and
   [FAQ](docs/FAQ.md).
2. Run:

   ```bash
   companion version
   companion doctor --json
   ```

3. Search existing issues and discussions.
4. Reduce the problem to the smallest command or MCP call that reproduces it.
5. Remove tokens, private URLs, document contents, and hostnames from logs
   before posting.

The bug template asks for the version, operating system, install method,
`doctor` output, reproduction steps, and expected/actual behavior.

## What to expect

- Maintainers triage when time allows.
- A clear reproduction and a failing test are the fastest path to a fix.
- Feature requests may be declined or deferred to match the
  [roadmap](ROADMAP.md) and pre-1.0 scope.
- A GitHub issue is not a support contract. Please do not include secrets.
