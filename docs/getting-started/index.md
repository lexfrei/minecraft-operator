# Getting Started

Welcome to Minecraft Operator! This section will guide you through setting up the operator and creating your first Minecraft server.

## Overview

The Minecraft Operator manages PaperMC servers in Kubernetes with:

- Automatic version updates for Paper and plugins
- Compatibility solving between plugins and server versions
- Scheduled maintenance windows with graceful shutdown
- Declarative configuration via Custom Resources

## Sections

<div class="grid cards" markdown>

-   :material-checkbox-marked-circle:{ .lg .middle } **Prerequisites**

    ---

    Required tools and Kubernetes cluster setup

    [:octicons-arrow-right-24: Check prerequisites](prerequisites.md)

-   :material-download:{ .lg .middle } **Installation**

    ---

    Install the operator using Helm

    [:octicons-arrow-right-24: Install](installation.md)

-   :material-play:{ .lg .middle } **Quick Start**

    ---

    Create your first server and add plugins

    [:octicons-arrow-right-24: Get started](quickstart.md)

</div>

## What You'll Learn

1. **Prerequisites** — Kubernetes and Helm version requirements
2. **Installation** — How to install CRDs and the operator via Helm
3. **Quick Start** — Create a PaperMCServer and add plugins

## Next Steps

After completing this guide, explore:

- [Configuration](../configuration/index.md) — Detailed CRD reference
- [Update Strategies](../configuration/update-strategies.md) — Version management options
- [Architecture](../architecture/index.md) — How the operator works
