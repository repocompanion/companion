# Third-party notices

Repo Companion is distributed under the Apache License 2.0. It is built with
open-source Go modules. The table below lists the direct dependencies and the
licenses shown by their upstream repositories at the time of writing. The full
dependency graph is recorded in `go.mod` and `go.sum`; transitive modules retain
their own copyright and license notices.

| Module | License |
|---|---|
| `github.com/spf13/cobra` | Apache-2.0 |
| `github.com/spf13/pflag` | BSD-3-Clause |
| `github.com/inconshreveable/mousetrap` | Apache-2.0 |
| `github.com/google/go-github/v68` | BSD-3-Clause |
| `github.com/google/go-querystring` | BSD-3-Clause |
| `github.com/google/jsonschema-go` | MIT |
| `github.com/modelcontextprotocol/go-sdk` | MIT and Apache-2.0 (transition) |
| `github.com/yosida95/uritemplate/v3` | BSD-3-Clause |
| `github.com/gofrs/flock` | BSD-3-Clause |
| `golang.org/x/tools` | BSD-3-Clause |
| `golang.org/x/mod` | BSD-3-Clause |
| `golang.org/x/sys` | BSD-3-Clause |
| `golang.org/x/oauth2` | BSD-3-Clause |
| `golang.org/x/sync` | BSD-3-Clause |
| `golang.org/x/time` | BSD-3-Clause |
| `github.com/segmentio/encoding` | MIT |
| `github.com/segmentio/asm` | MIT |
| `gopkg.in/yaml.v3` | MIT and Apache-2.0 |

## yaml.v3 NOTICE

The following notice is reproduced from `gopkg.in/yaml.v3`:

```text
Copyright 2011-2016 Canonical Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

## Redistributing binaries

If you redistribute a companion binary, keep this file and the project
[LICENSE](LICENSE) and [NOTICE](NOTICE) alongside it. The release archives
include all three. Full license texts are available from each upstream module
and are not repeated here to avoid drift; use `go mod download -json <module>`
to locate a module's license file.
