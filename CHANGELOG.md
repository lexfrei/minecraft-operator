# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.0] - 2025-12-22

### Added

- PaperMCServer CRD for managing PaperMC Minecraft servers
- Plugin CRD for declarative plugin management via Hangar repository
- Constraint solver for automatic version compatibility resolution
- Four update strategies: `latest`, `auto`, `pin`, `build-pin`
- Graceful shutdown with RCON support
- Web UI for server and plugin management
- Helm charts for operator and CRDs deployment
- Multi-architecture container images (amd64, arm64)
- Comprehensive documentation site

### Changed

- Renamed API fields for clarity: `paperVersion` to `version`, `paperBuild` to `build`, `versionPolicy` to `updateStrategy`

### Fixed

- Plugin deadlock when server deleted during plugin cleanup
- Solver progress indicator with proper condition tracking

[Unreleased]: https://github.com/lexfrei/minecraft-operator/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/lexfrei/minecraft-operator/releases/tag/v0.5.0
