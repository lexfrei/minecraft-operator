# minecraft-operator

[![Release](https://img.shields.io/github/v/release/lexfrei/minecraft-operator)](https://github.com/lexfrei/minecraft-operator/releases)
[![CI](https://github.com/lexfrei/minecraft-operator/actions/workflows/pr.yaml/badge.svg)](https://github.com/lexfrei/minecraft-operator/actions/workflows/pr.yaml)
[![Release](https://github.com/lexfrei/minecraft-operator/actions/workflows/release.yaml/badge.svg)](https://github.com/lexfrei/minecraft-operator/actions/workflows/release.yaml)

Kubernetes operator for managing PaperMC Minecraft servers

## Features

- Automatic version management for Paper and plugins
- Constraint solver ensures plugin compatibility
- Scheduled updates with graceful RCON shutdown
- VolumeSnapshot-based backups with RCON consistency hooks
- Declarative plugin management via label selectors
- Built-in Web UI for monitoring
- Multi-arch container images (amd64, arm64)
- Signed container images with cosign

> **Note:** This operator is designed for single-instance Minecraft servers. 5-10 minutes of downtime during updates is acceptable by design.

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| lexfrei | <f@lex.la> | <https://github.com/lexfrei> |

## Requirements

Kubernetes: `>=1.27.0-0`

## Prerequisites

- Kubernetes 1.27+
- Helm 3.14+

## Installation

CRDs are embedded in the operator binary and applied automatically at startup via server-side apply. A single Helm install deploys a fully working operator.

```bash
helm install minecraft-operator \
  oci://ghcr.io/lexfrei/minecraft-operator \
  --version 0.0.0-dev \
  --namespace minecraft-operator-system \
  --create-namespace
```

### Verify Chart Signature

Charts are signed with cosign. Verify before installing:

```bash
cosign verify ghcr.io/lexfrei/minecraft-operator:0.0.0-dev \
  --certificate-identity-regexp="https://github.com/lexfrei/minecraft-operator" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
```

## Quick Start

After installation, create a Minecraft server:

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: PaperMCServer
metadata:
  name: my-server
  namespace: minecraft
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

Add a plugin:

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

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](https://minecraft-operator.k8s.lex.la/getting-started/) | Installation and first steps |
| [Configuration](https://minecraft-operator.k8s.lex.la/configuration/) | CRD reference |
| [Update Strategies](https://minecraft-operator.k8s.lex.la/configuration/update-strategies/) | Version management guide |
| [Architecture](https://minecraft-operator.k8s.lex.la/architecture/) | System design |
| [Troubleshooting](https://minecraft-operator.k8s.lex.la/operations/troubleshooting/) | Common issues |

## Update Strategies

| Strategy | Description |
|----------|-------------|
| `latest` | Always use newest Paper version |
| `auto` | Solver finds compatible version |
| `pin` | Fixed version, auto-update builds |
| `build-pin` | Fully pinned version and build |

## Backups

Enable VolumeSnapshot-based backups in the PaperMCServer spec:

```yaml
spec:
  backup:
    enabled: true
    schedule: "0 */6 * * *"     # Every 6 hours
    beforeUpdate: true           # Backup before any update
    volumeSnapshotClassName: "" # Optional: specific VolumeSnapshotClass
    retention:
      maxCount: 10               # Keep last 10 snapshots
```

When RCON is enabled, the operator uses RCON hooks (`save-all`, `save-off`, `save-on`)
to ensure data consistency before creating a VolumeSnapshot from the server PVC.
Without RCON, snapshots are crash-consistent only.

Trigger a manual backup:

```bash
kubectl annotate papermcserver my-server \
  mc.k8s.lex.la/backup-now="$(date +%s)" \
  --namespace minecraft
```

**Requirements:** A CSI driver with VolumeSnapshot support must be installed in
the cluster (e.g., `csi-driver-host-path`, cloud provider CSI drivers).

## Web UI

Access the built-in dashboard:

```bash
kubectl port-forward svc/minecraft-operator-webui 8082:8082 \
  --namespace minecraft-operator-system
```

Open http://localhost:8082/ui

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` |  |
| crds.manage | bool | `true` | Operator manages CRDs at startup via server-side apply. Set to false to disable CRD management (e.g. if CRDs are managed externally). |
| fullnameOverride | string | `""` |  |
| health.port | int | `8081` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"ghcr.io/lexfrei/minecraft-operator"` |  |
| image.tag | string | `""` | Overrides the image tag whose default is the chart appVersion |
| imagePullSecrets | list | `[]` |  |
| leaderElection.enabled | bool | `true` |  |
| metrics.enabled | bool | `true` |  |
| metrics.port | int | `8080` |  |
| metrics.serviceMonitor.enabled | bool | `false` | Enable ServiceMonitor for Prometheus Operator auto-discovery |
| metrics.serviceMonitor.endpointAuth | object | `{}` | Endpoint authentication configuration. Configure based on your Prometheus Operator version:  Modern (Prometheus Operator >= 0.52):   endpointAuth:     authorization:       credentials:         name: prometheus-token-secret         key: token  Legacy (still functional but deprecated):   endpointAuth:     bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token |
| metrics.serviceMonitor.interval | string | `""` | Scrape interval (e.g., "30s"). Uses Prometheus default if empty |
| metrics.serviceMonitor.labels | object | `{}` | Additional labels for ServiceMonitor (e.g., for Prometheus selector matching) |
| metrics.serviceMonitor.scrapeTimeout | string | `""` | Scrape timeout (e.g., "10s"). Uses Prometheus default if empty |
| nameOverride | string | `""` |  |
| networkPolicy.enabled | bool | `false` | Enable NetworkPolicy for the operator pod. Restricts ingress to metrics/health/webui ports and egress to DNS + Kubernetes API. Requires a CNI that supports NetworkPolicy (Calico, Cilium, etc.). |
| networkPolicy.rconPort | int | `25575` | RCON port for egress rules to managed Minecraft server pods. Change this if your servers use a non-default RCON port. |
| nodeSelector | object | `{}` |  |
| podAnnotations | object | `{}` |  |
| podLabels | object | `{}` |  |
| podSecurityContext.runAsNonRoot | bool | `true` |  |
| podSecurityContext.runAsUser | int | `65534` |  |
| podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| replicaCount | int | `1` |  |
| resources.limits.cpu | string | `"500m"` |  |
| resources.limits.memory | string | `"512Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.memory | string | `"128Mi"` |  |
| securityContext.allowPrivilegeEscalation | bool | `false` |  |
| securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| securityContext.readOnlyRootFilesystem | bool | `true` |  |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account |
| serviceAccount.automountServiceAccountToken | bool | `true` | Automatically mount a ServiceAccount's API credentials |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created |
| serviceAccount.name | string | `""` | The name of the service account to use. If not set and create is true, a name is generated using the fullname template. |
| tolerations | list | `[]` |  |
| webhook.caBundle | string | `""` | Base64-encoded CA bundle for webhook TLS verification. Required when certManager.enabled is false. The API server uses this CA to verify the webhook's TLS certificate. You can generate one with:   openssl req -x509 -newkey rsa:4096 -keyout tls.key -out tls.crt -days 365 -nodes -subj '/CN=webhook'   kubectl create secret tls <release>-webhook-cert --cert=tls.crt --key=tls.key -n <namespace>   caBundle: $(base64 < tls.crt) |
| webhook.certManager | object | `{"enabled":false,"issuerRef":{"kind":"ClusterIssuer","name":"selfsigned-issuer"}}` | cert-manager integration for webhook TLS certificates. |
| webhook.certManager.enabled | bool | `false` | Enable cert-manager Certificate resource for webhook TLS. If false, the operator generates self-signed certificates at startup. |
| webhook.certManager.issuerRef.kind | string | `"ClusterIssuer"` | Issuer kind (Issuer or ClusterIssuer). |
| webhook.certManager.issuerRef.name | string | `"selfsigned-issuer"` | Issuer name. |
| webhook.enabled | bool | `false` | Enable validating admission webhooks for Plugin and PaperMCServer CRDs. Validates semantic rules (strategy/field consistency, cron syntax, etc.) at admission time. |
| webhook.failurePolicy | string | `"Fail"` | Failure policy for webhooks. "Fail" rejects invalid resources, "Ignore" allows them through. Use "Fail" in production, "Ignore" during initial rollout or debugging. |
| webui.enabled | bool | `true` |  |
| webui.httproute.annotations | object | `{}` |  |
| webui.httproute.enabled | bool | `false` |  |
| webui.httproute.hostnames[0] | string | `"minecraft-operator.local"` |  |
| webui.httproute.parentRefs[0].name | string | `"gateway"` |  |
| webui.httproute.parentRefs[0].namespace | string | `"gateway-system"` |  |
| webui.ingress.annotations | object | `{}` |  |
| webui.ingress.className | string | `""` |  |
| webui.ingress.enabled | bool | `false` |  |
| webui.ingress.hosts[0].host | string | `"minecraft-operator.local"` |  |
| webui.ingress.hosts[0].paths[0].path | string | `"/"` |  |
| webui.ingress.hosts[0].paths[0].pathType | string | `"Prefix"` |  |
| webui.ingress.tls | list | `[]` |  |
| webui.namespace | string | `""` |  |
| webui.port | int | `8082` |  |
| webui.service.port | int | `8082` |  |
| webui.service.type | string | `"ClusterIP"` |  |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
