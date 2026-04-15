# ADR-003: Ent (writes) + sqlc (reads) with shared *sql.DB pool

**Status**: Accepted  
**Date**: 2026-04-15

## Context

Database access in Go typically involves choosing between:
- Full ORM (GORM): schema + migration + query via single tool
- Query builder (sqlx, squirrel): lightweight, more SQL control
- Schema-as-code ORM (Ent): compile-time safe, code generation
- Raw SQL codegen (sqlc): SQL-first, zero reflection

No single tool excels at both schema management AND complex analytical queries.

## Decision

Use **Ent** for write operations and simple reads, **sqlc** for complex reads. Both share **one `*sql.DB` connection pool**.

```go
// Shared pool — both tools use this:
db, _ := sql.Open("pgx", cfg.DatabaseURL)
entClient := ent.NewClient(ent.Driver(entsql.OpenDB("pgx", db)))
queries   := sqlcdb.New(db) // or queries.WithTx(tx)
```

## Decision Matrix

| Operation | Tool | Reason |
|---|---|---|
| INSERT / UPDATE / DELETE | Ent | Schema-safe, compile-time field checks |
| SELECT by PK | Ent | Consistency with write path |
| JOIN queries | sqlc | SQL files, explicit, optimizable |
| Pagination | sqlc | LIMIT/OFFSET in pure SQL |
| Aggregation / analytics | sqlc | GROUP BY, HAVING, window functions |
| Full-text search | sqlc | ts_vector, to_tsquery |

## Consequences

- **Positive**: Type-safe writes via Ent codegen
- **Positive**: Optimized reads via hand-written SQL (sqlc)
- **Positive**: Single connection pool = no N+1 pool problem
- **Positive**: Transactions work across both (inject via context)
- **Negative**: Two tools to learn 
- **Mitigation**: Reference implementation (User domain) shows the pattern clearly
