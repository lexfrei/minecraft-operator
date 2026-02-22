# Development Setup

Set up your local development environment for Minecraft Operator.

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.26+ | Language runtime |
| Podman or Docker | Latest | Container builds |
| Helm | 3.14+ | Chart testing |
| kubectl | 1.27+ | Cluster access |
| Kind | Latest | Local Kubernetes (optional) |

### Install Go

=== "macOS"

    ```bash
    brew install go
    ```

=== "Linux"

    ```bash
    wget https://go.dev/dl/go1.26.linux-amd64.tar.gz
    sudo tar -C /usr/local -xzf go1.26.linux-amd64.tar.gz
    export PATH=$PATH:/usr/local/go/bin
    ```

### Install Tools

```bash
# Helm
brew install helm  # or follow helm.sh/docs/intro/install

# kubectl
brew install kubectl  # or kubernetes.io/docs/tasks/tools

# Kind (for local cluster)
brew install kind  # or kind.sigs.k8s.io/docs/user/quick-start
```

## Clone Repository

```bash
git clone https://github.com/lexfrei/minecraft-operator.git
cd minecraft-operator
```

## Generate Code

After modifying CRD types in `api/v1beta1/`, regenerate code:

```bash
# Generate CRDs and DeepCopy methods
make manifests generate

# Format code
make fmt

# Run vet
make vet
```

## Run Tests

```bash
# Unit tests with envtest
make test

# Helm chart template tests
helm unittest charts/minecraft-operator

# E2E tests (creates Kind cluster)
make test-e2e
```

### Test Coverage

```bash
# Run tests with coverage
make test

# View coverage report
go tool cover -html=cover.out

# Check controller coverage (target: 60%+)
go test ./internal/controller/... -coverprofile=cover.out \
  && go tool cover -func=cover.out | grep total
```

### Helm Chart Tests

The operator Helm chart uses [helm-unittest](https://github.com/helm-unittest/helm-unittest) for template validation:

```bash
# Install plugin
helm plugin install https://github.com/helm-unittest/helm-unittest

# Run chart tests
helm unittest charts/minecraft-operator
```

Test suites cover deployment, RBAC, service account, metrics service, and WebUI templates.
Tests live in `charts/minecraft-operator/tests/`.

## Run Linter

```bash
# Check for issues
make lint

# Auto-fix where possible
make lint-fix

# Verify config
make lint-config
```

!!! warning "All Errors Required"

    ALL linting errors must be fixed before commit.
    No exceptions for "cosmetic" issues.

## Run Locally

Run the operator against your current kubeconfig cluster:

```bash
# Default: JSON logs, Info level
make run

# Development: Text logs, Debug level
make run ARGS="--log-level=debug --log-format=text"
```

The operator will reconcile resources in your cluster.

## Build Container

```bash
# Build with Podman
make container-build IMG=ghcr.io/lexfrei/minecraft-operator:dev

# Push to registry
make container-push IMG=ghcr.io/lexfrei/minecraft-operator:dev
```

## Local Cluster (Kind)

Create a local Kubernetes cluster:

```bash
# Create cluster
kind create cluster --name minecraft-dev

# Run operator locally (CRDs are applied automatically at startup)
make run
```

## Helm Development

### Test Charts

```bash
# Run template tests
helm unittest charts/minecraft-operator
```

### Lint Charts

```bash
helm lint charts/minecraft-operator
```

### Template Charts

```bash
# See generated manifests
helm template minecraft-operator charts/minecraft-operator \
  --namespace minecraft-operator-system

# With custom values
helm template minecraft-operator charts/minecraft-operator \
  --set replicaCount=2
```

### Install Chart

```bash
# Install operator (CRDs are embedded and applied at startup)
helm install minecraft-operator charts/minecraft-operator \
  --create-namespace --namespace minecraft-operator-system \
  --set image.repository=ghcr.io/lexfrei/minecraft-operator \
  --set image.tag=dev
```

## IDE Setup

### VS Code

Recommended extensions:

- Go (golang.go)
- YAML (redhat.vscode-yaml)
- Kubernetes (ms-kubernetes-tools.vscode-kubernetes-tools)

### GoLand

Enable:

- Go Modules integration
- gofmt on save
- golangci-lint integration

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `KUBECONFIG` | Kubernetes config path | `~/.kube/config` |
| `ENABLE_WEBHOOKS` | Enable admission webhooks | `false` |
| `LOG_LEVEL` | Logging level | `info` |
| `LOG_FORMAT` | Log format (json/text) | `json` |

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make manifests` | Generate CRDs to internal/crdmanager/crds/ |
| `make generate` | Generate DeepCopy methods |
| `make fmt` | Run go fmt |
| `make vet` | Run go vet |
| `make test` | Run unit tests |
| `make test-e2e` | Run E2E tests (Kind cluster) |
| `helm unittest charts/minecraft-operator` | Run Helm chart template tests |
| `make lint` | Run golangci-lint |
| `make lint-fix` | Auto-fix lint issues |
| `make build` | Build binary to bin/manager |
| `make run` | Run operator locally |
| `make container-build` | Build container image |
| `make container-push` | Push container image |

## Troubleshooting

### envtest Not Working

```bash
# Install envtest binaries manually
make envtest
./bin/setup-envtest use 1.34.x
```

### golangci-lint Outdated

```bash
# Update linter
make golangci-lint
./bin/golangci-lint --version
```

### Kind Cluster Issues

```bash
# Delete and recreate
kind delete cluster --name minecraft-dev
kind create cluster --name minecraft-dev
```

## See Also

- [Contributing](contributing.md) — Contribution workflow
- [CLAUDE.md](https://github.com/lexfrei/minecraft-operator/blob/master/CLAUDE.md) — Full development standards
