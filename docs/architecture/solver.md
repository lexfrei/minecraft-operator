# Constraint Solver

The constraint solver finds compatible versions of Paper and plugins that satisfy all requirements.

## Problem Statement

Given:

- A set of PaperMCServer instances with their current versions
- A set of Plugins with available versions and compatibility metadata
- Relationships defined by label selectors

Find:

- Maximum Paper version compatible with ALL matched plugins
- Maximum plugin version compatible with ALL matched servers

## Algorithm

### Plugin Version Resolution

For a Plugin with `updateStrategy: latest`:

```text
1. Collect all matched PaperMCServer instances
2. Filter plugin versions by updateDelay:
   - Skip versions newer than (now - updateDelay)
3. For each plugin version (descending):
   - Check if compatible with ALL matched servers
   - If yes: return this version
4. If no compatible version found:
   - Emit warning
   - Keep current version
```

### Server Version Resolution

For a PaperMCServer with `updateStrategy: auto`:

```text
1. Collect all matched Plugin resources
2. Get available Paper versions from Docker Hub
3. Filter by updateDelay
4. For each Paper version (descending):
   - For each matched plugin:
     - Check if ANY plugin version is compatible
   - If all plugins have compatible versions:
     - Return this Paper version
5. If no compatible version found:
   - Set updateBlocked: true
   - Keep current version
```

## Compatibility Check

A plugin version is compatible with a server if:

```text
serverVersion ∈ plugin.minecraftVersions
```

Where `minecraftVersions` comes from:

1. Plugin repository metadata (Hangar API)
2. `compatibilityOverride` if enabled

## Example

### Scenario

```yaml
# Server running 1.20.4
PaperMCServer:
  name: survival
  labels:
    environment: production
  status:
    currentVersion: "1.20.4"

# Plugin A - compatible with 1.20.x and 1.21.x
Plugin:
  name: essentialsx
  instanceSelector:
    matchLabels:
      environment: production
  status:
    availableVersions:
      - version: "2.21.0"
        minecraftVersions: ["1.20.4", "1.21.1"]
      - version: "2.20.1"
        minecraftVersions: ["1.19.4", "1.20.4"]

# Plugin B - only compatible with 1.20.x
Plugin:
  name: old-plugin
  instanceSelector:
    matchLabels:
      environment: production
  status:
    availableVersions:
      - version: "1.5.0"
        minecraftVersions: ["1.20.4", "1.20.6"]
```

### Resolution

**Server version resolution** (with `updateStrategy: auto`):

1. Available Paper versions: `[1.21.4, 1.21.1, 1.20.6, 1.20.4, ...]`
2. Check 1.21.4:
   - EssentialsX: No compatible version → SKIP
3. Check 1.21.1:
   - EssentialsX: 2.21.0 compatible ✓
   - old-plugin: No compatible version → SKIP
4. Check 1.20.6:
   - EssentialsX: 2.21.0 compatible ✓
   - old-plugin: 1.5.0 compatible ✓
   - **Result: 1.20.6**

**Plugin version resolution** (EssentialsX with `updateStrategy: latest`):

1. Matched servers: `[survival (1.20.4)]`
2. Check 2.21.0:
   - Compatible with survival (1.20.4 ∈ [1.20.4, 1.21.1]) ✓
   - **Result: 2.21.0**

## Update Delay

The `updateDelay` field adds a grace period before using new versions:

```text
eligibleVersions = versions.filter(v =>
  v.releasedAt < (now - updateDelay)
)
```

This allows waiting for community feedback on new releases.

### Example

```yaml
spec:
  updateDelay: 168h  # 7 days
```

If today is January 15th:

| Version | Released | Eligible |
|---------|----------|----------|
| 2.21.0 | Jan 14 | No (1 day old) |
| 2.20.1 | Jan 5 | Yes (10 days old) |
| 2.20.0 | Dec 20 | Yes (26 days old) |

## Conflict Resolution

### Multiple Plugins, Same Project

When multiple Plugin resources reference the same project:

| Scenario | Resolution |
|----------|------------|
| Any has `latest` | Solver picks optimal version |
| All have `pin` | Highest semver wins + warning |

### No Compatible Version

When no compatible version exists:

1. Plugin sets `conditions.Compatible: False`
2. Server sets `updateBlocked: true` with reason
3. Current version remains installed
4. Operator logs warning

### Repository Unavailable

When the plugin repository is unreachable:

1. Use cached `status.availableVersions`
2. Set `repositoryStatus: orphaned`
3. Continue with cached data
4. Log warning for operator

## Implementation

The solver is implemented in `pkg/solver/`:

```go
type Solver interface {
    // FindBestPluginVersion finds the maximum plugin version compatible with ALL matched servers.
    FindBestPluginVersion(
        ctx context.Context,
        plugin *mcv1beta1.Plugin,
        servers []mcv1beta1.PaperMCServer,
        allVersions []plugins.PluginVersion,
    ) (string, error)

    // FindBestPaperVersion finds the maximum Paper version compatible with ALL matched plugins.
    FindBestPaperVersion(
        ctx context.Context,
        server *mcv1beta1.PaperMCServer,
        matchedPlugins []mcv1beta1.Plugin,
        paperVersions []string,
    ) (string, error)
}
```

### Current Implementation

The MVP uses a simple linear search algorithm:

1. Sort versions descending (newest first)
2. Check each version for compatibility
3. Return first compatible version

### Future: SAT Solver

For complex multi-server, multi-plugin scenarios, a SAT solver may be implemented:

- Model constraints as boolean satisfiability
- Use a SAT solver library for optimal solutions
- Handle complex dependency graphs

## See Also

- [Design](design.md) — System architecture
- [Update Strategies](../configuration/update-strategies.md) — Version management options
- [PaperMCServer](../configuration/papermcserver.md) — Server configuration
- [Plugin](../configuration/plugin.md) — Plugin configuration
