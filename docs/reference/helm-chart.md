# Helm Chart Reference

Reference for Minecraft Operator Helm chart values.

## Installation

```bash
# Single step — CRDs are embedded and applied at startup via server-side apply
helm install minecraft-operator \
  oci://ghcr.io/lexfrei/minecraft-operator \
  --namespace minecraft-operator-system \
  --create-namespace
```

## Values

### CRDs

| Value | Description | Default |
|-------|-------------|---------|
| `crds.manage` | Operator manages CRDs at startup via server-side apply | `true` |

### Image

| Value | Description | Default |
|-------|-------------|---------|
| `image.repository` | Container image | `ghcr.io/lexfrei/minecraft-operator` |
| `image.pullPolicy` | Pull policy | `IfNotPresent` |
| `image.tag` | Image tag | Chart appVersion |
| `imagePullSecrets` | Image pull secrets | `[]` |

### Deployment

| Value | Description | Default |
|-------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |
| `nameOverride` | Override chart name | `""` |
| `fullnameOverride` | Override full name | `""` |

### Service Account

| Value | Description | Default |
|-------|-------------|---------|
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.automountServiceAccountToken` | Automount API credentials | `true` |
| `serviceAccount.annotations` | SA annotations | `{}` |
| `serviceAccount.name` | SA name override | `""` |

### Pod Configuration

| Value | Description | Default |
|-------|-------------|---------|
| `podAnnotations` | Pod annotations | `{}` |
| `podLabels` | Pod labels | `{}` |

### Security Context

| Value | Description | Default |
|-------|-------------|---------|
| `podSecurityContext.runAsNonRoot` | Run as non-root | `true` |
| `podSecurityContext.runAsUser` | User ID | `65534` |
| `podSecurityContext.seccompProfile.type` | Seccomp profile | `RuntimeDefault` |
| `securityContext.allowPrivilegeEscalation` | Privilege escalation | `false` |
| `securityContext.capabilities.drop` | Dropped capabilities | `["ALL"]` |
| `securityContext.readOnlyRootFilesystem` | Read-only filesystem | `true` |

### Resources

| Value | Description | Default |
|-------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `512Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |

### Leader Election

| Value | Description | Default |
|-------|-------------|---------|
| `leaderElection.enabled` | Enable leader election | `true` |

### Metrics

| Value | Description | Default |
|-------|-------------|---------|
| `metrics.enabled` | Enable metrics | `true` |
| `metrics.port` | Metrics port | `8080` |

### Health Probes

| Value | Description | Default |
|-------|-------------|---------|
| `health.port` | Health probe port | `8081` |

### Web UI

| Value | Description | Default |
|-------|-------------|---------|
| `webui.enabled` | Enable Web UI | `true` |
| `webui.port` | Web UI port | `8082` |
| `webui.namespace` | Namespace to watch | `""` (all) |
| `webui.service.type` | Service type | `ClusterIP` |
| `webui.service.port` | Service port | `8082` |

### Web UI Ingress

| Value | Description | Default |
|-------|-------------|---------|
| `webui.ingress.enabled` | Enable Ingress | `false` |
| `webui.ingress.className` | Ingress class | `""` |
| `webui.ingress.annotations` | Ingress annotations | `{}` |
| `webui.ingress.hosts` | Ingress hosts | See below |
| `webui.ingress.tls` | TLS configuration | `[]` |

### Web UI HTTPRoute (Gateway API)

| Value | Description | Default |
|-------|-------------|---------|
| `webui.httproute.enabled` | Enable HTTPRoute | `false` |
| `webui.httproute.annotations` | HTTPRoute annotations | `{}` |

## Examples

### Minimal Installation

```yaml
# values.yaml
replicaCount: 1
```

### Production Configuration

```yaml
# values.yaml
replicaCount: 1

resources:
  limits:
    cpu: 1
    memory: 1Gi
  requests:
    cpu: 200m
    memory: 256Mi

leaderElection:
  enabled: true

webui:
  enabled: true
  ingress:
    enabled: true
    className: nginx
    annotations:
      cert-manager.io/cluster-issuer: letsencrypt-prod
    hosts:
      - host: minecraft-operator.example.com
        paths:
          - path: /
            pathType: Prefix
    tls:
      - secretName: minecraft-operator-tls
        hosts:
          - minecraft-operator.example.com
```

### Web UI with Gateway API

```yaml
# values.yaml
webui:
  enabled: true
  httproute:
    enabled: true
    annotations:
      external-dns.alpha.kubernetes.io/hostname: minecraft-operator.example.com
```

### Disable CRD Management

```yaml
# values.yaml
crds:
  manage: false  # Operator will NOT apply CRDs at startup (manage them externally)
```

When `crds.manage` is `false`, you must apply CRDs manually before starting the operator:

```bash
kubectl apply --server-side --filename internal/crdmanager/crds/
```

## See Also

- [Installation](../getting-started/installation.md) — Installation guide
- [Configuration](../configuration/index.md) — CRD configuration
