package middleware

import (
	"context"

	"github.com/axe-cute/axe/pkg/jwtauth"
)

// InjectClaimsForTest injects JWT claims into a context for use in handler
// unit tests. This bypasses the JWTAuth middleware and allows testing handlers
// that depend on ClaimsFromCtx() without a real JWT token.
//
// This function is intentionally exported for testing only.
// DO NOT use in production code.
func InjectClaimsForTest(ctx context.Context, claims *jwtauth.Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}
