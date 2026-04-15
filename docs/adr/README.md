# Architecture Decision Records

This directory documents the key architecture decisions made during the development of `axe`.

Each ADR follows the format:
- **Status**: Proposed | Accepted | Deprecated | Superseded
- **Context**: Why was this decision needed?
- **Decision**: What was decided?
- **Consequences**: What are the trade-offs?

## Index

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-001](./001-chi-over-gin-echo.md) | Chi over Gin/Echo for HTTP routing | Accepted | 2026-04-15 |
| [ADR-002](./002-cleanenv-over-viper.md) | Cleanenv over Viper for config | Accepted | 2026-04-15 |
| [ADR-003](./003-ent-writes-sqlc-reads.md) | Ent (writes) + sqlc (reads) with shared pool | Accepted | 2026-04-15 |
| [ADR-004](./004-compile-time-di.md) | Wire for compile-time dependency injection | Accepted | 2026-04-15 |
| [ADR-005](./005-outbox-pattern.md) | Outbox pattern for side effects | Accepted | 2026-04-15 |
