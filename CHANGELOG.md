# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Embedded CRD management via `--manage-crds` flag — operator applies CRDs at startup
  using server-side apply, eliminating the need for a separate CRD Helm chart install
- Plugin deletion lifecycle with finalizer and JAR cleanup tracking
- Immediate update apply via `mc.k8s.lex.la/apply-now` annotation
- Configurable Service per PaperMCServer (type, annotations, extra ports for plugins)
- RCON graceful shutdown with configurable timeout and save-all wait
- `updateDelay` field for both PaperMCServer and Plugin to delay auto-updates
- `compatibilityOverride` field for Plugin to manually specify compatibility
- Web UI: plugin CRUD, solver progress indicator, server labels display
- Service layer and REST API with OpenAPI spec
- Hangar download API fallback for externally-hosted plugins
- Docker Hub Registry API client for image tag verification
- Comprehensive test suites: controller reconciliation flows, solver, Helm unittest,
  E2E lifecycle tests
- PR validation CI workflow with multi-arch builds
- Release CI workflow with Cosign signing and SBOM generation
- MkDocs documentation site with versioning

### Changed

- **BREAKING**: API version graduated from `v1alpha1` to `v1beta1`. All manifests must
  change `apiVersion: mc.k8s.lex.la/v1alpha1` to `mc.k8s.lex.la/v1beta1`. The `v1alpha1`
  version is removed entirely — re-apply resources with the new apiVersion after upgrading
- **BREAKING**: CRD field renames in PaperMCServer: `paperVersion` → `version`,
  `paperBuild` → `build`
- **BREAKING**: CRD field renames in Plugin: `versionPolicy` → `updateStrategy`,
  `pinnedVersion` → `version`
- **BREAKING**: Four update strategies replace previous version policy:
  `latest`, `auto`, `pin`, `build-pin`
- **BREAKING**: Operator never uses `:latest` Docker tag — all image references resolve
  to concrete `version-build` tags (e.g., `docker.io/lexfrei/papermc:1.21.1-91`)
- Migrated logging from Zap to stdlib `log/slog` with `--log-level` and `--log-format`
  flags
- Updated Go to 1.26.0
- Updated controller-runtime to v0.23.1
- Constraint solver now used for plugin version pairs in available updates

### Removed

- **BREAKING**: Separate CRD Helm chart (`minecraft-operator-crds`) — CRDs are now
  embedded in the operator binary and applied at startup
- Kustomize deployment support (Helm-only)
- Deprecated version resolution code

### Fixed

- Invalid cron schedule now sets `CronScheduleValid` condition instead of causing
  infinite error-retry loop
- Shell injection vulnerability in curl arguments for plugin downloads
- XSS vulnerability in Web UI HTML responses
- Plugin deletion deadlock when server is deleted during plugin cleanup
- Race condition in plugin reconcileDelete after finalizer removal
- Never-installed plugins no longer block deletion with 10-minute timeout
- Missing `docker.io` prefix in container image tags
- Context propagation in slog calls and maintenance window checks
- Conditions persistence (setCondition before Status().Update())
- Status comparison for Conditions, MatchedInstances, and AvailableVersions fields
- Time calculations using injectable clock instead of `time.Since()`
- RCON.Enabled guard preventing shutdown calls when RCON is disabled
- UpdateHistory comparison including AppliedAt field
- Early return when plugin repository is unavailable (no versions)

### Security

- Container runs as non-root user with read-only filesystem
- RBAC scoped to specific CRD resource names where possible
- Secure metrics endpoint with TokenReview and SubjectAccessReview permissions

## [0.5.0] - 2025-12-22

### Added

- PaperMCServer controller with StatefulSet management and automatic build updates
- Plugin controller with Hangar repository integration and constraint solver
- Update controller with cron-based scheduling and maintenance windows
- Web UI for server and plugin management
- RCON client interface with mock for testing
- Helm chart with separate CRD chart for independent lifecycle
- Platform compatibility support for plugins via go-hangar library
- Configurable metrics endpoint with secure RBAC
- E2E test framework with Kind cluster
- Containerized builds with scratch base image, UPX compression, and Cosign signing
- Renovate configuration for automated dependency updates

### Changed

- Migrated from Kustomize to Helm-only deployment
- Integrated goPaperMC library for Paper API interactions
- Integrated go-hangar library for Hangar plugin repository

[Unreleased]: https://github.com/lexfrei/minecraft-operator/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/lexfrei/minecraft-operator/releases/tag/v0.5.0
