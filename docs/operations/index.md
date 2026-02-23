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

```text
NAME        VERSION   BUILD   STATUS   AGE
survival    1.21.4    125     Ready    7d
creative    1.20.6    89      Ready    14d
```

### Check Plugin Status

```bash
kubectl get plugin --namespace minecraft
```

```text
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

The operator supports built-in VolumeSnapshot backups. Enable them in your PaperMCServer spec:

```yaml
spec:
  backup:
    enabled: true
    schedule: "0 */6 * * *"
    retention:
      maxCount: 10
```

Backups use RCON hooks (`save-all`/`save-off`/`save-on`) for data consistency.

**Trigger a manual backup:**

```bash
kubectl annotate papermcserver survival mc.k8s.lex.la/backup-now="$(date +%s)"
```

**List existing snapshots:**

```bash
kubectl get volumesnapshots -l mc.k8s.lex.la/server-name=survival
```

!!! tip "Pre-Update Backups"

    Set `backup.beforeUpdate: true` (default) to automatically create a snapshot before any server update.

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

### Prometheus Metrics

The operator exposes custom metrics at `/metrics` (port 8080):

| Metric | Type | Description |
| --- | --- | --- |
| `minecraft_operator_reconcile_total` | counter | Reconciliations by controller |
| `minecraft_operator_reconcile_errors_total` | counter | Failed reconciliations |
| `minecraft_operator_reconcile_duration_seconds` | histogram | Reconciliation latency |
| `minecraft_operator_plugin_api_requests_total` | counter | Plugin API requests by source |
| `minecraft_operator_plugin_api_errors_total` | counter | Failed plugin API requests |
| `minecraft_operator_plugin_api_duration_seconds` | histogram | Plugin API latency |
| `minecraft_operator_solver_runs_total` | counter | Solver invocations by type |
| `minecraft_operator_solver_errors_total` | counter | Failed solver invocations |
| `minecraft_operator_solver_duration_seconds` | histogram | Solver latency |
| `minecraft_operator_updates_total` | counter | Update attempts (success/failure) |

Access metrics locally:

```bash
kubectl port-forward svc/minecraft-operator-controller-manager-metrics-service \
  8080:8080 --namespace minecraft-operator-system
curl --insecure https://localhost:8080/metrics
```

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
