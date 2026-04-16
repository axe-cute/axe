package ws

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/axe-cute/axe/pkg/jwtauth"
	"github.com/axe-cute/axe/pkg/logger"
)

// ── Context key ───────────────────────────────────────────────────────────────

type wsContextKey string

const wsClaimsKey wsContextKey = "ws_jwt_claims"

// WSBlocklist defines the contract to check token revocation.
// Satisfied by *cache.Client (same as middleware.Blocklist).
// Pass nil to disable blocklist checks.
type WSBlocklist interface {
	IsTokenBlocked(ctx context.Context, jti string) (bool, error)
}

// ClaimsFromCtx retrieves JWT claims stored by WSAuth middleware.
// Returns nil on unauthenticated requests.
func ClaimsFromCtx(ctx context.Context) *jwtauth.Claims {
	v, _ := ctx.Value(wsClaimsKey).(*jwtauth.Claims)
	return v
}

// ── Auth options ──────────────────────────────────────────────────────────────

// authOptions holds tunable parameters for WSAuth.
type authOptions struct {
	maxConns int // maximum concurrent connections per user
}

// AuthOption configures WSAuth behaviour.
type AuthOption func(*authOptions)

// WithMaxConnsPerUser sets the per-user WebSocket connection cap.
// Default: 5. Connections exceeding the limit get HTTP 429.
func WithMaxConnsPerUser(n int) AuthOption {
	return func(o *authOptions) {
		if n > 0 {
			o.maxConns = n
		}
	}
}

// ── UserConnTracker ───────────────────────────────────────────────────────────

// UserConnTracker tracks the number of active WebSocket connections per user.
// It is safe for concurrent use and designed to be long-lived (shared by all
// WSAuth handlers).
type UserConnTracker struct {
	m sync.Map // map[userID string]*int64
}

// NewUserConnTracker creates an empty tracker.
func NewUserConnTracker() *UserConnTracker { return &UserConnTracker{} }

// Acquire tries to increment the connection count for userID.
// Returns true (and increments) when count < max; false otherwise (count unchanged).
func (t *UserConnTracker) Acquire(userID string, max int) bool {
	actual, _ := t.m.LoadOrStore(userID, new(int64))
	counter := actual.(*int64)

	for {
		cur := atomic.LoadInt64(counter)
		if cur >= int64(max) {
			return false
		}
		if atomic.CompareAndSwapInt64(counter, cur, cur+1) {
			return true
		}
	}
}

// Release decrements the connection count for userID.
// Call this when a connection closes. Safe to call multiple times.
func (t *UserConnTracker) Release(userID string) {
	if v, ok := t.m.Load(userID); ok {
		counter := v.(*int64)
		if atomic.AddInt64(counter, -1) < 0 {
			atomic.StoreInt64(counter, 0) // clamp to 0 if underflow (safety)
		}
	}
}

// Count returns the current active connection count for userID.
func (t *UserConnTracker) Count(userID string) int64 {
	v, ok := t.m.Load(userID)
	if !ok {
		return 0
	}
	return atomic.LoadInt64(v.(*int64))
}

// ── WSAuth middleware ─────────────────────────────────────────────────────────

// WSAuth returns an HTTP middleware that validates a JWT for WebSocket upgrade
// requests. The token is read from:
//
//  1. Authorization: Bearer <token>  (standard header)
//  2. ?token=<jwt>                   (query param — required by browser WS clients)
//
// On success the validated *jwtauth.Claims are stored in the request context
// (retrieve via ws.ClaimsFromCtx). On failure the request is terminated with
// an appropriate HTTP status code before the WebSocket handshake occurs.
//
// The optional tracker enforces a per-user connection cap (default 5).
// When a tracker is provided it must be the *same* instance passed to
// Hub.UpgradeAuthenticated so that Release is called on disconnect.
//
// Usage:
//
//	tracker := ws.NewUserConnTracker()
//	r.With(ws.WSAuth(jwtSvc, cacheClient, tracker)).Get("/ws", wsHandler)
func WSAuth(
	svc *jwtauth.Service,
	blocklist WSBlocklist,
	tracker *UserConnTracker,
	opts ...AuthOption,
) func(http.Handler) http.Handler {
	options := &authOptions{maxConns: 5}
	for _, o := range opts {
		o(options)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.FromCtx(r.Context())

			// 1. Extract token.
			token := extractWSToken(r)
			if token == "" {
				log.Info("ws auth: missing token", "remote", r.RemoteAddr)
				http.Error(w, "missing token", http.StatusUnauthorized)
				wsConnectRejectedTotal.Inc()
				return
			}

			// 2. Validate JWT.
			claims, err := svc.Validate(token)
			if err != nil {
				log.Info("ws auth: invalid token", "remote", r.RemoteAddr, "error", err)
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				wsConnectRejectedTotal.Inc()
				return
			}

			// 3. Blocklist check (token revocation).
			if blocklist != nil && claims.JTI() != "" {
				blocked, blErr := blocklist.IsTokenBlocked(r.Context(), claims.JTI())
				if blErr != nil {
					log.Warn("ws auth: blocklist check failed", "error", blErr)
					// Fail-open: log the issue, allow the request.
				} else if blocked {
					log.Info("ws auth: token revoked", "jti", claims.JTI(), "user_id", claims.UserID)
					http.Error(w, "token revoked", http.StatusUnauthorized)
					wsConnectRejectedTotal.Inc()
					return
				}
			}

			// 4. Per-user connection limit.
			if tracker != nil {
				if !tracker.Acquire(claims.UserID, options.maxConns) {
					log.Warn("ws auth: connection limit reached",
						"user_id", claims.UserID,
						"max", options.maxConns,
						"current", tracker.Count(claims.UserID))
					http.Error(w, "too many connections", http.StatusTooManyRequests)
					wsConnectRejectedTotal.Inc()
					return
				}
				// The tracker release is deferred to hub.UpgradeAuthenticated
				// via the client's readPump cleanup goroutine.
			}

			// 5. Inject claims into context and proceed.
			ctx := context.WithValue(r.Context(), wsClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractWSToken extracts the JWT from the request.
// Order: Authorization header → ?token query param.
func extractWSToken(r *http.Request) string {
	// Try Authorization: Bearer <token>
	if h := r.Header.Get("Authorization"); h != "" {
		parts := strings.SplitN(h, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			if t := strings.TrimSpace(parts[1]); t != "" {
				return t
			}
		}
	}
	// Fallback: ?token=<jwt>  (browser WebSocket clients cannot set headers)
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
}


