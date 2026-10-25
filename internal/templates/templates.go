// Package templates contains the human-facing document skeletons used by
// `companion new`, `companion adr new` and the MCP scaffolding tools.
package templates

import (
	"bytes"
	"fmt"
	"text/template"
)

type Data struct {
	Title  string
	ID     string
	Date   string
	Status string
	Owner  string
}

var files = map[string]string{
	"adr": `---
id: {{.ID}}
kind: adr
title: {{.Title}}
status: {{.Status}}
superseded_by: null
overrides: null
topics: []
languages: []
related: []
owners: [{{.Owner}}]
last_reviewed: {{.Date}}
---

# {{.Title}}

## Context

What is the situation and why does a decision need to be made?

## Decision

What did we decide?

## Consequences

What becomes easier, what becomes harder, and what do we need to revisit?

## Alternatives considered

What else did we consider, and why was it rejected?
`,
	"pattern": `---
id: {{.ID}}
kind: pattern
title: {{.Title}}
status: {{.Status}}
superseded_by: null
overrides: null
topics: []
languages: []
related: []
reference_impls: []
owners: [{{.Owner}}]
last_reviewed: {{.Date}}
---

# {{.Title}}

## When to use

Describe the recurring problem and the situations where this pattern applies.

## Rules

- **MUST** …
- **SHOULD** …
- **MUST NOT** …

## Skeleton

~~~text
Add the smallest useful implementation skeleton here.
~~~

## Reference implementations

| Repository | Path | Why it is a good example |
|---|---|---|
|  |  |  |

## Observability checklist

- [ ] Logs
- [ ] Metrics
- [ ] Traces
- [ ] Alerts

## Pitfalls

- …
`,
	"flow": `---
id: {{.ID}}
kind: flow
title: {{.Title}}
status: approved
superseded_by: null
overrides: null
topics: []
languages: []
related: []
owners: [{{.Owner}}]
last_reviewed: {{.Date}}
---

# {{.Title}}

## Trigger

What starts this flow?

## Steps

1. …
2. …
3. …

## Failure modes

| Failure | Detection | Recovery |
|---|---|---|
|  |  |  |

## References

- …
`,
	"runbook": `---
id: {{.ID}}
kind: runbook
title: {{.Title}}
status: approved
superseded_by: null
overrides: null
topics: []
languages: []
related: []
owners: [{{.Owner}}]
last_reviewed: {{.Date}}
---

# {{.Title}}

## Symptoms

What does the operator see?

## Diagnosis

1. …
2. …

## Mitigation

1. …
2. …

## Escalation

Who owns this, and when should they be paged?
`,
	"standard": `---
id: {{.ID}}
kind: standard
title: {{.Title}}
status: approved
superseded_by: null
overrides: null
topics: []
languages: []
related: []
owners: [{{.Owner}}]
last_reviewed: {{.Date}}
---

# {{.Title}}

## Rule

State the rule precisely.

## Rationale

Why does this rule exist?

## Exceptions

Who can approve an exception, and how is it recorded?
`,
	"readme": `---
id: {{.ID}}
kind: project-doc
title: {{.Title}}
status: approved
superseded_by: null
overrides: null
topics: []
languages: []
related: []
owners: [{{.Owner}}]
last_reviewed: {{.Date}}
---

# {{.Title}}

## What it is

One paragraph describing the service and its responsibility.

## Architecture

~~~mermaid
flowchart LR
  Client --> API
  API --> Database
~~~

## Local development

How to run it, test it and debug it.

## Operations

Dashboards, alerts, runbooks and on-call ownership.

## Documentation map

- architecture.md — components, data flow and trade-offs
- adr/ — one dated file per decision
- flows/ — one file per runtime or business flow
- runbooks/ — one file per operational procedure

Do not collapse multiple decisions, flows or runbooks into one aggregate file.
`,
	"architecture": `---
id: {{.ID}}
kind: project-doc
title: {{.Title}}
status: approved
superseded_by: null
overrides: null
topics: []
languages: []
related: []
owners: [{{.Owner}}]
last_reviewed: {{.Date}}
---

# {{.Title}}

## Context

What does this system do, and what is in scope?

## Components

| Component | Responsibility | Owner |
|---|---|---|
|  |  |  |

## Data flow

~~~mermaid
flowchart LR
  A --> B
~~~

## Dependencies

What does it depend on, and what depends on it?

## Failure modes and trade-offs

What are the important limits and known weaknesses?
`,
}

var parsed = map[string]*template.Template{}

func init() {
	for name, text := range files {
		parsed[name] = template.Must(template.New(name).Option("missingkey=error").Parse(text))
	}
}

// Render returns a document skeleton. The special name "project-doc" maps to
// the README skeleton.
func Render(name string, data Data) (string, error) {
	if name == "project-doc" || name == "project" {
		name = "readme"
	}
	t, ok := parsed[name]
	if !ok {
		return "", fmt.Errorf("unknown template %q (choose adr, pattern, flow, runbook, standard, readme or architecture)", name)
	}
	if data.Status == "" {
		data.Status = "approved"
	}
	if data.Date == "" {
		data.Date = "unknown"
	}
	if data.Owner == "" {
		data.Owner = "platform-team"
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func Names() []string {
	return []string{"adr", "architecture", "flow", "pattern", "readme", "runbook", "standard"}
}
