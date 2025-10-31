# Minecraft Operator Examples

This directory contains example Custom Resources for the Minecraft Operator.

## Quick Start

### 1. Install CRDs and Operator

```bash
# Install CRD chart
helm install minecraft-operator-crds \
  ./charts/minecraft-operator-crds \
  --namespace minecraft-operator-system \
  --create-namespace

# Install operator chart
helm install minecraft-operator \
  ./charts/minecraft-operator \
  --namespace minecraft-operator-system
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
helm uninstall minecraft-operator-crds -n minecraft-operator-system
```

## Customization

### Change Minecraft Version

```yaml
spec:
  paperVersion: "1.21.1"  # Pin to specific version
```

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
  versionPolicy: latest
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
