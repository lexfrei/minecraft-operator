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

This project uses **Helm-first approach with separate CRD chart**: CRDs have independent lifecycle from the operator.

```text
minecraft-operator/
├── api/v1alpha1/                    # Go type definitions
├── controllers/                     # Reconciliation loops
├── pkg/                             # solver/, plugins/, rcon/, etc.
├── charts/
│   ├── minecraft-operator-crds/     # CRD chart (independent lifecycle)
│   │   ├── crds/                    # Generated by controller-gen
│   │   ├── Chart.yaml
│   │   └── values.yaml
│   └── minecraft-operator/          # Operator chart
│       ├── templates/               # Deployment, RBAC, Service, etc.
│       ├── Chart.yaml               # Depends on CRD chart
│       └── values.yaml              # crds.enabled: true (default)
└── main.go
```

### Common Commands

**Generate CRDs from Go types:**

```bash
controller-gen crd paths=./api/... output:crd:dir=./charts/minecraft-operator-crds/crds
```

**Generate deepcopy methods:**

```bash
controller-gen object paths=./api/...
```

**Install CRDs chart:**

```bash
helm install minecraft-operator-crds ./charts/minecraft-operator-crds --namespace minecraft-operator-system --create-namespace
```

**Deploy operator (with CRDs via dependency):**

```bash
# Update dependencies first
helm dependency update ./charts/minecraft-operator

# Install operator (will install CRDs if crds.enabled: true)
helm install minecraft-operator ./charts/minecraft-operator --namespace minecraft-operator-system
```

**Deploy operator without CRDs (if already installed):**

```bash
helm install minecraft-operator ./charts/minecraft-operator --namespace minecraft-operator-system --set crds.enabled=false
```

**Upgrade operator (CRDs are NOT upgraded by Helm):**

```bash
helm upgrade minecraft-operator ./charts/minecraft-operator --namespace minecraft-operator-system
```

**Upgrade CRDs (manual, if needed):**

```bash
kubectl apply --filename ./charts/minecraft-operator-crds/crds/
# OR
helm upgrade minecraft-operator-crds ./charts/minecraft-operator-crds --namespace minecraft-operator-system
```

**Uninstall:**

```bash
# Uninstall operator first
helm uninstall minecraft-operator --namespace minecraft-operator-system

# Then CRDs (WARNING: deletes all custom resources!)
helm uninstall minecraft-operator-crds --namespace minecraft-operator-system
```

**Run operator locally (for development):**

```bash
go run ./main.go
```

**Build operator image:**

```bash
podman build --tag ghcr.io/lexfrei/minecraft-operator:latest --file Containerfile .
```

### Development Steps

1. **Initialize with Kubebuilder** (if starting from scratch):

   ```bash
   kubebuilder init --domain mc.k8s.lex.la --repo github.com/lexfrei/minecraft-operator
   kubebuilder create api --group mc.k8s.lex.la --version v1alpha1 --kind Plugin --resource --controller
   kubebuilder create api --group mc.k8s.lex.la --version v1alpha1 --kind PaperMCServer --resource --controller
   ```

2. **Configure controller-gen to output to CRD chart**:
   - Use `output:crd:dir=./charts/minecraft-operator-crds/crds` in all CRD generation commands

3. **Create Helm chart structures**:

   ```bash
   # Create CRD chart
   helm create charts/minecraft-operator-crds
   rm -rf charts/minecraft-operator-crds/templates/*  # Remove default templates
   # CRDs will be in crds/ directory

   # Create operator chart
   helm create charts/minecraft-operator
   # Customize templates for operator deployment
   ```

4. **Configure operator chart dependency** in `charts/minecraft-operator/Chart.yaml`:

   ```yaml
   dependencies:
     - name: minecraft-operator-crds
       version: "0.1.0"
       repository: "file://../minecraft-operator-crds"
       condition: crds.enabled
   ```

5. **Add condition to values** in `charts/minecraft-operator/values.yaml`:

   ```yaml
   crds:
     enabled: true  # Set to false if CRDs are managed externally
   ```

6. **Implement controllers** following planned structure (DESIGN.md line 802-814)

7. **Testing Strategy**:
   - Unit tests: Constraint solver logic, version comparison
   - Integration tests: Kubernetes envtest for controller reconciliation
   - E2E tests: Real Paper server startup with plugin hot-swap

8. **Linting**:
   - Go: `golangci-lint run` (all errors MUST be fixed before push)
   - Markdown: `markdownlint` (configured in `.markdownlint.yaml`)
   - Helm: `helm lint ./charts/minecraft-operator-crds && helm lint ./charts/minecraft-operator`

## Design Decisions (Finalized)

- **Namespace scope**: Plugin and PaperMCServer are namespace-scoped
- **Datapack support**: Out of scope (no compatibility versions)
- **Paid plugins**: Nice-to-have, not in MVP (would need auth via Secret)
- **Manual approval**: Not implemented (fully automated updates)
- **Selector conflicts**: Resolved via version priority (see Architecture section)

## Important Notes

- **NEVER push directly to master**: All changes via feature branches + PR
- **API domain**: `mc.k8s.lex.la` (NOT `paperstack.io`)
- **License**: BSD-3-Clause
- **GPG signing**: Required for all commits (see global CLAUDE.md)
- See `DESIGN.md` for complete architectural details, CRD schemas, and solver algorithms
