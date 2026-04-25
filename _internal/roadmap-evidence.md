<!-- internal: not for the public docs site. Internal roadmap input only. -->

# Roadmap Evidence — axe v0.5.1

> **Honesty disclaimer.** axe currently has **n=1 user** (the author) and
> **one demo** (`examples/webtoon`). Every pain row below is from that one
> user building that one demo. **Not representative of "what axe users want"
> until external feedback contradicts it.** This file exists to keep us
> honest about *which* feature requests are evidence-backed vs. imagined.

This document is **roadmap input**, not positioning copy. The framework
references are citations of "how others solved this exact pain" — never
recommendations to copy a framework's whole stance.

---

## 1. Method

1. Mine `examples/webtoon/skills.md` and the webtoon git history.
2. Each entry must cite a file/section and an estimated time cost. **No
   source → not on the list.**
3. For each entry, look up at most 2–3 frameworks (Rails, Laravel, one Go
   peer) that solved this specific pain. **Frameworks are subordinate to
   the pain.**
4. Promote to roadmap **only** if the pain is in axe's scope (i.e. axe
   owns the surface area). Pains caused by Next.js, Postgres, or domain
   modeling are recorded but **not** roadmapped.

---

## 2. Evidence ledger

| ID  | Pain (1 sentence)                                              | Source            | Cost     | Severity | In axe scope? |
|-----|-----------------------------------------------------------------|-------------------|----------|----------|---------------|
| E1  | Migration runner runs whole file → `Down` block dropped a col. | `skills.md §1.1` | ~30 min  | High     | **Yes**       |
| E2  | Force-rerunning a half-applied migration is manual `psql`.      | `skills.md §1.2` | ~10 min  | Low      | **Yes**       |
| E3  | `WITH` vs `WITH RECURSIVE` produced opaque 500.                 | `skills.md §1.3` | ~20 min  | Med      | No (Postgres) |
| E4  | Default `psql` connect fails (`role "postgres" does not exist`). | `skills.md §1.4` | ~5 min   | Low      | Partial       |
| E5  | `NEXT_PUBLIC_*` baked at build time, runtime env ignored.       | `skills.md §2.2` | ~45 min  | High     | No (Next.js)  |
| E6  | In-cluster DNS confusion (`localhost` vs service name).         | `skills.md §2.3` | ~20 min  | Med      | **Yes** (template) |
| E7  | Go version drift between `go.mod` and Dockerfile.               | `skills.md §2.4` | ~10 min  | Low      | **Yes**       |
| E8  | `useSearchParams` without Suspense breaks prod build.           | `skills.md §3.1` | ~25 min  | Med      | No (Next.js)  |
| E9  | Page file rejected named export `AuthShell`.                    | `skills.md §3.2` | ~15 min  | Med      | No (Next.js)  |
| E10 | Comment threading: flat-vs-tree refactor + backfill migration.  | `skills.md §4`   | ~90 min  | High     | No (domain)   |
| E11 | API 500s with empty backend logs — hard to localize.            | `skills.md §5`   | ~30 min  | Med      | **Yes**       |
| E12 | Rebuilding web image for every env-var change (CI loop).        | session, no §    | repeated | Med      | **Yes** (template) |

**In axe scope** rows (the only ones we can roadmap): E1, E2, E4, E6, E7,
E11, E12.

---

## 3. Per-pain analysis (axe-scoped only)

### E1 — Migration runner runs whole file; Down can leak in

- **Cost paid:** ~30 min, lost a column in webtoon dev DB.
- **axe today:** `cmd/axe/main.go:applyMigration` reads file and `Exec`s
  it as one transaction. No marker parsing.
- **Prior art (cited):**
  - **goose:** parses `-- +goose Up` / `-- +goose Down` markers; only
    runs the requested direction.
  - **Rails:** `db/migrate/*_x.rb` separates `up`/`down` Ruby methods.
  - **Laravel:** migration class has `up()` and `down()` methods; CLI
    chooses which to invoke.
- **Cheapest axe response:** parse `-- +axe Up` / `-- +axe Down` markers
  in the existing runner; default to Up only. Reject Down unless the
  caller passes `axe migrate down --confirm`. ~150 LOC, ≤1 day.
- **Decision:** **P0** (see §4).

### E2 — Re-running a half-applied migration is manual psql

- **Cost paid:** small, but it's a footgun every time a migration errors
  partway through.
- **axe today:** user must `DELETE FROM schema_migrations WHERE filename = …`
  by hand.
- **Prior art:** Rails `db:migrate:redo`, Laravel `migrate:rollback --step=1`.
- **Cheapest axe response:** add `axe migrate forget <filename>` that
  removes the row. ~30 LOC. Rolled into the E1 work.
- **Decision:** **P0** (bundled with E1).

### E4 — psql `role "postgres" does not exist`

- **Cost paid:** ~5 min annoyance, but every new dev hits it.
- **axe today:** `make psql` doesn't exist; users `docker exec` directly.
- **Prior art:** Rails `bin/rails dbconsole`, Laravel `php artisan db`.
- **Cheapest axe response:** generate a `make db-shell` target in the
  scaffold that reads `.env` and runs the right `psql`. Trivial.
- **Decision:** **P1** — small lift, small payoff, batch with template
  refresh.

### E6 — In-cluster DNS: `localhost` vs `api`

- **Cost paid:** ~20 min, manifested as `ECONNREFUSED` from the Next
  server.
- **axe today:** Webtoon's `docker-compose.yml` was hand-fixed; the
  scaffold doesn't ship a working multi-service compose file.
- **Prior art:** Encore handles service discovery as a first-class
  primitive. Most Rails/Laravel templates dodge this by putting web +
  worker in the same container.
- **Cheapest axe response:** `axe new` produces a `docker-compose.yml`
  with **comments** at every cross-service URL explaining "this must use
  the service name, not localhost." Plus a one-page `docs/docker.md`.
  No code, just template polish. ~1 hour.
- **Decision:** **P1**.

### E7 — Go version drift between `go.mod` and Dockerfile

- **Cost paid:** ~10 min, surfaces only on Docker build (slow loop).
- **axe today:** scaffold pins a Go version literal in Dockerfile.
- **Prior art:** none in this peer group does this well; it's a
  language-tool gap, not a framework one.
- **Cheapest axe response:** `axe doctor` reads `go.mod`'s `go`
  directive and any `FROM golang:X.Y` in `Dockerfile*`, errors if they
  drift. ~50 LOC. Bonus: also flag `node:` version drift between
  Dockerfile and `web/package.json`'s `engines`.
- **Decision:** **P1** — bundle into a future `axe doctor` command.

### E11 — Internal 500 returns no log

- **Cost paid:** ~30 min, repeated. Forces guess-and-curl to localize.
- **axe today:** `pkg/apperror` + handler middleware wraps errors but
  some paths (esp. raw SQL) can panic/return 500 without a log line.
- **Prior art:**
  - **Laravel Telescope:** records every request, query, exception in
    a local DB; viewable at `/telescope` in dev.
  - **Rails:** dev mode shows the exception page with stack trace.
  - **Sentry / errortracking-vendor X:** standard prod story.
- **Cheapest axe response:** make sure the central error middleware
  **always** logs `{request_id, route, error}` even on panic; add
  `?debug=1` query param (dev-only, gated by `APP_ENV=dev`) that
  returns the error JSON inline. ~80 LOC.
- **Decision:** **P0**.

### E12 — Rebuilding the web image on every env change

- **Cost paid:** Each `NEXT_INTERNAL_API_URL` change = ~1 min rebuild +
  redeploy. Hits 5–10× during environment shaping.
- **axe today:** scaffold doesn't ship a Next.js example, so no
  guidance.
- **Cheapest axe response:** N/A unless axe ships a "Next.js companion"
  template (which §5 *rejects*). Document the trick (build-arg + ENV)
  in `docs/docker.md`.
- **Decision:** **Defer** — pure docs item, ride along with E6.

---

## 4. Prioritized response list

### P0 — do next (this sprint)

1. ~~**`axe migrate` Up/Down marker parsing + `forget` subcommand**~~ —
   addresses E1 + E2. **✅ Shipped.** Parser in
   `cmd/axe/migrate/migrate.go:splitUpDown`, mirrored into
   `cmd/axe/new/tmpl/cmd_axe_main.go.tmpl`. Regression guard:
   `TestSplitUpDown_BothSections_DownIsIsolated`. `forget` subcommand
   already existed, now documented in `SKILL.md §B.2`.
2. ~~**Error middleware always-logs + dev `?debug=1`**~~ — addresses
   E11. **✅ Shipped.** `WriteError` always logs 5xx via slog.Default;
   new `WriteErrorCtx(w, r, err)` adds request_id/method/path tags and
   exposes Cause inline as `debug` in JSON when **both** `APP_ENV=dev`
   AND `?debug=1`. 4xx do not log to avoid client-error spam. Mirrored
   into the scaffold template (`cmd/axe/new/templates.go:tmplMiddleware`).
   Regression guards: `TestWriteError_5xx_AlwaysLogsServerSide`,
   `TestWriteErrorCtx_DebugQuery_GatedByAppEnv` (4 sub-cases),
   `TestWriteError_4xx_DoesNotLog`. Documented in `SKILL.md §B.7`.

### P1 — next quarter

3. **`axe doctor` (v1)**: Go/Node/Docker version drift checks —
   addresses E7. Estimated 0.5 day.
4. **Scaffold polish**: better-commented `docker-compose.yml` + one-page
   `docs/docker.md` — addresses E6. Estimated 0.5 day.
5. **`make db-shell` in scaffold** — addresses E4. Estimated 1 hour.

### Defer

6. Document the `NEXT_PUBLIC_*` rebuild trick (E12) inside the doc
   landing from item 4. No standalone work.

### Out of scope (recorded, not roadmapped)

- E3, E5, E8, E9, E10 — caused by Postgres / Next.js / app domain.
  axe cannot meaningfully shorten the loop without taking on opinions
  outside its current surface.

---

## 5. Anti-roadmap (explicit rejections)

> Each item below was tempting after building webtoon. Rejected because
> **no row in §2 traces to it.**

- ❌ **Auto-admin UI (Django/Filament-style).** Zero webtoon papercuts
  trace to admin pain. Ship when an external user requests, not because
  Django has it.
- ❌ **Mailer subsystem.** Zero webtoon papercuts. Author hand-rolled an
  SMTP call once; that is not pain.
- ❌ **Inertia/Hotwire-style frontend adapter.** Webtoon shipped with
  vanilla Next.js fetches. No coupling pain. Adding an adapter creates
  surface area we don't yet need to maintain.
- ❌ **`axe console` REPL.** `psql` + `curl` covered every debug need
  in the webtoon session. Reconsider when 3+ external users complain.
- ❌ **More starter templates beyond webtoon.** One demo not yet
  validated by anyone outside; building #2 dilutes focus.
- ❌ **Performance benchmarking work.** Already covered in
  `benchmarks/`. No webtoon pain traces here.

---

## 6. Re-validation triggers

This file is **rewritten** when any of the following happen:

- A 3rd-party user files a GitHub issue describing a pain not in §2 →
  add an evidence row, re-prioritize. Don't merge fixes for invented
  pains.
- The author starts a 2nd demo and hits a paper-cut not predicted →
  evidence-row first, code second.
- A P0/P1 item ships and the originating row's cost does not measurably
  shrink on the next demo build → revert the change or rewrite the
  prescription.

---

## 7. Confidence statement

n=1. The author. One demo. This document does **not** claim to know
what "axe users" want. It claims to know what cost the author time on
**one specific build**. Plan v1 of this analysis tried to extrapolate
from "what 9 frameworks have"; PersonaTwin (synthetic Mom-Test
reviewer) flagged that as Competitor Comparison + Feature Dumping +
Future Tense Trap, and it was rewritten to start from real cost rather
than imagined gaps.

If you are reading this with axe usage data from beyond the author's
laptop, **trust your data, not this file.**
