# Examples

This directory contains standalone, runnable examples demonstrating axe packages.

Each example is self-contained and focuses on a specific capability.
All examples work **without** the full axe framework — just `go get` the package you need.

## Quick Start

```bash
# Run any example:
cd examples/01-apperror
go run main.go

# Or from the root:
go run ./examples/01-apperror
```

## Examples

| # | Name | Package | Description |
|---|---|---|---|
| 01 | [Error Taxonomy](01-apperror/) | `pkg/apperror` | Typed HTTP errors with consistent JSON responses |
| 02 | [Transactions](02-txmanager/) | `pkg/txmanager` | Unit of Work pattern — no more manual `*sql.Tx` passing |
| 03 | [Plugin System](03-plugin/) | `pkg/plugin` | Build and register a custom plugin with dependency injection |

## Using These in Your Project

See the [Incremental Adoption Guide](../docs/guides/incremental-adoption.md) for
step-by-step instructions on adopting axe packages into your existing Go project.
