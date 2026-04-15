# ADR-001: Chi over Gin/Echo for HTTP routing

**Status**: Accepted  
**Date**: 2026-04-15

## Context

axe needed an HTTP router. The Go ecosystem has several mature options: `net/http` (stdlib), Chi, Gin, Echo, Fiber.

## Decision

Use **Chi v5** as the HTTP router.

## Rationale

| Criterion | Chi | Gin | Echo |
|---|---|---|---|
| Interface-based middleware | ✅ `http.Handler` | ❌ custom `HandlerFunc` | ❌ custom |
| stdlib compatible | ✅ | ❌ | ❌ |
| Reflection at runtime | ❌ none | ⚠️ some | ⚠️ some |
| Testability (`httptest`) | ✅ native | ⚠️ wrapper needed | ⚠️ wrapper needed |
| Zero-dependency routing | ✅ | ❌ | ❌ |

Chi's `http.Handler` interface compatibility means:
- Standard `httptest.NewRecorder()` works directly in tests
- Middleware is composable with any stdlib-compatible library
- No vendor lock-in for middleware logic

## Consequences

- **Positive**: Clean separation, stdlib-compatible, easier testing
- **Positive**: Any `http.Handler` middleware works without adapters
- **Negative**: Less "magic" features (no binding, no validation built-in)
- **Mitigation**: Handled explicitly in handler layer (json.Decode + apperror)
