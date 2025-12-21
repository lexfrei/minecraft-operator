# Reference

Reference documentation for Minecraft Operator.

## Sections

<div class="grid cards" markdown>

-   :material-helm:{ .lg .middle } **Helm Chart**

    ---

    Helm chart values and configuration

    [:octicons-arrow-right-24: Helm Chart](helm-chart.md)

</div>

## API Reference

### API Group

```text
mc.k8s.lex.la/v1alpha1
```

### Resources

| Kind | Namespaced | Description |
|------|------------|-------------|
| PaperMCServer | Yes | Minecraft server instance |
| Plugin | Yes | Plugin for matched servers |

### Versions

| Version | Status |
|---------|--------|
| v1alpha1 | Current |

## Container Images

### Operator

```text
ghcr.io/lexfrei/minecraft-operator:latest
```

Available tags:

- `latest` — Latest stable release
- `v1.0.0` — Specific version
- `v1.0` — Minor version track
- `v1` — Major version track

### Paper Server

```text
lexfrei/papermc:1.21.4-125
```

Format: `lexfrei/papermc:{version}-{build}`

## Helm Charts

### Operator Chart

```text
oci://ghcr.io/lexfrei/minecraft-operator
```

### CRD Chart

```text
oci://ghcr.io/lexfrei/minecraft-operator-crds
```

## External Links

| Resource | URL |
|----------|-----|
| GitHub Repository | [lexfrei/minecraft-operator](https://github.com/lexfrei/minecraft-operator) |
| Issues | [GitHub Issues](https://github.com/lexfrei/minecraft-operator/issues) |
| Releases | [GitHub Releases](https://github.com/lexfrei/minecraft-operator/releases) |
| Hangar API | [hangar.papermc.io](https://hangar.papermc.io) |
| Paper Downloads | [papermc.io](https://papermc.io/downloads) |

## See Also

- [Configuration](../configuration/index.md) — CRD reference
- [Architecture](../architecture/index.md) — System design
