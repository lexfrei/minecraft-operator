# Development

This section covers development setup and contribution guidelines.

## Overview

Minecraft Operator is built with:

- **Go 1.26+** — Primary language
- **controller-runtime** — Kubernetes operator framework
- **Kubebuilder v4** — Project scaffolding
- **Helm** — Deployment charts

## Sections

<div class="grid cards" markdown>

-   :material-laptop:{ .lg .middle } **Setup**

    ---

    Development environment setup

    [:octicons-arrow-right-24: Setup](setup.md)

-   :material-source-pull:{ .lg .middle } **Contributing**

    ---

    Contribution guidelines and workflow

    [:octicons-arrow-right-24: Contributing](contributing.md)

</div>

## Quick Start

```bash
# Clone repository
git clone https://github.com/lexfrei/minecraft-operator.git
cd minecraft-operator

# Install dependencies
make manifests generate

# Run tests
make test

# Run linter
make lint

# Run locally
make run
```

## Project Structure

```text
minecraft-operator/
├── api/v1beta1/           # CRD type definitions
├── internal/
│   ├── controller/         # Controller implementations
│   └── crdmanager/crds/    # Embedded CRDs (controller-gen output)
├── pkg/                    # Shared packages
│   ├── solver/             # Constraint solver
│   ├── plugins/            # Plugin API clients
│   ├── rcon/               # RCON client
│   ├── registry/           # Docker Hub client
│   ├── testutil/           # Test mocks and helpers
│   └── webui/              # Web UI
├── charts/
│   └── minecraft-operator/ # Helm chart
├── cmd/main.go             # Entrypoint
└── Makefile                # Build automation
```

## Key Files

| File | Purpose |
|------|---------|
| `api/v1beta1/*_types.go` | CRD definitions |
| `internal/controller/*_controller.go` | Reconciliation logic |
| `.architecture.yaml` | Technical decisions (ADRs) |
| `CLAUDE.md` | Development standards |

## Testing

The project uses multiple testing strategies:

| Type | Command | Description |
| --- | --- | --- |
| Unit | `make test` | Controller and package tests with envtest |
| Helm | `helm unittest charts/minecraft-operator` | Chart template validation |
| E2E | `make test-e2e` | Full cluster tests (Kind) |
| Lint | `make lint` | Code quality checks |

## TDD Workflow

This project follows strict TDD:

1. Write failing test
2. Implement minimal code
3. Run test → pass
4. Refactor
5. Repeat

See [CLAUDE.md](https://github.com/lexfrei/minecraft-operator/blob/master/CLAUDE.md) for detailed standards.

## See Also

- [Setup](setup.md) — Development environment
- [Contributing](contributing.md) — Contribution workflow
- [Architecture](../architecture/index.md) — System design
