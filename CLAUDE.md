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

Four main controllers work together:

1. **Plugin Controller**: Watches Plugin CRDs, fetches metadata from plugin repositories (Hangar, direct URL) or extracts from JARs, runs constraint solver to find best compatible version for ALL matched servers, updates Plugin.status
2. **PaperMCServer Controller**: Watches PaperMCServer CRDs, finds all matched Plugins via selectors, ensures StatefulSet/Service/NetworkPolicy exist, runs solver for Paper version upgrades
3. **Update Controller**: Triggers on cron schedule, performs graceful RCON shutdown, downloads JARs to `plugins/update/`, deletes pod for StatefulSet recreation
4. **Backup Controller**: Manages VolumeSnapshot-based backups with RCON hooks (`save-all`/`save-off`/`save-on`) for data consistency. Supports scheduled (cron), pre-update, and manual (annotation) triggers with configurable retention

### Critical Relationships

- **Plugin → PaperMCServer**: Many-to-many via `instanceSelector` (label selector)
- **Selector Conflict Resolution**: When multiple Plugins with same `source.project` match same servers:
  - If any has `updateStrategy: latest`: constraint solver picks optimal version
  - If all have `updateStrategy: pin`: highest semver wins (with warning)

### Cross-Resource Triggers

- Plugin spec/status changes → trigger PaperMCServer reconciliation for all matched servers
- PaperMCServer label changes → trigger Plugin reconciliation for all potentially matching Plugins
- Optimization: Use owner references, cache selector matching, debounce reconciliations

### Annotations

- `mc.k8s.lex.la/apply-now`: Unix timestamp — triggers immediate update outside maintenance window
- `mc.k8s.lex.la/backup-now`: Unix timestamp — triggers immediate VolumeSnapshot backup outside cron schedule

## CRD Design

### Plugin CRD (`mc.k8s.lex.la/v1beta1`)

**Spec:**

- `source`: Plugin repository (`hangar` or `url`; `modrinth`/`spigot` planned but not implemented). For `url` type, includes `url` (HTTPS download URL) and optional `checksum` (SHA256 hex string for integrity verification)
- `updateStrategy`: `latest`, `auto`, `pin`, or `build-pin` (defines version management behavior). For URL-source plugins, all strategies behave similarly since only one version exists (the JAR at the URL)
- `version`: Specific version when using `pin` or `build-pin` strategy (also used as fallback version for URL-source plugins when JAR has no plugin.yml version)
- `updateDelay`: Grace period before auto-applying new releases (e.g., `168h` for 7 days)
- `port`: Optional TCP port exposed by the plugin (e.g., 8123 for Dynmap); used for Service ports and NetworkPolicy ingress rules
- `instanceSelector`: Label selector to match PaperMCServer instances
- `compatibilityOverride`: Manual compatibility specification for edge cases

**Status:**

- `resolvedVersion`: Constraint solver result
- `availableVersions`: Cached metadata from API (for orphaned plugin fallback)
- `matchedInstances`: List of servers this plugin applies to with compatibility status
- `repositoryStatus`: `available`/`unavailable`/`orphaned`

### PaperMCServer CRD (`mc.k8s.lex.la/v1beta1`)

**Spec:**

- `updateStrategy`: `latest`, `auto`, `pin`, or `build-pin` (defines version management behavior)
- `version`: Paper version (e.g., `1.21.1`), required for `pin` and `build-pin` strategies
- `build`: Paper build number (e.g., `91`), required for `build-pin` strategy
- `updateDelay`: Grace period before auto-applying Paper updates
- `updateSchedule.checkCron`: Daily update check schedule
- `updateSchedule.maintenanceWindow.cron`: When to actually apply updates
- `gracefulShutdown.timeout`: Must match StatefulSet `terminationGracePeriodSeconds`
- `rcon`: RCON configuration for graceful shutdown
- `backup`: VolumeSnapshot backup configuration (schedule, retention, beforeUpdate)
- `network`: Network policy configuration (per-server NetworkPolicy with ingress/egress rules)
- `gateway`: Gateway API configuration (TCPRoute/UDPRoute for game traffic via Gateway API)
- `podTemplate`: StatefulSet pod spec

**Status:**

- `currentPaperVersion`: Observed running version
- `currentPaperBuild`: Observed running build number
- `desiredPaperVersion`: Target version (resolved from updateStrategy)
- `desiredPaperBuild`: Target build (resolved from updateStrategy)
- `plugins`: List of matched Plugins with current/desired versions
- `availableUpdate`: Solver result for next possible update
- `lastUpdate`: History of previous update attempt
- `updateBlocked`: Indicates if updates are blocked due to compatibility issues
- `backup`: Backup status (lastBackup record, backupCount)

## Constraint Solver

**Problem**: Find maximum Paper version AND plugin versions that satisfy ALL compatibility constraints.

**Algorithm (for Plugin with updateStrategy: latest):**

1. Collect all matched PaperMCServer instances
2. Filter plugin versions by `updateDelay` (skip versions newer than `now - updateDelay`)
3. Find MAX plugin version where: `∀ server ∈ servers: compatible(plugin_version, server.version)`
4. If no solution: emit warning in Plugin.status

**Algorithm (for PaperMCServer with updateStrategy: auto):**

1. Collect all matched Plugin resources
2. Filter Paper versions by `updateDelay`
3. Find MAX Paper version where: `∀ plugin ∈ plugins: ∃ plugin_version compatible with paper_version`
4. Store result in `status.desiredPaperVersion` and `status.desiredPaperBuild`
5. Compare with current version to determine if update is available

**Update Strategy Behaviors:**

- `latest`: Always use newest available Paper version from Docker Hub (ignores plugin compatibility)
- `auto`: Use constraint solver to find best Paper version compatible with ALL plugins
- `pin`: Stay on specified version, auto-update to latest build
- `build-pin`: Fully pinned, no automatic updates

**Edge Cases:**

- Plugin without version metadata: assume compatibility, log warning, use `compatibilityOverride` if set
- Repository unavailable: use cached `status.availableVersions`, mark as `orphaned`
- Multiple selectors match one server with different plugins: allowed (all applied)

## Update Workflow

1. **Check Phase** (daily via `checkCron`):
   - Plugin Controller: Fetch metadata, run solver, update `resolvedVersion`
   - PaperMCServer Controller: Based on `updateStrategy`, resolve desired version:
     - `latest`: Query Docker Hub for newest version-build
     - `auto`: Run constraint solver for plugin compatibility
     - `pin`: Find latest build for pinned version
     - `build-pin`: Validate specified version-build exists
   - Update `status.desiredPaperVersion` and `status.desiredPaperBuild`

2. **Maintenance Window** (via `maintenanceWindow.cron`):
   - Check if `desiredPaperVersion` differs from `currentPaperVersion` and `updateDelay` satisfied
   - Download Paper JAR (if changed) and plugin JARs to PVC
   - Copy plugins to `/data/plugins/update/` (Paper hot-swap mechanism)
   - Update StatefulSet image to `docker.io/lexfrei/papermc:{version}-{build}` (never uses `:latest` tag)
   - `kubectl delete pod` → StatefulSet recreates with same PVC
   - Paper startup: moves `/plugins/update/*.jar` to `/plugins/`, deletes old versions

3. **Graceful Shutdown**:
   - K8s sends SIGTERM → pod has `terminationGracePeriodSeconds` (default 300s)
   - Paper saves all chunks, player data, unloads plugins
   - **CRITICAL**: Never set `terminationGracePeriodSeconds` too low (world corruption risk)

## Dependencies

### External Libraries

- `github.com/lexfrei/go-hangar`: Hangar API client (use this for PaperMC plugin repository)
- `controller-runtime`: Kubernetes operator framework
- `robfig/cron`: Cron scheduling
- `Masterminds/semver`: Version comparison
- `log/slog`: Structured logging (Go stdlib, no external dependency)
- `github.com/go-logr/logr`: Logging bridge for controller-runtime integration

### Container Images (Both Customizable)

- **Docker Image**: `lexfrei/papermc:latest` from Docker Hub ([source](https://github.com/lexfrei/papermc-docker))
- **Helm Chart**: `oci://ghcr.io/lexfrei/charts/papermc` from GHCR
- Both can be extended for operator-specific features (RCON wrappers, metrics, etc.)

## Development Workflow

### Project Structure

This project uses **Helm-only approach** with embedded CRDs. CRDs are compiled into the operator binary and applied at startup via server-side apply.

```text
minecraft-operator/
├── api/v1beta1/                    # Go type definitions
├── internal/
│   ├── controller/                  # Reconciliation loops
│   └── crdmanager/                  # CRD lifecycle management
│       └── crds/                    # Generated CRDs (controller-gen output, embedded via Go embed.FS)
├── pkg/                             # solver/, plugins/, rcon/, etc.
├── cmd/
│   └── main.go                      # Operator entrypoint
├── charts/
│   └── minecraft-operator/          # Operator Helm chart
│       ├── templates/               # Deployment, RBAC, Service
│       ├── Chart.yaml
│       └── values.yaml              # crds.manage: true (default)
├── examples/                        # Example CRs for testing
├── .architecture.yaml               # Single source of truth for technical decisions (ADRs)
└── Makefile                         # Build automation
```

**Note**: CRDs are generated to `internal/crdmanager/crds/` via `make manifests` and embedded into the operator binary. The operator applies them at startup using server-side apply (essential for PaperMCServer CRD which is ~600KB, exceeding the 262KB annotation limit). The `--manage-crds` flag (default: true) controls this behavior.

### Quick Start for Developers

```bash
# Prerequisites
go version    # Need 1.26+
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

# Deploy to cluster via Helm (single step — CRDs are embedded and applied at startup)
helm install minecraft-operator ./charts/minecraft-operator \
  --create-namespace --namespace minecraft-operator-system \
  --set image.repository=ghcr.io/lexfrei/minecraft-operator \
  --set image.tag=dev

# Clean up
helm uninstall minecraft-operator --namespace minecraft-operator-system
```

### Common Makefile Commands

**Code generation:**

```bash
make manifests  # Generate CRDs to internal/crdmanager/crds/
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
# Single step — CRDs are embedded in the operator and applied at startup via server-side apply
helm install minecraft-operator ./charts/minecraft-operator \
  --create-namespace --namespace minecraft-operator-system
```

**Upgrade:**

```bash
# Upgrade operator (CRDs are automatically updated at startup)
helm upgrade minecraft-operator ./charts/minecraft-operator \
  --namespace minecraft-operator-system
```

**Uninstall:**

```bash
helm uninstall minecraft-operator --namespace minecraft-operator-system

# CRDs are NOT automatically removed (Kubernetes safety).
# To remove CRDs manually (WARNING: deletes all Plugin and PaperMCServer custom resources!):
# kubectl delete crd papermcservers.mc.k8s.lex.la plugins.mc.k8s.lex.la
```

**Useful Helm commands:**

```bash
# Dry-run to see generated manifests
helm install minecraft-operator ./charts/minecraft-operator \
  --dry-run --debug --namespace minecraft-operator-system

# Lint chart
helm lint ./charts/minecraft-operator

# List installed releases
helm list --namespace minecraft-operator-system

# Get values
helm get values minecraft-operator --namespace minecraft-operator-system
```

### Logging Standards

**This project uses stdlib `log/slog` for structured logging.**

**Configuration:**

The operator exposes two flags for logging configuration:
- `--log-level`: Set log level (`debug`, `info`, `warn`, `error`). Default: `info`
- `--log-format`: Set log format (`json`, `text`). Default: `json`

```bash
# Production (default): JSON format, Info level
./bin/manager

# Development: Text format, Debug level
./bin/manager --log-level=debug --log-format=text

# Silent: Error-only logging
./bin/manager --log-level=error --log-format=json
```

**slog Philosophy - Key-Value Structured Logging:**

All logging must follow slog best practices:

1. **Constant Message Strings**: Log messages MUST be constant, never interpolate variables into message text
2. **Variable Data in KV Pairs**: All variable data goes into separate key-value pairs
3. **Context Propagation**: Always use `slog.*Context(ctx, ...)` variants to propagate context

**Examples:**

❌ **BAD** (variable data in message):
```go
slog.InfoContext(ctx, fmt.Sprintf("Update to %s available", version))
slog.InfoContext(ctx, "Creating StatefulSet: " + name)
slog.ErrorContext(ctx, "Failed to get server "+key, "error", err)
```

✅ **GOOD** (constant message + KV pairs):
```go
slog.InfoContext(ctx, "Update available", "version", version)
slog.InfoContext(ctx, "Creating StatefulSet", "name", name)
slog.ErrorContext(ctx, "Failed to get server", "error", err, "key", key)
```

**Log Level Guidelines:**

- `slog.DebugContext()`: Detailed information for debugging (verbose)
- `slog.InfoContext()`: Normal operational messages (reconciliation events, resource creation)
- `slog.WarnContext()`: Warning conditions (deprecated features, potential issues)
- `slog.ErrorContext()`: Error conditions (failed operations, exceptions)

**Error Logging:**

Always include the error as a key-value pair with the key "error":
```go
slog.ErrorContext(ctx, "Failed to reconcile resource", "error", err, "resource", resourceName)
```

**Integration with controller-runtime:**

The operator bridges slog to controller-runtime's logr interface using `logr.FromSlogHandler()`.
This allows controller-runtime to use slog while maintaining compatibility with the Kubernetes ecosystem.

### Test-Driven Development (TDD)

**CRITICAL: This project follows strict TDD methodology for ALL code changes.**

TDD applies to:
- **Go code**: Controllers, packages, utilities
- **Helm charts**: Use `helm-unittest` for template testing

**TDD Workflow:**

1. **Write test FIRST** — Define expected behavior before implementation
2. **Run test → see it FAIL** — Confirms test is actually testing something
3. **Write minimal code** — Just enough to make test pass
4. **Run test → see it PASS** — Confirms implementation works
5. **Refactor** — Clean up while keeping tests green
6. **Repeat** — For each new feature or bug fix

**Go TDD Example:**

```go
// 1. Write test first (pkg/solver/simple_solver_test.go)
func TestSolverFindsCompatibleVersion(t *testing.T) {
    solver := NewSimpleSolver()
    result, err := solver.Solve(plugins, paperVersions)
    require.NoError(t, err)
    assert.Equal(t, "1.21.1", result.PaperVersion)
}

// 2. Run test → FAIL (function doesn't exist yet)
// 3. Implement Solve() method
// 4. Run test → PASS
// 5. Refactor if needed
```

**Helm TDD Example (using helm-unittest):**

```yaml
# charts/minecraft-operator/tests/deployment_test.yaml
suite: deployment tests
templates:
  - templates/deployment.yaml
tests:
  - it: should set correct replicas
    set:
      replicaCount: 3
    asserts:
      - equal:
          path: spec.replicas
          value: 3

  - it: should use non-root security context
    asserts:
      - equal:
          path: spec.template.spec.securityContext.runAsNonRoot
          value: true
```

**Why TDD is mandatory:**

- **Prevents regressions** — Tests catch breaking changes immediately
- **Documents behavior** — Tests serve as executable specifications
- **Improves design** — Writing tests first forces cleaner APIs
- **Enables refactoring** — Confidence to improve code without breaking it

**No exceptions**: Even "simple" changes require tests. If it's worth changing, it's worth testing.

### Development Workflow

**Typical development iteration (TDD-based):**

1. **Write failing test** for new feature/fix
2. **Modify API types** in `api/v1beta1/*.go` (if needed)
3. **Regenerate code**: `make manifests generate`
4. **Implement minimal code** to make test pass
5. **Run tests**: `make test` — must pass
6. **Check linting**: `make lint` (fix ALL errors before commit)
7. **Test locally**: `make run` (runs against your kubeconfig cluster)
   - Use `--log-level=debug --log-format=text` for better local debugging
8. **Commit changes** with semantic commit message and GPG signature
9. **Push to feature branch** (NEVER directly to master)
10. **Create PR** for review

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

- **Helm**: `helm lint charts/minecraft-operator`

**Building and Deploying:**

```bash
# Build and test locally
make manifests generate fmt vet build
./bin/manager  # Or use 'make run'

# Build container image
make container-build IMG=ghcr.io/lexfrei/minecraft-operator:v1.0.0

# Push to registry
make container-push IMG=ghcr.io/lexfrei/minecraft-operator:v1.0.0

# Deploy to cluster via Helm (single step — CRDs are embedded and applied at startup)
helm install minecraft-operator ./charts/minecraft-operator \
  --create-namespace --namespace minecraft-operator-system \
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
- **Logging**: stdlib log/slog with configurable level and format (ADR-028)
- **Container runtime**: Podman (per global CLAUDE.md)
- **Container base**: distroless/static-debian12:nonroot for security (ADR-006)
- **Error handling**: cockroachdb/errors for stack traces (ADR-007)
- **Deployment**: Helm-only approach with embedded CRDs applied at startup via server-side apply (ADR-005)
- **Datapack support**: Out of scope (no compatibility versions)
- **Paid plugins**: Nice-to-have, not in MVP (would need auth via Secret)
- **Manual approval**: Not implemented (fully automated updates with updateDelay)
- **Selector conflicts**: Resolved via version priority (latest > pin, highest semver for all-pin) (ADR-019)

See `.architecture.yaml` for complete ADR history and technical stack details.

## Important Notes

- **Single source of truth**: `.architecture.yaml` contains all technical decisions (ADRs), dependencies, and standards
- **NEVER use :latest Docker tag**: All update strategies resolve to concrete version-build tags (e.g., `docker.io/lexfrei/papermc:1.21.1-91`) for predictable deployments (ADR-029)
- **Four update strategies**: `latest`, `auto`, `pin`, `build-pin` for different use cases - see `docs/update-strategies.md` for detailed guide (ADR-029)
- **Breaking API changes**: v1.5.0 renamed fields: `paperVersion`→`version`, `paperBuild`→`build`, `versionPolicy`→`updateStrategy` (ADR-029)
- **NEVER push directly to master**: All changes via feature branches + PR
- **GPG signing**: Required for all commits (see global CLAUDE.md)
- **Linting**: ALL errors must be fixed before push, no exceptions
- **Logging**: Use stdlib slog with constant messages and KV pairs (see Logging Standards section)
- **API domain**: `mc.k8s.lex.la` (NOT `paperstack.io`)
- **License**: BSD-3-Clause
- **Main entrypoint**: `cmd/main.go` (not root `main.go`)
- **CRD generation**: Outputs to `internal/crdmanager/crds/` via `make manifests`
- **Deployment method**: Helm-only (no Kustomize)
- **Testing framework**: Ginkgo/Gomega (BDD style, controller-runtime standard)
- **E2E tests**: Auto-create/destroy Kind cluster named `minecraft-operator-test-e2e`
- **NOT high-availability**: 5-10 min downtime during updates is acceptable by design (ADR-015)

## Documentation

- **Architecture**: `DESIGN.md` - Complete architectural details, CRD schemas, solver algorithms
- **Technical decisions**: `.architecture.yaml` - ADRs, dependencies, tech stack
- **Update strategies**: `docs/update-strategies.md` - Comprehensive guide to version management strategies (ADR-029)
- **Project config**: `PROJECT` - Kubebuilder metadata
- **Development guide**: This file (`CLAUDE.md`)
- **Examples**: `examples/` - Example custom resources

## GitHub Issue Management

### Label System

This project uses a structured label system (borrowed from cloudflare-tunnel-gateway-controller):

**Type Labels (mutually exclusive):**

- `bug` - Something isn't working
- `enhancement` - New feature or request
- `documentation` - Improvements or additions to documentation
- `test` - Test coverage and testing
- `ci` - CI/CD and automation
- `security` - Security-related issues
- `research` - Research and RFC

**Priority Labels (prefix: `priority/`):**

- `priority/critical` - Blocks release, needs immediate attention
- `priority/high` - Important for milestone
- `priority/medium` - Should be done for milestone
- `priority/low` - Nice to have, can defer

**Status Labels (prefix: `status/`):**

- `status/needs-design` - Requires design/RFC
- `status/needs-triage` - Requires analysis
- `status/ready` - Ready to work on
- `status/in-progress` - Currently being worked on
- `status/blocked` - Blocked by dependency
- `status/needs-info` - Waiting for clarification from author
- `status/needs-review` - Waiting for review/feedback

**Area Labels (prefix: `area/`):**

- `area/controller` - Controller code
- `area/helm` - Helm chart
- `area/api` - CRD and API types
- `area/docs` - Documentation
- `area/plugins` - Plugin sources and resolution
- `area/solver` - Constraint solver
- `area/webui` - Web UI

**Size Labels (prefix: `size/`):**

- `size/XS` - < 1 hour
- `size/S` - 1-4 hours
- `size/M` - 1-2 days
- `size/L` - 3-5 days
- `size/XL` - > 1 week

**Standard GitHub Labels:**

- `good first issue` - Good for newcomers
- `help wanted` - Extra attention is needed
- `duplicate` - This issue or pull request already exists
- `wontfix` - This will not be worked on
- `dependencies` - Pull requests that update a dependency file
