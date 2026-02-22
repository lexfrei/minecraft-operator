# Contributing

Guidelines for contributing to Minecraft Operator.

## Getting Started

1. Fork the repository
2. Clone your fork
3. Set up development environment (see [Setup](setup.md))
4. Create a feature branch
5. Make changes
6. Submit a pull request

## Workflow

### Branch Naming

Use descriptive branch names:

```text
feat/modrinth-support
fix/rcon-timeout
docs/update-strategies
refactor/solver-algorithm
```

### Commit Messages

Use semantic commit format:

```text
type(scope): brief description

Optional longer explanation.

Co-Authored-By: Your Name <email@example.com>
```

**Types:**

| Type | Description |
|------|-------------|
| `feat` | New features |
| `fix` | Bug fixes |
| `docs` | Documentation |
| `refactor` | Code refactoring |
| `test` | Test changes |
| `chore` | Maintenance |
| `ci` | CI/CD changes |

**Example:**

```text
feat(plugins): add Modrinth source support

Implement Modrinth API client for fetching plugin metadata.
Supports version filtering by Minecraft version compatibility.

Co-Authored-By: Developer <dev@example.com>
```

### Pull Requests

1. **Create draft PR** initially
2. **Fill template** completely
3. **Request review** when ready
4. **Address feedback**
5. **Squash merge** when approved

## Code Standards

### Go Code

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` formatting
- Pass `golangci-lint` without errors
- Add tests for new code

### Testing

**TDD is mandatory:**

1. Write failing test first
2. Implement minimal code
3. Pass test
4. Refactor

```go
func TestSolverFindsCompatibleVersion(t *testing.T) {
    // 1. Write test first
    solver := NewSimpleSolver()
    result, err := solver.Solve(plugins, servers)

    require.NoError(t, err)
    assert.Equal(t, "1.21.1", result.PaperVersion)
}
```

### Logging

Use `log/slog` with constant messages:

```go
// Good
slog.InfoContext(ctx, "Update available", "version", version)

// Bad - variable in message
slog.InfoContext(ctx, fmt.Sprintf("Update to %s available", version))
```

## Review Checklist

Before submitting:

- [ ] Tests pass: `make test`
- [ ] Helm chart tests pass: `helm unittest charts/minecraft-operator`
- [ ] Linter passes: `make lint`
- [ ] Code generated: `make manifests generate`
- [ ] Documentation updated
- [ ] Commits signed off

## Areas for Contribution

### High Priority

- **Modrinth plugin source** support
- **Documentation improvements**
- **Plugin E2E tests** — requires mock Hangar server in-cluster

### Medium Priority

- Prometheus metrics
- Web UI enhancements
- Spigot plugin source support

### Good First Issues

Check [GitHub Issues](https://github.com/lexfrei/minecraft-operator/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22) for beginner-friendly tasks.

## Code of Conduct

Be respectful and constructive in all interactions.

## License

By contributing, you agree your code will be licensed under BSD-3-Clause.

## See Also

- [Setup](setup.md) — Development environment
- [Architecture](../architecture/index.md) — System design
- [GitHub Issues](https://github.com/lexfrei/minecraft-operator/issues) — Open tasks
