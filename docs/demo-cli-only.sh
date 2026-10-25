#!/usr/bin/env bash
# Guided, offline companion CLI demo.
#
# Usage:
#   bash docs/demo-cli-only.sh
#   DEMO=/tmp/my-demo bash docs/demo-cli-only.sh
#   COMPANION_BIN=/path/to/companion bash docs/demo-cli-only.sh
#
# The script creates a throwaway docs store and Go code repo under $DEMO,
# builds companion from this checkout, and walks through the CLI without MCP or
# GitHub. It leaves the demo in place for inspection; remove $DEMO to reset.

set -euo pipefail

if [[ -t 1 ]]; then
  BOLD=$'\033[1m'
  DIM=$'\033[2m'
  GREEN=$'\033[32m'
  YELLOW=$'\033[33m'
  RESET=$'\033[0m'
else
  BOLD="" DIM="" GREEN="" YELLOW="" RESET=""
fi

section() { printf '\n%s== %s ==%s\n' "$BOLD" "$*" "$RESET"; }
note()    { printf '%s# %s%s\n' "$DIM" "$*" "$RESET"; }
say()     { printf '\n%s%s%s\n' "$YELLOW" "$*" "$RESET"; }
ok()      { printf '%sOK:%s %s\n' "$GREEN" "$RESET" "$*"; }

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
demo="${DEMO:-/tmp/companion-cli-demo}"
binary="${COMPANION_BIN:-$repo_root/companion}"

case "$demo" in
  ""|"/") echo "refusing unsafe DEMO path: $demo" >&2; exit 1 ;;
esac
if [[ "$demo" == "$repo_root" || "$demo" == "$repo_root"/* ]]; then
  echo "refusing to use a demo path inside the source checkout: $demo" >&2
  exit 1
fi
if [[ "$binary" != /* ]]; then
  binary="$repo_root/$binary"
fi

if [[ ! -x "$binary" ]]; then
  say "Building companion from $repo_root"
  (cd "$repo_root" && go build -trimpath -o "$binary" ./cmd/companion)
fi

rm -rf "$demo"
mkdir -p "$demo"
export DEMO="$demo"
export COMPANION_BIN="$binary"
export COMPANION_HOME="$demo/companion-home"
export COMPANION_CONFIG="$demo/companion-config.yaml"
export GIT_AUTHOR_NAME="Companion Demo"
export GIT_AUTHOR_EMAIL="demo@example.com"
export GIT_COMMITTER_NAME="Companion Demo"
export GIT_COMMITTER_EMAIL="demo@example.com"

today="$(date +%F)"

section "1. Create a throwaway docs store"
note "A bare Git repo acts as the central docs store; no GitHub is needed."

mkdir -p "$demo/seed-store/org/patterns/messaging"
mkdir -p "$demo/seed-store/projects/acme/api"

cat > "$demo/seed-store/org/patterns/messaging/sqs.md" <<EOF
---
id: sqs-consumer
kind: pattern
title: SQS consumer
status: approved
topics: [sqs, queue, consumer, messaging]
languages: [go]
owners: [payments]
last_reviewed: $today
reference_impls:
  - repo: acme/api
    path: internal/consumer/consumer.go
    symbol: Consume
    kind: function
    ref: main
---
# SQS consumer

Use a dead-letter queue, idempotency keys and bounded concurrency.

## When to use

Use this pattern when a Go service consumes an SQS queue.

## Rules

- MUST configure a dead-letter queue.
- MUST make the handler idempotent.
- SHOULD bound worker concurrency.

## Reference implementations

| Repository | Path | Why |
|---|---|---|
| acme/api | \`internal/consumer/consumer.go\` | existing consumer in this repo |
EOF

cat > "$demo/seed-store/projects/acme/api/README.md" <<EOF
---
id: readme
kind: project-doc
title: Acme API
status: approved
owners: [payments]
last_reviewed: $today
---
# Acme API

API service that handles refunds and payment events.
EOF

git -C "$demo/seed-store" init -q -b main
git -C "$demo/seed-store" add .
git -C "$demo/seed-store" commit -qm "seed docs"
git init -q --bare "$demo/docs-store.git"
git -C "$demo/seed-store" remote add origin "$demo/docs-store.git"
git -C "$demo/seed-store" push -q origin main
ok "seed docs committed and pushed to $demo/docs-store.git"

section "2. Create a demo code repository"
note "Project and team context come from .companion.yaml; the Go file is used by guidance."

mkdir -p "$demo/acme-api/internal/consumer"
cat > "$demo/acme-api/.companion.yaml" <<'EOF'
project: acme/api
team: payments
languages: [go]
EOF
cat > "$demo/acme-api/go.mod" <<'EOF'
module example.com/acme/api

go 1.25.0
EOF
cat > "$demo/acme-api/internal/consumer/consumer.go" <<'EOF'
package consumer

// Consume handles one SQS message.
func Consume(body string) error { return nil }
EOF

git -C "$demo/acme-api" init -q -b main
git -C "$demo/acme-api" add .
git -C "$demo/acme-api" commit -qm "seed api"
ok "code repo at $demo/acme-api"

section "3. Initialize companion"
say "companion init clones the central store and creates a read-only mirror of main."
printf '$ companion init --remote %s\n' "$demo/docs-store.git"
(cd "$demo/acme-api" && "$binary" init --remote "$demo/docs-store.git")
note "Expected: store/mirror/config paths, 'ssh: ok', and a GitHub-token warning."
note "The warning is expected offline; branch pushes still work, PR creation is skipped."
note "The init output also prints an MCP snippet; CLI-only users can ignore it."

section "4. Inspect context"
say "An agent or human asks where docs live instead of guessing."
printf '$ companion context\n'
(cd "$demo/acme-api" && "$binary" context)
note "Expected: project acme/api, team payments, languages go, area projects/acme/api."

section "5. Ask for guidance before coding"
say "Guidance returns ranked patterns with validated reference implementations."
printf '$ companion guidance "implement an SQS consumer in Go" --lang go\n'
(cd "$demo/acme-api" && "$binary" guidance "implement an SQS consumer in Go" --lang go)
note "Expected: SQS consumer, a ref: line, and a code: line with valid path/symbol."
note "The provider is go, or ctags when Universal Ctags is installed."

section "6. Validate code references"
printf '$ companion validate-code-refs --all\n'
(cd "$demo/acme-api" && "$binary" validate-code-refs --all)
note "Expected: 1 valid path, 0 missing, 0 stale."

section "7. Search project and org docs"
printf '$ companion search "dead-letter" --all\n'
(cd "$demo/acme-api" && "$binary" search "dead-letter" --all)
note "Expected: a hit in org/patterns/messaging/sqs.md."

section "8. Start a changeset"
say "Docs are never edited on main. A changeset gets a branch and a worktree."
printf '$ companion change start "refund flow"\n'
(cd "$demo/acme-api" && "$binary" change start "refund flow")
note "Expected: changeset ID, docs/<user>/projects-acme-api/refund-flow branch, worktree path, PR - (none)."

section "9. Author a document"
printf '$ companion new flows/refund.md --title "Refund flow"\n'
(cd "$demo/acme-api" && "$binary" new flows/refund.md --title "Refund flow")
printf '$ companion cat flows/refund.md\n'
(cd "$demo/acme-api" && "$binary" cat flows/refund.md | sed -n '1,24p')
note "Expected: frontmatter plus the flow template, read through the active overlay."

section "10. Check status and lint the changeset"
printf '$ companion status\n'
(cd "$demo/acme-api" && "$binary" status)
printf '$ companion lint --store "$COMPANION_HOME/changes/projects-acme-api--refund-flow"\n'
worktree="$COMPANION_HOME/changes/projects-acme-api--refund-flow"
(cd "$demo/acme-api" && "$binary" lint --store "$worktree")
note "Expected: active changeset marked *, DIRTY 0, and 'ok: 3 documents, no issues'."

section "11. Push the changeset branch (offline)"
say "sync commits, integrates the remote branch, pushes, and would open a draft PR."
printf '$ companion change sync\n'
(cd "$demo/acme-api" && "$binary" change sync)
note "Expected: pushed files and a warning that no GitHub token is available."
note "main is untouched; the branch now exists in the local docs store."

section "12. Inspect the local commit"
printf '$ companion log flows/refund.md --wip\n'
(cd "$demo/acme-api" && "$binary" log flows/refund.md --wip)
note "Expected: v1 with subject 'docs(api): refund flow'."

section "13. Export context for non-MCP agents"
printf '$ companion export agents-md\n'
(cd "$demo/acme-api" && "$binary" export agents-md | sed -n '1,18p')
note "Expected: a managed <!-- companion:begin --> block with docs and missing required docs."

section "14. Self-check"
note "The script fails here if the documented core behavior did not happen."
test -f "$worktree/projects/acme/api/flows/refund.md"
(cd "$demo/acme-api" && "$binary" cat flows/refund.md | grep -q '# Refund flow')
git --git-dir="$demo/docs-store.git" for-each-ref --format='%(refname)' refs/heads/docs | grep -q .
(cd "$demo/acme-api" && "$binary" log flows/refund.md --wip | grep -q 'docs(api): refund flow')
ok "worktree, overlay read, pushed branch and changeset commit verified"

section "15. Where is everything?"
printf '\n  code repo:   %s\n' "$demo/acme-api"
printf '  docs store:  %s\n' "$demo/docs-store.git"
printf '  companion home: %s\n' "$COMPANION_HOME"
printf '  config:      %s\n' "$COMPANION_CONFIG"
printf '\n  Inspect the pushed branch:\n'
printf '    git --git-dir=%q for-each-ref --format="%%(refname)" refs/heads/docs\n' "$demo/docs-store.git"
printf '\n  Reset:\n    rm -rf %q\n' "$demo"
printf '\n%sCLI demo complete.%s\n' "$GREEN" "$RESET"
