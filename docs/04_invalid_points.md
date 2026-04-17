# ❌ Invalid / Overreaching Points
> Arguments that are **wrong**, **exaggerated**, or **create unrealistic expectations**
> in both reports.
>
> 🇻🇳 [Phiên bản tiếng Việt](vi/04_invalid_points.md)

---

## 1. "Unlimited Scalability" — Dangerous Exaggeration

**From Report 1:**
> "an explicit platform with **unlimited scalability**"

**Why wrong:**
No system has unlimited scalability. This is marketing language, not an engineering statement.

**Reality:**
- Go handles concurrency better than Ruby, but **database is still the primary bottleneck**
- PostgreSQL connection pool has limits (~500–1000 practical connections)
- Network bandwidth, disk I/O, memory all have physical limits

**Honest version:**
> "Go enables more efficient horizontal scaling than Ruby due to lightweight goroutines and static binary deployment, but bottlenecks shift to the database and infrastructure layer sooner."

---

## 2. Complete GORM Ban — Lacks Nuance

**From Report 1:**
> "GORM must be removed from the architecture design"

**Why unreasonable:**
GORM has valid use cases:
- Internal admin tools that don't need high performance
- Rapid prototyping new features before optimization
- Small teams with tight MVP deadlines

**More reasonable version:**
```
GORM Usage Policy:
❌ FORBIDDEN in: core business logic, high-traffic endpoints
⚠️  ALLOWED in: admin scripts, one-off data migrations, internal tooling
✅  PREFERRED: Ent (mutations) + sqlc (queries)
```

---

## 3. sqlc "Maximum Performance" — Insufficient Context

**From Report 1:**
> "sqlc: Maximum performance, only limited by network speed and driver"

**Why misleading:**
- sqlc only eliminates **code-level** overhead
- Actual performance depends on **query quality** — missing indexes, N+1 still occur with sqlc
- `pgx` driver (instead of `database/sql`) is much faster but wasn't mentioned

**Reality:**
```
Bottleneck priority order:
1. Missing indexes          ← most important
2. N+1 query problems       ← very common
3. Connection pool config   ← often forgotten
4. Network latency (DB location)
5. ORM/library overhead     ← GORM ~5-10%, sqlc ~0%
```
→ Library choice is the **last step** in the performance optimization chain.

---

## 4. "Circular Dependency Is an MVC Architecture Flaw" — Partially Wrong

**From Report 1:**
> "reproducing Rails folder structure is architectural suicide"

**Why overblown:**
- Circular dependency is a **design issue**, not a folder structure issue
- MVC folder structure doesn't inherently create circular imports
- Circular imports happen when **package responsibilities are unclear**, regardless of folder structure

**More accurate version:**
> "MVC folder structure in Go has a high risk of circular imports if the team doesn't clearly understand dependency direction. Clean Architecture solves this by enforcing explicit direction."

---

## 5. Wire — Presented as Silver Bullet

**From Report 1:**
> "If the project grows... the platform can integrate Google Wire"

**Why murky:**
- Wire has a high learning curve and complex debugging
- Wire-generated code is long and hard to read
- Wire doesn't solve all DI scenarios (circular deps in DI graph)
- Many large Go projects (k8s ecosystem) **don't use Wire**

**Reality:**
For projects < 50 endpoints: `main.go` manual wiring is entirely sufficient.
Wire is only needed with > 100+ components.

---

## 6. Report 2 — "10 Gaps" But Wrong Priority Order

**From Report 2:**
> 10 gaps listed in order: Transaction model, Outbox, Domain boundary...

Report 2 places "Developer adoption" at #8 and "Observability" at #7 — **wrong order for real-world impact**.

**Correct order by impact:**
```
1. Transaction model          → immediate data corruption
2. Error taxonomy             → API immediately unusable
3. Developer adoption         → team can't use the architecture
4. Observability              → production incidents can't be debugged
5. Outbox pattern             → data inconsistency (only with queues)
6. CQRS light                 → performance degradation (gradual)
7. Domain boundary            → architectural decay (long-term)
8. Failure strategy           → production incidents (eventually)
9. Performance/caching        → scale issue (later stage)
10. Deployment model          → devops concern (separate team)
```

---

## 7. No Mention of pgx — Important Omission

**Problem:**
Both reports mention `database/sql` standard library but never mention `pgx` (jackc/pgx).

**Reality:**
- `pgx` is the recommended PostgreSQL driver for production Go apps
- Native PostgreSQL type support (JSONB, arrays, custom types)
- Higher performance than `database/sql` built-in driver
- sqlc and Ent both support pgx natively

**This is a missing foundation** — affects the entire data layer.

---

## Summary

```
❌ "Unlimited scalability"  → marketing, not engineering
❌ Absolute GORM ban        → lacks nuance, creates hidden workarounds
❌ sqlc "max performance"   → misleading, query quality matters most
❌ MVC = circular import    → overblown, issue is package design
❌ Wire = silver bullet     → over-engineered for small teams
❌ Report 2 priority order  → doesn't match real-world impact
❌ Missing pgx entirely     → foundation gap
```
