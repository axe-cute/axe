# Skills & Gotchas — axe framework

Operational knowledge for working on **axe itself**. Read this *before*
making changes; it is the list of mistakes already paid for. Companion to
the per-example notes (see `examples/webtoon/skills.md`).

This file has two parts:

- **Part A — Working principles.** How to approach any change, agnostic of
  the stack. Borrowed from
  [Karpathy's coding guidelines](https://github.com/forrestchang/andrej-karpathy-skills)
  and grounded in real mistakes from this repo.
- **Part B — Concrete gotchas.** Traps specific to axe's stack: code
  generation (Ent + sqlc + Wire), migrations, multi-DB matrix, scaffold
  templates, lint config, plugin layout.

When the two conflict, **principles win**.

---

# Part A — Working principles

> The models make wrong assumptions on your behalf and just run along with
> them without checking. They don't manage their confusion, don't seek
> clarifications, don't surface inconsistencies, don't present tradeoffs,
> don't push back when they should. — A. Karpathy

## A.1 Think before coding

**Don't assume. Surface tradeoffs. Push back when warranted.**

- State your assumptions out loud — one written assumption beats three
  speculative commits.
- When two interpretations of a request exist, **name both** before
  picking. Don't silently pick the "obvious" one.
- If asked for *X* but *Y* solves the underlying need with one-tenth the
  code, say so before writing *X*.
- Stop when confused. "I'll figure it out as I go" is how `CreateComment`
  shipped flat in the webtoon demo even though the user wanted a real
  reply chain.

## A.2 Simplicity first

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "configurability" the user didn't request.
- No error handling for impossible scenarios.
- Prefer **single-line upstream fixes** to elaborate downstream
  workarounds.

**Test:** would a senior engineer call this overcomplicated? Then simplify.

**Example from this repo:** an early `ListComments` reached for a
recursive CTE because "comments are a tree." The visual tree was at most
1 level — a flat 2-row JOIN was enough. The recursive CTE then footgunned
us with `WITH` vs `WITH RECURSIVE`.

## A.3 Surgical changes

**Touch only what you must.** When editing existing code:

- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd write it differently.
- Notice unrelated dead code? **Mention** it — don't delete it.

When your changes create orphans:

- Remove imports / helpers / variables that **your** changes made unused.
- Don't remove pre-existing dead code unless explicitly asked.

**Test:** every changed line should trace directly to the user's request.

## A.4 Goal-driven execution

**Define success criteria. Loop until verified.**

Before non-trivial work, write down what "done" looks like — in a form a
script could check, not prose.

| Imperative                | Verifiable goal                                                  |
|---------------------------|------------------------------------------------------------------|
| "Add validation"          | "Reject invalid inputs with 400; tests cover empty + too-long."  |
| "Fix the 500 on comments" | "`curl /comments` returns 200 + `{data:[…]}`; tests pass."      |
| "Refactor X"              | "All existing tests + this new one pass before *and* after."     |

For multi-step work, sketch the plan with verifications:

```
1. <step>   → verify: <observable check>
2. <step>   → verify: <observable check>
```

Strong success criteria let you loop without back-and-forth.

## A.5 The 30-second pre-flight

Before each non-trivial change, answer in your head:

1. **What is the user actually asking for?** State it in one sentence.
2. **What am I assuming?** Name two assumptions.
3. **Is there a 1-line fix?** Could a tiny upstream change replace what
   I'm about to write?
4. **What's the verification?** What command or test proves this works?
5. **What am I touching that I shouldn't?** Anything outside the diff
   surface justifies a comment, not a silent edit.

If any answer is fuzzy — **stop and ask**. One question is cheaper than
one revert.

## A.6 Communication norms

- **Direct over polite.** Skip "Great question!" / "You're absolutely right!".
- **Cite files and lines.** `@/path/to/file.ts:12-30` beats prose.
- **Bold the verb.** "**Fix:** …", "**Cause:** …", "**Verify:** …" make
  long messages skim-able.
- **End with status.** Every reply ends with what's done, what's broken,
  and what's next.

---

# Part B — Concrete gotchas (axe-specific)

## B.1 Generated code lives in git

axe commits all generated code so that `go build ./...` works on a fresh
clone. Three generators:

| Tool      | Output                              | Regenerate command          |
|-----------|-------------------------------------|------------------------------|
| Ent       | `ent/*.go` (excl. `schema/`)        | `make generate-ent`          |
| sqlc      | `internal/repository/db/`           | `make generate-sqlc`         |
| Wire      | `wire_gen.go` per package           | `make generate` (all)        |

**Rules:**

- After editing `ent/schema/*.go`, **always** run `make generate-ent`
  *and commit the diff*. CI compares working tree to generated output;
  uncommitted regen → red build (fixed in `852a7c8`).
- `wire_gen.go` is `.gitignore`d historically but the build expects it.
  If you delete it, regenerate before committing.
- sqlc query files in `db/queries/*.sql` drive `internal/repository/db/`.
  Schema drift between `db/migrations/` and `db/queries/` will surface
  only at sqlc-generate time — run it locally before pushing schema PRs.

## B.2 Migration runner — marker-aware as of v0.5.2

`cmd/axe/migrate` (used by axe itself) and the same logic mirrored into
`cmd/axe/new/tmpl/cmd_axe_main.go.tmpl` (used by every scaffolded
project) parse `-- +migrate Up` / `-- +migrate Down` markers. **`up`
only executes the Up section.** Down sections never run via `up`,
even if present.

**Backward-compat:** files with no markers are run as a single Up
block — legacy migrations keep working.

```sql
-- ✅ With markers: only the Up block runs on `axe migrate up`.
-- +migrate Up
ALTER TABLE foo ADD COLUMN IF NOT EXISTS bar INT;
CREATE INDEX IF NOT EXISTS idx_foo_bar ON foo (bar);
-- +migrate Down
ALTER TABLE foo DROP COLUMN bar;

-- ✅ No markers: whole file runs as Up. Idiomatic when there's no
--    rollback intent.
ALTER TABLE foo ADD COLUMN IF NOT EXISTS bar INT;
```

If you change the parser, run the regression tests in
`cmd/axe/migrate/migrate_test.go` — especially
`TestSplitUpDown_BothSections_DownIsIsolated`, which is the one that
guards E1 (the column-dropping bug from the webtoon build).

### Forgetting a half-applied migration

`axe migrate forget <filename>` removes a specific row from
`schema_migrations` without reversing SQL. Use when a migration aborted
partway through and the bookkeeping row is out of sync with reality:

```bash
axe migrate forget 20260424180000_scale_indexes.sql
# fix the SQL file, then:
axe migrate up
```

## B.3 Multi-DB matrix is non-negotiable

Every change that touches schema or queries must work on **all three**:
Postgres (pgx v5), MySQL 8+, SQLite (no CGO). CI's
`integration-matrix` job (`integration.yml`) will catch you, but the
loop is slow. Local pre-flight:

```bash
make test-integration              # postgres, requires docker
make test-integration-mysql        # mysql 8, requires docker
make test-integration-sqlite       # no docker
```

**Common landmines:**

- **Index size:** MySQL 8 with `utf8mb4` caps index keys at 3072 bytes;
  combined string indexes need explicit prefix lengths. Fixed in
  `8dbdbe7` for the auth tables — replicate that pattern.
- **`UUID` type:** Postgres has native `uuid`; MySQL stores as
  `BINARY(16)` or `CHAR(36)`; SQLite as `TEXT`. Ent abstracts it; raw
  SQL must hand-roll.
- **`ON CONFLICT` vs `ON DUPLICATE KEY UPDATE` vs `INSERT OR REPLACE`:**
  three different syntaxes. Use Ent's `OnConflict()` builder when
  possible; fall back to per-DB raw SQL with a switch on `cfg.DBKind`.
- **Boolean:** SQLite stores as 0/1. `bool` round-trips through Ent but
  hand-rolled SQL with `t.field = TRUE` will silently fail.

## B.4 Lint config has a real opinion

`.golangci.yml` enforces:

- **`goimports.local-prefixes: github.com/axe-cute/axe`** — internal
  imports must be in their own group, after stdlib and third-party. The
  CI lint step (`ci.yml`) will fail otherwise. `make fmt` fixes it
  locally.
- **`staticcheck` all checks except `SA1019`** (deprecation), because
  the `nhooyr.io/websocket → coder/websocket` migration is tracked
  separately. Don't add new deprecated calls.
- **`revive exported`** without stuttering check. Don't try to silence
  with `//nolint:revive` unless there's no clean rename.

Run locally: `make lint`. CI runs `--timeout=5m`.

## B.5 Resource generator — invariants the template MUST satisfy

`cmd/axe/generate/templates.go` writes 10 files in one shot. Two
invariants are easy to break and were broken once already (`006f80a`):

1. **`BelongsTo` FK threads end-to-end.** When a resource has a parent
   (e.g. `axe generate resource Comment --belongs-to=Post`), the
   generated handler's request DTO must expose the FK as
   `PostID string \`json:"post_id"\``, parse it with `uuid.Parse`, and
   forward it as `PostID: parentID` into the domain input. Missing any
   step silently drops the FK — service then errors with
   `"post_id is required"`.
2. **`Decoder.DisallowUnknownFields()` on Create + Update.** Without
   this, typos in JSON fields fail open. The regression test
   `TestGenerateResource_HandlerPropagatesBelongsTo` locks both
   invariants in.

If you change the handler template, **run that test** and update its
assertions if the contract intentionally changes.

## B.6 Plugin layout

Plugins live under `pkg/plugin/<name>/` and are registered explicitly
(no init magic). Conventions:

- One package per plugin. Public surface lives in `<name>.go`; helpers
  in `internal_*.go` are unexported.
- A plugin that needs DB access **takes** an `*ent.Client` and a
  `*pgxpool.Pool` (or equivalents) in its constructor — never reaches
  for global state.
- Maturity is tracked in `docs/plugin-maturity.md`. **Don't promote a
  plugin level without updating that file** in the same commit.

## B.7 Error responses and the dev `?debug=1` channel

The middleware package guarantees two invariants — both have regression
tests in `internal/handler/middleware/middleware_test.go`:

1. **Every 5xx logs server-side.** `WriteError` and `WriteErrorCtx`
   emit a structured `slog.Error` with `code/status/message/cause`
   (and `request_id/method/path` when a request is in scope) before
   returning. A 500 must never reach the client silently.
2. **`?debug=1` is gated by `APP_ENV=dev`.** Both gates are required to
   include the wrapped Cause in the JSON envelope as `debug`. Production
   builds physically cannot expose stack traces via this channel even
   if a curl tries.

Use the right helper:

```go
// In handlers (you have *http.Request) — preferred.
//   - Logs include request_id from context.
//   - ?debug=1 in dev surfaces Cause inline.
middleware.WriteErrorCtx(w, r, err)

// In contexts without a request (rare — e.g. shutdown hooks).
//   - Falls back to slog.Default.
//   - No ?debug=1 support.
middleware.WriteError(w, err)
```

4xx responses are **not** logged at error level (avoids client-error
spam). If you want to track 4xx, use the request `Logger` middleware,
which logs every response at INFO with status.

When chasing a 5xx in dev:

```bash
APP_ENV=dev make run
curl -s 'http://localhost:8080/v1/orders/123?debug=1' | jq .
# {"code":"INTERNAL_ERROR","message":"…","debug":"db: connection refused"}
```

## B.8 Quick-fix checklist when something is on fire

1. `make lint` & `go vet ./...` — eliminates the trivial.
2. `make test` (≤ 30 s by DX SLO) — unit tests.
3. Reproduce against a real DB:
   `make test-integration` (or the MySQL/SQLite variant).
4. For scaffold/generator regressions: `make test-scaffold` runs the
   full generate-then-`go build` integration test. Slow but catches
   template breakage that unit tests miss.
5. For runtime 500s in a scaffolded app:
   - `docker compose logs api --tail=50`
   - reproduce the failing query directly with `psql` / `mysql`.
6. Cross-DB regression? Run all three integration suites locally before
   pushing — CI will reject otherwise.

---

## See also

- `docs/architecture_contract.md` — the durable architectural rules.
- `docs/dev_experience_spec.md` — DX SLOs (10-min CRUD, 30-s tests).
- `docs/adr/` — past decisions with rationale (Chi over Gin, Ent for
  writes + sqlc for reads, fsstore over S3-only).
- `examples/webtoon/skills.md` — Next.js / Docker / threading gotchas
  from a real demo build.
- `_internal/roadmap-evidence.md` — what we've decided to fix next and
  what we've decided **not** to build, with evidence.
