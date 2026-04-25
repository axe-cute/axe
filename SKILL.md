# Skills & Gotchas — axe framework

Operational notes for **axe itself**. List of mistakes already paid for.
Read before changing. Companion: `examples/webtoon/skills.md`.

- **Part A** — stack-agnostic principles. From
  [Karpathy guidelines](https://github.com/forrestchang/andrej-karpathy-skills),
  grounded in real repo mistakes.
- **Part B** — axe-specific traps: codegen (Ent + sqlc + Wire),
  migrations, multi-DB matrix, scaffold templates, lint, plugins.

Conflict → **principles win**.

> *Compressed copy.* Original at `SKILL.original.md`.

---

# Part A — Working principles

> Models make wrong assumptions on your behalf, run with them. No
> confusion-management, no clarification, no pushback. — A. Karpathy

## A.1 Think before coding

**Don't assume. Surface tradeoffs. Push back.**

- State assumptions out loud. One written assumption beats three
  speculative commits.
- Two interpretations? Name **both** before picking.
- Asked for *X* but *Y* solves the need at one-tenth the code? Say so.
- Stop when confused. "I'll figure it out" → `CreateComment` shipped
  flat in webtoon when user wanted a real reply chain.

## A.2 Simplicity first

**Minimum code. Nothing speculative.**

- No features beyond ask.
- No abstractions for single-use code.
- No configurability not requested.
- No error paths for impossible scenarios.
- Prefer **single-line upstream fix** over downstream workaround.

**Test:** senior engineer would call this overcomplicated? Simplify.

**Repo example:** `ListComments` early reached for recursive CTE
because "comments are a tree." Tree was max 1 level — flat 2-row JOIN
sufficed. CTE then footgunned us with `WITH` vs `WITH RECURSIVE`.

## A.3 Surgical changes

**Touch only what you must.**

- No "improving" adjacent code/comments/formatting.
- No refactoring non-broken things.
- Match existing style even if you'd write it differently.
- Spot dead code? **Mention** — don't delete.
- Remove imports/helpers **your** changes orphaned. Leave pre-existing
  dead code alone unless asked.

**Test:** every changed line traces to user request.

## A.4 Goal-driven execution

**Define success. Loop until verified.**

Write "done" as something a script could check, not prose.

| Imperative                | Verifiable goal                                                  |
|---------------------------|------------------------------------------------------------------|
| "Add validation"          | "Reject invalid inputs with 400; tests cover empty + too-long."  |
| "Fix the 500 on comments" | "`curl /comments` returns 200 + `{data:[…]}`; tests pass."      |
| "Refactor X"              | "All existing tests + this new one pass before *and* after."     |

Multi-step:

```
1. <step>   → verify: <observable check>
2. <step>   → verify: <observable check>
```

## A.5 30-second pre-flight

Answer before each non-trivial change:

1. **What's the user asking?** One sentence.
2. **What am I assuming?** Two assumptions.
3. **1-line fix?** Tiny upstream change replaces what I'd write?
4. **Verification?** Command/test that proves it.
5. **Touching what I shouldn't?** → comment, not silent edit.

Any answer fuzzy → **stop, ask**. One question < one revert.

## A.6 Communication norms

- **Direct over polite.** Skip "Great question!" / "You're right!".
- **Cite files+lines.** `@/path/to/file.ts:12-30` beats prose.
- **Bold the verb.** "**Fix:**", "**Cause:**", "**Verify:**".
- **End with status.** Done, broken, next.

---

# Part B — Concrete gotchas (axe-specific)

## B.1 Generated code lives in git

`go build ./...` works on fresh clone. Three generators:

| Tool      | Output                              | Regenerate command          |
|-----------|-------------------------------------|------------------------------|
| Ent       | `ent/*.go` (excl. `schema/`)        | `make generate-ent`          |
| sqlc      | `internal/repository/db/`           | `make generate-sqlc`         |
| Wire      | `wire_gen.go` per package           | `make generate` (all)        |

**Rules:**

- Edit `ent/schema/*.go` → `make generate-ent` + commit diff. CI
  compares working tree to gen output; uncommitted regen → red build
  (fixed `852a7c8`).
- `wire_gen.go` historically `.gitignore`d but build expects it.
  Delete → regenerate before commit.
- sqlc query files in `db/queries/*.sql` drive
  `internal/repository/db/`. Drift between `db/migrations/` and
  `db/queries/` only surfaces at sqlc-generate. Run locally before
  pushing schema PRs.

## B.2 Migration runner — marker-aware as of v0.5.2

`cmd/axe/migrate` (axe itself) + mirrored
`cmd/axe/new/tmpl/cmd_axe_main.go.tmpl` (every scaffold) parse
`-- +migrate Up` / `-- +migrate Down`. **`up` runs Up only.** Down
never runs via `up`.

**Compat:** files w/o markers run as single Up block.

```sql
-- ✅ With markers: only Up runs.
-- +migrate Up
ALTER TABLE foo ADD COLUMN IF NOT EXISTS bar INT;
CREATE INDEX IF NOT EXISTS idx_foo_bar ON foo (bar);
-- +migrate Down
ALTER TABLE foo DROP COLUMN bar;

-- ✅ No markers: whole file runs as Up.
ALTER TABLE foo ADD COLUMN IF NOT EXISTS bar INT;
```

Change parser → run `cmd/axe/migrate/migrate_test.go`, especially
`TestSplitUpDown_BothSections_DownIsIsolated` (guards E1, the
column-dropping bug from webtoon).

### Forgetting a half-applied migration

`axe migrate forget <filename>` removes one row from
`schema_migrations`, no SQL reversal. Use when migration aborted
mid-flight and bookkeeping row is stuck:

```bash
axe migrate forget 20260424180000_scale_indexes.sql
# fix file, then:
axe migrate up
```

## B.3 Multi-DB matrix is non-negotiable

Schema/query change must work on **all three**: Postgres (pgx v5),
MySQL 8+, SQLite (no CGO). CI `integration-matrix`
(`integration.yml`) catches you, but slow. Local pre-flight:

```bash
make test-integration              # postgres, requires docker
make test-integration-mysql        # mysql 8, requires docker
make test-integration-sqlite       # no docker
```

**Landmines:**

- **Index size:** MySQL 8 + `utf8mb4` caps index keys at 3072 bytes.
  Combined string indexes need explicit prefix lengths. Pattern:
  `8dbdbe7` (auth tables).
- **`UUID`:** Postgres native `uuid`; MySQL `BINARY(16)` or `CHAR(36)`;
  SQLite `TEXT`. Ent abstracts; raw SQL hand-rolls.
- **`ON CONFLICT` vs `ON DUPLICATE KEY UPDATE` vs `INSERT OR REPLACE`:**
  three syntaxes. Ent `OnConflict()` when possible; else per-DB raw
  SQL switched on `cfg.DBKind`.
- **Boolean:** SQLite stores 0/1. `bool` round-trips through Ent;
  hand-rolled `t.field = TRUE` silently fails.

## B.4 Lint config has a real opinion

`.golangci.yml`:

- **`goimports.local-prefixes: github.com/axe-cute/axe`** — internal
  imports own group, after stdlib + 3rd-party. CI lint (`ci.yml`)
  fails otherwise. `make fmt` fixes.
- **`staticcheck` all checks except `SA1019`** (deprecation). The
  `nhooyr.io/websocket → coder/websocket` migration tracked
  separately. No new deprecated calls.
- **`revive exported`** without stuttering check. No `//nolint:revive`
  unless rename impossible.

Run: `make lint`. CI: `--timeout=5m`.

## B.5 Resource generator — invariants the template MUST satisfy

`cmd/axe/generate/templates.go` writes 10 files per shot. Two
invariants easy to break, broken once already (`006f80a`):

1. **`BelongsTo` FK threads end-to-end.** Resource w/ parent (e.g.
   `axe generate resource Comment --belongs-to=Post`) → handler DTO
   exposes FK as `PostID string \`json:"post_id"\``, parses via
   `uuid.Parse`, forwards as `PostID: parentID` into domain input.
   Skip any step → FK silently dropped → service errors
   `"post_id is required"`.
2. **`Decoder.DisallowUnknownFields()` on Create + Update.** Without
   it, JSON typos fail open. Regression test
   `TestGenerateResource_HandlerPropagatesBelongsTo` locks both.

Change handler template → **run that test**. Update assertions only if
contract change is intentional.

## B.6 Plugin layout

Plugins under `pkg/plugin/<name>/`. Registered explicitly (no init
magic).

- One package per plugin. Public surface in `<name>.go`; helpers in
  `internal_*.go` unexported.
- Plugin needing DB **takes** `*ent.Client` + `*pgxpool.Pool` (or
  equivalents) in constructor. Never reach for global state.
- Maturity in `docs/plugin-maturity.md`. **Don't promote a level
  without updating that file** in the same commit.

## B.7 Error responses + dev `?debug=1` channel

middleware package guarantees two invariants — both tested in
`internal/handler/middleware/middleware_test.go`:

1. **Every 5xx logs server-side.** `WriteError` and `WriteErrorCtx`
   emit structured `slog.Error` with `code/status/message/cause` (and
   `request_id/method/path` when request in scope) before returning.
   500 never reaches client silently.
2. **`?debug=1` gated by `APP_ENV=dev`.** Both gates required to
   include wrapped Cause as `debug` in JSON envelope. Production
   physically cannot leak stack traces this way even if curl tries.

Pick the right helper:

```go
// In handlers (have *http.Request) — preferred.
//   - Logs include request_id from context.
//   - ?debug=1 in dev surfaces Cause inline.
middleware.WriteErrorCtx(w, r, err)

// No request (rare — shutdown hooks etc).
//   - Falls back to slog.Default.
//   - No ?debug=1.
middleware.WriteError(w, err)
```

4xx **not** logged at error level (anti client-error spam). Track 4xx
via `Logger` middleware — logs every response at INFO with status.

Chase a 5xx in dev:

```bash
APP_ENV=dev make run
curl -s 'http://localhost:8080/v1/orders/123?debug=1' | jq .
# {"code":"INTERNAL_ERROR","message":"…","debug":"db: connection refused"}
```

## B.8 Quick-fix checklist when something is on fire

1. `make lint` & `go vet ./...` — kills trivials.
2. `make test` (≤ 30 s per DX SLO) — unit tests.
3. Real DB: `make test-integration` (or MySQL/SQLite).
4. Scaffold/generator regression: `make test-scaffold` runs
   generate-then-`go build`. Slow but catches template breakage unit
   tests miss.
5. Runtime 500 in a scaffolded app:
   - `docker compose logs api --tail=50`
   - reproduce failing query via `psql` / `mysql`.
6. Cross-DB regression? Run all three integration suites locally
   before pushing — CI rejects otherwise.

---

## See also

- `docs/architecture_contract.md` — durable architectural rules.
- `docs/dev_experience_spec.md` — DX SLOs (10-min CRUD, 30-s tests).
- `docs/adr/` — past decisions + rationale (Chi over Gin, Ent for
  writes + sqlc for reads, fsstore over S3-only).
- `examples/webtoon/skills.md` — Next.js / Docker / threading gotchas
  from real demo build.
- `_internal/roadmap-evidence.md` — next + decided-not-to-build, with
  evidence.
- `SKILL.original.md` — uncompressed original of this file.
