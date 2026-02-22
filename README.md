# Minecraft Operator

A Kubernetes operator for managing PaperMC servers with automatic version management, plugin compatibility solving, and scheduled updates.

## Features

- **Automatic Version Management** — Four update strategies: `latest`, `auto`, `pin`, `build-pin`
- **Plugin Compatibility Solver** — Constraint solver ensures plugins work with selected Paper version
- **Scheduled Updates** — Cron-based maintenance windows with graceful RCON shutdown
- **Declarative Plugin Management** — Plugins are matched to servers via label selectors
- **Web UI** — Built-in dashboard for monitoring servers and plugins
- **Hangar Integration** — Automatic plugin downloads from PaperMC Hangar

## Quick Start

### Prerequisites

- Kubernetes 1.27+
- Helm 3.14+

### Installation

```bash
helm install minecraft-operator oci://ghcr.io/lexfrei/minecraft-operator \
  --create-namespace --namespace minecraft-operator-system
```

CRDs are embedded in the operator binary and applied automatically at startup via server-side apply.

### Create a Minecraft Server

```yaml
apiVersion: mc.k8s.lex.la/v1alpha1
kind: PaperMCServer
metadata:
  name: my-server
  labels:
    environment: production
spec:
  updateStrategy: "auto"  # Solver picks best version for plugins
  updateSchedule:
    checkCron: "0 3 * * *"        # Check daily at 3am
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"           # Apply updates Sunday 4am
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
apiVersion: mc.k8s.lex.la/v1alpha1
kind: Plugin
metadata:
  name: essentialsx
spec:
  source:
    type: hangar
    project: "EssentialsX/Essentials"
  updateStrategy: "latest"
  instanceSelector:
    matchLabels:
      environment: production
```

## Update Strategies

| Strategy | Description |
|----------|-------------|
| `latest` | Always use newest Paper version (ignores plugin compatibility) |
| `auto` | Solver finds best version compatible with all plugins |
| `pin` | Stay on specific version, auto-update builds |
| `build-pin` | Fully pinned version and build |

See [docs/configuration/update-strategies.md](docs/configuration/update-strategies.md) for detailed guide.

## Architecture

The operator consists of three controllers:

1. **Plugin Controller** — Syncs plugin metadata from Hangar, runs compatibility solver
2. **PaperMCServer Controller** — Manages StatefulSet, Service, resolves versions
3. **Update Controller** — Executes scheduled updates with graceful shutdown

For detailed architecture, see [DESIGN.md](DESIGN.md).

## Web UI

Access the built-in dashboard:

```bash
kubectl port-forward svc/minecraft-operator-webui 8082:8082 -n minecraft-operator-system
```

Open http://localhost:8082/ui

## Development

```bash
# Generate code and CRDs
make manifests generate

# Run tests
make test

# Run linter
make lint

# Run locally
make run
```

## Contributing

See [GitHub Issues](https://github.com/lexfrei/minecraft-operator/issues) for roadmap and open tasks.

Priority areas:
- Test coverage for `pkg/solver/` and `pkg/plugins/`
- Modrinth plugin source support
- Documentation improvements

## License

BSD-3-Clause. See [LICENSE](LICENSE).
