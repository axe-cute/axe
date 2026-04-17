// Package benchmarks compares axe (Chi v5) against Gin, Echo, and Fiber.
//
// Run:
//   go test -bench=. -benchmem -count=5 -timeout=10m | tee results.txt
//
// Pretty:
//   go install golang.org/x/perf/cmd/benchstat@latest
//   benchstat results.txt
package benchmarks

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/gin-gonic/gin"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"github.com/gofiber/fiber/v2"
	fibermw "github.com/gofiber/fiber/v2/middleware/recover"
	fiberlog "github.com/gofiber/fiber/v2/middleware/logger"
)

// ─────────────────────────────────────────────────────────────────────────────
// Shared data
// ─────────────────────────────────────────────────────────────────────────────

var jsonPayload = []byte(`{"name":"Alice","email":"alice@example.com","age":30}`)

type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

// ═════════════════════════════════════════════════════════════════════════════
// 1. Static JSON — GET /health → {"status":"ok"}
// ═════════════════════════════════════════════════════════════════════════════

// ── Chi (axe) ────────────────────────────────────────────────────────────────

func chiStaticJSON() http.Handler {
	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})
	return r
}

func BenchmarkStaticJSON_Chi(b *testing.B) {
	h := chiStaticJSON()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

// ── Gin ──────────────────────────────────────────────────────────────────────

func ginStaticJSON() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

func BenchmarkStaticJSON_Gin(b *testing.B) {
	h := ginStaticJSON()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

// ── Echo ─────────────────────────────────────────────────────────────────────

func echoStaticJSON() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	return e
}

func BenchmarkStaticJSON_Echo(b *testing.B) {
	h := echoStaticJSON()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

// ── Fiber ────────────────────────────────────────────────────────────────────
// Fiber uses fasthttp, not net/http. Use app.Test() for fair comparison.

func fiberStaticJSON() *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	return app
}

func BenchmarkStaticJSON_Fiber(b *testing.B) {
	app := fiberStaticJSON()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, _ := app.Test(req, -1)
		resp.Body.Close()
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// 2. URL Params — GET /users/:id → {"id":"<id>"}
// ═════════════════════════════════════════════════════════════════════════════

func chiURLParam() http.Handler {
	r := chi.NewRouter()
	r.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"` + id + `"}`)) //nolint:errcheck
	})
	return r
}

func BenchmarkURLParam_Chi(b *testing.B) {
	h := chiURLParam()
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func ginURLParam() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/users/:id", func(c *gin.Context) {
		id := c.Param("id")
		c.JSON(http.StatusOK, gin.H{"id": id})
	})
	return r
}

func BenchmarkURLParam_Gin(b *testing.B) {
	h := ginURLParam()
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func echoURLParam() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.GET("/users/:id", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"id": c.Param("id")})
	})
	return e
}

func BenchmarkURLParam_Echo(b *testing.B) {
	h := echoURLParam()
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func fiberURLParam() *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/users/:id", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"id": c.Params("id")})
	})
	return app
}

func BenchmarkURLParam_Fiber(b *testing.B) {
	app := fiberURLParam()
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, _ := app.Test(req, -1)
		resp.Body.Close()
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// 3. Middleware Stack — Recovery + Logger + RequestID → JSON
// ═════════════════════════════════════════════════════════════════════════════

func chiMiddleware() http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	r.Use(chimw.RequestID)
	r.Use(chimw.SetHeader("X-Request-Id", "bench"))
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})
	return r
}

func BenchmarkMiddleware_Chi(b *testing.B) {
	h := chiMiddleware()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func ginMiddleware() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.LoggerWithWriter(io.Discard))
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

func BenchmarkMiddleware_Gin(b *testing.B) {
	h := ginMiddleware()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func echoMiddleware() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.Use(echomw.Recover())
	e.Use(echomw.RequestID())
	e.Use(echomw.LoggerWithConfig(echomw.LoggerConfig{Output: io.Discard}))
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	return e
}

func BenchmarkMiddleware_Echo(b *testing.B) {
	h := echoMiddleware()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func fiberMiddleware() *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(fibermw.New())
	app.Use(fiberlog.New(fiberlog.Config{Output: io.Discard}))
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	return app
}

func BenchmarkMiddleware_Fiber(b *testing.B) {
	app := fiberMiddleware()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, _ := app.Test(req, -1)
		resp.Body.Close()
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// 4. JSON Body Parse — POST /users (parse JSON → echo back)
// ═════════════════════════════════════════════════════════════════════════════

func chiJSONParse() http.Handler {
	r := chi.NewRouter()
	r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
		var u User
		if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(u) //nolint:errcheck
	})
	return r
}

func BenchmarkJSONParse_Chi(b *testing.B) {
	h := chiJSONParse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func ginJSONParse() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.POST("/users", func(c *gin.Context) {
		var u User
		if err := c.ShouldBindJSON(&u); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, u)
	})
	return r
}

func BenchmarkJSONParse_Gin(b *testing.B) {
	h := ginJSONParse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func echoJSONParse() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.POST("/users", func(c echo.Context) error {
		var u User
		if err := c.Bind(&u); err != nil {
			return c.JSON(400, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, u)
	})
	return e
}

func BenchmarkJSONParse_Echo(b *testing.B) {
	h := echoJSONParse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func fiberJSONParse() *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Post("/users", func(c *fiber.Ctx) error {
		var u User
		if err := c.BodyParser(&u); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(u)
	})
	return app
}

func BenchmarkJSONParse_Fiber(b *testing.B) {
	app := fiberJSONParse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req, -1)
		resp.Body.Close()
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// 5. Multi-Route Matching — 50 registered routes, hit the last one
// ═════════════════════════════════════════════════════════════════════════════

func chiMultiRoute() http.Handler {
	r := chi.NewRouter()
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok")) //nolint:errcheck
	}
	for i := 0; i < 50; i++ {
		r.Get("/route"+strings.Repeat("x", i), handler)
	}
	return r
}

func BenchmarkMultiRoute_Chi(b *testing.B) {
	h := chiMultiRoute()
	req := httptest.NewRequest(http.MethodGet, "/route"+strings.Repeat("x", 49), nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func ginMultiRoute() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	handler := func(c *gin.Context) { c.String(200, "ok") }
	for i := 0; i < 50; i++ {
		r.GET("/route"+strings.Repeat("x", i), handler)
	}
	return r
}

func BenchmarkMultiRoute_Gin(b *testing.B) {
	h := ginMultiRoute()
	req := httptest.NewRequest(http.MethodGet, "/route"+strings.Repeat("x", 49), nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func echoMultiRoute() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	handler := func(c echo.Context) error { return c.String(200, "ok") }
	for i := 0; i < 50; i++ {
		e.GET("/route"+strings.Repeat("x", i), handler)
	}
	return e
}

func BenchmarkMultiRoute_Echo(b *testing.B) {
	h := echoMultiRoute()
	req := httptest.NewRequest(http.MethodGet, "/route"+strings.Repeat("x", 49), nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}
}

func fiberMultiRoute() *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	handler := func(c *fiber.Ctx) error { return c.SendString("ok") }
	for i := 0; i < 50; i++ {
		app.Get("/route"+strings.Repeat("x", i), handler)
	}
	return app
}

func BenchmarkMultiRoute_Fiber(b *testing.B) {
	app := fiberMultiRoute()
	req := httptest.NewRequest(http.MethodGet, "/route"+strings.Repeat("x", 49), nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, _ := app.Test(req, -1)
		resp.Body.Close()
	}
}
