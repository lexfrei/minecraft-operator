# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Minecraft Operator** is a Kubernetes operator for managing PaperMC servers with automatic version management for plugins and the server, ensuring compatibility through constraint solving.

**Key Design Principles:**

- NOT a high-availability system (5-10 min downtime during updates is acceptable)
- Declarative plugin management via label selectors
- Constraint solver ensures compatibility between Paper version and all plugins
- Updates only during scheduled maintenance windows with graceful RCON shutdown

## Architecture

### Core Components

Three main controllers work together:

1. **Plugin Controller**: Watches Plugin CRDs, fetches metadata from plugin repositories (Hangar/Modrinth), runs constraint solver to find best compatible version for ALL matched servers, updates Plugin.status
2. **PaperMCServer Controller**: Watches PaperMCServer CRDs, finds all matched Plugins via selectors, ensures StatefulSet exists, runs solver for Paper version upgrades
3. **Update Controller**: Triggers on cron schedule, performs graceful RCON shutdown, downloads JARs to `plugins/update/`, deletes pod for StatefulSet recreation

### Critical Relationships

- **Plugin → PaperMCServer**: Many-to-many via `instanceSelector` (label selector)
- **Selector Conflict Resolution**: When multiple Plugins with same `source.project` match same servers:
  - If any has `versionPolicy: latest`: constraint solver picks optimal version
  - If all have `versionPolicy: pinned`: highest semver wins (with warning)

### Cross-Resource Triggers

- Plugin spec/status changes → trigger PaperMCServer reconciliation for all matched servers
- PaperMCServer label changes → trigger Plugin reconciliation for all potentially matching Plugins
- Optimization: Use owner references, cache selector matching, debounce reconciliations

## CRD Design

### Plugin CRD (`mc.k8s.lex.la/v1alpha1`)

**Spec:**

- `source`: Plugin repository (hangar/modrinth/spigot/url)
- `versionPolicy`: `latest` or `pinned`
- `updateDelay`: Grace period before auto-applying new releases (e.g., `168h` for 7 days)
- `instanceSelector`: Label selector to match PaperMCServer instances
- `compatibilityOverride`: Manual compatibility specification for edge cases

**Status:**

- `resolvedVersion`: Constraint solver result
- `availableVersions`: Cached metadata from API (for orphaned plugin fallback)
- `matchedInstances`: List of servers this plugin applies to with compatibility status
- `repositoryStatus`: `available`/`unavailable`/`orphaned`

### PaperMCServer CRD (`mc.k8s.lex.la/v1alpha1`)

**Spec:**

- `paperVersion`: `latest` or specific version (e.g., `1.21.1`)
- `updateDelay`: Grace period before auto-applying Paper updates
- `updateSchedule.checkCron`: Daily update check schedule
- `updateSchedule.maintenanceWindow.cron`: When to actually apply updates
- `gracefulShutdown.timeout`: Must match StatefulSet `terminationGracePeriodSeconds`
- `rcon`: RCON configuration for graceful shutdown
- `podTemplate`: StatefulSet pod spec (uses `lexfrei/papermc:latest` image)

**Status:**

- `currentPaperVersion`: Observed running version
- `plugins`: List of matched Plugins with resolved versions
- `availableUpdate`: Solver result for next possible update
- `lastUpdate`: History of previous update attempt

## Constraint Solver

**Problem**: Find maximum Paper version AND plugin versions that satisfy ALL compatibility constraints.

**Algorithm (for Plugin with versionPolicy: latest):**

1. Collect all matched PaperMCServer instances
2. Filter plugin versions by `updateDelay` (skip versions newer than `now - updateDelay`)
3. Find MAX plugin version where: `∀ server ∈ servers: compatible(plugin_version, server.paperVersion)`
4. If no solution: emit warning in Plugin.status

**Algorithm (for PaperMCServer update):**

1. Collect all matched Plugin resources
2. Filter Paper versions by `updateDelay`
3. Find MAX Paper version where: `∀ plugin ∈ plugins: ∃ plugin_version compatible with paper_version`
4. Store result in `status.availableUpdate`

**Edge Cases:**

- Plugin without version metadata: assume compatibility, log warning, use `compatibilityOverride` if set
- Repository unavailable: use cached `status.availableVersions`, mark as `orphaned`
- Multiple selectors match one server with different plugins: allowed (all applied)

## Update Workflow

1. **Check Phase** (daily via `checkCron`):
   - Plugin Controller: Fetch metadata, run solver, update `resolvedVersion`
   - PaperMCServer Controller: Run solver, update `availableUpdate`

2. **Maintenance Window** (via `maintenanceWindow.cron`):
   - Check if `availableUpdate` exists and `updateDelay` satisfied
   - Download Paper JAR (if changed) and plugin JARs to PVC
   - Copy plugins to `/data/plugins/update/` (Paper hot-swap mechanism)
   - `kubectl delete pod` → StatefulSet recreates with same PVC
   - Paper startup: moves `/plugins/update/*.jar` to `/plugins/`, deletes old versions

3. **Graceful Shutdown**:
   - K8s sends SIGTERM → pod has `terminationGracePeriodSeconds` (default 300s)
   - Paper saves all chunks, player data, unloads plugins
   - **CRITICAL**: Never set `terminationGracePeriodSeconds` too low (world corruption risk)

## Dependencies

### External Libraries

- `github.com/lexfrei/go-hungar`: Hangar API client (use this for PaperMC plugin repository)
- `controller-runtime`: Kubernetes operator framework
- `robfig/cron`: Cron scheduling
- `Masterminds/semver`: Version comparison

### Container Images (Both Customizable)

- **Docker Image**: `lexfrei/papermc:latest` from Docker Hub ([source](https://github.com/lexfrei/papermc-docker))
- **Helm Chart**: `oci://ghcr.io/lexfrei/charts/papermc` from GHCR
- Both can be extended for operator-specific features (RCON wrappers, metrics, etc.)

## Development Workflow

### Project Structure

This project uses **Helm-only approach** with separate CRD chart. CRDs have independent lifecycle from the operator.

```text
minecraft-operator/
├── api/v1alpha1/                    # Go type definitions
├── internal/controller/             # Reconciliation loops
├── pkg/                             # solver/, plugins/, rcon/, etc.
├── cmd/
│   └── main.go                      # Operator entrypoint
├── charts/
│   ├── minecraft-operator-crds/     # CRD Helm chart (independent lifecycle)
│   │   ├── crds/                    # Generated CRDs (controller-gen output)
│   │   ├── Chart.yaml
│   │   └── values.yaml
│   └── minecraft-operator/          # Operator Helm chart
│       ├── templates/               # Deployment, RBAC, Service
│       ├── Chart.yaml               # Can depend on CRD chart via dependency
│       └── values.yaml              # crds.enabled: true (default)
├── examples/                        # Example CRs for testing
├── .architecture.yaml               # Single source of truth for technical decisions (ADRs)
└── Makefile                         # Build automation
```

**Note**: CRDs are generated directly to `charts/minecraft-operator-crds/crds/` via `make manifests`.

### Quick Start for Developers

```bash
# Prerequisites
go version    # Need 1.25+
podman --version
helm version  # Need 3.14+

# Generate code and CRDs
make manifests generate

# Run unit tests
make test

# Run linters (ALL errors must be fixed)
make lint

# Run operator locally (connects to your kubeconfig cluster)
make run

# Build container image
make container-build IMG=ghcr.io/lexfrei/minecraft-operator:dev

# Deploy to cluster via Helm
helm install minecraft-operator-crds ./charts/minecraft-operator-crds \
  --create-namespace --namespace minecraft-operator-system

helm install minecraft-operator ./charts/minecraft-operator \
  --namespace minecraft-operator-system \
  --set image.repository=ghcr.io/lexfrei/minecraft-operator \
  --set image.tag=dev

# Clean up
helm uninstall minecraft-operator --namespace minecraft-operator-system
helm uninstall minecraft-operator-crds --namespace minecraft-operator-system
```

### Common Makefile Commands

**Code generation:**

```bash
make manifests  # Generate CRDs to charts/minecraft-operator-crds/crds/
make generate   # Generate deepcopy methods for API types
make fmt        # Run go fmt
make vet        # Run go vet
```

**Testing:**

```bash
make test          # Run unit tests with envtest
make test-e2e      # Run e2e tests (auto-creates Kind cluster 'minecraft-operator-test-e2e')
```

**Linting:**

```bash
make lint          # Run golangci-lint (must pass before push)
make lint-fix      # Auto-fix linting issues where possible
make lint-config   # Verify golangci-lint configuration
```

**Building:**

```bash
make build         # Build operator binary to bin/manager
make run           # Run operator locally against kubeconfig cluster
```

**Container images:**

```bash
make container-build        # Build image with podman
make container-push         # Push image to registry
make container-buildx       # Multi-arch build and push
```

### Helm Deployment Commands

**Install (fresh deployment):**

```bash
# Install CRDs chart (independent lifecycle)
helm install minecraft-operator-crds ./charts/minecraft-operator-crds \
  --create-namespace --namespace minecraft-operator-system

# Install operator chart
helm install minecraft-operator ./charts/minecraft-operator \
  --namespace minecraft-operator-system

# OR: Install operator with embedded CRDs (if crds.enabled=true in values.yaml)
helm dependency update ./charts/minecraft-operator
helm install minecraft-operator ./charts/minecraft-operator \
  --create-namespace --namespace minecraft-operator-system
```

**Upgrade:**

```bash
# Upgrade operator (CRDs are NOT auto-upgraded by Helm best practice)
helm upgrade minecraft-operator ./charts/minecraft-operator \
  --namespace minecraft-operator-system

# Manually upgrade CRDs when needed
kubectl apply --filename ./charts/minecraft-operator-crds/crds/
# OR
helm upgrade minecraft-operator-crds ./charts/minecraft-operator-crds \
  --namespace minecraft-operator-system
```

**Uninstall:**

```bash
# Remove operator first
helm uninstall minecraft-operator --namespace minecraft-operator-system

# Remove CRDs (WARNING: deletes all Plugin and PaperMCServer custom resources!)
helm uninstall minecraft-operator-crds --namespace minecraft-operator-system
```

**Useful Helm commands:**

```bash
# Dry-run to see generated manifests
helm install minecraft-operator ./charts/minecraft-operator \
  --dry-run --debug --namespace minecraft-operator-system

# Lint charts
helm lint ./charts/minecraft-operator-crds
helm lint ./charts/minecraft-operator

# List installed releases
helm list --namespace minecraft-operator-system

# Get values
helm get values minecraft-operator --namespace minecraft-operator-system
```

### Development Workflow

**Typical development iteration:**

1. **Modify API types** in `api/v1alpha1/*.go`
2. **Regenerate code**: `make manifests generate`
3. **Run tests**: `make test`
4. **Check linting**: `make lint` (fix ALL errors before commit)
5. **Test locally**: `make run` (runs against your kubeconfig cluster)
6. **Commit changes** with semantic commit message and GPG signature
7. **Push to feature branch** (NEVER directly to master)
8. **Create PR** for review

**Testing Strategy:**

- **Unit tests**: `make test` runs with envtest (fake Kubernetes API)
  - Test framework: Ginkgo/Gomega (controller-runtime standard)
  - Coverage target: 80%+ for pkg/
  - Location: `*_test.go` files alongside code

- **E2E tests**: `make test-e2e` creates Kind cluster, deploys operator, runs tests
  - Kind cluster name: `minecraft-operator-test-e2e`
  - Auto-cleanup after tests
  - Location: `test/e2e/`

- **Integration tests**: Use envtest with real Kubernetes API machinery
  - Location: `internal/controller/*_test.go`

**Linting Requirements:**

- **Go**: `make lint` runs golangci-lint with config from `.golangci.yml`
  - ALL linting errors MUST be fixed before push
  - No exceptions for "cosmetic" errors
  - Use `make lint-fix` for auto-fixable issues

- **Markdown**: markdownlint with `.markdownlint.yaml` config

- **Helm**: `helm lint charts/minecraft-operator-crds && helm lint charts/minecraft-operator`

**Building and Deploying:**

```bash
# Build and test locally
make manifests generate fmt vet build
./bin/manager  # Or use 'make run'

# Build container image
make container-build IMG=ghcr.io/lexfrei/minecraft-operator:v1.0.0

# Push to registry
make container-push IMG=ghcr.io/lexfrei/minecraft-operator:v1.0.0

# Deploy to cluster via Helm
helm install minecraft-operator-crds ./charts/minecraft-operator-crds \
  --create-namespace --namespace minecraft-operator-system

helm install minecraft-operator ./charts/minecraft-operator \
  --namespace minecraft-operator-system \
  --set image.repository=ghcr.io/lexfrei/minecraft-operator \
  --set image.tag=v1.0.0
```

## Design Decisions and Technical Standards

All architectural decisions are documented as ADRs in `.architecture.yaml` (single source of truth).

**Key finalized decisions:**

- **Namespace scope**: Plugin and PaperMCServer are namespace-scoped (ADR-008)
- **Framework**: controller-runtime v0.22.3 with Kubebuilder v4 scaffolding (ADR-001, ADR-009)
- **Solver**: Simple linear search for MVP, SAT solver in Phase 2 (ADR-010)
- **Testing**: envtest for integration, Ginkgo/Gomega framework (ADR-011, ADR-017)
- **Container runtime**: Podman (per global CLAUDE.md)
- **Container base**: distroless/static-debian12:nonroot for security (ADR-006)
- **Error handling**: cockroachdb/errors for stack traces (ADR-007)
- **Deployment**: Helm-only approach with separate CRD chart (ADR-005)
- **Datapack support**: Out of scope (no compatibility versions)
- **Paid plugins**: Nice-to-have, not in MVP (would need auth via Secret)
- **Manual approval**: Not implemented (fully automated updates with updateDelay)
- **Selector conflicts**: Resolved via version priority (latest > pinned, highest semver for all-pinned) (ADR-019)

See `.architecture.yaml` for complete ADR history and technical stack details.

## Important Notes

- **Single source of truth**: `.architecture.yaml` contains all technical decisions (ADRs), dependencies, and standards
- **NEVER push directly to master**: All changes via feature branches + PR
- **GPG signing**: Required for all commits (see global CLAUDE.md)
- **Linting**: ALL errors must be fixed before push, no exceptions
- **API domain**: `mc.k8s.lex.la` (NOT `paperstack.io`)
- **License**: BSD-3-Clause
- **Main entrypoint**: `cmd/main.go` (not root `main.go`)
- **CRD generation**: Outputs to `charts/minecraft-operator-crds/crds/` via `make manifests`
- **Deployment method**: Helm-only (no Kustomize)
- **Testing framework**: Ginkgo/Gomega (BDD style, controller-runtime standard)
- **E2E tests**: Auto-create/destroy Kind cluster named `minecraft-operator-test-e2e`
- **NOT high-availability**: 5-10 min downtime during updates is acceptable by design (ADR-015)

## Documentation

- **Architecture**: `DESIGN.md` - Complete architectural details, CRD schemas, solver algorithms
- **Technical decisions**: `.architecture.yaml` - ADRs, dependencies, tech stack
- **Project config**: `PROJECT` - Kubebuilder metadata
- **Development guide**: This file (`CLAUDE.md`)
- **Examples**: `examples/` - Example custom resources
