# Troubleshooting

Common issues and their solutions when running Minecraft Operator.

## Server Issues

### Server Not Starting

**Symptoms**: Pod stuck in `Pending` or `CrashLoopBackOff`

**Check pod status**:

```bash
kubectl describe pod survival-0 --namespace minecraft
```

**Common causes**:

=== "Insufficient Resources"

    ```
    Events:
      Warning  FailedScheduling  Insufficient memory
    ```

    **Solution**: Reduce memory requests or add nodes:

    ```yaml
    spec:
      podTemplate:
        spec:
          containers:
            - name: minecraft
              resources:
                requests:
                  memory: "1Gi"  # Reduce from 2Gi
    ```

=== "PVC Not Bound"

    ```
    Events:
      Warning  FailedMount  persistentvolumeclaim "data-survival-0" not found
    ```

    **Solution**: Check StorageClass availability:

    ```bash
    kubectl get storageclass
    kubectl get pvc --namespace minecraft
    ```

=== "Image Pull Error"

    ```
    Events:
      Warning  Failed  Failed to pull image
    ```

    **Solution**: Check image name and registry access:

    ```bash
    kubectl get papermcserver survival --namespace minecraft \
      --output jsonpath='{.status.desiredVersion}'
    ```

### Server Crashes on Startup

**Symptoms**: Pod restarts repeatedly

**Check logs**:

```bash
kubectl logs survival-0 --namespace minecraft --previous
```

**Common causes**:

=== "Java Memory Issues"

    Look for `OutOfMemoryError` in logs.

    **Solution**: Increase memory limits:

    ```yaml
    spec:
      podTemplate:
        spec:
          containers:
            - name: minecraft
              resources:
                limits:
                  memory: "4Gi"  # Increase limit
              env:
                - name: JAVA_OPTS
                  value: "-Xmx3G"  # Max heap < limit
    ```

=== "Plugin Conflict"

    Look for plugin errors in startup logs.

    **Solution**: Temporarily remove plugins:

    ```bash
    # Delete problematic plugin
    kubectl delete plugin problematic-plugin --namespace minecraft

    # Restart server
    kubectl delete pod survival-0 --namespace minecraft
    ```

=== "World Corruption"

    Look for `RegionFileVersion` or chunk errors.

    **Solution**: Restore from backup or delete world:

    ```bash
    # DANGER: This deletes the world
    kubectl exec -it survival-0 --namespace minecraft -- rm -rf /data/world
    kubectl delete pod survival-0 --namespace minecraft
    ```

## Plugin Issues

### Plugin Not Installing

**Symptoms**: Plugin shows `Ready` but not installed on server

**Check plugin status**:

```bash
kubectl get plugin essentialsx --namespace minecraft --output yaml
```

**Common causes**:

=== "Selector Mismatch"

    Plugin doesn't match server labels.

    **Check**:

    ```bash
    # Plugin selector
    kubectl get plugin essentialsx --namespace minecraft \
      --output jsonpath='{.spec.instanceSelector}'

    # Server labels
    kubectl get papermcserver survival --namespace minecraft \
      --output jsonpath='{.metadata.labels}'
    ```

    **Solution**: Fix labels or selector to match.

=== "Compatibility Issue"

    Plugin incompatible with server version.

    **Check**:

    ```bash
    kubectl get plugin essentialsx --namespace minecraft \
      --output jsonpath='{.status.matchedInstances}'
    ```

    **Solution**: Use compatibility override or downgrade server:

    ```yaml
    spec:
      compatibilityOverride:
        enabled: true
        minecraftVersions:
          - "1.21.1"
          - "1.21.4"
    ```

=== "Update Not Applied Yet"

    Updates only apply during maintenance windows.

    **Check**:

    ```bash
    kubectl get papermcserver survival --namespace minecraft \
      --output jsonpath='{.spec.updateSchedule}'
    ```

    **Solution**: Wait for maintenance window or force restart:

    ```bash
    kubectl delete pod survival-0 --namespace minecraft
    ```

### Plugin Download Failed

**Symptoms**: Plugin status shows errors

**Check conditions**:

```bash
kubectl get plugin bluemap --namespace minecraft \
  --output jsonpath='{.status.conditions}'
```

**Common causes**:

=== "Repository Unavailable"

    ```yaml
    status:
      repositoryStatus: unavailable
    ```

    **Solution**: Wait for Hangar to recover. Cached versions are used.

=== "Network Issues"

    Operator cannot reach Hangar API.

    **Check operator logs**:

    ```bash
    kubectl logs deployment/minecraft-operator \
      --namespace minecraft-operator-system | grep -i hangar
    ```

    **Solution**: Check network policies and egress rules.

## Update Issues

### Updates Blocked

**Symptoms**: `updateBlocked: true` in server status

**Check reason**:

```bash
kubectl get papermcserver survival --namespace minecraft \
  --output jsonpath='{.status.updateBlocked}'
```

**Common causes**:

=== "Plugin Incompatibility"

    ```yaml
    updateBlocked:
      blocked: true
      reason: "Plugin incompatible with target version"
      blockedBy:
        plugin: "old-plugin"
        supportedVersions: ["1.20.4"]
    ```

    **Solution**:

    1. Wait for plugin update
    2. Use compatibility override
    3. Remove incompatible plugin

=== "No Compatible Version"

    All plugins together have no common compatible version.

    **Solution**: Review plugin combinations and consider removing one.

### Updates Not Happening

**Symptoms**: `availableUpdate` exists but not applied

**Check schedule**:

```bash
kubectl get papermcserver survival --namespace minecraft \
  --output jsonpath='{.spec.updateSchedule}'
```

**Common causes**:

=== "Outside Maintenance Window"

    Updates only apply during scheduled windows.

    **Check current cron**:

    ```yaml
    spec:
      updateSchedule:
        maintenanceWindow:
          enabled: true
          cron: "0 4 * * 0"  # Sunday 4am only
    ```

=== "Update Delay Active"

    `updateDelay` prevents immediate updates.

    **Check delay**:

    ```bash
    kubectl get papermcserver survival --namespace minecraft \
      --output jsonpath='{.spec.updateDelay}'
    ```

=== "Maintenance Window Disabled"

    ```yaml
    spec:
      updateSchedule:
        maintenanceWindow:
          enabled: false  # Updates disabled!
    ```

## Operator Issues

### Operator Not Running

**Check deployment**:

```bash
kubectl get deployment minecraft-operator \
  --namespace minecraft-operator-system
```

**Check pod status**:

```bash
kubectl describe pod -l app=minecraft-operator \
  --namespace minecraft-operator-system
```

### Operator Errors

**Check logs**:

```bash
kubectl logs deployment/minecraft-operator \
  --namespace minecraft-operator-system \
  --tail=100
```

**Common errors**:

=== "RBAC Issues"

    ```
    cannot list resource "papermcservers" in API group "mc.k8s.lex.la"
    ```

    **Solution**: Reinstall or check RBAC:

    ```bash
    kubectl get clusterrole minecraft-operator-manager-role --output yaml
    ```

=== "CRD Not Found"

    ```
    no matches for kind "PaperMCServer" in version "mc.k8s.lex.la/v1alpha1"
    ```

    **Solution**: CRDs are embedded in the operator and applied at startup. Check that
    `crds.manage` is `true` in Helm values (default), or apply CRDs manually:

    ```bash
    kubectl apply --server-side --filename internal/crdmanager/crds/
    ```

## Getting Help

If you can't resolve an issue:

1. **Check existing issues**: [GitHub Issues](https://github.com/lexfrei/minecraft-operator/issues)
2. **Create a new issue** with:
   - Operator version
   - Kubernetes version
   - Resource YAML (sanitized)
   - Operator logs
   - Pod events (`kubectl describe pod`)

## See Also

- [Operations](index.md) — Common operational tasks
- [Configuration](../configuration/index.md) — CRD reference
- [Architecture](../architecture/index.md) — How the operator works
