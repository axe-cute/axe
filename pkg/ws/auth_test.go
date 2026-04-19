package ws_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"

	"github.com/axe-cute/axe/pkg/jwtauth"
	"github.com/axe-cute/axe/pkg/ws"
)

const testSecret = "test-secret-key-for-ws-auth-32bytes!"

func newTestJWTService() *jwtauth.Service {
	return jwtauth.New(testSecret, 15*time.Minute, 7*24*time.Hour)
}

func TestWSAuth_ValidToken_Header(t *testing.T) {
	svc := newTestJWTService()
	tracker := ws.NewUserConnTracker()

	pair, err := svc.GenerateTokenPair(uuid.New(), "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	var gotClaims *jwtauth.Claims
	mw := ws.WSAuth(svc, nil, tracker)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims = ws.ClaimsFromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()

	mw(inner).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotClaims == nil {
		t.Fatal("claims not set in context")
	}
	if gotClaims.Role != "user" {
		t.Errorf("role = %q, want %q", gotClaims.Role, "user")
	}
}

func TestWSAuth_ValidToken_QueryParam(t *testing.T) {
	svc := newTestJWTService()

	pair, err := svc.GenerateTokenPair(uuid.New(), "admin")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	var gotClaims *jwtauth.Claims
	mw := ws.WSAuth(svc, nil, nil) // nil tracker
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims = ws.ClaimsFromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/ws?token="+pair.AccessToken, nil)
	w := httptest.NewRecorder()

	mw(inner).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotClaims == nil || gotClaims.Role != "admin" {
		t.Errorf("claims role = %v, want admin", gotClaims)
	}
}

func TestWSAuth_InvalidToken(t *testing.T) {
	svc := newTestJWTService()

	mw := ws.WSAuth(svc, nil, nil)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner should not be called")
	})

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Authorization", "Bearer invalid.jwt.token")
	w := httptest.NewRecorder()

	mw(inner).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestWSAuth_ConnectionLimit(t *testing.T) {
	svc := newTestJWTService()
	tracker := ws.NewUserConnTracker()
	userID := uuid.New()

	pair, err := svc.GenerateTokenPair(userID, "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	mw := ws.WSAuth(svc, nil, tracker, ws.WithMaxConnsPerUser(2))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	makeReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/ws", nil)
		req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		w := httptest.NewRecorder()
		mw(inner).ServeHTTP(w, req)
		return w
	}

	// 2 connections should succeed.
	w1 := makeReq()
	if w1.Code != http.StatusOK {
		t.Errorf("conn 1: status = %d, want 200", w1.Code)
	}
	w2 := makeReq()
	if w2.Code != http.StatusOK {
		t.Errorf("conn 2: status = %d, want 200", w2.Code)
	}

	// 3rd should be rejected with 429.
	w3 := makeReq()
	if w3.Code != http.StatusTooManyRequests {
		t.Errorf("conn 3: status = %d, want 429", w3.Code)
	}

	// Release one slot, try again.
	tracker.Release(userID.String())
	w4 := makeReq()
	if w4.Code != http.StatusOK {
		t.Errorf("conn 4 (after release): status = %d, want 200", w4.Code)
	}
}

// mockBlocklist implements ws.WSBlocklist for testing.
type mockBlocklist struct {
	blocked map[string]bool
	failErr error
}

func (m *mockBlocklist) IsTokenBlocked(_ context.Context, jti string) (bool, error) {
	if m.failErr != nil {
		return false, m.failErr
	}
	return m.blocked[jti], nil
}

func TestWSAuth_BlockedToken(t *testing.T) {
	svc := newTestJWTService()
	userID := uuid.New()

	pair, err := svc.GenerateTokenPair(userID, "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	// Extract the JTI from the token to block it.
	claims, err := svc.Validate(pair.AccessToken)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	bl := &mockBlocklist{blocked: map[string]bool{claims.JTI(): true}}

	mw := ws.WSAuth(svc, bl, nil)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner should not be called for blocked token")
	})

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()

	mw(inner).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestWSAuth_BlocklistError_FailOpen(t *testing.T) {
	svc := newTestJWTService()
	userID := uuid.New()

	pair, err := svc.GenerateTokenPair(userID, "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	bl := &mockBlocklist{failErr: fmt.Errorf("redis timeout")}

	mw := ws.WSAuth(svc, bl, nil)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // fail-open: should reach here
	})

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()

	mw(inner).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (fail-open)", w.Code)
	}
}

// ── Hub options tests ────────────────────────────────────────────────────────

func TestHub_WithAdapter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	hub := ws.NewHub(ws.WithAdapter(ws.MemoryAdapter{}))
	go hub.Run(ctx)

	if hub.ClientCount() != 0 {
		t.Error("expected 0 clients")
	}
}

func TestHub_WithLogger(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	hub := ws.NewHub(ws.WithLogger(slog.Default()))
	go hub.Run(ctx)

	if hub.ClientCount() != 0 {
		t.Error("expected 0 clients")
	}
}

func TestHub_UpgradeAuthenticated(t *testing.T) {
	svc := newTestJWTService()
	tracker := ws.NewUserConnTracker()
	userID := uuid.New()

	pair, err := svc.GenerateTokenPair(userID, "user")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := ws.NewHub()
	go hub.Run(ctx)

	connected := make(chan struct{})
	srv := httptest.NewServer(ws.WSAuth(svc, nil, tracker)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			client, err := hub.UpgradeAuthenticated(w, r, tracker)
			if err != nil {
				return
			}
			close(connected)
			<-client.Done()
		}),
	))
	defer srv.Close()

	// Dial with token in query param (like browser WS client).
	wsURL := "ws" + srv.URL[4:] + "?token=" + pair.AccessToken
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow() //nolint:errcheck

	select {
	case <-connected:
		// Client connected successfully with auth.
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for authenticated connection")
	}

	time.Sleep(50 * time.Millisecond)
	if hub.ClientCount() != 1 {
		t.Errorf("ClientCount = %d, want 1", hub.ClientCount())
	}
	if got := tracker.Count(userID.String()); got != 1 {
		t.Errorf("tracker.Count = %d, want 1", got)
	}

	// Disconnect → tracker should release.
	conn.CloseNow() //nolint:errcheck
	time.Sleep(200 * time.Millisecond)

	if got := tracker.Count(userID.String()); got != 0 {
		t.Errorf("after disconnect: tracker.Count = %d, want 0", got)
	}
}
