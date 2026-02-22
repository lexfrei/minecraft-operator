# Installation

Install Minecraft Operator using the Helm chart from the GitHub Container Registry.

## Install Operator

A single Helm install deploys a fully working operator. CRDs are embedded in the operator binary and applied automatically at startup via server-side apply.

```bash
helm install minecraft-operator \
  oci://ghcr.io/lexfrei/minecraft-operator \
  --namespace minecraft-operator-system \
  --create-namespace
```

!!! info "Embedded CRDs"

    CRDs (PaperMCServer, Plugin) are embedded in the operator binary and applied at startup
    using server-side apply. This is required because the PaperMCServer CRD exceeds the
    262KB annotation limit for client-side apply. The Helm value `crds.manage` (default: `true`)
    controls whether the operator manages CRDs.

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
helm upgrade minecraft-operator \
  oci://ghcr.io/lexfrei/minecraft-operator \
  --namespace minecraft-operator-system
```

CRDs are automatically updated at operator startup via server-side apply.

## Uninstall

Remove the operator:

```bash
helm uninstall minecraft-operator --namespace minecraft-operator-system
```

CRDs are NOT automatically removed (Kubernetes safety). To remove them manually:

```bash
# WARNING: deletes all PaperMCServer and Plugin custom resources!
kubectl delete crd papermcservers.mc.k8s.lex.la plugins.mc.k8s.lex.la
```

!!! danger "Data Loss Warning"

    Deleting CRDs will delete all custom resources (PaperMCServer, Plugin).
    Minecraft world data on PVCs is NOT deleted automatically.

## Next Steps

Proceed to [Quick Start](quickstart.md) to create your first Minecraft server.
