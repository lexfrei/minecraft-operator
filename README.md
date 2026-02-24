# Minecraft Operator

A Kubernetes operator for managing [PaperMC](https://papermc.io/) servers with automatic version management, plugin compatibility solving, and scheduled updates.

> **Note:** This operator is designed for single-instance Minecraft servers.
> 5-10 minutes of downtime during updates is acceptable by design.

## Features

- **Automatic Version Management** — Four update strategies (`latest`, `auto`, `pin`, `build-pin`) for both Paper and plugins
- **Plugin Compatibility Solver** — Constraint solver ensures all plugins work with the selected Paper version
- **Scheduled Updates** — Cron-based maintenance windows with graceful RCON shutdown
- **Declarative Plugin Management** — Plugins matched to servers via label selectors
- **VolumeSnapshot Backups** — Cron-scheduled and on-demand backups with RCON consistency hooks and retention policy
- **Self-Managed CRDs** — CRDs embedded in the operator binary, applied at startup via server-side apply
- **Web UI** — Built-in dashboard for monitoring servers and plugins
- **Hangar Integration** — Automatic plugin downloads from PaperMC Hangar repository

## Quick Start

### Prerequisites

- Kubernetes 1.27+
- Helm 3.14+

### Install the Operator

```bash
helm install minecraft-operator oci://ghcr.io/lexfrei/minecraft-operator \
  --create-namespace \
  --namespace minecraft-operator-system
```

CRDs are embedded in the operator binary and applied automatically at startup.
No separate CRD chart or `kubectl apply` step is needed.

### Create a Minecraft Server

First, create an RCON secret:

```bash
kubectl create secret generic my-server-rcon \
  --namespace default \
  --from-literal=password="your-rcon-password"
```

Then apply the server manifest:

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: my-server
  labels:
    environment: production
spec:
  updateStrategy: "auto"
  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"
  gracefulShutdown:
    timeout: 300s
  rcon:
    enabled: true
    passwordSecret:
      name: my-server-rcon
      key: password
  podTemplate:
    spec:
      containers:
        - name: minecraft
          resources:
            requests:
              memory: "2Gi"
            limits:
              memory: "4Gi"
```

### Add a Plugin

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: essentialsx
spec:
  source:
    type: hangar
    project: "Essentials"
  updateStrategy: "latest"
  updateDelay: 168h
  instanceSelector:
    matchLabels:
      environment: production
```

The plugin is automatically downloaded and installed on all servers matching the label selector.

See the [examples/](examples/) directory for more use cases.

## Update Strategies

| Strategy    | Description                                               |
| ----------- | --------------------------------------------------------- |
| `latest`    | Always use newest Paper version (ignores plugin compat)   |
| `auto`      | Solver finds best version compatible with all plugins     |
| `pin`       | Stay on specific version, auto-update to latest build     |
| `build-pin` | Fully pinned version and build, no automatic updates      |

See [Update Strategies Guide](docs/configuration/update-strategies.md) for details.

## Architecture

Four controllers work together:

1. **Plugin Controller** — Fetches plugin metadata from Hangar, runs compatibility solver, updates Plugin status
2. **PaperMCServer Controller** — Manages StatefulSet and Service, resolves Paper version based on update strategy
3. **Update Controller** — Executes scheduled updates during maintenance windows with graceful RCON shutdown
4. **Backup Controller** — Creates VolumeSnapshots with RCON save hooks for data consistency, supports cron scheduling, manual triggers, and retention

For detailed architecture and constraint solver algorithms, see [DESIGN.md](DESIGN.md).

## Backups

Enable VolumeSnapshot-based backups:

```yaml
spec:
  backup:
    enabled: true
    schedule: "0 */6 * * *"     # Every 6 hours
    beforeUpdate: true           # Backup before any update
    retention:
      maxCount: 10               # Keep last 10 snapshots
```

When RCON is enabled, the operator uses RCON hooks (`save-all`, `save-off`, `save-on`)
to flush data to disk and disable auto-save before creating a VolumeSnapshot.
Without RCON, snapshots are crash-consistent only.

Trigger a manual backup:

```bash
kubectl annotate papermcserver my-server \
  mc.k8s.lex.la/backup-now="$(date +%s)" \
  --namespace minecraft
```

**Requirements:** A CSI driver with VolumeSnapshot support must be installed in
the cluster.

## Helm Configuration

Key values:

```yaml
crds:
  manage: true          # Apply embedded CRDs at startup (default: true)

webui:
  enabled: true         # Enable built-in Web UI (default: true)
  port: 8082

leaderElection:
  enabled: true         # Enable leader election for HA (default: true)

metrics:
  enabled: true         # Enable Prometheus metrics (default: true)
  port: 8080
  serviceMonitor:
    enabled: false      # Enable ServiceMonitor for Prometheus Operator
```

See [charts/minecraft-operator/values.yaml](charts/minecraft-operator/values.yaml) for all options.

## Monitoring

The operator exposes Prometheus metrics at `/metrics` (port 8080 by default):

- **Reconciliation**: duration, total count, and error count per controller
- **Plugin API**: request count, error count, and latency per source
- **Solver**: invocation count and duration per solver type
- **Updates**: success/failure count per server update

Enable `metrics.serviceMonitor.enabled` in Helm values for automatic
Prometheus Operator discovery.

## Web UI

Access the built-in dashboard:

```bash
kubectl port-forward svc/minecraft-operator-webui 8082:8082 \
  --namespace minecraft-operator-system
```

Open <http://localhost:8082/ui>

## Development

```bash
make manifests generate   # Generate CRDs and deepcopy methods
make test                 # Run unit tests with envtest
make lint                 # Run golangci-lint
make run                  # Run operator locally against kubeconfig cluster
```

This project follows strict TDD methodology. See [CLAUDE.md](CLAUDE.md) for development standards.

## Links

- [Documentation Site](https://lexfrei.github.io/minecraft-operator/)
- [Changelog](CHANGELOG.md)
- [Design Document](DESIGN.md)
- [GitHub Issues](https://github.com/lexfrei/minecraft-operator/issues)

## Contributing

Contributions are welcome. See [GitHub Issues](https://github.com/lexfrei/minecraft-operator/issues) for open tasks.

## License

BSD-3-Clause. See [LICENSE](LICENSE).
