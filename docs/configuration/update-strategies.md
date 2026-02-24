# Update Strategies

This document describes the update strategies available in the minecraft-operator for managing Paper server versions and plugin versions.

## Overview

The minecraft-operator provides flexible update strategies that allow you to control how your PaperMC servers and plugins are updated. Both `PaperMCServer` and `Plugin` resources support update strategies, giving you fine-grained control over version management.

**Key principles:**

- **Never uses `:latest` Docker tags** - The operator always resolves to concrete version-build combinations (e.g., `1.21.1-91`)
- **Unified approach** - Similar concepts across both server and plugin resources
- **Compatibility-first** - The constraint solver ensures all components work together
- **Scheduled updates** - Updates only happen during configured maintenance windows

## Version and Build Concepts

Understanding the difference between versions and builds is essential:

### Paper Versioning

Paper uses a two-part versioning scheme:

- **Version**: The Minecraft version (e.g., `1.21.1`, `1.20.4`)
- **Build**: The Paper build number for that version (e.g., `91`, `115`)
- **Docker tag**: Combines both as `{version}-{build}` (e.g., `1.21.1-91`)

Example timeline for Paper 1.21.1:

- `1.21.1-86` (initial release)
- `1.21.1-87` (bug fixes)
- `1.21.1-91` (latest build)

**Build updates** are typically bug fixes and performance improvements without gameplay changes.
**Version updates** introduce new Minecraft features and may break plugin compatibility.

### Plugin Versioning

Plugins typically use semantic versioning:

- Standard semver: `2.5.0`, `1.18.2`, `3.0.1`
- Some plugins include builds: `2.5.0-build.123`

The operator fetches version metadata from plugin repositories (Hangar, Modrinth, etc.) to determine compatibility.

## PaperMCServer Update Strategies

The `PaperMCServer` resource supports four update strategies via the `updateStrategy` field.

### Strategy: `latest`

**Purpose**: Always run the newest available Paper version from Docker Hub.

**Behavior**:

- Automatically updates to the latest Paper version (both version and build)
- Ignores plugin compatibility constraints
- Best for bleeding-edge testing or vanilla servers without plugins

**Version resolution**:

- Queries Docker Hub for the latest available Paper image tag
- Resolves to the highest version-build combination (e.g., `1.21.1-91`)

**Update behavior**:

- Updates happen during maintenance windows
- Both version upgrades (1.21.1 → 1.21.2) and build upgrades (91 → 92) are automatic

**Use cases**:

- Test/development environments
- Vanilla servers (no plugins)
- Bleeding-edge experimental servers
- When you want the absolute latest features

**Example**:

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: test-server
spec:
  updateStrategy: "latest"
  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"
```

**Important notes**:

- May break plugin compatibility
- Suitable for servers without plugins or with very flexible plugins
- Always resolves to concrete tag (never uses `:latest` Docker tag)

### Strategy: `auto`

**Purpose**: Automatically pick the best Paper version compatible with all matched plugins.

**Behavior**:

- Uses the constraint solver to find the maximum Paper version where ALL plugins have compatible versions
- Balances staying up-to-date with maintaining plugin compatibility
- The solver considers both Paper versions and plugin version compatibility

**Version resolution**:

1. Fetch all available Paper versions
2. For each Paper version (newest first), check if ALL matched plugins have at least one compatible version
3. Return the highest Paper version that satisfies all plugin constraints
4. Respects `updateDelay` if configured

**Update behavior**:

- Automatic version updates when solver finds a better solution
- Updates happen during maintenance windows
- Both version and build updates are automatic
- Blocks updates if no compatible solution exists

**Use cases**:

- Production servers with multiple plugins
- When you want automatic updates but need plugin compatibility
- Servers where you trust the solver to make good decisions
- Balanced approach between stability and staying current

**Example**:

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: production-server
  labels:
    environment: production
    type: modded
spec:
  updateStrategy: "auto"
  updateDelay: 168h  # Wait 7 days before applying updates
  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"
```

**How it works on initial deployment**:

On first deployment, the solver analyzes all matched plugins and selects the best Paper version. The operator then sets the StatefulSet image to the resolved version-build tag (e.g., `lexfrei/papermc:1.21.1-91`).

**Status tracking**:

The operator maintains detailed status information:

```yaml
status:
  currentVersion: "1.21.1"
  currentBuild: 91
  desiredVersion: "1.21.1"
  desiredBuild: 91
  availableUpdate:
    version: "1.21.2"
    build: 115
    releasedAt: "2024-01-15T10:00:00Z"
    foundAt: "2024-01-22T03:00:00Z"
```

### Strategy: `pin`

**Purpose**: Pin to a specific Paper version but automatically update to the latest build.

**Behavior**:

- Stays on the specified Paper version (e.g., `1.21.1`)
- Automatically updates to newer builds of that version (e.g., `91 → 92 → 95`)
- Build updates are typically safe (bug fixes, performance improvements)

**Version resolution**:

- Uses the Paper version specified in `version` field
- Finds the latest build available for that version
- Validates compatibility with matched plugins (for the pinned version)

**Update behavior**:

- No version updates (stays on pinned version)
- Automatic build updates during maintenance windows
- If you want to change versions, update the `version` field manually

**Use cases**:

- Production servers that need stability on a specific Minecraft version
- When plugins only support a specific version range
- Gradual migration to new versions (manually update `version` when ready)
- When you want bug fixes but not feature changes

**Example**:

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: stable-server
spec:
  updateStrategy: "pin"
  version: "1.21.1"  # Required for pin strategy
  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"
```

**Version changes**:

To upgrade to a new version, edit the spec:

```bash
kubectl patch papermcserver stable-server --type=merge -p '{"spec":{"version":"1.21.2"}}'
```

The operator will then resolve the latest build for 1.21.2 during the next maintenance window.

### Strategy: `build-pin`

**Purpose**: Pin to both a specific Paper version AND build - no automatic updates.

**Behavior**:

- Completely static configuration
- No automatic updates whatsoever
- Full manual control over both version and build

**Version resolution**:

- Uses exact `version` and `build` specified in spec
- Validates the version-build combination exists
- Checks plugin compatibility for information purposes

**Update behavior**:

- No automatic updates
- To update, manually change `version` and/or `build` fields
- Useful for maximum stability and predictability

**Use cases**:

- Extremely sensitive production environments
- Certification/compliance requirements
- When you need exact reproducibility
- Regression testing against specific builds
- When any change requires approval/testing

**Example**:

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: certified-server
spec:
  updateStrategy: "build-pin"
  version: "1.21.1"   # Required
  build: 91            # Required
  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"
```

**Manual updates**:

```bash
# Update to a new build
kubectl patch papermcserver certified-server --type=merge \
  -p '{"spec":{"build":95}}'

# Update to a new version and build
kubectl patch papermcserver certified-server --type=merge \
  -p '{"spec":{"version":"1.21.2","build":115}}'
```

## Plugin Update Strategies

The `Plugin` resource uses the same `updateStrategy` field as `PaperMCServer`, providing a unified approach to version management.

### Strategy: `latest`

**Purpose**: Automatically use the latest plugin version compatible with matched servers.

**Behavior**:

- Solver finds the maximum plugin version compatible with ALL matched servers
- Considers Paper versions of all servers this plugin applies to
- Respects `updateDelay` for stability

**Version resolution**:

1. Fetch all available plugin versions from the repository (Hangar, Modrinth, etc.)
2. Filter versions by `updateDelay` (exclude too-new versions)
3. For each version (newest first), check compatibility with ALL matched servers' Paper versions
4. Return the highest compatible version

**Update behavior**:

- Automatic updates when new compatible versions are available
- Updates respect server maintenance windows
- If multiple servers match, uses the most conservative compatible version

**Use cases**:

- Plugins you want to keep current automatically
- Well-maintained plugins with good backward compatibility
- Non-critical plugins where updates are low-risk

**Example**:

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: bluemap
spec:
  source:
    type: hangar
    project: "BlueMap"
  updateStrategy: latest
  updateDelay: 168h  # Wait 7 days before applying new versions
  instanceSelector:
    matchLabels:
      type: vanilla
```

### Strategy: `auto`

**Purpose**: Let the solver pick the best plugin version compatible with all matched servers.

**Behavior**:

- Solver finds optimal plugin version that works with all matched servers' Paper versions
- Automatically adapts as servers update their Paper versions
- Balances compatibility with staying current

**Version resolution**:

1. Fetch all available plugin versions from repository
2. Filter versions by `updateDelay`
3. For each matched server, find plugin versions compatible with that server's Paper version
4. Return the highest plugin version compatible with ALL servers
5. If multiple servers run different Paper versions, picks most conservative compatible version

**Update behavior**:

- Automatic updates when new compatible versions are available
- Updates respect server maintenance windows
- Adapts to server Paper version changes

**Use cases**:

- Plugins that should stay compatible with server versions
- Multi-server environments with different Paper versions
- When you want automatic updates but need cross-server compatibility
- Plugins with good semantic versioning and compatibility metadata

**Example**:

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: worldedit
spec:
  source:
    type: hangar
    project: "WorldEdit"
  updateStrategy: auto
  updateDelay: 168h
  instanceSelector:
    matchExpressions:
      - key: type
        operator: In
        values: ["creative", "modded"]
```

**How it works**:

- Server A: Paper 1.21.1
- Server B: Paper 1.20.4
- Both match this plugin
- Plugin versions: 7.3.0 (1.21+), 7.2.0 (1.20-1.21), 7.1.0 (1.20)
- Solver picks 7.2.0 (compatible with both servers)

### Strategy: `pin`

**Purpose**: Pin to a specific plugin version, with automatic build updates.

**Behavior**:

- Uses the exact version specified in `version` field
- Automatically updates to newer builds of the same version (if available)
- Manual control over major/minor version, automatic patch/build updates

**Version resolution**:

- Returns the `version` value directly
- Validates the version exists in the repository
- Automatically updates to latest build of that version
- Checks compatibility with matched servers (warning if incompatible)

**Update behavior**:

- Build updates are automatic (e.g., 2.5.0-build.100 → 2.5.0-build.101)
- Version updates require manual change of the `version` field

**Use cases**:

- Critical plugins where stability is paramount
- Plugins known to have breaking changes between versions
- When you need to test plugin updates in staging first
- Regulatory/compliance requirements

**Example**:

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: essentialsx
spec:
  source:
    type: hangar
    project: "Essentials"
  updateStrategy: pin
  version: "2.20.1"  # Required for pin strategy
  instanceSelector:
    matchLabels:
      environment: production
```

**Manual updates**:

```bash
kubectl patch plugin essentialsx --type=merge \
  -p '{"spec":{"version":"2.21.0"}}'
```

### Strategy: `build-pin`

**Purpose**: Pin to exact plugin version and build - no automatic updates.

**Behavior**:

- Completely static configuration
- No automatic updates whatsoever
- Full manual control over both version and build
- Maximum stability and reproducibility

**Version resolution**:

- Uses exact `version` and `build` specified in spec
- Validates the version-build combination exists in repository
- Checks compatibility for information purposes only

**Update behavior**:

- No automatic updates
- To update, manually change `version` and/or `build` fields
- Useful for maximum stability and predictability

**Use cases**:

- Extremely critical plugins (permissions, economy, core mechanics)
- Certification/compliance requirements
- Exact reproducibility needed
- Regression testing against specific builds
- When any change requires approval/testing

**Example**:

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: luckperms
spec:
  source:
    type: hangar
    project: "LuckPerms"
  updateStrategy: build-pin
  version: "5.4.102"  # Required
  build: 1543         # Required
  instanceSelector:
    matchLabels:
      environment: production
      criticality: high
```

**Manual updates**:

```bash
# Update to a new build
kubectl patch plugin luckperms --type=merge \
  -p '{"spec":{"build":1550}}'

# Update to a new version and build
kubectl patch plugin luckperms --type=merge \
  -p '{"spec":{"version":"5.4.110","build":1555}}'
```

## Strategy Comparison

### PaperMCServer Strategies

| Strategy   | Automatic Version Updates | Automatic Build Updates | Version Selection              | Use Case                          |
|------------|---------------------------|-------------------------|--------------------------------|-----------------------------------|
| `latest`   | Yes                       | Yes                     | Highest available from Hub     | Testing, vanilla servers          |
| `auto`     | Yes                       | Yes                     | Solver (plugin-compatible)     | Production with plugins           |
| `pin`      | No (manual)               | Yes                     | Pinned version, latest build   | Stable version, get bug fixes     |
| `build-pin`| No (manual)               | No (manual)             | Fully pinned                   | Maximum stability, compliance     |

### Plugin Strategies

| Strategy   | Automatic Version Updates | Automatic Build Updates | Version Selection                    | Use Case                          |
|------------|---------------------------|-------------------------|--------------------------------------|-----------------------------------|
| `latest`   | Yes                       | Yes                     | Highest server-compatible            | Keep plugins current              |
| `auto`     | Yes                       | Yes                     | Solver (cross-server compatible)     | Multi-server compatibility        |
| `pin`      | No (manual)               | Yes                     | Pinned version, latest build         | Stable version, get bug fixes     |
| `build-pin`| No (manual)               | No (manual)             | Fully pinned                         | Maximum stability, compliance     |

## Common Configuration Scenarios

### Scenario 1: Production Server with Automatic Plugin Updates

**Goal**: Run a production server that stays on a stable Paper version but gets the latest compatible plugins.

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: prod-server
  labels:
    environment: production
spec:
  updateStrategy: "pin"
  version: "1.21.1"
  updateDelay: 168h
---
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: plugin-a
spec:
  updateStrategy: latest
  updateDelay: 336h  # 14 days - more conservative for production
  instanceSelector:
    matchLabels:
      environment: production
```

**Result**: Server stays on Paper 1.21.1 (with build updates), plugins update to latest compatible versions after 14-day delay.

### Scenario 2: Fully Automatic Server

**Goal**: Development server that always runs the latest compatible versions of everything.

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: dev-server
  labels:
    environment: dev
spec:
  updateStrategy: "auto"
  updateDelay: 24h  # Short delay for dev
---
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: plugin-b
spec:
  updateStrategy: latest
  updateDelay: 24h
  instanceSelector:
    matchLabels:
      environment: dev
```

**Result**: Both Paper and plugins update automatically to latest compatible versions after 24 hours.

### Scenario 3: Maximum Stability

**Goal**: Production server with zero automatic updates - full manual control.

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: critical-server
  labels:
    environment: production
    criticality: high
spec:
  updateStrategy: "build-pin"
  version: "1.21.1"
  build: 91
---
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: plugin-c
spec:
  updateStrategy: build-pin
  version: "2.5.0"
  build: 123
  instanceSelector:
    matchLabels:
      criticality: high
```

**Result**: Nothing updates automatically. All changes require manual spec updates.

### Scenario 4: Mixed Strategy - Stable Server, Experimental Plugins

**Goal**: Keep server version stable but allow some plugins to update automatically.

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: mixed-server
  labels:
    type: mixed
spec:
  updateStrategy: "pin"
  version: "1.21.1"
---
# Critical plugin - pinned
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: essential-plugin
spec:
  updateStrategy: pin
  version: "3.0.1"
  instanceSelector:
    matchLabels:
      type: mixed
---
# Non-critical plugin - latest
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: cosmetic-plugin
spec:
  updateStrategy: latest
  updateDelay: 168h
  instanceSelector:
    matchLabels:
      type: mixed
```

**Result**: Server gets build updates for 1.21.1. Essential plugin stays pinned. Cosmetic plugin updates automatically.

## How the Solver Works

The constraint solver ensures compatibility between Paper versions and plugin versions.

### For Plugin Version Resolution (updateStrategy: latest)

1. Fetch all available plugin versions from repository
2. Filter out versions newer than `now - updateDelay`
3. Sort versions in descending order (newest first)
4. For each version:
   - Check if compatible with ALL matched servers' Paper versions
   - If yes, return this version
   - If no, try next version
5. If no compatible version found, report error

**Example**:

- Plugin has versions: 3.0.0 (requires 1.21.x), 2.9.0 (requires 1.20-1.21), 2.8.0 (requires 1.20.x)
- Server A runs Paper 1.21.1
- Server B runs Paper 1.20.4
- Both servers match this plugin's selector
- Solver picks version 2.9.0 (compatible with both servers)

### For Paper Version Resolution (updateStrategy: auto)

1. Fetch all available Paper versions
2. Filter out versions newer than `now - updateDelay`
3. Sort versions in descending order (newest first)
4. For each Paper version:
   - For each matched plugin, check if at least one plugin version is compatible
   - If ALL plugins have compatible versions, return this Paper version
   - If not, try next Paper version
5. If no compatible Paper version found, block updates

**Example**:

- Paper versions available: 1.21.2, 1.21.1, 1.21.0
- Plugin A: compatible with 1.21.1, 1.21.0 (not yet updated for 1.21.2)
- Plugin B: compatible with 1.21.2, 1.21.1, 1.21.0
- Solver picks Paper 1.21.1 (highest version compatible with both plugins)
- Status shows `availableUpdate` with Paper 1.21.2 but blocked due to Plugin A

## Update Delay Behavior

The `updateDelay` field provides a grace period before applying new versions, allowing the community to discover issues.

**Field specification**:

- **Type**: Duration (e.g., `168h`, `336h`) — uses Go duration format (`h`, `m`, `s`)
- **Optional**: Yes (defaults to 0 = no delay)
- **Applies to**: Both `PaperMCServer` and `Plugin` resources
- **Behavior**: When not specified or set to `0`, updates apply immediately (subject to maintenance windows)

**How it works**:

- When a new version is released, the operator records the release time
- The solver excludes versions where `releaseTime + updateDelay > now`
- After the delay expires, the version becomes eligible for updates
- Updates only apply during maintenance windows

**Example timeline**:

```text
Day 0:  Plugin 3.0.0 released
Day 1:  Operator detects new version, records in availableVersions
Day 7:  updateDelay expires (if set to 168h)
Day 7+: Next maintenance window applies the update
```

**Best practices**:

- Development: `24h` - Quick updates for testing
- Staging: `168h` (7 days) - Standard delay
- Production: `336h` (14 days) - Conservative delay
- Critical production: Use `pin`/`build-pin` with manual updates

## Initial Deployment Behavior

### For `auto` Strategy

On initial deployment (no existing server):

1. Operator analyzes all matched plugins
2. Solver finds the best Paper version compatible with all plugins
3. Operator creates StatefulSet with resolved image (e.g., `lexfrei/papermc:1.21.1-91`)
4. Status is populated with current/desired versions

**Important**: The `:latest` Docker tag is NEVER used. Even on first deployment, the operator resolves to a concrete version-build tag.

### For `latest` Strategy

1. Query Docker Hub for latest available Paper image
2. Extract version and build from tag
3. Create StatefulSet with that specific tag
4. Continue monitoring for newer versions

### For `pin` Strategy

1. Validate `version` is specified
2. Find latest build for that version
3. Create StatefulSet with `version-latestBuild`
4. Monitor for new builds of the pinned version

### For `build-pin` Strategy

1. Validate both `version` and `build` are specified
2. Verify the version-build combination exists
3. Create StatefulSet with exact tag `version-build`
4. No further version changes without spec updates

## Important Notes

### Docker Tag Resolution

The operator NEVER uses the `:latest` Docker tag. All strategies resolve to concrete version-build combinations:

- `latest` strategy → `lexfrei/papermc:1.21.1-91` (highest available)
- `auto` strategy → `lexfrei/papermc:1.21.1-91` (solver result)
- `pin` strategy → `lexfrei/papermc:1.21.1-95` (pinned version, latest build)
- `build-pin` strategy → `lexfrei/papermc:1.21.1-91` (exact specification)

This ensures reproducibility and prevents unexpected changes.

### Compatibility Validation

The operator validates compatibility using metadata from plugin repositories:

- Plugin versions declare supported Minecraft versions (e.g., "1.20.x", "1.21.x")
- Operator matches Paper version against these ranges
- If metadata is unavailable, `compatibilityOverride` can provide manual constraints

### Update Blocking

When the solver cannot find a compatible solution:

- Updates are blocked
- `status.updateBlocked` indicates the reason
- Status shows which plugin is blocking and why
- Operator waits until constraints can be satisfied

Example blocked status:

```yaml
status:
  updateBlocked:
    blocked: true
    reason: "No compatible Paper version found for all plugins"
    blockedBy:
      plugin: "essentialsx"
      version: "2.20.1"
      supportedVersions: ["1.20.4", "1.21.1"]
```

### Maintenance Windows

Updates only occur during configured maintenance windows:

```yaml
updateSchedule:
  checkCron: "0 3 * * *"        # Check daily at 3 AM
  maintenanceWindow:
    enabled: true
    cron: "0 4 * * 0"           # Apply Sundays at 4 AM
```

- `checkCron`: When to run the solver and discover new versions
- `maintenanceWindow.cron`: When to actually apply updates
- Allows discovery without immediate application

### Pre-Update Backups

When `spec.backup.beforeUpdate` is `true` (the default), the operator automatically creates a VolumeSnapshot before applying any server or plugin update. If the backup fails for any reason, the update is aborted to prevent data loss. This includes:

- RCON hook failures (save-all, save-off)
- VolumeSnapshot creation errors
- VolumeSnapshot CRD not installed in the cluster

```yaml
spec:
  backup:
    enabled: true
    beforeUpdate: true  # Default — backup before every update
```

This ensures you always have a recovery point before any change.

> **Note:** If `backup.enabled: true` and `beforeUpdate: true` but the VolumeSnapshot CRD
> (`snapshot.storage.k8s.io`) is not installed in the cluster, updates will be blocked until
> the CRD is installed or `beforeUpdate` is set to `false`.

### Graceful Shutdown

All updates use graceful shutdown via RCON:

1. Operator sends warning messages to players via RCON `say` commands (with intervals)
2. Operator sends RCON `save-all` command
3. Wait for save to complete
4. Operator sends RCON `stop` command
5. Paper saves world, unloads plugins, shuts down cleanly
6. Pod terminates
7. StatefulSet recreates pod with new image
8. Paper starts with updated version/plugins

This prevents world corruption and data loss.

## Troubleshooting

### Update Not Happening

**Check**:

1. Is maintenance window enabled? (`maintenanceWindow.enabled: true`)
2. Has the maintenance window cron triggered? (Check current time vs cron schedule)
3. Is `updateDelay` blocking? (Check `status.availableUpdate.foundAt` + delay)
4. Are updates blocked? (Check `status.updateBlocked`)

### Incompatible Plugin Version

**Solution**:

1. Check `status.plugins` for compatibility status
2. Use `compatibilityOverride` if metadata is incorrect
3. Consider pinning the plugin to a compatible version
4. Report missing metadata to plugin maintainer

### Unexpected Version

**Check**:

1. Verify `updateStrategy` is what you expect
2. Check `status.desiredVersion` vs `status.currentVersion`
3. Review solver logic in `status.availableUpdate`
4. Ensure `updateDelay` is configured correctly

### Rollback Needed

The operator does not perform downgrades by default. To rollback:

```bash
# Change to build-pin strategy with old version
kubectl patch papermcserver my-server --type=merge \
  -p '{"spec":{"updateStrategy":"build-pin","version":"1.21.1","build":91}}'
```

Rollbacks require manual intervention for safety.

## Further Reading

- [Architecture](../architecture/design.md) - Detailed architectural design
- [Constraint Solver](../architecture/solver.md) - How version compatibility is resolved
- [PaperMCServer Reference](papermcserver.md) - Full PaperMCServer CRD specification
- [Plugin Reference](plugin.md) - Full Plugin CRD specification
