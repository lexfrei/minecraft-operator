# Prerequisites

Before installing Minecraft Operator, ensure your environment meets these requirements.

## Kubernetes Cluster

| Requirement | Minimum Version |
|-------------|-----------------|
| Kubernetes | 1.27+ |
| kubectl | 1.27+ |

The operator uses features available in Kubernetes 1.27 and later:

- StatefulSets with stable network identities
- Custom Resource Definitions (CRDs)
- RBAC for controller permissions

## Helm

| Requirement | Minimum Version |
|-------------|-----------------|
| Helm | 3.14+ |

Helm 3.14+ is required for OCI registry support used by the operator charts.

### Install Helm

=== "macOS"

    ```bash
    brew install helm
    ```

=== "Linux"

    ```bash
    curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
    ```

=== "Windows"

    ```powershell
    choco install kubernetes-helm
    ```

### Verify Installation

```bash
helm version
# version.BuildInfo{Version:"v3.14.0", ...}
```

## Storage

The operator creates StatefulSets with PersistentVolumeClaims for Minecraft world data.

!!! warning "Storage Class Required"

    Ensure your cluster has a default StorageClass or specify one in the PaperMCServer spec.

```bash
# Check available storage classes
kubectl get storageclass
```

## Optional: Container Runtime

If you plan to build custom images or run the operator locally:

| Tool | Purpose |
|------|---------|
| Podman or Docker | Container image builds |
| Go 1.26+ | Local development |

## Network Requirements

The operator needs to access:

| Endpoint | Purpose |
|----------|---------|
| `hub.docker.com` | Pull Paper server images |
| `hangar.papermc.io` | Plugin metadata and downloads |
| `api.modrinth.com` | (Future) Modrinth plugin support |

Ensure your cluster has outbound internet access or configure a proxy.

## Next Steps

Once prerequisites are met, proceed to [Installation](installation.md).
