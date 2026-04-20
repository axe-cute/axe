# Epic 10 — Audit v3 Hardening (v0.5.1)

**Goal**: Fix all 22 issues from PersonaTwin audit v3 to reach early-adopter-ready quality. Focus on scaffold correctness, security hardening, and API contract honesty.

**Business Value**: Framework scaffold is the first thing every user touches. Broken scaffold = zero adoption. This epic ensures `axe new` produces working, secure, honest code.

**Status**: 🔄 In Progress (Sprint 34)

**Priority**: P0 — Blocks early adopter outreach

**Prerequisites**:
- Audit v2 P0/P1 fixes (done ✅)
- PersonaTwin audit v3 report completed ✅

> **Source**: PersonaTwin v3 audit — 22 issues, score 5.5/10

---

## Stories

### Story 10.1 — Scaffold Critical Fixes (Sprint 34) — P0

**Status**: ✅ Done

**Goal**: Fix 4 scaffold bugs that affect 100% of new users in their first 5 minutes.

**Fixes**:
| # | Issue | File | Fix |
|---|---|---|---|
| A1 | readyHandler never sends status code | `cmd/axe/new/templates.go` | Use `w.WriteHeader(status)` before JSON write |
| A2 | `go 1.22.0` in generated go.mod | `cmd/axe/new/templates.go` | Change to `go 1.25.0` |
| A3 | slog `%s` literal (not printf) | `cmd/axe/new/templates.go` | Use `data.Name` string concat |
| A5 | Dockerfile uses debian (100MB) instead of distroless (10MB) | `cmd/axe/new/templates.go` | Switch to `gcr.io/distroless/static-debian12` |

**Acceptance Criteria**:
- [x] `axe new test --db=sqlite --yes` → project compiles without modification
- [x] go.mod says `go 1.25.0`
- [x] First log line shows app name (not `%s`)
- [x] `/ready` returns 503 when DB is unreachable
- [x] `docker build .` produces image < 20MB

---

### Story 10.2 — Plugin Version + MinAxeVersion Sweep (Sprint 34) — P0

**Status**: ✅ Done

**Goal**: Fix Kafka `MinAxeVersion: "v1.0.0"` and sweep entire codebase for any other instances.

**Fixes**:
| # | Issue | File |
|---|---|---|
| A4 | Kafka MinAxeVersion v1.0.0 | `pkg/plugin/kafka/plugin.go` |

**Acceptance Criteria**:
- [x] `grep -rn 'MinAxeVersion.*v1.0' pkg/` returns zero results
- [x] `grep -rn 'MinAxeVersion' pkg/` shows only `v0.5.0` or higher

---

### Story 10.3 — Unify JWT Systems (Sprint 34) — P0

**Status**: ✅ Done

**Goal**: Refactor OAuth2 plugin to use `jwtauth.Service` instead of homebrew HMAC-SHA256 JWT.

**Before**: Two incompatible JWT systems — framework tokens have JTI/uid/role, OAuth2 tokens have sub/email/provider. OAuth2 tokens fail `JWTAuth` middleware validation.

**After**: Single JWT system. All tokens go through `jwtauth.Service.GenerateTokenPair()`. OAuth2 `OnSuccess` callback becomes the identity bridge (returns `{UserID, Role, RedirectURL}`).

**Breaking Changes**:
- `Config.JWTSecret` and `Config.JWTExpiry` removed
- `Config.OnSuccess` signature: `func(ctx, *UserInfo) (string, error)` → `func(ctx, *UserInfo) (*Identity, error)`
- `*jwtauth.Service` added to `plugin.AppConfig`

**Acceptance Criteria**:
- [x] OAuth2 tokens parsed by `jwtauth.Service.Validate()` without error
- [x] OAuth2 tokens have JTI → can be revoked via `cache.BlockToken()`
- [x] Auth middleware accepts OAuth2 tokens (same claims structure)
- [x] Unit tests verify token interoperability

---

### Story 10.4 — Security Hardening (Sprint 34) — P1

**Status**: ✅ Done

**Goal**: Fix 4 security issues: IP spoofing, admin auth, cookie Secure flag, rate limiter fail-mode.

**Fixes**:
| # | Issue | File | Fix |
|---|---|---|---|
| B2 | OAuth2 cookie missing `Secure` flag | `pkg/plugin/oauth2/plugin.go` | Add `Secure: r.TLS != nil || XFP==https` |
| B3 | Rate limiter trusts X-Forwarded-For blindly | `pkg/ratelimit/ratelimit.go` | Add `TrustedProxies` config; default: RemoteAddr only |
| B4 | Admin dashboard unprotected by default | `pkg/plugin/admin/plugin.go` | Read-only mode when no Secret; warning log |
| B7 | Rate limiter fail-mode not configurable | `pkg/ratelimit/ratelimit.go` | Add `FailMode` config (open/closed) |

**Acceptance Criteria**:
- [x] Rate limiter uses `RemoteAddr` by default (not X-Forwarded-For)
- [x] Admin PUT endpoints blocked when Secret is empty
- [x] OAuth2 state cookie has `Secure` flag when behind HTTPS
- [x] Rate limiter `FailMode` configurable with documented default

---

### Story 10.5 — API Contract Fixes (Sprint 34) — P1

**Status**: ✅ Done

**Goal**: Fix event bus lie (`Publish` always returns nil) and pagination memory bomb.

**Fixes**:
| # | Issue | File | Fix |
|---|---|---|---|
| B5 | `Publish()` always returns nil | `pkg/plugin/events/bus.go` | Return `errors.Join(syncErrors...)` |
| B6 | No pagination limit cap | `internal/handler/user_handler.go` | Add `maxPageLimit = 100` |

**Acceptance Criteria**:
- [x] `Publish()` returns error when sync handler fails
- [x] `?limit=999999` capped to 100
- [x] Existing tests pass (some may need update for new Publish error semantics)

---

### Story 10.6 — DX & Code Quality (Sprint 34–35) — P2

**Status**: ✅ Done

**Goal**: Fix remaining P2/P3 issues — generated code quality, framework hygiene, Docker defaults.

**Fixes**:
| # | Issue | Fix |
|---|---|---|
| C1 | Post handler `Views` client-settable | ✅ Removed from `createPostRequest` DTO |
| C2 | Worker has domain logic in framework | ✅ Removed `WelcomeEmailHandler` from `pkg/worker` |
| C3 | JWT_SECRET placeholder passes validation | ✅ Changed to `CHANGE_ME_BEFORE_DEPLOY` |
| C4 | Docker compose: password = username | ✅ Use `{name}_dev_password` |
| C5 | Async handlers unbounded goroutines | ✅ Added semaphore (max 100 concurrent) |
| C6 | No IDOR protection docs | ✅ Created `docs/guides/idor-protection.md` |
| C7 | Scaffold pkg/ convention | ✅ Changed to `internal/infra/` |

---

## Risk Assessment

| Risk | Mitigation |
|---|---|
| OAuth2 JWT refactor breaks existing tokens | Pre-1.0, no production users yet. Document in CHANGELOG as breaking change. |
| Publish() returning errors breaks callers | Only 6 call sites, all use `_ = Publish(...)`. They already ignore errors — behavior unchanged. |
| Rate limiter default change (RemoteAddr only) | Users behind reverse proxy must set `TrustedProxies`. Document in migration guide. |
| Distroless Dockerfile has no shell | HEALTHCHECK removed — document k8s liveness probe alternative. |

---

## Sprint Timeline

| Sprint | Stories | Effort | Focus |
|---|---|---|---|
| 34 | 10.1, 10.2, 10.4, 10.5, 10.6 | ~5h | All P0/P1 + P2 done ✅ |
| 35 | 10.3 (OAuth2 JWT unification) | ~2h | Breaking change — single JWT system |

---

*Last updated: 2026-04-20*
