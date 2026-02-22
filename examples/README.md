# Minecraft Operator Examples

This directory contains example Custom Resources for the Minecraft Operator.

## Quick Start

### 1. Install Operator

```bash
# Single step — CRDs are embedded and applied at startup via server-side apply
helm install minecraft-operator \
  ./charts/minecraft-operator \
  --namespace minecraft-operator-system \
  --create-namespace
```

### 2. Deploy a Simple Minecraft Server

```bash
kubectl apply -f examples/simple-server.yaml
```

This creates:
- PaperMCServer resource
- RCON Secret
- PersistentVolumeClaim for world data

The operator will automatically:
- Create a StatefulSet with the Paper server
- Create a LoadBalancer Service on port 25565
- Configure RCON on port 25575

### 3. Add Plugins (Optional)

```bash
kubectl apply -f examples/plugin-essentialsx.yaml
```

The operator will:
- Fetch plugin metadata from Hangar API
- Find the latest compatible version with the server
- Add plugin info to PaperMCServer status
- (Update controller would download and install the plugin)

## Check Status

```bash
# Check server status
kubectl get papermcserver test-server -o yaml

# Check plugin status
kubectl get plugin essentialsx -o yaml

# Check created resources
kubectl get statefulset,service,pvc -l app.kubernetes.io/instance=test-server

# Check operator logs
kubectl logs -n minecraft-operator-system -l control-plane=controller-manager -f
```

## Connect to Server

Get the LoadBalancer IP:

```bash
kubectl get svc test-server -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

Connect with Minecraft client to `<IP>:25565`

## RCON Access

```bash
# Get RCON password
kubectl get secret test-server-rcon -o jsonpath='{.data.password}' | base64 -d

# Use with RCON client
# Example: mcrcon -H <IP> -P 25575 -p <password> "list"
```

## Cleanup

```bash
# Delete server (will also delete StatefulSet and Service)
kubectl delete papermcserver test-server

# Delete plugin
kubectl delete plugin essentialsx

# Delete PVC and Secret manually if needed
kubectl delete pvc test-server-data
kubectl delete secret test-server-rcon

# Uninstall operator
helm uninstall minecraft-operator -n minecraft-operator-system
```

## Customization

### Paper Version Management

The operator supports four version management modes:

**1. Latest (`updateStrategy: "latest"`)**

- Always uses the latest available Paper version from Docker Hub
- Suitable for development/testing environments
- See: `examples/simple-server.yaml`

**2. Auto (`updateStrategy: "auto"`)**

- Solver automatically picks the best version compatible with ALL matched plugins
- Ideal for production with multiple plugins
- See: `examples/server-auto-version.yaml`

**3. Version Pin (`updateStrategy: "pin"`, `version: "1.21.10"`)**

- Pin major.minor.patch version, auto-update to latest build
- Operator verifies each build exists in Docker Hub before applying
- Good balance between stability and security updates
- Example: `version: "1.21.10"` → uses latest available build (e.g., 1.21.10-91)

**4. Full Pin (`updateStrategy: "build-pin"`, `version: "1.21.10"`, `build: 91`)**

- Pin specific version AND build number
- No automatic updates - full manual control
- Best for maximum stability
- See: `examples/server-pinned-version.yaml`

**Note:** The operator manages the container image automatically based on `updateStrategy` and `version`. The `image` field in `podTemplate.spec.containers` is ignored for the Paper container.

### Add More Plugins

Create additional Plugin resources with different `instanceSelector` to target specific servers:

```yaml
apiVersion: mc.k8s.lex.la/v1alpha1
kind: Plugin
metadata:
  name: worldedit
spec:
  source:
    type: hangar
    project: "WorldEdit"
  updateStrategy: "latest"
  instanceSelector:
    matchLabels:
      environment: creative  # Only on creative servers
```

### Resource Limits

Adjust in `podTemplate.spec.containers[0].resources`

### Storage Size

Adjust in PVC `spec.resources.requests.storage`

## Notes

- The operator uses Paper's `/data/plugins/update/` hot-swap mechanism
- Updates are only applied during maintenance windows
- `updateDelay` provides a grace period for community testing
- RCON password should be changed from default in production
