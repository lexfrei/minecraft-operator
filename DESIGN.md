# Minecraft Operator - Kubernetes Operator for PaperMC

## Overview

Minecraft Operator is a Kubernetes operator for managing PaperMC servers with a focus on automatic version management for plugins and the server with compatibility guarantees.

### Goals

1. **Version Control**: Automatic tracking of PaperMC server and plugin updates
2. **Compatibility Guarantee**: Using a constraint solver to find the newest possible server version while ensuring compatibility with all plugins
3. **Predictable Updates**: Updates only occur during defined maintenance windows
4. **Graceful Updates**: Proper server shutdown before updates with sufficient time to save the world

### Non-Goals (Out of Scope)

- **High Availability**: This is NOT a HA system. 5-10 minute downtime during updates is normal for Minecraft servers. We do not promise "five nines" (99.999% uptime)
- World backups (this is a task for separate tools)
- Update rollbacks (world format updates are destructive)
- Performance monitoring (TPS, lag)
- Horizontal scaling
- Network policies or ingress management
- Zero-downtime updates

## Architecture

### Components

```text
┌─────────────────────────────────────────────────┐
│              K8s API Server                      │
│                                                   │
│  ┌──────────────┐       ┌──────────────────┐   │
│  │ Plugin CRD   │       │ PaperMCServer    │   │
│  │              │       │ CRD              │   │
│  └──────────────┘       └──────────────────┘   │
└────────────────┬────────────┬───────────────────┘
                 │            │
                 │   Watch    │
                 │            │
┌────────────────▼────────────▼───────────────────┐
│         Minecraft Operator                       │
│                                                   │
│  ┌─────────────────────────────────────────┐   │
│  │   Plugin Controller                      │   │
│  │   - Match servers via selector           │   │
│  │   - Fetch from plugin APIs               │   │
│  │   - Run solver for all matched servers   │   │
│  │   - Update Plugin status                 │   │
│  └─────────────────────────────────────────┘   │
│                                                   │
│  ┌─────────────────────────────────────────┐   │
│  │   PaperMCServer Controller               │   │
│  │   - Find matched plugins                 │   │
│  │   - Ensure pod/StatefulSet exists        │   │
│  │   - Run solver for Paper version         │   │
│  │   - Update Server status                 │   │
│  └─────────────────────────────────────────┘   │
│                                                   │
│  ┌─────────────────────────────────────────┐   │
│  │   Update Controller                      │   │
│  │   - Watch cron schedule                  │   │
│  │   - Graceful shutdown via RCON           │   │
│  │   - Update JARs                          │   │
│  │   - Restart pod                          │   │
│  └─────────────────────────────────────────┘   │
└────────────────┬────────────┬───────────────────┘
                 │            │
        ┌────────┴────────┐   │
        │                 │   │
┌───────▼──────┐  ┌──────▼───▼───────┐
│  Plugin APIs │  │  Minecraft Pods   │
│  - Hangar    │  │  + RCON           │
│  - Modrinth  │  │  + StatefulSet    │
│  - SpigotMC  │  │                   │
└──────────────┘  └───────────────────┘

Relationships:
Plugin --[instanceSelector]--> PaperMCServer (many-to-many)
PaperMCServer --[manages]--> StatefulSet/Pod (one-to-one)
```

### Component Interactions

#### 1. Plugin → PaperMCServer (declarative)

- Plugin defines `instanceSelector`
- Plugin Controller finds all matched servers
- Solver calculates the best version for all matched servers
- PaperMCServer Controller reads resolved versions from Plugin.status

#### 2. PaperMCServer → Plugin (reactive)

- When labels change on PaperMCServer
- Plugin Controller recalculates matching
- May require new solver solution

#### 3. Updates

- Trigger: Cron in PaperMCServer.spec.updateSchedule
- Update Controller coordinates the update:
  - Reads resolved versions from all matched Plugins
  - Reads availableUpdate from PaperMCServer.status
  - Performs graceful shutdown and update

### Custom Resource Definitions (CRDs)

#### Plugin CRD

Plugin is a plugin definition with source and versioning policy. The plugin selects which servers to apply to via `instanceSelector`.

```yaml
apiVersion: mc.k8s.lex.la/v1alpha1
kind: Plugin
metadata:
  name: essentialsx
  namespace: minecraft
spec:
  # Plugin source
  source:
    type: hangar  # Currently only hangar, planned: modrinth, spigot, url
    project: "EssentialsX"

  # Update strategy for version management
  # Valid values: latest, auto, pin, build-pin
  updateStrategy: latest  # Default: latest
  # If pin or build-pin, specify exact version:
  # version: "2.20.1"

  # Delay before applying new version (optional)
  # Allows community to test release before applying
  updateDelay: 168h  # 7 days after release (format: duration string)

  # Optional: network port that this plugin exposes (e.g., 8123 for Dynmap)
  # If specified, this port will be added to the Service of all matched servers (TCP+UDP)
  # port: 8123

  # Selector for choosing servers to apply plugin to
  instanceSelector:
    matchLabels:
      type: survival  # Applies to all servers with this label
    # Can use matchExpressions for complex conditions:
    # matchExpressions:
    #   - key: region
    #     operator: In
    #     values: ["eu", "us"]

  # Manual compatibility override (for edge cases)
  compatibilityOverride:
    enabled: false
    # If enabled: true, completely replaces metadata from API
    minecraftVersions: ["1.20.0", "1.20.4", "1.21.0"]

status:
  # Cached metadata from API
  availableVersions:
    - version: "2.20.1"
      minecraftVersions: ["1.20.0", "1.21.0"]
      downloadURL: "https://..."
      hash: "sha256:..."
      cachedAt: "2025-10-20T03:00:00Z"
      releasedAt: "2025-10-18T10:00:00Z"  # For updateDelay calculation
    - version: "2.20.0"
      minecraftVersions: ["1.19.0", "1.20.0"]
      downloadURL: "https://..."
      hash: "sha256:..."
      cachedAt: "2025-10-20T03:00:00Z"
      releasedAt: "2025-10-01T10:00:00Z"

  # Repository status
  repositoryStatus: available  # available, unavailable, orphaned
  lastFetched: "2025-10-20T03:00:00Z"

  # List of servers this plugin applies to
  matchedInstances:
    - name: survival-server
      namespace: minecraft
      version: "1.21.0"
      compatible: true
    - name: creative-server
      namespace: minecraft
      version: "1.20.4"
      compatible: true

  # Deletion progress (populated during plugin deletion with finalizer)
  # deletionProgress:
  #   - serverName: survival-server
  #     namespace: minecraft
  #     jarDeleted: false
  #     deletionRequestedAt: "2025-10-20T03:00:00Z"

  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: "2025-10-20T03:00:00Z"

    - type: RepositoryAvailable
      status: "True"

    - type: VersionResolved
      status: "True"
```

#### PaperMCServer CRD

PaperMCServer is a Minecraft server instance. Plugins are applied to it automatically through selectors in Plugin resources.

```yaml
apiVersion: mc.k8s.lex.la/v1alpha1
kind: PaperMCServer
metadata:
  name: survival-server
  namespace: minecraft
  labels:
    server: "example-server"
    type: survival
    region: eu
spec:
  # Update strategy for Paper version management
  # Valid values: latest, auto, pin, build-pin
  # - latest: always use newest available version from Docker Hub
  # - auto: use constraint solver to find best version compatible with plugins
  # - pin: pin to specific version, auto-update to latest build (requires version)
  # - build-pin: pin to specific version and build (requires version and build)
  updateStrategy: auto

  # Paper version (required for pin and build-pin strategies)
  # version: "1.21.1"

  # Paper build number (required for build-pin strategy)
  # build: 91

  # Delay before applying new Paper version (optional)
  # Allows community to test release before applying
  updateDelay: 72h  # 3 days after release (format: duration string)

  # Schedule for checking and applying updates
  updateSchedule:
    # Check for updates (daily at 3 AM)
    checkCron: "0 3 * * *"
    # Maintenance window (Sunday at 4 AM)
    maintenanceWindow:
      cron: "0 4 * * 0"
      enabled: true

  # Graceful shutdown settings
  gracefulShutdown:
    # Timeout for proper server shutdown
    # Server must save all chunks, player data, unload plugins
    # This value is set in pod's terminationGracePeriodSeconds
    timeout: 300s  # 5 minutes (default), can be increased for larger servers

  # RCON settings for graceful shutdown
  rcon:
    enabled: true
    passwordSecret:
      name: rcon-password
      key: password
    port: 25575

  # Service configuration (optional)
  service:
    type: LoadBalancer  # LoadBalancer, NodePort, or ClusterIP
    # annotations:
    #   metallb.universe.tf/loadBalancerIPs: "192.168.1.100"
    # loadBalancerIP: "192.168.1.100"

  # Pod settings (simplified, can be extended)
  podTemplate:
    spec:
      containers:
      - name: papermc
        # NOTE: Image is managed by operator. All update strategies resolve to
        # concrete version-build tags (e.g., docker.io/lexfrei/papermc:1.21.1-91).
        # The :latest tag is NEVER used.
        resources:
          requests:
            memory: "2Gi"
            cpu: "1000m"
          limits:
            memory: "4Gi"
            cpu: "2000m"

status:
  # Observed state (separate version and build fields)
  currentVersion: "1.21.0"
  currentBuild: 123

  # Desired state (resolved from updateStrategy)
  desiredVersion: "1.21.1"
  desiredBuild: 145

  # Plugins applied to this server via selectors
  plugins:
    - pluginRef:
        name: essentialsx
        namespace: minecraft
      resolvedVersion: "2.20.1"
      currentVersion: "2.20.0"
      desiredVersion: "2.20.1"
      compatible: true
      source: hangar
      installedJarName: "essentialsx.jar"

    - pluginRef:
        name: dynmap
        namespace: minecraft
      resolvedVersion: "3.7-beta-1"
      compatible: true
      source: hangar
      # pendingDeletion: false  # Set true when Plugin CRD is deleted

  # Available update (solver result)
  availableUpdate:
    version: "1.21.1"
    build: 145
    releasedAt: "2025-10-17T10:00:00Z"  # For updateDelay calculation
    plugins:
      - pluginRef:
          name: essentialsx
        version: "2.20.1"  # Stayed the same
      - pluginRef:
          name: dynmap
        version: "3.7-beta-2"
    foundAt: "2025-10-20T03:00:00Z"

  # Last update
  lastUpdate:
    appliedAt: "2025-10-13T04:00:00Z"
    previousVersion: "1.21.0"
    successful: true

  # Update blocked status (set when plugin compatibility prevents update)
  # updateBlocked:
  #   blocked: true
  #   reason: "Plugin 'dynmap' is incompatible with Paper 1.22.0"
  #   blockedBy:
  #     plugin: dynmap
  #     version: "3.7-beta-1"

  # Conditions (standard K8s pattern)
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: "2025-10-20T04:15:00Z"

    - type: StatefulSetReady
      status: "True"

    - type: UpdateAvailable
      status: "True"
      lastTransitionTime: "2025-10-20T03:00:00Z"
      message: "Update to 1.21.1 available"

    - type: UpdateBlocked
      status: "False"

    - type: Updating
      status: "False"
      lastTransitionTime: "2025-10-13T04:05:00Z"

    - type: CronScheduleValid
      status: "True"
```

#### Usage Examples

**Plugin for all survival servers:**

```yaml
apiVersion: mc.k8s.lex.la/v1alpha1
kind: Plugin
metadata:
  name: essentialsx-survival
spec:
  source:
    type: hangar
    project: "EssentialsX"
  updateStrategy: latest
  instanceSelector:
    matchLabels:
      type: survival
```

**Plugin for specific server only:**

```yaml
apiVersion: mc.k8s.lex.la/v1alpha1
kind: Plugin
metadata:
  name: custom-example-plugin
spec:
  source:
    type: hangar
    project: "SomePlugin"
  updateStrategy: pin
  version: "1.5.0"
  instanceSelector:
    matchLabels:
      server: "example-server"
```

**Different plugin versions for different servers:**

```yaml
# New version for production
apiVersion: mc.k8s.lex.la/v1alpha1
kind: Plugin
metadata:
  name: dynmap-prod
spec:
  source:
    type: hangar
    project: "Dynmap"
  updateStrategy: latest
  port: 8123  # Dynmap web interface port
  instanceSelector:
    matchLabels:
      env: production

---
# Old pinned version for testing
apiVersion: mc.k8s.lex.la/v1alpha1
kind: Plugin
metadata:
  name: dynmap-staging
spec:
  source:
    type: hangar
    project: "Dynmap"
  updateStrategy: pin
  version: "3.6.0"
  port: 8123
  instanceSelector:
    matchLabels:
      env: staging
```

## Constraint Solver

### Problem

For each Plugin resource, find the optimal plugin version that is compatible with **all** PaperMCServer instances matched via `instanceSelector`.

### Input Data

From platform APIs we receive (see `pkg/plugins/client.go`):

```go
type PluginVersion struct {
    Version           string
    ReleaseDate       time.Time
    PaperVersions     []string  // Compatible Paper versions
    MinecraftVersions []string  // Supported versions, e.g. ["1.20.0", "1.20.4", "1.21.0"]
    DownloadURL       string
    Hash              string
}
```

Paper versions are fetched via the Paper API client (`pkg/paper/`).

### Algorithm

#### For Plugin with updateStrategy: latest

1. **Collect matched servers:**
   - Find all PaperMCServer resources via `instanceSelector`
   - Get their current Paper versions

2. **Filter versions by updateDelay:**
   - If `updateDelay` is set, filter out versions newer than `(now - updateDelay)`
   - This allows the community to test releases before automatic application

3. **Build constraint model:**

   ```text
   Given:
   - servers = [server1(Paper 1.20.4), server2(Paper 1.21.0), server3(Paper 1.21.0)]
   - plugin_versions = [v1.0(MC 1.19-1.20), v2.0(MC 1.20-1.21), v2.1(MC 1.21)]
   - updateDelay = 168h (7 days)
   - now = 2025-10-20

   Filtering:
   - v2.1 released 2025-10-18 → (now - releasedAt) = 2 days < 7 days → SKIP
   - v2.0 released 2025-10-01 → (now - releasedAt) = 19 days > 7 days → OK

   Constraint:
   - Find MAX plugin_version (after filtering) such that:
     ∀ server ∈ servers: compatible(plugin_version, server.paperVersion) == true

   Solution:
   - v2.1: SKIP (too new due to updateDelay)
   - v2.0: YES (compatible with all: 1.20.4, 1.21.0, 1.21.0)

   Result: v2.0
   ```

4. **Application:**
   - All matched servers receive the same plugin version
   - If no solution found - warning in Plugin status

#### For Plugin with updateStrategy: pin

1. **Check compatibility:**
   - Find matched servers
   - Verify that `version` is compatible with all

2. **Result:**
   - If compatible - apply to all
   - If not - warning in status, but don't block (user made pin decision)

#### For PaperMCServer

When updating the server itself:

1. **Collect plugins:**
   - Find all Plugin resources whose `instanceSelector` matches this server

2. **Filter Paper versions by updateDelay:**
   - If `updateDelay` is set in PaperMCServer spec, filter Paper versions newer than `(now - updateDelay)`
   - This allows waiting for stable builds after release

3. **Build constraint model:**

   ```text
   Given:
   - current_paper = "1.20.4"
   - available_paper = ["1.20.4", "1.21.0", "1.21.1"]
   - plugins = [plugin1, plugin2, plugin3] (matched via selectors)
   - updateDelay = 72h (3 days)
   - now = 2025-10-20

   Filter Paper:
   - 1.21.1 released 2025-10-18 → (now - releasedAt) = 2 days < 3 days → SKIP
   - 1.21.0 released 2025-09-15 → (now - releasedAt) = 35 days > 3 days → OK

   Constraint:
   - Find MAX paper_version (after filtering) such that:
     ∀ plugin ∈ plugins: ∃ plugin_version compatible with paper_version

   Objective:
   - maximize(paper_version)
   ```

4. **Solution:**
   - If all plugins have versions for new Paper (after updateDelay filtering) - update
   - If at least one plugin is incompatible - stay on current or suggest maximum possible

### Handling Edge Cases

#### Version Conflict

```yaml
# We have 3 servers: server1(1.20), server2(1.21), server3(1.21)
# Plugin has: v1.0(up to 1.20), v2.0(only 1.21+)
# Solution: v1.0 for all (maximum compatible with ALL)
```

If v2.0 is needed on new servers - options:

1. Update server1 to Paper 1.21
2. Create two Plugin resources with different selectors:

   ```yaml
   Plugin dynmap-legacy:
     updateStrategy: pin
     version: "1.0"
     instanceSelector:
       matchLabels:
         server: server1

   Plugin dynmap-new:
     updateStrategy: latest
     instanceSelector:
       matchExpressions:
         - key: server
           operator: In
           values: [server2, server3]
   ```

#### Plugin without version specification in API

- Assume compatibility with any version
- Log warning
- If `compatibilityOverride` exists - use it

#### Plugin with upper bound (e.g., "up to 1.20.4")

- If Paper > max_version, but this is latest plugin release - log warning
- Don't block automatically (author may not have updated metadata)
- User can use `compatibilityOverride` for explicit specification

#### Repository unavailable (orphaned)

- Use cached data from `status.availableVersions`
- Don't update plugin while repository is unavailable
- Warning in status: "Using cached metadata, repository unavailable"

#### Multiple selectors match one server

- All matched plugins are applied
- If plugin with same name matched multiple times - error, selectors need fixing

### Implementation

```go
// Solver interface (pkg/solver/solver.go)
type Solver interface {
    // FindBestPluginVersion finds the maximum plugin version compatible with ALL matched servers.
    FindBestPluginVersion(
        ctx context.Context,
        plugin *mcv1alpha1.Plugin,
        servers []mcv1alpha1.PaperMCServer,
        allVersions []plugins.PluginVersion,
    ) (string, error)

    // FindBestPaperVersion finds the maximum Paper version compatible with ALL matched plugins.
    FindBestPaperVersion(
        ctx context.Context,
        server *mcv1alpha1.PaperMCServer,
        matchedPlugins []mcv1alpha1.Plugin,
        paperVersions []string,
    ) (string, error)
}
```

Currently implemented as simple linear search (`SimpleSolver`). SAT solver planned for Phase 2.

## Update Flow

### 1. Plugin Reconciliation Loop

```text
┌─────────────────────────────────────────┐
│  Watch Plugin CRD changes                │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Find all matched PaperMCServer          │
│  via instanceSelector                    │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Fetch plugin metadata from API          │
│  (Hangar/Modrinth/etc, with caching)     │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Apply compatibilityOverride if enabled  │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Run solver to find best version         │
│  compatible with ALL matched servers     │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Update Plugin status:                   │
│  - resolvedVersion                       │
│  - matchedInstances                      │
│  - availableVersions (cache)             │
│  - conditions                            │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Trigger reconciliation of matched       │
│  PaperMCServer instances                 │
└─────────────────────────────────────────┘
```

### 2. PaperMCServer Reconciliation Loop

```text
┌─────────────────────────────────────────┐
│  Watch PaperMCServer CRD changes         │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Ensure pod/StatefulSet exists           │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Find all Plugin resources that match    │
│  this server via their selectors         │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Update status.plugins with resolved     │
│  versions from matched Plugins           │
└────────────────┬────────────────────────┘
                 │
                 ▼
         Check updateSchedule.checkCron
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Fetch Paper metadata (available builds) │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Run solver to find max Paper version    │
│  compatible with all matched plugins     │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Update status.availableUpdate           │
│  Set condition UpdateAvailable           │
└─────────────────────────────────────────┘
```

### 3. Update Process (Scheduled maintenanceWindow)

```text
┌─────────────────────────────────────────┐
│  Cron triggers maintenance window        │
└────────────────┬────────────────────────┘
                 │
                 ▼
        status.availableUpdate exists?
                 │
                 ├─ No → Skip
                 │
                 ▼ Yes
┌─────────────────────────────────────────┐
│  Check updateDelay:                      │
│  If (now - releasedAt) < updateDelay     │
│  → Skip, not ready yet                   │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Set condition Updating = True           │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Download new Paper JAR (if changed)     │
│  Download new plugin JARs from Plugin    │
│  resources' resolvedVersion              │
│  Place them in PVC mounted volume        │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Copy plugin JARs to plugins/update/     │
│  (Paper automatically applies them on    │
│   next start)                            │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  kubectl delete pod <pod-name>           │
│  (explicit pod deletion)                 │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  K8s sends SIGTERM to pod                │
│  Waits terminationGracePeriodSeconds     │
│  (default 300s / 5 minutes)              │
│  Paper properly saves the world          │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  StatefulSet automatically creates       │
│  new pod with same name and PVC          │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Paper starts:                           │
│  - Sees files in plugins/update/         │
│  - Moves them to plugins/                │
│  - Removes old versions                  │
│  - Loads updated plugins                 │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Wait for pod Ready                      │
│  Check readiness probe                   │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│  Update status.lastUpdate                │
│  Set condition Updating = False          │
│  Set condition Ready = True              │
└─────────────────────────────────────────┘
```

**Important Notes:**

- **Not a high-availability system**: 5-10 minute downtime during updates is normal for a Minecraft server
- **plugins/update/ mechanism**: Standard Paper/Bukkit method for hot-swapping plugins without manual deletion of old versions
- **terminationGracePeriodSeconds**: CRITICALLY important! Don't set too low, or the world may be corrupted. For large servers can be set to 10+ minutes
- **PVC persists**: Even when deleting pod, PVC remains and is remounted to the new pod
- **Atomicity**: If pod fails to start after update - StatefulSet will retry with exponential backoff

### 4. Cross-Resource Triggers

#### Plugin changed → Trigger PaperMCServer reconciliation

- When Plugin spec changes (source, updateStrategy, selector)
- When plugin metadata (availableVersions) changes in Plugin status
- All matched servers must be recalculated

#### PaperMCServer changed → Trigger Plugin reconciliation

- When labels change (may change matching)
- When spec.version or spec.updateStrategy changes
- All Plugins with selectors that might match this server

#### Optimization

- Use owner references where possible
- Cache selector matching results
- Debounce multiple reconciliations

## Technical Details

### Project Structure (Go)

```text
minecraft-operator/
├── api/
│   └── v1alpha1/
│       ├── plugin_types.go           # Plugin CRD types
│       ├── papermcserver_types.go    # PaperMCServer CRD types
│       └── zz_generated.deepcopy.go
├── cmd/
│   └── main.go                       # Operator entrypoint
├── internal/
│   └── controller/
│       ├── constants.go              # Shared constants (strategies, finalizers, annotations)
│       ├── plugin_controller.go      # Reconciliation for Plugin
│       ├── papermcserver_controller.go # Reconciliation for PaperMCServer
│       └── update_controller.go      # Maintenance window updates
├── pkg/
│   ├── api/                          # API utilities
│   ├── cron/                         # Cron scheduler abstraction
│   ├── paper/                        # Paper API client for versions
│   ├── plugins/
│   │   ├── client.go                 # Common interface for plugin APIs
│   │   ├── hangar.go                 # Hangar API client (using go-hungar)
│   │   └── cache.go                  # API response caching
│   ├── rcon/
│   │   └── client.go                 # RCON client (gorcon/rcon wrapper)
│   ├── registry/                     # Docker registry API client
│   ├── selector/                     # Label selector matching logic
│   ├── service/                      # Service utilities
│   ├── solver/
│   │   ├── solver.go                 # Constraint solver interface
│   │   └── simple_solver.go          # Linear search implementation
│   ├── testutil/                     # Test helpers
│   ├── version/                      # Minecraft version comparison
│   └── webui/                        # Web UI for server status
├── charts/
│   ├── minecraft-operator-crds/      # CRD chart (separate lifecycle)
│   │   ├── crds/                     # Generated CRDs (controller-gen output)
│   │   ├── Chart.yaml
│   │   └── values.yaml
│   └── minecraft-operator/           # Operator chart
│       ├── templates/                # Helm templates (Deployment, RBAC, etc.)
│       ├── Chart.yaml                # With dependency on CRD chart
│       └── values.yaml               # crds.enabled: true (default)
└── examples/                         # Example CRs for testing
```

**Note**: CRDs are in a separate chart with independent lifecycle. Operator chart depends on CRD chart via `Chart.yaml` dependencies with `condition: crds.enabled` for optional installation.

### Libraries

- **controller-runtime**: Framework for writing operators
- **client-go**: Kubernetes client
- **github.com/lexfrei/go-hungar**: Hangar API client for PaperMC plugin repository
- **cockroachdb/errors**: Error handling with stack traces
- **gorcon/rcon**: RCON client for Minecraft server communication
- **robfig/cron**: Cron scheduling
- **Masterminds/semver**: Version comparison
- **log/slog**: Structured logging (Go stdlib)

### Container Images and Charts

- **Docker Image**: [lexfrei/papermc](https://hub.docker.com/r/lexfrei/papermc) - Custom PaperMC container image (source: [papermc-docker](https://github.com/lexfrei/papermc-docker))
- **Helm Chart**: `oci://ghcr.io/lexfrei/charts/papermc` - Published to GitHub Container Registry as OCI artifact

### Custom Image and Chart Integration

Since both the Docker image and Helm chart are maintained within the project, they can be modified and extended as needed for operator requirements:

**Customization capabilities:**

- **Docker Image** (`lexfrei/papermc`): Can be extended with operator-specific features
  - Currently provides standard PaperMC functionality
  - Future extensions possible: RCON API wrappers, metrics exporters, plugin management endpoints
  - Source: <https://github.com/lexfrei/papermc-docker>

- **Helm Chart** (`oci://ghcr.io/lexfrei/charts/papermc`): Can be adapted for operator-managed deployments
  - Configurable for operator's StatefulSet requirements
  - Can include operator-specific labels, annotations, and resource templates
  - Source: <https://github.com/lexfrei/papermc-docker>

**Integration approach:**

- Operator manages PaperMCServer resources declaratively
- Operator creates/updates StatefulSets using the custom image
- Chart can be used as reference or for standalone deployments
- Image and operator evolve together based on operational requirements

### Selector Matching

Use standard K8s label selector logic:

```go
import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/labels"
)

func MatchesSelector(server *PaperMCServer, plugin *Plugin) (bool, error) {
    selector, err := metav1.LabelSelectorAsSelector(&plugin.Spec.InstanceSelector)
    if err != nil {
        return false, err
    }

    return selector.Matches(labels.Set(server.Labels)), nil
}
```

### JAR File Storage and Update Mechanism

#### Architecture: PVC + plugins/update/ folder

Paper/Bukkit servers have a built-in hot-swap mechanism for plugins through a special `plugins/update/` folder:

1. On startup, server checks `plugins/update/`
2. All JARs from `plugins/update/` are moved to `plugins/`
3. Old plugin versions are automatically removed
4. `plugins/update/` folder is cleared

#### Volume Structure

```yaml
# PVC for each server
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: survival-server-data
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 10Gi  # For world data + plugins

---
# StatefulSet
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: survival-server
spec:
  template:
    spec:
      terminationGracePeriodSeconds: 300  # 5 minutes for graceful shutdown

      containers:
      - name: papermc
        # Image is managed by operator using concrete version-build tags.
        # Example: docker.io/lexfrei/papermc:1.21.1-91
        # The :latest tag is NEVER used.
        image: docker.io/lexfrei/papermc:1.21.1-91
        volumeMounts:
        - name: data
          mountPath: /data
          # Structure:
          # /data/
          #   plugins/         - current plugins
          #   plugins/update/  - new plugins to replace
          #   world/           - main world
          #   server.jar       - Paper JAR

      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: survival-server-data
```

**Update process by operator (actual implementation):**

```text
1. Delete plugins marked for PendingDeletion (rm via exec in pod)
2. Download plugin JARs via exec in pod:
   - curl -fsSL -o /data/plugins/update/{name}.jar {downloadURL}
   - Verify SHA256 checksum
3. For Paper version change: update StatefulSet container image tag
4. RCON graceful shutdown (warn players, save-all, stop)
5. Delete pod via Kubernetes API
6. StatefulSet recreates pod with same PVC
7. Paper starts and applies plugins from /data/plugins/update/
```

#### Verified Behavior

- ✅ `plugins/update/` folder: Paper creates it on startup if missing
- ✅ Automatic deletion of old versions: Paper matches by `plugin.yml` name, not JAR filename (since PR #6575, April 2022)
- ⚠️ Plugin dependencies during hot-swap: not handled by Paper, load order may vary

### API Metadata Caching

**Two-level caching:**

1. **In-memory cache** (`CachedPluginClient` in `pkg/plugins/cache.go`):
   - TTL-based wrapper around `PluginClient` interface
   - Caches `GetVersions` and `GetCompatibility` results
   - Reset on operator restart

2. **Persistent cache in Plugin.status**:
   - Plugin controller stores fetched versions in `status.availableVersions`
   - Fallback when repository is unavailable (status set to `orphaned`)
   - Each entry includes `cachedAt` timestamp

**Caching flow in Plugin controller:**

1. Fetch from repository API (through `CachedPluginClient`)
2. On success: update `status.availableVersions`, set `repositoryStatus: available`
3. On failure with cache: use `status.availableVersions`, set `repositoryStatus: orphaned`
4. On failure without cache: set `repositoryStatus: unavailable`, requeue after 5 minutes

## Monitoring and Observability

### Metrics (Prometheus)

**Plugin metrics:**

```text
minecraft_operator_plugin_reconcile_duration_seconds{plugin="essentialsx"}
minecraft_operator_plugin_matched_servers_total{plugin="essentialsx"}
minecraft_operator_plugin_resolved_version_info{plugin="essentialsx", version="2.20.1"}
minecraft_operator_plugin_repository_status{plugin="essentialsx", status="available"}
minecraft_operator_plugin_api_fetch_errors_total{plugin="essentialsx", source="hangar"}
```

**PaperMCServer metrics:**

```text
minecraft_operator_server_reconcile_duration_seconds{server="survival-server"}
minecraft_operator_server_solver_duration_seconds{server="survival-server"}
minecraft_operator_server_update_success_total{server="survival-server"}
minecraft_operator_server_update_failure_total{server="survival-server"}
minecraft_operator_server_paper_version_info{server="survival-server", version="1.21.0"}
minecraft_operator_server_plugins_total{server="survival-server"}
minecraft_operator_server_rcon_connection_status{server="survival-server"}
```

### Events (K8s Events)

**Plugin events:**

```text
Normal  Resolved            Plugin essentialsx resolved to version 2.20.1
Normal  MatchedServers      Plugin essentialsx matched 3 servers
Warning IncompatibleVersion No compatible version found for all servers
Warning RepositoryUnavailable Hangar API unavailable, using cached data
Normal  OverrideApplied     Compatibility override applied for essentialsx
```

**PaperMCServer events:**

```text
Normal  UpdateAvailable     New Paper version 1.21.1 available
Normal  UpdateStarted       Starting maintenance update
Normal  PluginsResolved     Resolved 5 plugins for this server
Normal  PluginDownloaded    Downloaded plugin EssentialsX 2.20.1
Warning PluginIncompatible  Plugin dynmap has no compatible version for Paper 1.21.1
Normal  UpdateCompleted     Update successful, server restarted
Warning UpdateFailed        Update failed: RCON timeout
Warning NoPluginsMatched    No plugins matched via selectors
```

## Security

### RBAC

**Operator requires following permissions:**

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: minecraft-operator
rules:
# CRD management
- apiGroups: ["mc.k8s.lex.la"]
  resources: ["plugins", "papermcservers"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["mc.k8s.lex.la"]
  resources: ["plugins/status", "papermcservers/status"]
  verbs: ["get", "update", "patch"]

# Pod and StatefulSet management
- apiGroups: ["apps"]
  resources: ["statefulsets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch", "delete"]
- apiGroups: [""]
  resources: ["pods/exec"]
  verbs: ["create"]  # For RCON via kubectl exec if needed

# Storage management
- apiGroups: [""]
  resources: ["persistentvolumeclaims"]
  verbs: ["get", "list", "watch", "create", "update", "patch"]

# Secrets for RCON passwords
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "list", "watch"]

# Events
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
```

#### Additional

- **RCON password**: Via Secret, not in plaintext in spec
- **API keys** (for CurseForge in future): Via Secret
- **Network policies**: Operator can only access:
  - Kubernetes API
  - Plugin repository APIs (Hangar, Modrinth, etc)
  - RCON port of pods (25575)

## Future Development

### Phase 1 (MVP)

- ✅ Plugin CRD with instanceSelector
- ✅ PaperMCServer CRD with labels
- ✅ Basic reconcile loop for both resources
- ✅ Hangar API integration
- ✅ Simplified solver (linear search)
- ✅ RCON graceful shutdown
- ✅ Cron-based updates
- ✅ Persistent cache in status

### Phase 2

- Modrinth and SpigotMC support
- SAT solver integration for optimization
- Enhanced Web UI (basic web UI exists in `pkg/webui/`)
- Webhook validation for CRDs (validate selector correctness)
- URL-based plugin support with manual compatibility override

### Phase 3

- Plugin dependency support (one plugin requires another)
- Canary deployments (update one server with canary label, verify, then others)
- Automatic compatibility testing (spin up test server)
- Integration tests via Minecraft server startup suite
- Metrics dashboard (Grafana)

### Phase 4

- Multi-cluster management (federation)
- GitOps integration (Flux/ArgoCD support)
- Backup/restore integration for world data
- Advanced scheduling (blue-green updates, per-server maintenance windows)
- Plugin dependency graph visualization

## Design Decisions

### Finalized Decisions

1. **Namespace scope for Plugin**: ✅ **namespace-scoped**
   - Plugin resources are namespaced for isolation
   - Each namespace manages its own plugin catalog

2. **Datapack support**: ❌ **Out of scope**
   - Minecraft datapacks are not supported in current design
   - They don't have compatibility versions
   - May be reconsidered as separate feature in future

3. **Paid plugins**: 🔮 **Nice to have, future consideration**
   - SpigotMC premium plugins support is planned but not in MVP
   - Would require authentication for download via Secret with API key
   - Not blocking current implementation

4. **Manual approval for updates**: ❌ **Not implemented**
   - No manual approval mechanism in current design
   - Updates are fully automated based on schedules and constraints
   - If needed in future, could be added via annotation like `mc.k8s.lex.la/auto-update: "false"`

5. **Selector conflicts**: ✅ **Resolved via version priority**

   **Problem scenario:**

   ```yaml
   # Plugin 1: latest version
   Plugin essentialsx-latest:
     project: "EssentialsX"
     updateStrategy: latest  # → 2.20.1
     instanceSelector:
       matchLabels:
         type: survival

   # Plugin 2: pinned old version
   Plugin essentialsx-legacy:
     project: "EssentialsX"  # SAME plugin!
     updateStrategy: pin
     version: "2.19.0"
     instanceSelector:
       matchLabels:
         type: survival      # SAME servers!
   ```

   **Resolution strategy:**

   When multiple Plugin resources with the same `source.project` match the same servers:

   1. **If any Plugin has `updateStrategy: latest`**: Use constraint solver
      - The solver automatically finds the best compatible version
      - Pinned versions from other Plugins are treated as constraints
      - Result: optimal version that satisfies all compatibility requirements

   2. **If all Plugins have `updateStrategy: pin`**: Use newest version
      - Compare semantic versions (e.g., 2.20.1 > 2.19.0)
      - The highest version wins
      - Warning event is emitted about the conflict

   **Example outcomes:**
   - `latest` + `pinned 2.19.0` → Solver picks compatible version (likely latest if compatible)
   - `pinned 2.20.1` + `pinned 2.19.0` → 2.20.1 wins (newer version)
   - `latest` + `latest` → Solver picks best version (both are identical, no conflict)

## Implementation Status

### Current Implementation vs Design

As of 2026-02, all core components described in the design are fully implemented.

#### Implemented Components (100%)

- ✅ **API Types (CRDs)**: Fully implemented
  - Plugin CRD with all spec and status fields
  - PaperMCServer CRD with all required fields including `version`, `build`, `updateStrategy`
  - Four update strategies: `latest`, `auto`, `pin`, `build-pin`

- ✅ **Plugin Controller**: Complete
  - Fetches plugin metadata from repositories (Hangar)
  - Finds matched servers via label selectors
  - Updates Plugin status with available versions and matched instances
  - Handles orphaned state when repository unavailable
  - Finalizer-based JAR cleanup on Plugin deletion

- ✅ **PaperMCServer Controller**: Complete
  - Ensures StatefulSet and Service exist
  - Finds matched plugins via selectors
  - Resolves plugin versions per-server using solver
  - Resolves desired Paper version based on update strategy
  - Detects available updates and sets conditions
  - Verifies image existence in Docker Hub registry

- ✅ **Update Controller**: Complete
  - Cron-based scheduling for maintenance windows
  - Apply-now annotation for immediate updates
  - RCON graceful shutdown before updates
  - Plugin JAR downloads to `/data/plugins/update/`
  - StatefulSet image update for Paper version changes
  - Pod deletion to trigger StatefulSet recreation
  - Plugin deletion (PendingDeletion cleanup)
  - Update history tracking in server status

- ✅ **Cross-Resource Triggers**: Fully implemented
  - Plugin changes trigger PaperMCServer reconciliation
  - PaperMCServer label changes trigger Plugin reconciliation

- ✅ **Supporting Packages**: All implemented
  - `pkg/solver/` - Constraint solver (simple linear search)
  - `pkg/plugins/` - Plugin API clients (Hangar)
  - `pkg/paper/` - Paper API client
  - `pkg/rcon/` - RCON client (gorcon/rcon wrapper)
  - `pkg/registry/` - Docker registry API client
  - `pkg/selector/` - Label selector matching
  - `pkg/version/` - Minecraft version comparison
  - `pkg/cron/` - Cron scheduler abstraction
  - `pkg/webui/` - Web UI for server status

#### Structural Notes

- **Controllers location**: `internal/controller/` (standard Go practice)
- **Main entrypoint**: `cmd/main.go` (recommended structure)
- **Service creation**: Automatic Service management for each PaperMCServer
- **Version resolution**: Plugin version resolution is per-server (in PaperMCServer controller), not global (Plugin controller only caches metadata)

## Glossary

- **PaperMC**: High-performance Spigot Minecraft server fork
- **RCON**: Remote Console protocol for remote server management
- **Constraint Solver**: Algorithm for finding solutions considering constraints
- **Graceful Shutdown**: Proper shutdown with state preservation
