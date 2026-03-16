# Migration Guide: v1.x to v2.0

This guide covers breaking changes introduced in v2.0 and how to migrate existing resources.

## Breaking Changes Summary

| Change | Impact | Action Required |
| --- | --- | --- |
| `Plugin.spec.port` removed | Plugins using `port` field will fail validation | Replace with `endpoints` |
| Service port naming changed | External references to port names may break | Update any hardcoded port name references |

## Plugin: `port` replaced by `endpoints`

### What changed

The single `port` field has been replaced by `endpoints` — a list of named network endpoints with protocol awareness (TCP, UDP, HTTP).

**Before (v1.x):**

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: bluemap
spec:
  source:
    type: hangar
    project: BlueMap
  updateStrategy: latest
  port: 8100
  instanceSelector:
    matchLabels:
      type: vanilla
```

**After (v2.0):**

```yaml
apiVersion: mc.k8s.lex.la/v1beta1
kind: Plugin
metadata:
  name: bluemap
spec:
  source:
    type: hangar
    project: BlueMap
  updateStrategy: latest
  endpoints:
    - name: web-ui
      port: 8100
      protocol: HTTP
  instanceSelector:
    matchLabels:
      type: vanilla
```

### Migration steps

1. For each Plugin that uses `port`, replace it with an `endpoints` entry:

    ```yaml
    # Old
    port: 8123

    # New
    endpoints:
      - name: web-ui        # choose a descriptive name
        port: 8123
        protocol: HTTP       # or TCP/UDP depending on the plugin
    ```

2. Choose the correct `protocol`:

    - **HTTP** — for plugins with web interfaces (BlueMap, Dynmap, Plan, etc.)
    - **TCP** — for plugins exposing raw TCP (default if omitted)
    - **UDP** — for plugins using UDP

3. Apply the updated Plugin manifests:

    ```bash
    kubectl apply --filename plugin.yaml --server-side
    ```

### Choosing endpoint names

Endpoint names must be valid DNS labels (lowercase alphanumeric and hyphens, max 63 characters). Common conventions:

| Plugin Type | Suggested Name |
| --- | --- |
| Web map (BlueMap, Dynmap) | `web-ui` or `web-map` |
| Analytics (Plan) | `web-ui` |
| Metrics endpoint | `metrics` |
| Custom TCP service | `game-tcp` |
| Custom UDP service | `voice-udp` |

## Service Port Naming

### What changed

Service port names changed from `plugin-{port}-tcp`/`plugin-{port}-udp` to `ep-{port}-tcp`/`ep-{port}-udp`.

**Before:** `plugin-8100-tcp`, `plugin-8100-udp`

**After:** `ep-8100-tcp` (for TCP/HTTP endpoints), `ep-8100-udp` (for UDP endpoints)

### Impact

- If you reference port names in NetworkPolicy, Ingress, or other resources, update them
- If you use port numbers (not names), no change needed
- HTTP endpoints only create a TCP port (not both TCP+UDP as before)

## New Features in v2.0

These are additive (no migration needed) but worth knowing about:

### Plugin endpoints with HTTPRoute support

HTTP-protocol endpoints can now be routed through Gateway API HTTPRoutes. See [Plugin Configuration](../configuration/plugin.md#endpoints) and [Server Gateway Configuration](../configuration/papermcserver.md#gateway).

### Declarative config management

Plugin and server configuration files can now be managed via ConfigMap references. See [Plugin Configs](../configuration/plugin.md#configs) and [Server Configs](../configuration/papermcserver.md#pluginconfigs).

## Upgrade Procedure

1. **Before upgrading the operator:**

    Update all Plugin manifests to replace `port` with `endpoints`.

2. **Upgrade the operator:**

    ```bash
    helm upgrade minecraft-operator oci://ghcr.io/lexfrei/charts/papermc \
      --namespace minecraft-operator-system
    ```

3. **Verify:**

    ```bash
    # Check all Plugins are valid
    kubectl get plugins --all-namespaces

    # Check Services have correct ports
    kubectl get svc -l app=papermc
    ```

## Rollback

If you need to rollback to v1.x:

1. Revert Plugin manifests to use `port` instead of `endpoints`
2. Downgrade the operator Helm chart to the previous version
3. CRDs will need to be manually reverted (the operator applies CRDs at startup)
