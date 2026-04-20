package ws

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/axe-cute/examples-webtoon/pkg/jwtauth"
	"github.com/axe-cute/examples-webtoon/pkg/logger"
)

type wsContextKey string

const wsClaimsKey wsContextKey = "ws_jwt_claims"

// WSBlocklist checks if a JWT has been revoked.
type WSBlocklist interface {
	IsTokenBlocked(ctx context.Context, jti string) (bool, error)
}

// ClaimsFromCtx extracts JWT claims set by WSAuth middleware.
func ClaimsFromCtx(ctx context.Context) *jwtauth.Claims {
	v, _ := ctx.Value(wsClaimsKey).(*jwtauth.Claims)
	return v
}

type authOptions struct{ maxConns int }

// AuthOption configures WSAuth behavior.
type AuthOption func(*authOptions)

// WithMaxConnsPerUser sets the maximum concurrent WebSocket connections per user.
func WithMaxConnsPerUser(n int) AuthOption {
	return func(o *authOptions) {
		if n > 0 {
			o.maxConns = n
		}
	}
}

// UserConnTracker tracks per-user WebSocket connection counts.
type UserConnTracker struct{ m sync.Map }

// NewUserConnTracker creates a new tracker.
func NewUserConnTracker() *UserConnTracker { return &UserConnTracker{} }

// Acquire attempts to increment the connection count for a user. Returns false if at max.
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

// Release decrements the connection count for a user.
func (t *UserConnTracker) Release(userID string) {
	if v, ok := t.m.Load(userID); ok {
		if atomic.AddInt64(v.(*int64), -1) < 0 {
			atomic.StoreInt64(v.(*int64), 0)
		}
	}
}

// Count returns the current connection count for a user.
func (t *UserConnTracker) Count(userID string) int64 {
	v, ok := t.m.Load(userID)
	if !ok {
		return 0
	}
	return atomic.LoadInt64(v.(*int64))
}

// WSAuth is a middleware that authenticates WebSocket connections via JWT.
func WSAuth(svc *jwtauth.Service, blocklist WSBlocklist, tracker *UserConnTracker, opts ...AuthOption) func(http.Handler) http.Handler {
	options := &authOptions{maxConns: 5}
	for _, o := range opts {
		o(options)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.FromCtx(r.Context())
			token := extractWSToken(r)
			if token == "" {
				log.Info("ws auth: missing token", "remote", r.RemoteAddr)
				http.Error(w, "missing token", http.StatusUnauthorized)
				wsConnectRejectedTotal.Inc()
				return
			}
			claims, err := svc.Validate(token)
			if err != nil {
				log.Info("ws auth: invalid token", "remote", r.RemoteAddr, "error", err)
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				wsConnectRejectedTotal.Inc()
				return
			}
			if blocklist != nil && claims.JTI() != "" {
				blocked, blErr := blocklist.IsTokenBlocked(r.Context(), claims.JTI())
				if blErr != nil {
					log.Warn("ws auth: blocklist check failed", "error", blErr)
				} else if blocked {
					http.Error(w, "token revoked", http.StatusUnauthorized)
					wsConnectRejectedTotal.Inc()
					return
				}
			}
			if tracker != nil && !tracker.Acquire(claims.UserID, options.maxConns) {
				http.Error(w, "too many connections", http.StatusTooManyRequests)
				wsConnectRejectedTotal.Inc()
				return
			}
			ctx := context.WithValue(r.Context(), wsClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractWSToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if parts := strings.SplitN(h, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			if t := strings.TrimSpace(parts[1]); t != "" {
				return t
			}
		}
	}
	return r.URL.Query().Get("token")
}
