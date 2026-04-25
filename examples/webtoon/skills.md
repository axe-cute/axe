# Skills & Gotchas — Webtoon

Operational knowledge for working on this codebase. Read this **before** making
changes; it's the list of mistakes already paid for.

This file has two parts:

- **Part A — Working principles**: how to approach a change. Borrows from
  [Karpathy's coding guidelines](https://github.com/forrestchang/andrej-karpathy-skills)
  and grounds each principle in mistakes we've already paid for *in this repo*.
- **Part B — Concrete gotchas**: traps in the actual stack (Postgres, Docker,
  Next.js 15, comment threading).

When the two conflict, principles win.

---

# Part A — Working principles

> The models make wrong assumptions on your behalf and just run along with
> them without checking. They don't manage their confusion, don't seek
> clarifications, don't surface inconsistencies, don't present tradeoffs,
> don't push back when they should.
> — A. Karpathy

The principles below exist because each one maps to a concrete time we
*didn't* follow it in this repo and burned an hour debugging.

## A.1 Think before coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

- State assumptions explicitly. If uncertain, ask one question rather than
  guess. One blocking question beats three speculative commits.
- When two interpretations exist, name them both before picking. Don't
  silently pick the "obvious" one.
- Push back when warranted. If the user asks for *X* but *Y* solves the
  underlying need with one-tenth the code, say so before writing *X*.
- Stop when confused. Name what is unclear (a column, a flag, an env var)
  and ask. "I'll figure it out as I go" is how flattening showed up in
  `CreateComment` even though the user later wanted a real reply chain.

**Anchors in this repo:**
- The first comment-threading pass *assumed* "users will only reply to
  top-level comments" and flattened reply-to-reply. The user then asked for
  visible `@User` for nested replies. We had to add `root_comment_id` and
  `parent_user_id`, plus a backfill migration. Pre-stating that assumption
  in writing would have surfaced the disagreement before the migration.
- The first Docker pass *assumed* `NEXT_PUBLIC_API_URL` was read at runtime.
  It is baked at build time. Five minutes reading the Next docs first would
  have skipped the broken redeploy.

## A.2 Simplicity first

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" the user didn't request.
- No error handling for impossible scenarios.
- If 200 lines could be 50, rewrite it.
- Prefer **single-line upstream fixes** to elaborate downstream workarounds.

**Test:** Would a senior engineer call this overcomplicated? If yes, simplify.

**Anchors in this repo:**
- The original `ListComments` reached for a recursive CTE because "comments
  are a tree." But the visual tree is at most 1 level — a flat 2-row JOIN is
  enough (current code). Simpler, faster, no `WITH RECURSIVE` footgun.
- The `WITH RECURSIVE` 500 itself was caused by adding complexity (recursive
  CTE) the data model didn't actually need.
- `parseReplyMention` was a regex helper used in exactly one render path,
  later orphaned. Helpers used once should be inlined; helpers used zero
  times should be deleted.

## A.3 Surgical changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd write it differently.
- If you notice unrelated dead code, mention it — don't delete it.

When your changes create orphans:
- Remove imports / variables / helpers that **your changes** made unused.
- Don't remove pre-existing dead code unless the user asks.

**Test:** Every changed line should trace directly to the user's request.

**Anchors in this repo:**
- The `AuthShell`-export-from-page bug was pre-existing. We fixed it because
  the Docker build couldn't proceed without it (in-scope), and we *didn't*
  also rewrite the auth pages, even though we noticed style nits there.
- Migration rollbacks belong in a separate `*_down.sql` file run by hand —
  *not* tacked onto the Up migration. We learned this the hard way: a single
  file containing both Up and Down ran end-to-end and dropped the column it
  had just created.

## A.4 Goal-driven execution

**Define success criteria. Loop until verified.**

Before starting non-trivial work, write down what "done" looks like — in a
form a script could check, not prose.

| Imperative                | Verifiable goal                                                  |
|---------------------------|------------------------------------------------------------------|
| "Add validation"          | "Reject invalid inputs with 400; tests cover empty + too-long."  |
| "Fix the 500 on comments" | "`curl /comments` returns 200 + JSON `{data:[…]}`; tests pass."  |
| "Refactor X"              | "All existing tests + this new one pass before *and* after."     |
| "Make replies threaded"   | "Reply-to-reply shows `@parent_user`; `root_comment_id` set."    |

For multi-step work, sketch the plan in the chat or `progress.txt`:

```
1. <step>   → verify: <observable check>
2. <step>   → verify: <observable check>
3. <step>   → verify: <observable check>
```

Strong success criteria let you loop without back-and-forth. Weak criteria
("make it work") force the user to babysit each step.

**Anchors in this repo:**
- Every comment-thread issue had an exact `curl` smoke test (Part B §6).
  When the response shape changed, the test caught it instantly. Vague
  checks like "open the page and look" did not.
- "Put server + frontend in Docker" was the goal. The verifiable subgoals
  were: (1) `docker compose ps` shows both healthy; (2) host `curl` works on
  both ports; (3) browser smoke test on `/series/<id>` succeeds. Without
  those, we'd have called it done at step 1.

---

## A.5 The 30-second pre-flight

Before each non-trivial change, answer these in your head — or out loud:

1. **What is the user actually asking for?** State it in one sentence.
2. **What am I assuming?** Name two assumptions you're making.
3. **Is there a 1-line fix?** Could a tiny upstream change replace what I'm
   about to write?
4. **What's the verification?** What command/test proves this works?
5. **What am I touching that I shouldn't?** Anything outside the diff
   surface justifies a comment, not a silent edit.

If any answer is fuzzy, **stop and ask**. One question is cheaper than one
revert.

---

## A.6 Communication norms

- **Direct over polite.** Skip "Great question!" / "You're absolutely right!".
- **Cite files and lines.** `@/path/to/file.ts:12-30` beats prose.
- **Bold the verb.** "**Fix:** ...", "**Cause:** ...", "**Verify:** ..." make
  long messages skim-able.
- **End with status.** Every reply ends with what's done, what's broken, and
  what's next. No silent "I think it's fine."

---

# Part B — Concrete gotchas

## 1. Database & Migrations

### 1.1 The migrate runner is marker-aware (axe v0.5.2+)

The runner parses `-- +migrate Up` / `-- +migrate Down` markers and
**only executes the Up section**. Files with no markers are run as a
single Up block (legacy compat).

This **was not** true in v0.5.1 — the runner exec'd the whole file in
one transaction, so Down blocks dropped what Up just created. We lost a
column to that bug; the parser exists because of it. See
`cmd/axe/migrate/migrate_test.go:TestSplitUpDown_BothSections_DownIsIsolated`
for the regression guard.

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

Idempotent statements (`IF NOT EXISTS`) are still recommended so a
half-applied migration can be safely re-run after `axe migrate forget`.

### 1.2 Re-running a failed migration

If a migration applied partially (e.g. created column then errored), the
`schema_migrations` table may or may not have the row. Use the explicit
forget command:

```bash
axe migrate forget 20260424180000_scale_indexes.sql
make migrate-up
```

If you don't have the binary in PATH, the equivalent raw SQL is:

```bash
docker exec webtoon_postgres psql -U webtoon -d webtoon_dev \
  -c "DELETE FROM schema_migrations WHERE filename = 'XXX.sql';"
```

### 1.3 Recursive CTEs
Postgres requires `WITH RECURSIVE`, **not** plain `WITH`, for CTEs that
reference themselves. Forgetting `RECURSIVE` produces:

```
ERROR: relation "thread" does not exist
HINT:  Use WITH RECURSIVE, or re-order the WITH items to remove forward references.
```

### 1.4 Connect to the dev DB
```bash
docker exec -it webtoon_postgres psql -U webtoon -d webtoon_dev
```
Note: user is `webtoon`, **not** `postgres`. The default `psql` (no `-U`)
errors with `role "postgres" does not exist`.

---

## 2. Docker

### 2.1 Bring everything up
```bash
docker compose up -d --build api web   # rebuilds the two app images
docker compose ps                       # check status
docker compose logs -f api              # tail API logs
docker compose logs -f web              # tail Next.js logs
docker compose logs -f api web          # both
```

### 2.2 `NEXT_PUBLIC_*` is baked at build time
Anything starting with `NEXT_PUBLIC_` is **inlined into the build artifact**
by Next.js. Setting it as a runtime `environment:` value in compose has no
effect on values read by the build, including `next.config.mjs`'s `rewrites()`
destinations (which are written into `routes-manifest.json` at build time).

**Pattern for runtime-configurable URLs that must be reachable from the Next
server (not the browser):**

1. Use a non-`NEXT_PUBLIC_` name, e.g. `NEXT_INTERNAL_API_URL`.
2. Pass it as an `ARG` in `web/Dockerfile`, then promote to `ENV` *before*
   `npm run build` so the rewrite reads it during build.
3. In `docker-compose.yml`, set both `build.args.NEXT_INTERNAL_API_URL` (build-
   time, gets baked) and `environment.NEXT_INTERNAL_API_URL` (runtime, used by
   server-side fetches in RSC/route handlers).

### 2.3 In-cluster DNS vs `localhost`
Inside a container, `localhost` is the container itself, **not the host**.
Service-to-service URLs must use the compose service name:

| From         | API URL              |
|--------------|----------------------|
| Browser      | `http://localhost:8080` |
| Web (Next)   | `http://api:8080`       |
| API → MinIO  | `http://minio:9000`     |

`STORAGE_PUBLIC_URL` is the URL **clients receive** in JSON responses for
fetching uploaded assets — it must be reachable from the user's browser, so
keep it `http://localhost:9000` (or your CDN), even though the API container
itself uses `minio:9000` to talk to MinIO.

### 2.4 Go version drift
The Dockerfile must match `go.mod`'s `go` directive. We hit:
```
go: go.mod requires go >= 1.25.0 (running go 1.22.12; GOTOOLCHAIN=local)
```
Bump `FROM golang:X.Y-alpine` whenever `go.mod` advances.

---

## 3. Next.js 15 (App Router)

### 3.1 `useSearchParams()` requires a Suspense boundary in prod build
`output: "standalone"` does static prerendering for client pages. Any client
component calling `useSearchParams()` (or `useRouter`'s search-aware APIs)
must be wrapped in `<Suspense>` or build fails:

```
useSearchParams() should be wrapped in a suspense boundary at page "/auth/login"
```

Pattern:
```tsx
export default function LoginPage() {
  return (
    <Suspense fallback={<Skeleton />}>
      <LoginForm />
    </Suspense>
  );
}
function LoginForm() {
  const sp = useSearchParams();
  // ...
}
```

`export const dynamic = "force-dynamic"` does **not** rescue client
components — it only opts server pages out of static generation.

### 3.2 Page files have a restricted export surface
A file named `app/<route>/page.tsx` may only export:
- `default` (the page component)
- Reserved fields: `metadata`, `generateMetadata`, `dynamic`, `revalidate`,
  `fetchCache`, `runtime`, `preferredRegion`, `viewport`, `generateViewport`,
  `generateStaticParams`.

Any other named export fails the build:
```
Type error: Page "app/auth/login/page.tsx" does not match the required types of a Next.js Page.
  "AuthShell" is not a valid Page export field.
```

**Fix:** put shared UI in `app/<group>/_components/<Name>.tsx`. The leading
underscore tells Next this folder is private (no route).

### 3.3 Rewrites are evaluated at build time for `output: standalone`
`next.config.mjs`'s `async rewrites()` runs while building the standalone
server. The destination URL is serialized into `.next/routes-manifest.json`.
Runtime `process.env` changes are ignored. See §2.2.

---

## 4. Comment threading (current model)

Replies are logically multi-level but rendered as a flat 1-level tree:

| Column              | Meaning                                                |
|---------------------|--------------------------------------------------------|
| `parent_comment_id` | The comment the user actually replied to (any depth). |
| `root_comment_id`   | Top-level ancestor. Drives **grouping** in the UI.    |

On `CreateComment(parentID)`:
- Look up `parent.root_comment_id`. If NULL, `parent` is itself a top-level
  → `root = parent.id`. Else `root = parent.root_comment_id`.
- Store both `parent_comment_id` and `root_comment_id`.

On `ListComments`, `LEFT JOIN` the parent row to expose `parent_user_id` so
the frontend can render `@User` mentions for replies-to-replies.

Frontend grouping key is `root_comment_id ?? parent_comment_id ?? null`. The
`?? parent_comment_id` fallback handles legacy rows; remove it once backfill
is verified.

**When adding a Reply form on a reply (rc):** submit with `rc.id`, NOT
`top.id`. The backend resolves the root automatically. Passing the top-level
id silently flattens the chain and loses `parent_user_id`.

---

## 5. Quick-fix checklist when an API endpoint returns 500

1. `docker compose logs api --tail=50` — backend may print the error.
2. If logs are unhelpful (often the case for repo errors), reproduce the
   query directly:
   ```bash
   docker exec webtoon_postgres psql -U webtoon -d webtoon_dev -c "<the SQL>"
   ```
3. For Next.js `/api/*` 500s with empty backend logs, check `docker compose
   logs web` — it logs proxy failures (`ECONNREFUSED`, etc.) to stderr.

---

## 6. Useful smoke tests

```bash
curl -sf http://localhost:8080/health
curl -s 'http://localhost:8080/api/v1/episodes/<EPISODE_ID>/comments' | jq .
curl -s 'http://localhost:3000/api/v1/serieses/<SERIES_ID>' | jq .   # via Next rewrite
```

If the second works but the third fails with 500, it's a Next.js rewrite
issue (see §2.2 / §3.3).
