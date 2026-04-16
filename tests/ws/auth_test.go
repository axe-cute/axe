package ws_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/axe-cute/axe/pkg/jwtauth"
	"github.com/axe-cute/axe/pkg/ws"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// newTestJWTSvc returns a jwtauth.Service suitable for test use.
func newTestJWTSvc() *jwtauth.Service {
	return jwtauth.New("test-secret-32-bytes-xxxxxxxxxxxx", 1*time.Hour, 24*time.Hour)
}

// validToken generates a signed JWT for the given user.
func validToken(t *testing.T, svc *jwtauth.Service, userID uuid.UUID, role string) string {
	t.Helper()
	pair, err := svc.GenerateTokenPair(userID, role)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return pair.AccessToken
}

// okHandler is the stub "next" handler that writes 200.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// applyWSAuth wraps okHandler with WSAuth and calls it via httptest.
func applyWSAuth(
	svc *jwtauth.Service,
	tracker *ws.UserConnTracker,
	opts []ws.AuthOption,
	buildReq func() *http.Request,
) *httptest.ResponseRecorder {
	mw := ws.WSAuth(svc, nil /* no blocklist */, tracker, opts...)
	h := mw(okHandler)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, buildReq())
	return rec
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestWSAuth_RejectsNoToken ensures a request with no token gets 401.
func TestWSAuth_RejectsNoToken(t *testing.T) {
	svc := newTestJWTSvc()
	tracker := ws.NewUserConnTracker()

	rec := applyWSAuth(svc, tracker, nil, func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "/ws", nil)
	})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rec.Code)
	}
}

// TestWSAuth_RejectsInvalidToken ensures a malformed / wrong-secret token gets 401.
func TestWSAuth_RejectsInvalidToken(t *testing.T) {
	svc := newTestJWTSvc()
	tracker := ws.NewUserConnTracker()

	rec := applyWSAuth(svc, tracker, nil, func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.Header.Set("Authorization", "Bearer this.is.not.valid")
		return r
	})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rec.Code)
	}
}

// TestWSAuth_AcceptsHeaderToken verifies that a valid Bearer header passes.
func TestWSAuth_AcceptsHeaderToken(t *testing.T) {
	svc := newTestJWTSvc()
	tracker := ws.NewUserConnTracker()
	token := validToken(t, svc, uuid.New(), "user")

	rec := applyWSAuth(svc, tracker, nil, func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	})

	// okHandler returns 200; the WS upgrade itself won't run in httptest.
	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rec.Code)
	}
}

// TestWSAuth_AcceptsQueryParam verifies that a valid ?token= query param passes.
// This covers the browser WebSocket client path (no custom headers).
func TestWSAuth_AcceptsQueryParam(t *testing.T) {
	svc := newTestJWTSvc()
	tracker := ws.NewUserConnTracker()
	token := validToken(t, svc, uuid.New(), "user")

	rec := applyWSAuth(svc, tracker, nil, func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "/ws?token="+token, nil)
	})

	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rec.Code)
	}
}

// TestWSAuth_PrefersHeaderOverQuery verifies header takes precedence over ?token=.
func TestWSAuth_PrefersHeaderOverQuery(t *testing.T) {
	svc := newTestJWTSvc()
	tracker := ws.NewUserConnTracker()
	goodToken := validToken(t, svc, uuid.New(), "user")

	rec := applyWSAuth(svc, tracker, nil, func() *http.Request {
		// Query param has a bad token; header has a good one.
		r := httptest.NewRequest(http.MethodGet, "/ws?token=bad.token.here", nil)
		r.Header.Set("Authorization", "Bearer "+goodToken)
		return r
	})

	// Should pass because header is used first.
	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rec.Code)
	}
}

// TestWSAuth_MaxConns_Rejects6th verifies that the 6th connection for the same
// user is rejected with 429 when max=5.
func TestWSAuth_MaxConns_Rejects6th(t *testing.T) {
	svc := newTestJWTSvc()
	tracker := ws.NewUserConnTracker()
	limitedUserID := uuid.New()
	token := validToken(t, svc, limitedUserID, "user")

	opts := []ws.AuthOption{ws.WithMaxConnsPerUser(5)}

	// Acquire 5 slots manually (simulating 5 active connections).
	for i := 0; i < 5; i++ {
		if !tracker.Acquire(limitedUserID.String(), 5) {
			t.Fatalf("acquire %d failed unexpectedly", i+1)
		}
	}

	// 6th request should be rejected.
	rec := applyWSAuth(svc, tracker, opts, func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	})

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("want 429, got %d", rec.Code)
	}
}

// TestWSAuth_MaxConns_AllowsAfterRelease verifies that releasing a slot allows
// a new connection.
func TestWSAuth_MaxConns_AllowsAfterRelease(t *testing.T) {
	svc := newTestJWTSvc()
	tracker := ws.NewUserConnTracker()
	releaseUserID := uuid.New()
	token := validToken(t, svc, releaseUserID, "user")
	opts := []ws.AuthOption{ws.WithMaxConnsPerUser(2)}

	// Fill to max.
	tracker.Acquire(releaseUserID.String(), 2)
	tracker.Acquire(releaseUserID.String(), 2)

	// Release one.
	tracker.Release(releaseUserID.String())

	rec := applyWSAuth(svc, tracker, opts, func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	})

	if rec.Code != http.StatusOK {
		t.Errorf("want 200 after release, got %d", rec.Code)
	}
}

// TestUserConnTracker_Concurrent exercises the tracker under concurrent load
// to ensure no data races (run with -race).
func TestUserConnTracker_Concurrent(t *testing.T) {
	tracker := ws.NewUserConnTracker()
	const max = 5
	const goroutines = 50

	var wg sync.WaitGroup
	acquired := make([]bool, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			acquired[i] = tracker.Acquire("concurrent-user", max)
		}()
	}
	wg.Wait()

	count := 0
	for _, ok := range acquired {
		if ok {
			count++
		}
	}
	if count > max {
		t.Errorf("acquired %d slots but max is %d", count, max)
	}

	// Verify tracker.Count matches actual acquired.
	if got := tracker.Count("concurrent-user"); got != int64(count) {
		t.Errorf("tracker.Count = %d, want %d", got, count)
	}

	// Verify context injection.
	ctx := context.Background()
	_ = ctx // ClaimsFromCtx on background ctx should return nil
	if c := ws.ClaimsFromCtx(ctx); c != nil {
		t.Error("expected nil claims from empty context")
	}
}
