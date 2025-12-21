# Operations

This section covers day-to-day operations for running Minecraft servers with the operator.

## Overview

The Minecraft Operator automates most operational tasks:

- Version updates during maintenance windows
- Plugin compatibility resolution
- Graceful server shutdown

However, some situations require manual intervention.

## Sections

<div class="grid cards" markdown>

-   :material-wrench:{ .lg .middle } **Troubleshooting**

    ---

    Common issues and how to resolve them

    [:octicons-arrow-right-24: Troubleshoot](troubleshooting.md)

</div>

## Common Operations

### Check Server Status

```bash
kubectl get papermcserver --namespace minecraft
```

```
NAME        VERSION   BUILD   STATUS   AGE
survival    1.21.4    125     Ready    7d
creative    1.20.6    89      Ready    14d
```

### Check Plugin Status

```bash
kubectl get plugin --namespace minecraft
```

```
NAME          SOURCE   VERSION   MATCHED   STATUS
essentialsx   hangar   2.21.0    2         Ready
bluemap       hangar   5.4.0     1         Ready
```

### View Server Logs

```bash
kubectl logs statefulset/survival --namespace minecraft --follow
```

### Force Update Check

Delete the server pod to trigger immediate reconciliation:

```bash
kubectl delete pod survival-0 --namespace minecraft
```

!!! warning "World Data"

    Deleting the pod is safe - world data is stored on PVC.
    The server will restart with the same data.

### Manual RCON Access

Port-forward to RCON port:

```bash
kubectl port-forward statefulset/survival 25575:25575 --namespace minecraft
```

Connect with an RCON client:

```bash
# Using mcrcon
mcrcon -H localhost -P 25575 -p your-password "list"
```

### Access Web UI

```bash
kubectl port-forward svc/minecraft-operator-webui 8082:8082 \
  --namespace minecraft-operator-system
```

Open http://localhost:8082/ui

## Maintenance Tasks

### Backup World Data

World data is stored on PVCs. Back up using your preferred method:

```bash
# Example: Copy to local machine
kubectl cp minecraft/survival-0:/data/world ./backup/world
```

!!! tip "Scheduled Backups"

    Consider using Velero or similar tools for scheduled PVC backups.

### Scale Down for Maintenance

```bash
# Scale down (stops the server)
kubectl scale statefulset survival --replicas=0 --namespace minecraft

# Scale back up
kubectl scale statefulset survival --replicas=1 --namespace minecraft
```

### View Operator Logs

```bash
kubectl logs deployment/minecraft-operator \
  --namespace minecraft-operator-system \
  --follow
```

Filter by log level:

```bash
# Errors only
kubectl logs deployment/minecraft-operator \
  --namespace minecraft-operator-system | grep '"level":"error"'
```

## Monitoring

### Resource Status

Check conditions on resources:

```bash
kubectl get papermcserver survival --namespace minecraft \
  --output jsonpath='{.status.conditions}'
```

### Update Status

Check for pending updates:

```bash
kubectl get papermcserver survival --namespace minecraft \
  --output jsonpath='{.status.availableUpdate}'
```

### Plugin Compatibility

Check if updates are blocked:

```bash
kubectl get papermcserver survival --namespace minecraft \
  --output jsonpath='{.status.updateBlocked}'
```

## See Also

- [Troubleshooting](troubleshooting.md) — Common issues and solutions
- [Configuration](../configuration/index.md) — CRD reference
