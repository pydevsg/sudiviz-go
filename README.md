# sudiviz

[![License](https://img.shields.io/badge/license-GPL--3.0--or--later-green?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8?style=flat-square&logo=go)](https://go.dev)

> X-ray vision for your cloud infrastructure

**sudiviz** is a single static Go binary that connects to a live AWS account, discovers resources across multiple services, builds an in-memory topology graph of their relationships, runs a diagnostic engine over that graph, and renders the result as a web UI, TUI, static image, or terminal table. It can also auto-remediate the issues it finds.

This repository is a ground-up Go reimplementation of the Python tool at [github.com/pydevsg/sudiviz](https://github.com/pydevsg/sudiviz). It preserves the same user-facing commands, flags, findings, and exit codes.

```
discover → graph → diagnose → render / fix / explain
```

**Auth:** Uses the standard AWS SDK credential chain (`~/.aws/credentials`, environment variables, SSO, or instance profile). Credentials are never accepted as CLI flags.

---

## Features

- Live topology of ALB → target groups → EC2 → security groups, plus ECS, EKS, RDS, Lambda, and S3
- Concurrent discovery (one goroutine per service via `errgroup`)
- Seven core diagnostic rules plus ECS / EKS / RDS / Lambda health checks
- Auto-fix with dry-run by default; destructive deletes require `--force`
- Embedded Cytoscape.js web UI (AWS icons, cost heatmap, traffic animation)
- Bubble Tea TUI
- PNG / SVG export via Graphviz `dot`
- Terraform drift detection (`terraform show -json` vs live AWS)
- Amazon Bedrock Nova Lite explanations (`sudiviz explain`)
- MCP server for AI agents (`sudiviz-mcp`)

---

## Requirements

| Tool | Why |
|------|-----|
| [Go 1.26+](https://go.dev/dl/) | Build and test |
| AWS credentials | Live discovery (not needed for unit tests) |
| [Graphviz](https://graphviz.org/) `dot` | Optional — only for `graph --output png` / `svg` |
| Bedrock model access for `amazon.nova-lite-v1:0` | Optional — only for `explain` |

---

## How to run

### 1. Clone and build

```bash
git clone https://github.com/pydevsg/sudiviz-go.git
cd sudiviz-go

make build
# binaries: bin/sudiviz  and  bin/sudiviz-mcp
```

Or without Make:

```bash
go build -ldflags="-s -w" -o bin/sudiviz ./cmd/sudiviz
go build -ldflags="-s -w" -o bin/sudiviz-mcp ./cmd/sudiviz-mcp
```

### 2. Configure AWS

sudiviz never takes access keys as flags. Use any of:

```bash
# named profile
export AWS_PROFILE=prod

# or env vars
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
export AWS_REGION=us-east-1

# or SSO
aws sso login --profile prod
```

Optional YAML config (`sudiviz.yaml` in the current directory, `~/.config/sudiviz/`, or `--config path`):

```yaml
profile: prod
region: us-east-1
vpc-id: vpc-0123456789abcdef0
service-tag: Service=checkout
```

Environment variables use the `SUDIVIZ_` prefix (`SUDIVIZ_REGION`, `SUDIVIZ_PROFILE`, …).

### 3. Run

```bash
# from the repo after make build
./bin/sudiviz diagnose --region us-east-1

# or install onto GOPATH/bin
go install github.com/pydevsg/sudiviz-go/cmd/sudiviz@latest
sudiviz diagnose
```

### Docker

```bash
make docker
docker run --rm \
  -v ~/.aws:/root/.aws:ro \
  -e AWS_PROFILE=prod \
  sudiviz:dev diagnose --region us-east-1
```

The image is a multi-stage build with a `scratch` final stage (CA certs included so the AWS SDK can talk HTTPS).

### Other install options

```bash
# Homebrew (after a tagged GoReleaser release)
brew install pydevsg/tap/sudiviz
```

---

## Commands

Global flags (available on every command):

| Flag | Description |
|------|-------------|
| `--region` | AWS region override |
| `--profile` | AWS named profile |
| `--vpc-id` | Limit discovery to one VPC |
| `--service-tag` | Tag filter, e.g. `Service=checkout` or `k=v,k2=v2` |
| `--config` | Path to a YAML config file |
| `-v` / `--verbose` | Verbose logging |
| `--output` | Default output mode used by `graph` |

**CI exit codes:** `0` = clean, `1` = drift detected or a fix apply failed, `2` = critical issues found.

### `diagnose`

Discover live resources, build the graph, run every diagnostic rule, print a topology tree and a severity-sorted table.

```bash
sudiviz diagnose
sudiviz diagnose --region us-east-1 --profile prod
sudiviz diagnose --json                          # machine-readable for CI
sudiviz diagnose --format table --severity critical
sudiviz diagnose --vpc-id vpc-abc --service-tag Service=checkout
```

`--json` / `--format json` writes `{ "graph": ..., "diagnosis": ... }` and exits `2` if any finding is critical.

### `graph`

Export the topology or serve the interactive web UI.

```bash
sudiviz graph --output web --open                # http://127.0.0.1:8000
sudiviz graph --output web --host 127.0.0.1 --port 8000 --refresh-interval 30
sudiviz graph --output png --file topology.png --open
sudiviz graph --output svg --file topology.svg
sudiviz graph --output json --file topology.json
```

Web UI includes AWS service icons, a cost heatmap, health / orphan overlays, traffic-flow animation, live WebSocket refresh, dark/light theme, cluster grouping, and an SG-flow toggle. PNG/SVG require `dot` on `PATH`.

### `fix`

Preview or apply remediations for diagnosed issues. Default is dry-run.

```bash
sudiviz fix                    # list AWS CLI commands (dry-run)
sudiviz fix --dry-run          # same
sudiviz fix 1 --apply          # apply fix #1
sudiviz fix 1,3 --apply        # apply #1 and #3
sudiviz fix 1-3 --apply        # apply #1, #2, #3
sudiviz fix --apply            # apply all non-destructive fixes
sudiviz fix --apply --force    # include deletes (orphan TGs, unused SGs)
sudiviz fix --issue "S3" --json
```

Automated remediations: missing ALB→SG ingress, S3 public-access block, S3 encryption, RDS public access, orphan target-group delete, unused SG delete, deregister unhealthy targets.

### `tui`

Interactive terminal UI (resource list, detail pane, findings). Keys: `q` quit, `r` refresh, `o` orphans only, `j`/`k` move.

```bash
sudiviz tui --refresh-interval 30
```

### `explain`

Sends findings to Amazon Bedrock Nova Lite (`amazon.nova-lite-v1:0`) and streams a plain-English analysis. This is the only command that uses LLM tokens.

```bash
sudiviz explain
sudiviz explain "why is my target group unhealthy?"
sudiviz explain --resource arn:aws:s3:::my-bucket
```

Requires `bedrock:InvokeModelWithResponseStream` (or `AmazonBedrockFullAccess`) and Nova Lite model access in the region.

### `drift`

Compare `terraform show -json` output against live AWS.

```bash
terraform show -json > state.json
sudiviz drift --tfstate state.json
sudiviz drift --tfstate state.json --json   # exit 1 if any drift
```

Reports `missing` (in Terraform, not in AWS), `orphan_in_aws` (in AWS, not in Terraform), and `orphan_listener`.

### `watch`

Re-run diagnose on an interval.

```bash
sudiviz watch --interval 30
```

### `compare`

Diff a saved Cytoscape JSON snapshot against the live topology.

```bash
sudiviz graph --output json --file baseline.json
sudiviz compare --baseline baseline.json
sudiviz compare --baseline baseline.json --json
```

### `share`

Write a Cytoscape JSON snapshot to a temp file (upload is opt-in and off by default for safety).

```bash
sudiviz share --no-upload
```

### `mcp` / `sudiviz-mcp`

Start the Model Context Protocol server on stdio.

```bash
sudiviz mcp
# or
./bin/sudiviz-mcp
```

Claude Desktop / Cursor example (`claude_desktop_config.json` or `.mcp.json`):

```json
{
  "mcpServers": {
    "sudiviz": {
      "command": "sudiviz-mcp",
      "env": { "AWS_PROFILE": "production" }
    }
  }
}
```

| Tool | Description |
|------|-------------|
| `sudiviz_discover` | Live AWS resources |
| `sudiviz_diagnose` | Discover + analyse |
| `sudiviz_graph` | Cytoscape topology JSON |
| `sudiviz_fix` | Generate or apply remediations |
| `sudiviz_drift` | Terraform vs live AWS |
| `sudiviz_costs` | Estimated monthly cost breakdown |
| `sudiviz_list_resources` | List resources by kind |

Resources: `infra://aws/{region}/topology`, `infra://aws/{region}/health`, `infra://aws/{region}/costs`.

Prompts: `diagnose-infrastructure`, `cost-optimization`, `security-audit`, `incident-triage`.

### `version`

Print the ASCII banner and version.

```bash
sudiviz version
```

---

## Diagnostic rules

| Check | Severity | Detection |
|-------|----------|-----------|
| Unhealthy targets in a target group | critical (0 healthy) / warning (partial) | Target health ≠ healthy |
| SG missing ingress from ALB | critical | Instance SG has no inbound rule from the ALB SG (or `0.0.0.0/0`) on the TG port |
| S3 bucket with public access | critical | Public Access Block is not fully enabled |
| RDS instance publicly accessible | warning | `PubliclyAccessible == true` |
| Storage not encrypted (EBS, RDS, S3) | warning | Encryption flags false / missing |
| Orphan target group | warning | No ALB listener `forwards_to` this TG |
| Unused security group | info | No `guarded_by` edge |
| Instance not in any target group | info | No `registered_in` edge |
| ECS / EKS / RDS / Lambda unhealthy | critical or warning | Desired vs running, cluster status, DB status, function state |

---

## File architecture

```
sudiviz-go/
├── cmd/
│   ├── sudiviz/main.go          # CLI entry point
│   └── sudiviz-mcp/main.go      # MCP stdio entry point
├── internal/
│   ├── cli/                     # Cobra command tree (diagnose, graph, fix, …)
│   ├── config/                  # Viper: file + env + flags
│   ├── version/                 # Banner and link-time version
│   ├── run/                     # Shared discover → orphan-mark → diagnose pipeline
│   ├── awsurl/                  # Console / CloudWatch / pricing deep links
│   ├── discovery/               # Cloud-agnostic Discoverer + AWS implementations
│   │   ├── discoverer.go        # Interface + concurrent DiscoverAll
│   │   ├── awsauth.go           # Credential chain, STS whoami
│   │   ├── alb.go, targetgroup.go, ec2.go, sg.go
│   │   ├── ecs.go, eks.go, rds.go, lambda.go, s3.go
│   │   └── costs.go             # Estimated monthly cost (heatmap)
│   ├── graph/                   # InfraGraph (gonum directed graph)
│   │   ├── resource.go          # Cloud-agnostic node (kind, health, cost, attrs)
│   │   ├── edge.go              # Typed relations (forwards_to, guarded_by, …)
│   │   └── graph.go
│   ├── diagnose/                # Rule engine
│   │   ├── rule.go, finding.go, engine.go, orphans.go
│   │   └── rules/               # One file per check
│   ├── fix/                     # Remediator interface + apply / dry-run
│   │   └── remediators/
│   ├── render/
│   │   ├── cytoscape.go         # Web / JSON graph payload
│   │   ├── tree.go              # Terminal topology tree
│   │   ├── table/               # Diagnosis table
│   │   ├── web/                 # HTTP + WebSocket server
│   │   ├── tui/                 # Bubble Tea TUI
│   │   └── static/              # Graphviz DOT → PNG/SVG
│   ├── explain/                 # Bedrock Nova Lite streaming
│   ├── drift/                   # Terraform state vs live graph
│   └── mcp/                     # MCP tools, resources, prompts
├── web/
│   ├── embed.go                 # //go:embed static
│   └── static/                  # index.html, style.css, AWS icons
├── testdata/                    # Sample terraform show -json
├── Makefile
├── Dockerfile                   # multi-stage → scratch
├── .goreleaser.yml
├── .github/workflows/ci.yml
├── go.mod
├── LICENSE                      # GPL-3.0
└── README.md
```

### How the layers connect

1. **`internal/cli`** parses flags and calls `run.Live`.
2. **`internal/run`** loads AWS config, fans out discoverers, marks orphans, runs the rule engine.
3. **`internal/discovery`** implements `Discoverer`. Each service is its own file. Provider-specific clients are injected at construction so the interface stays cloud-agnostic (GCP / Azure can implement the same type later).
4. **`internal/graph`** stores resources and typed edges. Diagnostic rules never call AWS — they only read the graph.
5. **`internal/diagnose/rules`** emit `Finding`s. **`internal/fix/remediators`** turn those into AWS CLI text plus an apply closure.
6. **`internal/render`** is the only place that knows about terminals, browsers, or Graphviz.

---

## Tests

Unit tests use [testify](https://github.com/stretchr/testify). Discovery tests use interface mocks — **no real AWS calls**.

```bash
make test          # go test ./...
make vet           # go vet ./...
go test ./... -count=1
go test ./internal/diagnose/rules -v
go test ./internal/discovery -v
go test ./internal/fix/remediators -v
```

| Package | What is covered |
|---------|-----------------|
| `internal/graph` | Placeholder nodes, merge semantics, edge replace, attr helpers |
| `internal/discovery` | ALB, TG (+ health), EC2, SG, ECS, EKS, RDS, Lambda, S3 mocks; tag filters; S3 wiring; cost estimates |
| `internal/diagnose` / `rules` | Every diagnostic rule + engine sort order + orphan marking |
| `internal/fix` / `remediators` | Plan + apply against fake AWS clients; destructive flags; manual fallback |
| `internal/drift` | Parse `testdata/terraform_state.json` and detect missing / orphan |
| `internal/render` | Cytoscape JSON + Graphviz DOT |
| `internal/explain` | Empty-findings short-circuit (no Bedrock call) |
| `internal/mcp` | Server construction |
| `internal/awsurl` | Console / metrics / logs / pricing URLs |

CI (`.github/workflows/ci.yml`) runs `go test ./...`, `go vet ./...`, and `make build` on every push and pull request.

---

## Makefile targets

| Target | Action |
|--------|--------|
| `make build` | Static binaries in `bin/` |
| `make test` | `go test ./...` |
| `make vet` | `go vet ./...` |
| `make lint` | `golangci-lint run` |
| `make tidy` | `go mod tidy` |
| `make docker` | Multi-stage image |
| `make cross` | linux/darwin/windows amd64+arm64 |
| `make release` | GoReleaser (tag-based) |
| `make install` | `go install …@latest` |

Cross-compilation is CGo-free: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64.

---

## Multi-cloud

The `Discoverer` interface does not take `aws.Config`. AWS clients are constructed in `NewAWSDiscoverers` and stored on each discoverer. Graph nodes carry a `Provider` field (`aws` / `gcp` / `azure`) and put vendor-specific detail in `Attrs`. Diagnostic rules query kinds and relations only.

---

## License

[GPL-3.0-or-later](LICENSE). Ground-up Go rewrite of [sudiviz](https://github.com/pydevsg/sudiviz).
