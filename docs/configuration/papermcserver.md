# PaperMCServer

The `PaperMCServer` CRD defines a Minecraft server instance managed by the operator.

## Overview

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: my-server
  namespace: minecraft
spec:
  updateStrategy: "auto"
  # ... configuration
```

## Spec Fields

### updateStrategy

**Required** — Defines how Paper version updates are handled.

| Value | Description |
|-------|-------------|
| `latest` | Always use newest Paper version from Docker Hub (ignores plugin compatibility) |
| `auto` | Constraint solver picks best version compatible with all plugins |
| `pin` | Stay on specific version, auto-update to latest build |
| `build-pin` | Fully pinned version and build, no automatic updates |

```yaml
spec:
  updateStrategy: "auto"  # Recommended for production
```

See [Update Strategies](update-strategies.md) for detailed guide.

### version

**Optional** — Target Paper version. Required for `pin` and `build-pin` strategies.

```yaml
spec:
  updateStrategy: "pin"
  version: "1.21.1"  # Pinned Minecraft version
```

### build

**Optional** — Target Paper build number.

- With `pin` strategy: minimum build (operator will still auto-update to newer builds)
- With `build-pin` strategy: exact build (no automatic updates)

```yaml
spec:
  updateStrategy: "build-pin"
  version: "1.21.1"
  build: 125  # Exact build number
```

### updateDelay

**Optional** — Grace period before applying Paper updates. Useful for waiting for community feedback on new releases.

```yaml
spec:
  updateDelay: "168h"  # Wait 7 days before applying updates
```

### updateSchedule

**Required** — Defines when to check and apply updates.

| Field | Description |
|-------|-------------|
| `checkCron` | Cron expression for checking updates |
| `maintenanceWindow.enabled` | Enable scheduled updates |
| `maintenanceWindow.cron` | Cron expression for applying updates |

```yaml
spec:
  updateSchedule:
    checkCron: "0 3 * * *"           # Check daily at 3am
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"              # Apply updates Sunday 4am
```

!!! tip "Cron Format"

    Standard cron format: `minute hour day-of-month month day-of-week`

### gracefulShutdown

**Required** — Configures graceful server shutdown.

| Field | Description |
|-------|-------------|
| `timeout` | Shutdown timeout (should match `terminationGracePeriodSeconds`) |

```yaml
spec:
  gracefulShutdown:
    timeout: 300s  # 5 minutes for world save
```

!!! warning "Timeout Configuration"

    The timeout should match your StatefulSet's `terminationGracePeriodSeconds`.
    Too short may cause world corruption. Minimum recommended: 300s.

### rcon

**Required** — Configures RCON for graceful shutdown commands.

| Field | Description | Default |
|-------|-------------|---------|
| `enabled` | Enable RCON | — |
| `passwordSecret.name` | Secret name | — |
| `passwordSecret.key` | Key in Secret | — |
| `port` | RCON port | `25575` |

```yaml
spec:
  rcon:
    enabled: true
    passwordSecret:
      name: my-server-rcon
      key: password
    port: 25575
```

Create the secret:

```bash
kubectl create secret generic my-server-rcon \
  --from-literal=password=your-secure-password
```

### service

**Optional** — Configures the Kubernetes Service for the server.

| Field | Description | Default |
|-------|-------------|---------|
| `type` | Service type | `LoadBalancer` |
| `annotations` | Custom annotations | — |
| `loadBalancerIP` | Static IP for LoadBalancer | — |

```yaml
spec:
  service:
    type: LoadBalancer
    annotations:
      metallb.universe.tf/loadBalancerIPs: "192.168.1.100"
    loadBalancerIP: "192.168.1.100"
```

### backup

**Optional** — Configures VolumeSnapshot-based backups with RCON consistency hooks.

| Field | Description | Default |
| --- | --- | --- |
| `enabled` | Enable backups | — |
| `schedule` | Cron schedule for periodic backups | — |
| `beforeUpdate` | Create backup before any server update | `true` |
| `volumeSnapshotClassName` | VolumeSnapshotClass to use | cluster default |
| `retention.maxCount` | Maximum snapshots to retain per server | `10` |

```yaml
spec:
  backup:
    enabled: true
    schedule: "0 */6 * * *"        # Every 6 hours
    beforeUpdate: true              # Backup before updates
    volumeSnapshotClassName: csi-hostpath-snapclass
    retention:
      maxCount: 10
```

The operator uses RCON hooks (`save-all`, `save-off`, `save-on`) to ensure world data consistency before creating the VolumeSnapshot.

**Manual trigger:**

```bash
kubectl annotate papermcserver my-server \
  mc.k8s.lex.la/backup-now="$(date +%s)"
```

### podTemplate

**Required** — Template for the StatefulSet pod.

This is a standard Kubernetes `PodTemplateSpec`. Key fields:

| Field | Description |
|-------|-------------|
| `spec.containers[0].resources` | CPU/memory requests and limits |
| `spec.containers[0].env` | Environment variables |
| `spec.volumes` | Additional volumes |

```yaml
spec:
  podTemplate:
    spec:
      containers:
        - name: minecraft
          resources:
            requests:
              memory: "2Gi"
              cpu: "500m"
            limits:
              memory: "4Gi"
          env:
            - name: JAVA_OPTS
              value: "-Xmx3G -Xms1G"
      volumes:
        - name: config
          configMap:
            name: server-config
```

## Status Fields

The operator updates the status to reflect the current state.

### currentVersion / currentBuild

Currently running Paper version and build.

```yaml
status:
  currentVersion: "1.21.4"
  currentBuild: 125
```

### desiredVersion / desiredBuild

Target version the operator wants to run (resolved from updateStrategy).

```yaml
status:
  desiredVersion: "1.21.4"
  desiredBuild: 130
```

### plugins

List of matched Plugin resources and their versions.

```yaml
status:
  plugins:
    - pluginRef:
        name: essentialsx
        namespace: minecraft
      resolvedVersion: "2.21.0"
      currentVersion: "2.20.1"
      desiredVersion: "2.21.0"
      compatible: true
      source: hangar
```

### availableUpdate

Next available update if any.

```yaml
status:
  availableUpdate:
    version: "1.21.4"
    build: 130
    releasedAt: "2024-01-15T10:00:00Z"
    foundAt: "2024-01-16T03:00:00Z"
    plugins:
      - pluginRef:
          name: essentialsx
          namespace: minecraft
        version: "2.21.0"
```

### lastUpdate

Record of the most recent update attempt.

```yaml
status:
  lastUpdate:
    appliedAt: "2024-01-14T04:00:00Z"
    previousVersion: "1.21.3"
    successful: true
```

### updateBlocked

Indicates if updates are blocked due to compatibility issues.

```yaml
status:
  updateBlocked:
    blocked: true
    reason: "Plugin incompatible with target version"
    blockedBy:
      plugin: "old-plugin"
      version: "1.0.0"
      supportedVersions:
        - "1.20.4"
        - "1.20.6"
```

### backup (status)

Observed backup state for the server.

```yaml
status:
  backup:
    backupCount: 5
    lastBackup:
      snapshotName: "survival-backup-1708742400"
      startedAt: "2026-02-24T00:00:00Z"
      completedAt: "2026-02-24T00:00:05Z"
      successful: true
      trigger: "scheduled"
```

| Field | Description |
| --- | --- |
| `backupCount` | Current number of retained VolumeSnapshots |
| `lastBackup.snapshotName` | Name of the VolumeSnapshot resource (empty when backup failed before snapshot creation) |
| `lastBackup.startedAt` | When the backup process started |
| `lastBackup.completedAt` | When the backup completed |
| `lastBackup.successful` | Whether the backup succeeded |
| `lastBackup.trigger` | What triggered the backup (`scheduled`, `before-update`, `manual`) |

### conditions

Standard Kubernetes conditions.

| Type | Description |
| --- | --- |
| `Ready` | Server reconciled successfully |
| `StatefulSetReady` | StatefulSet has ready replicas |
| `UpdateAvailable` | New Paper version/build available |
| `UpdateBlocked` | Update blocked by plugin incompatibility |
| `Updating` | Update currently in progress |
| `SolverRunning` | Constraint solver is executing |
| `CronScheduleValid` | Maintenance window cron is valid |
| `BackupCronValid` | Backup cron schedule is valid |

## Complete Example

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: survival
  namespace: minecraft
  labels:
    environment: production
    server-type: survival
spec:
  updateStrategy: "auto"
  updateDelay: "168h"  # Wait 7 days

  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"

  gracefulShutdown:
    timeout: 300s

  backup:
    enabled: true
    schedule: "0 */6 * * *"
    beforeUpdate: true
    volumeSnapshotClassName: "csi-hostpath-snapclass"
    retention:
      maxCount: 10

  rcon:
    enabled: true
    passwordSecret:
      name: survival-rcon
      key: password

  service:
    type: LoadBalancer
    annotations:
      external-dns.alpha.kubernetes.io/hostname: survival.minecraft.example.com

  podTemplate:
    spec:
      containers:
        - name: minecraft
          resources:
            requests:
              memory: "4Gi"
              cpu: "1"
            limits:
              memory: "8Gi"
          env:
            - name: JAVA_OPTS
              value: "-Xmx6G -Xms2G -XX:+UseG1GC"
      nodeSelector:
        minecraft: "true"
      tolerations:
        - key: "minecraft"
          operator: "Exists"
          effect: "NoSchedule"
```

## See Also

- [Plugin](plugin.md) — Plugin CRD reference
- [Update Strategies](update-strategies.md) — Detailed version management guide
- [Troubleshooting](../operations/troubleshooting.md) — Common issues
