# Dependencies

This project relies on several external components and libraries.

## Container Image

### Why lexfrei/papermc?

The operator uses `lexfrei/papermc` — a minimal, predictable PaperMC image designed for Kubernetes.

**Key difference from itzg/docker-minecraft-server:**

| Aspect | lexfrei/papermc | itzg/docker-minecraft-server |
|--------|-----------------|------------------------------|
| JAR location | Baked into image at build time | Downloaded at container startup |
| Dockerfile | ~40 lines, direct `java -jar` | Hundreds of lines + scripts |
| Scope | Paper only | Vanilla, Forge, Fabric, etc. |
| Startup | Instant (JAR already present) | Network-dependent |

**Why this matters for the operator:**

1. **Predictable deployments** — The JAR is part of the image, not downloaded at runtime. No network failures during pod startup.

2. **Simpler debugging** — Direct Java execution, no wrapper scripts to troubleshoot.

3. **Version pinning** — Tag `1.21.4-125` contains exactly that version. No `VERSION=LATEST` magic.

4. **Kubernetes-native** — Runs as UID 9001 (non-root), includes socket-based health checks.

**Source:** [github.com/lexfrei/papermc-docker](https://github.com/lexfrei/papermc-docker)

### Image Tags

Format: `lexfrei/papermc:{version}-{build}`

Examples:

- `lexfrei/papermc:1.21.4-125`
- `lexfrei/papermc:1.20.6-148`

The operator resolves concrete tags based on `updateStrategy`:

| Strategy | Tag Resolution |
|----------|----------------|
| `latest` | Newest version-build from Docker Hub |
| `auto` | Solver-picked compatible version-build |
| `pin` | Specified version, latest build |
| `build-pin` | Exact version-build |

## Go Libraries

### go-hangar

Client library for the PaperMC Hangar API.

**Repository:** [github.com/lexfrei/go-hangar](https://github.com/lexfrei/go-hangar)

**Purpose:** Fetches plugin metadata (versions, compatibility, download URLs) from [hangar.papermc.io](https://hangar.papermc.io).

**Usage in operator:**

```go
import "github.com/lexfrei/go-hangar"

client := hangar.NewClient()
versions, err := client.GetProjectVersions("EssentialsX/Essentials")
```

### Why a custom client?

1. **Type-safe API** — Strongly typed responses for plugin metadata
2. **Pagination handling** — Automatic handling of paginated results
3. **Rate limiting** — Built-in rate limiting to respect API limits
4. **Caching-friendly** — Response structures designed for caching

## External APIs

### Hangar API

**URL:** `https://hangar.papermc.io/api/v1/`

**Used for:**

- Plugin metadata (versions, compatibility)
- Download URLs
- Release dates (for `updateDelay` calculation)

**Rate limits:** Respected via client-side throttling

### Docker Hub Registry API

**URL:** `https://hub.docker.com/v2/`

**Used for:**

- Listing available Paper versions
- Getting image digests for version resolution

## Kubernetes Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| controller-runtime | v0.22+ | Operator framework |
| client-go | v0.34+ | Kubernetes API client |
| apimachinery | v0.34+ | API types and utilities |

## Future Dependencies

### Modrinth API (Planned)

**URL:** `https://api.modrinth.com/v2/`

**Status:** Not yet implemented (see [#2](https://github.com/lexfrei/minecraft-operator/issues/2))

### SpigotMC API (Planned)

**Status:** Not yet implemented (see [#3](https://github.com/lexfrei/minecraft-operator/issues/3))

## See Also

- [Architecture](design.md) — System design
- [papermc-docker](https://github.com/lexfrei/papermc-docker) — Container image source
- [go-hangar](https://github.com/lexfrei/go-hangar) — Hangar API client
