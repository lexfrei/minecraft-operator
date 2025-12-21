# Quick Start

Create your first Minecraft server with plugins using Minecraft Operator.

## Create a Namespace

Create a namespace for your Minecraft server:

```bash
kubectl create namespace minecraft
```

## Create RCON Secret

Create a secret for RCON password (used for graceful shutdown):

```bash
kubectl create secret generic my-server-rcon \
  --namespace minecraft \
  --from-literal=password=your-secure-password
```

## Create a PaperMCServer

Create a file `my-server.yaml`:

```yaml
apiVersion: mc.k8s.lex.la/v1alpha1
kind: PaperMCServer
metadata:
  name: my-server
  namespace: minecraft
  labels:
    environment: production
spec:
  # Use constraint solver to pick best version for plugins
  updateStrategy: "auto"

  # Check for updates daily, apply on Sundays
  updateSchedule:
    checkCron: "0 3 * * *"
    maintenanceWindow:
      enabled: true
      cron: "0 4 * * 0"

  # Graceful shutdown via RCON
  gracefulShutdown:
    timeout: 300s
  rcon:
    enabled: true
    passwordSecret:
      name: my-server-rcon
      key: password

  # Pod resources
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

Apply the manifest:

```bash
kubectl apply --filename my-server.yaml
```

## Verify Server Creation

Check the PaperMCServer status:

```bash
kubectl get papermcserver --namespace minecraft
```

Expected output:

```
NAME        VERSION   BUILD   STATUS   AGE
my-server   1.21.4    125     Ready    2m
```

Check the created resources:

```bash
# StatefulSet
kubectl get statefulset my-server --namespace minecraft

# Pod
kubectl get pods --namespace minecraft

# Service
kubectl get svc my-server --namespace minecraft
```

## Add a Plugin

Create a file `essentials-plugin.yaml`:

```yaml
apiVersion: mc.k8s.lex.la/v1alpha1
kind: Plugin
metadata:
  name: essentialsx
  namespace: minecraft
spec:
  source:
    type: hangar
    project: "EssentialsX/Essentials"

  # Always use latest compatible version
  updateStrategy: "latest"

  # Match servers with environment=production label
  instanceSelector:
    matchLabels:
      environment: production
```

Apply the plugin:

```bash
kubectl apply --filename essentials-plugin.yaml
```

## Verify Plugin

Check the Plugin status:

```bash
kubectl get plugin --namespace minecraft
```

Expected output:

```
NAME          SOURCE   VERSION   MATCHED   STATUS
essentialsx   hangar   2.21.0    1         Ready
```

The plugin will be downloaded and installed during the next maintenance window, or immediately if you restart the server.

## Connect to Your Server

Get the server's external IP or use port-forward:

```bash
# Port forward for local testing
kubectl port-forward svc/my-server 25565:25565 --namespace minecraft
```

Connect with your Minecraft client to `localhost:25565`.

## View Logs

Check server logs:

```bash
kubectl logs statefulset/my-server --namespace minecraft --follow
```

## Access Web UI

If Web UI is enabled, port-forward to view the dashboard:

```bash
kubectl port-forward svc/minecraft-operator-webui 8082:8082 \
  --namespace minecraft-operator-system
```

Open http://localhost:8082/ui in your browser.

## Next Steps

- [Configuration](../configuration/index.md) — Learn about all CRD options
- [Update Strategies](../configuration/update-strategies.md) — Understand version management
- [Troubleshooting](../operations/troubleshooting.md) — Common issues and solutions
