# Plugin

The `Plugin` CRD defines a Minecraft plugin to be installed on matched servers.

## Overview

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: essentialsx
  namespace: minecraft
spec:
  source:
    type: hangar
    project: "EssentialsX/Essentials"
  updateStrategy: "latest"
  instanceSelector:
    matchLabels:
      environment: production
```

## Spec Fields

### source

**Required** — Defines where to fetch the plugin.

| Field | Description |
| --- | --- |
| `type` | Repository type (see supported sources below) |
| `project` | Plugin identifier (for hangar) |
| `url` | Direct download URL (for type: url) |
| `checksum` | Optional SHA256 hash for integrity verification (for type: url) |

```yaml
spec:
  source:
    type: hangar
    project: "EssentialsX/Essentials"
```

!!! warning "Supported Sources"

    **Currently implemented:**

    - `hangar` — PaperMC Hangar ([hangar.papermc.io](https://hangar.papermc.io))
    - `url` — Direct URL download (GitHub releases, private repos, etc.)

    **Planned (not yet implemented):**

    - `modrinth` — [#2](https://github.com/lexfrei/minecraft-operator/issues/2)
    - `spigot` — [#3](https://github.com/lexfrei/minecraft-operator/issues/3)

#### URL Source

For plugins not published on marketplaces, use `type: url` with a direct HTTPS download link.

The operator downloads the JAR and extracts metadata (name, version, API version) from
`plugin.yml` or `paper-plugin.yml` inside the archive. If extraction fails, `spec.version`
is used as fallback.

```yaml
spec:
  source:
    type: url
    url: "https://github.com/example/plugin/releases/download/v1.0.0/plugin-1.0.0.jar"
    checksum: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
```

!!! info "Checksum Verification"

    The `checksum` field accepts a SHA256 hex string (64 characters). If provided, the
    operator verifies the downloaded JAR against this hash. If omitted, the operator
    logs a warning but proceeds with the unverified download.

### updateStrategy

**Optional** — Defines how plugin versions are managed. Default: `latest`.

| Value | Description |
|-------|-------------|
| `latest` | Always use newest version from repository |
| `auto` | Constraint solver picks best version compatible with servers |
| `pin` | Pin to specific version (requires `version` field) |
| `build-pin` | Pin to specific version and build |

```yaml
spec:
  updateStrategy: "latest"
```

### version

**Optional** — Target plugin version. Required for `pin` and `build-pin` strategies.

```yaml
spec:
  updateStrategy: "pin"
  version: "2.20.1"
```

### build

**Optional** — Target build number. Only used with `build-pin` strategy.

```yaml
spec:
  updateStrategy: "build-pin"
  version: "2.20.1"
  build: 456
```

### updateDelay

**Optional** — Grace period before applying new plugin versions.

```yaml
spec:
  updateDelay: "72h"  # Wait 3 days before using new releases
```

### instanceSelector

**Required** — Label selector to match PaperMCServer instances.

```yaml
spec:
  instanceSelector:
    matchLabels:
      environment: production
      server-type: survival
```

Or with expressions:

```yaml
spec:
  instanceSelector:
    matchExpressions:
      - key: environment
        operator: In
        values:
          - production
          - staging
```

### port

**Optional** — Network port exposed by the plugin. Added to matched servers' Services.

Useful for plugins with web interfaces (Dynmap, BlueMap, Plan).

```yaml
spec:
  port: 8123  # Dynmap web interface
```

!!! info "Port Handling"

    The port is added as both TCP and UDP to the Service.
    Port name format: `plugin-{pluginname}`

### compatibilityOverride

**Optional** — Manual compatibility specification for plugins without proper metadata.

| Field | Description |
|-------|-------------|
| `enabled` | Enable the override |
| `minecraftVersions` | List of compatible Minecraft versions |

```yaml
spec:
  compatibilityOverride:
    enabled: true
    minecraftVersions:
      - "1.20.4"
      - "1.20.6"
      - "1.21.1"
```

!!! warning "Use Sparingly"

    Only use compatibility overrides when the plugin repository lacks version metadata.
    Incorrect overrides may cause plugin compatibility issues.

## Status Fields

### availableVersions

Cached metadata from the plugin repository.

```yaml
status:
  availableVersions:
    - version: "2.21.0"
      minecraftVersions:
        - "1.20.4"
        - "1.21.1"
      downloadURL: "https://..."
      hash: "sha256:abc123..."
      cachedAt: "2024-01-15T10:00:00Z"
      releasedAt: "2024-01-10T12:00:00Z"
```

### matchedInstances

Servers matched by the instanceSelector.

```yaml
status:
  matchedInstances:
    - name: survival
      namespace: minecraft
      version: "1.21.4"
      compatible: true
    - name: creative
      namespace: minecraft
      version: "1.20.4"
      compatible: true
```

### repositoryStatus

Plugin repository availability.

| Value | Description |
|-------|-------------|
| `available` | Repository accessible, metadata current |
| `unavailable` | Repository temporarily unreachable |
| `orphaned` | Repository removed, using cached data |

### lastFetched

Timestamp of the last successful API fetch.

```yaml
status:
  lastFetched: "2024-01-15T10:00:00Z"
```

### conditions

Standard Kubernetes conditions.

| Type | Description |
|------|-------------|
| `Ready` | Plugin reconciled successfully |
| `RepositoryAvailable` | Plugin repository is accessible |
| `VersionResolved` | Metadata fetched and servers matched |

## Complete Example

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: bluemap
  namespace: minecraft
spec:
  source:
    type: hangar
    project: "BlueMap/BlueMap"

  updateStrategy: "latest"
  updateDelay: "168h"  # Wait 7 days

  # Match all production servers
  instanceSelector:
    matchLabels:
      environment: production

  # Expose BlueMap web interface
  port: 8100

---
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: essentialsx
  namespace: minecraft
spec:
  source:
    type: hangar
    project: "EssentialsX/Essentials"

  updateStrategy: "pin"
  version: "2.20.1"

  instanceSelector:
    matchLabels:
      environment: production

---
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: worldedit
  namespace: minecraft
spec:
  source:
    type: hangar
    project: "EngineHub/WorldEdit"

  updateStrategy: "auto"

  instanceSelector:
    matchExpressions:
      - key: server-type
        operator: In
        values:
          - creative
          - build

---
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: custom-plugin
  namespace: minecraft
spec:
  source:
    type: url
    url: "https://github.com/example/plugin/releases/download/v1.2.0/plugin-1.2.0.jar"
    checksum: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

  version: "1.2.0"  # Fallback if plugin.yml extraction fails
  updateStrategy: "latest"

  instanceSelector:
    matchLabels:
      environment: production
```

## Plugin Deletion

When a Plugin is deleted:

1. The operator marks the plugin for deletion on matched servers
2. During next server restart, the JAR file is removed from `/data/plugins/`
3. Once all JARs are cleaned up, the Plugin resource is fully deleted

Check deletion progress:

```bash
kubectl get plugin essentialsx -o yaml
```

```yaml
status:
  deletionProgress:
    - serverName: survival
      namespace: minecraft
      jarDeleted: true
      deletedAt: "2024-01-15T04:00:00Z"
    - serverName: creative
      namespace: minecraft
      jarDeleted: false
```

## See Also

- [PaperMCServer](papermcserver.md) — Server CRD reference
- [Update Strategies](update-strategies.md) — Version management guide
- [Architecture](../architecture/solver.md) — Constraint solver details
