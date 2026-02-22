# Minecraft Operator

Kubernetes operator for managing PaperMC servers with automatic version management for plugins and the server, ensuring compatibility through constraint solving.

## Features

- **Automatic Version Management** — Four update strategies: `latest`, `auto`, `pin`, `build-pin`
- **Plugin Compatibility Solver** — Constraint solver ensures plugins work with selected Paper version
- **Scheduled Updates** — Cron-based maintenance windows with graceful RCON shutdown
- **Declarative Plugin Management** — Plugins are matched to servers via label selectors
- **Web UI** — Built-in dashboard for monitoring servers and plugins
- **Hangar Integration** — Automatic plugin downloads from PaperMC Hangar

!!! warning "Not a High-Availability System"

    This operator is designed for single-instance Minecraft servers.
    5-10 minutes of downtime during updates is acceptable by design.

## Quick Start

```bash
# Single step — CRDs are embedded and applied at startup via server-side apply
helm install minecraft-operator \
  oci://ghcr.io/lexfrei/minecraft-operator \
  --namespace minecraft-operator-system \
  --create-namespace
```

See [Getting Started](getting-started/index.md) for detailed setup instructions.

## Documentation Sections

<div class="grid cards" markdown>

-   :material-rocket-launch:{ .lg .middle } **Getting Started**

    ---

    Prerequisites, installation, and your first Minecraft server

    [:octicons-arrow-right-24: Get started](getting-started/index.md)

-   :material-cog:{ .lg .middle } **Configuration**

    ---

    CRD reference for PaperMCServer, Plugin, and update strategies

    [:octicons-arrow-right-24: Configure](configuration/index.md)

-   :material-sitemap:{ .lg .middle } **Architecture**

    ---

    System design, constraint solver, and update workflow

    [:octicons-arrow-right-24: Learn more](architecture/index.md)

-   :material-wrench:{ .lg .middle } **Operations**

    ---

    Troubleshooting, monitoring, and maintenance

    [:octicons-arrow-right-24: Operate](operations/index.md)

-   :material-code-tags:{ .lg .middle } **Development**

    ---

    Development setup, testing, and contributing

    [:octicons-arrow-right-24: Develop](development/index.md)

-   :material-book-open-variant:{ .lg .middle } **Reference**

    ---

    Helm chart values, API reference, and more

    [:octicons-arrow-right-24: Reference](reference/index.md)

</div>

## Project Links

- [GitHub Repository](https://github.com/lexfrei/minecraft-operator)
- [Issues](https://github.com/lexfrei/minecraft-operator/issues)
- [Releases](https://github.com/lexfrei/minecraft-operator/releases)

## License

BSD 3-Clause License - see [LICENSE](https://github.com/lexfrei/minecraft-operator/blob/master/LICENSE) for details.
