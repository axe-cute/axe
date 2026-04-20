# IDOR Protection Guide

> **IDOR** (Insecure Direct Object Reference) occurs when an API endpoint
> accepts a resource ID from the client without verifying that the
> authenticated user has permission to access that resource.

## The Problem

```go
// ❌ VULNERABLE — any authenticated user can read/modify ANY user's data
r.Get("/api/v1/users/{id}", userHandler.GetUser)
r.Put("/api/v1/users/{id}", userHandler.UpdateUser)
```

An attacker with a valid JWT for `user-123` can call:
```
GET /api/v1/users/user-456
PUT /api/v1/users/user-456 {"name": "pwned"}
```

## Solution: Ownership Checks

### Pattern 1: Service-Layer Guard (Recommended)

```go
// internal/service/user_service.go
func (s *UserService) GetUser(ctx context.Context, callerID, targetID string) (*domain.User, error) {
    // Self-access only (unless admin)
    if callerID != targetID {
        return nil, apperror.ErrForbidden.WithMessage("cannot access other user's data")
    }
    return s.repo.FindByID(ctx, targetID)
}
```

### Pattern 2: Middleware Guard (For Resource-Wide Protection)

```go
// internal/handler/middleware/ownership.go
func OwnerOnly(paramName string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            claims := jwtauth.ClaimsFromContext(r.Context())
            resourceOwner := chi.URLParam(r, paramName)
            
            if claims.UID != resourceOwner {
                WriteError(w, apperror.ErrForbidden)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}

// Usage in router:
r.With(middleware.OwnerOnly("id")).Put("/users/{id}", handler.UpdateUser)
```

### Pattern 3: Implicit User from JWT (Best — No ID in URL)

```go
// ❌ Explicit ID in URL — IDOR risk
r.Get("/api/v1/users/{id}/profile", handler.GetProfile)

// ✅ Implicit from JWT — no IDOR possible
r.Get("/api/v1/me/profile", handler.GetMyProfile)

func (h *Handler) GetMyProfile(w http.ResponseWriter, r *http.Request) {
    claims := jwtauth.ClaimsFromContext(r.Context())
    profile, err := h.svc.GetProfile(r.Context(), claims.UID)
    // ...
}
```

## Axe Framework Conventions

1. **Self-access endpoints** → Use `/me/` prefix (no ID parameter)
2. **Admin endpoints** → Use role-based middleware (`RequireRole("admin")`)
3. **Multi-tenant** → Use tenant middleware to scope all queries
4. **Generated CRUD** → `axe generate resource` creates admin-only routes by default

## Checklist for New Endpoints

```
□ Does this endpoint accept a resource ID from the URL/body?
□ Is the caller's identity verified against the resource owner?
□ Can a non-admin user access another user's resources?
□ Are list endpoints scoped to the caller's resources?
□ Are batch operations scoped to the caller's resources?
```

## Related

- [Architecture Contract](architecture_contract.md) — layer rules
- `pkg/jwtauth` — JWT claims extraction
- `internal/handler/middleware/auth.go` — auth middleware
