# Installation

Install Minecraft Operator using Helm charts from the GitHub Container Registry.

## Install CRDs

The operator uses two Custom Resource Definitions (CRDs):

- **PaperMCServer** — Defines a Minecraft server instance
- **Plugin** — Defines a plugin to be installed on matched servers

Install the CRD chart first:

```bash
helm install minecraft-operator-crds \
  oci://ghcr.io/lexfrei/minecraft-operator-crds \
  --namespace minecraft-operator-system \
  --create-namespace
```

!!! info "Separate CRD Lifecycle"

    CRDs are installed separately to allow independent upgrades.
    Helm does not automatically upgrade CRDs, so they have their own chart.

## Install Operator

Install the operator chart:

```bash
helm install minecraft-operator \
  oci://ghcr.io/lexfrei/minecraft-operator \
  --namespace minecraft-operator-system
```

### Verify Installation

Check that the operator is running:

```bash
kubectl get pods --namespace minecraft-operator-system
```

Expected output:

```text
NAME                                  READY   STATUS    RESTARTS   AGE
minecraft-operator-xxxxxxxxxx-xxxxx   1/1     Running   0          30s
```

## Configuration Options

### Common Values

| Value | Description | Default |
|-------|-------------|---------|
| `replicaCount` | Number of operator replicas | `1` |
| `image.repository` | Container image | `ghcr.io/lexfrei/minecraft-operator` |
| `image.tag` | Image tag | Chart appVersion |
| `resources.requests.memory` | Memory request | `128Mi` |
| `resources.limits.memory` | Memory limit | `256Mi` |
| `webui.enabled` | Enable Web UI | `true` |
| `webui.port` | Web UI port | `8082` |

### Custom Values File

Create a `values.yaml` for customization:

```yaml
replicaCount: 1

resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    memory: 256Mi

webui:
  enabled: true
  port: 8082

# Leader election for HA (if running multiple replicas)
leaderElection:
  enabled: true
```

Install with custom values:

```bash
helm install minecraft-operator \
  oci://ghcr.io/lexfrei/minecraft-operator \
  --namespace minecraft-operator-system \
  --values values.yaml
```

## Upgrade

Upgrade the operator to a new version:

```bash
# Upgrade operator
helm upgrade minecraft-operator \
  oci://ghcr.io/lexfrei/minecraft-operator \
  --namespace minecraft-operator-system

# Upgrade CRDs manually (Helm doesn't auto-upgrade CRDs)
helm upgrade minecraft-operator-crds \
  oci://ghcr.io/lexfrei/minecraft-operator-crds \
  --namespace minecraft-operator-system
```

## Uninstall

Remove the operator and CRDs:

```bash
# Remove operator first
helm uninstall minecraft-operator --namespace minecraft-operator-system

# Remove CRDs (WARNING: deletes all PaperMCServer and Plugin resources!)
helm uninstall minecraft-operator-crds --namespace minecraft-operator-system
```

!!! danger "Data Loss Warning"

    Uninstalling CRDs will delete all custom resources (PaperMCServer, Plugin).
    Minecraft world data on PVCs is NOT deleted automatically.

## Next Steps

Proceed to [Quick Start](quickstart.md) to create your first Minecraft server.
