# Architecture

This section covers the internal architecture of Minecraft Operator.

## Overview

The operator consists of three main controllers that work together:

```mermaid
graph TB
    subgraph "Custom Resources"
        Plugin[Plugin CRD]
        Server[PaperMCServer CRD]
    end

    subgraph "Controllers"
        PC[Plugin Controller]
        SC[PaperMCServer Controller]
        UC[Update Controller]
    end

    subgraph "External APIs"
        Hangar[Hangar API]
        Docker[Docker Hub]
    end

    subgraph "Kubernetes Resources"
        STS[StatefulSet]
        SVC[Service]
        PVC[PersistentVolumeClaim]
    end

    Plugin --> PC
    Server --> SC

    PC --> Hangar
    PC --> Server

    SC --> Docker
    SC --> STS
    SC --> SVC
    SC --> PVC

    UC --> Server
    UC --> STS
```

## Controllers

| Controller | Responsibility |
|------------|----------------|
| **Plugin Controller** | Fetches plugin metadata, runs compatibility solver |
| **PaperMCServer Controller** | Manages StatefulSet, Service, resolves versions |
| **Update Controller** | Executes scheduled updates with graceful shutdown |

## Sections

<div class="grid cards" markdown>

-   :material-file-document:{ .lg .middle } **Design**

    ---

    Complete system architecture and design decisions

    [:octicons-arrow-right-24: Design](design.md)

-   :material-puzzle:{ .lg .middle } **Constraint Solver**

    ---

    How version compatibility is resolved

    [:octicons-arrow-right-24: Solver](solver.md)

-   :material-package-variant:{ .lg .middle } **Dependencies**

    ---

    External libraries, container images, and APIs

    [:octicons-arrow-right-24: Dependencies](dependencies.md)

</div>

## Key Design Principles

### Not High-Availability

This operator is designed for single-instance Minecraft servers. 5-10 minutes of downtime during updates is acceptable by design.

### Declarative Plugin Management

Plugins are matched to servers via label selectors, enabling:

- Many-to-many relationships between plugins and servers
- Centralized plugin management across multiple servers
- Automatic version resolution per server

### Constraint Solving

The operator uses a constraint solver to find compatible versions:

- **For Plugins**: Find maximum plugin version compatible with ALL matched servers
- **For Servers**: Find maximum Paper version compatible with ALL matched plugins

### Graceful Updates

Updates are applied safely:

1. RCON `save-all` command saves world data
2. RCON `stop` command gracefully stops the server
3. Pod termination respects `terminationGracePeriodSeconds`
4. StatefulSet recreates the pod with new version

## Technology Stack

| Component | Technology |
|-----------|------------|
| Framework | controller-runtime v0.22+ |
| Language | Go 1.25+ |
| Container | distroless/static-debian12:nonroot |
| Charts | Helm 3.14+ |
| API Group | `mc.k8s.lex.la/v1alpha1` |

## Next Steps

- [Design](design.md) — Detailed architecture documentation
- [Constraint Solver](solver.md) — Version resolution algorithm
