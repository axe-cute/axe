# Request Lifecycle — Tracing a Request Through axe

> How an HTTP request flows through every layer, from middleware to database and back.
> Use this doc when debugging at 2AM — it tells you exactly where to look.

---

## The 10-Second Version

```
HTTP Request
  → Middleware (RequestID → Logger → RateLimit → Auth)
    → Handler (parse JSON → call service)
      → Service (validate → authorize → transaction)
        → Repository (Ent query → database)
      ← Service (return result or apperror)
    ← Handler (write JSON response)
  ← Middleware (log completion)
HTTP Response
```

**File hop count**: 4 files max (handler → service → repository → ent schema).
If you're touching more than 4 files to trace a bug, something is wrong.

---

## Full Trace: `POST /api/v1/posts`

```
POST /api/v1/posts
│
├── [Middleware] chimiddleware.RequestID
│   └── Injects X-Request-Id into context (auto-generated UUID)
│
├── [Middleware] chimiddleware.Logger
│   └── Logs request start: method, path, remote_addr
│
├── [Middleware] metrics.Middleware
│   └── Increments Prometheus counter: axe_http_requests_total
│
├── [Middleware] ratelimit (if enabled)
│   └── Redis sliding-window check → 429 if exceeded
│
├── [Middleware] jwtauth.ChiMiddleware (if route requires auth)
│   └── Validate JWT → inject Claims into context
│       On failure: 401 Unauthorized (never reaches handler)
│
├── [Handler] postHandler.Create(w, r)
│   │
│   │  File: internal/handler/post_handler.go
│   │
│   ├── json.Decode(r.Body) → createPostRequest{}
│   │   └── DisallowUnknownFields: rejects unexpected JSON keys → 400
│   │
│   ├── Parse/validate request fields (UUID parsing, required checks)
│   │   └── On failure: apperror.ErrInvalidInput → 400
│   │
│   └── result, err := h.svc.CreatePost(r.Context(), input)
│       │
│       │  ⚠️  Handler does NOT contain business logic, auth checks,
│       │     or database calls. It only parses and delegates.
│       │
│       ├── On error: middleware.WriteError(w, err)
│       │   └── apperror type → HTTP status (see Error Flow below)
│       │
│       └── On success: middleware.WriteJSON(w, 201, response)
│
├── [Service] postService.CreatePost(ctx, input)
│   │
│   │  File: internal/service/post_service.go
│   │
│   ├── Domain validation (business rules)
│   │   └── e.g. title not empty, author exists, no duplicates
│   │
│   ├── Authorization (if applicable)
│   │   └── claims := jwtauth.ClaimsFromCtx(ctx)
│   │       Check ownership, role, permissions → ErrForbidden
│   │
│   └── Transaction boundary (if >1 write)
│       │
│       │  txMgr.WithTx(ctx, func(tx) error {
│       │      post, err := s.repo.Create(ctx, input)
│       │      if err != nil { return err }           // → rollback
│       │      return s.outboxRepo.Append(ctx, event) // same tx
│       │  })
│       │
│       │  Single write (no outbox): no transaction needed.
│       │  See: docs/data_consistency.md
│       │
│       └── logger.FromCtx(ctx).Info("post created", "id", post.ID)
│
├── [Repository] postRepo.Create(ctx, input)
│   │
│   │  File: internal/repository/post_repo.go
│   │
│   └── entClient.Post.Create().
│           SetTitle(input.Title).
│           SetBody(input.Body).
│           Save(ctx)
│       │
│       └── On DB error: fmt.Errorf("PostRepo.Create: %w", err)
│           (wrapped, preserves original for debugging)
│
├── [Outbox Poller — Background] (if outbox event was appended)
│   ├── Every 5s: read unprocessed outbox_events
│   ├── Publish to Asynq task queue
│   └── Mark event as processed
│
└── [Handler] return 201 Created + JSON response
```

---

## Error Flow

Errors bubble up through layers, gaining context at each level:

```
Layer          What happens                        Example
─────────────────────────────────────────────────────────────────
Repository     Wrap DB error with context           fmt.Errorf("PostRepo.Create: %w", err)
Service        Map to apperror taxonomy             apperror.ErrNotFound.WithMessage("post not found")
Handler        (automatic) WriteError middleware     apperror type → HTTP status + JSON body
```

### Status Code Mapping

| apperror Type     | HTTP Status | When                              |
|-------------------|-------------|-----------------------------------|
| `ErrInvalidInput` | 400         | Bad request body, missing fields  |
| `ErrUnauthorized` | 401         | Missing/expired JWT               |
| `ErrForbidden`    | 403         | Valid JWT but insufficient role    |
| `ErrNotFound`     | 404         | Resource doesn't exist            |
| `ErrConflict`     | 409         | Duplicate email, business rule    |
| `ErrInternal`     | 500         | Unexpected error (always logged)  |
| Unknown `error`   | 500         | Untyped error (always logged)     |

**Rule**: 5xx errors are always logged with full stack context. 4xx errors are user-facing and intentional — log at Info level, not Error.

---

## Where to Look When Debugging

| Symptom                          | Check first                        | File                           |
|----------------------------------|------------------------------------|--------------------------------|
| 400 Bad Request                  | Request JSON shape, field types    | `handler/*_handler.go`         |
| 401 Unauthorized                 | JWT token, expiry, blocklist       | `internal/infra/jwtauth/`      |
| 403 Forbidden                    | Role/ownership check in service    | `service/*_service.go`         |
| 404 Not Found                    | Repository query, ID format        | `repository/*_repo.go`         |
| 409 Conflict                     | Business rule in service           | `service/*_service.go`         |
| 500 Internal Server Error        | Check logs first (always logged)   | Any layer — follow the `%w`    |
| Data inconsistency               | Transaction boundary in service    | `service/*_service.go`         |
| Missing side effect (no email)   | Outbox poller, Asynq worker        | `docs/data_consistency.md`     |
| Slow response                    | `/metrics` endpoint, DB queries    | `repository/*_repo.go`         |

---

## Layer Import Rules (Compiler-Enforced)

```
domain/       → stdlib only (context, time, fmt, errors, uuid)
                ❌ No database, HTTP, logging, framework imports

handler/      → domain (interfaces), infra/apperror, infra/jwtauth
                ❌ No repository imports, no direct DB access

service/      → domain (interfaces), infra/apperror, infra/logger
                ❌ No HTTP imports (net/http, chi)

repository/   → domain (interfaces), ent client
                ❌ No business logic, no HTTP, no calling other repos
```

**Violation = compile error.** Interfaces in `domain/` ensure each layer depends only on abstractions, never on concrete implementations. If `go build` passes, layer boundaries are intact.

---

## See Also

- **[Data Consistency](../data_consistency.md)** — Transaction manager + outbox pattern in depth
- **[Architecture Contract](../architecture_contract.md)** — Full rules document
- **[Getting Started](getting-started.md)** — Project setup and first resource

---

*Last updated: 2026-04-26*
