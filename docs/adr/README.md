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
| ADR-002 | Cleanenv over Viper for config | Accepted | 2026-04-15 |
| [ADR-003](./003-ent-writes-sqlc-reads.md) | Ent (writes) + sqlc (reads) with shared pool | Accepted | 2026-04-15 |
| ADR-004 | Compile-time DI with Wire | Accepted | 2026-04-15 |
| ADR-005 | Outbox pattern for side effects | Accepted | 2026-04-15 |
| ADR-006 | Asynq over Machinery for background jobs | Accepted | 2026-04-15 |
| ADR-007 | go-redis/redis_rate for sliding window | Accepted | 2026-04-15 |
| ADR-008 | chi WebSocket over gorilla/websocket | Accepted | 2026-04-15 |
| ADR-009 | Plugin service locator pattern | Accepted | 2026-04-16 |
| [ADR-010](./010-fsstore-posix-over-s3.md) | FSStore POSIX over S3 SDK — zero deps, works with JuiceFS | Accepted | 2026-04-16 |

> **Note**: ADR files for 002, 004–009 are listed above but not yet written as full documents. Creating them is tracked in Sprint 23 v1.0 polish.
