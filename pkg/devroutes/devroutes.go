// Package devroutes provides development-mode route debugging utilities.
// When a 404 occurs in development, it renders an HTML page listing all
// registered routes (similar to Rails' routing error page).
// In production, it returns a standard JSON error response.
package devroutes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RouteInfo holds metadata about a single registered route.
type RouteInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// Collect walks a chi.Router and returns all registered routes sorted by path.
func Collect(routers ...chi.Router) []RouteInfo {
	var routes []RouteInfo
	seen := make(map[string]bool)

	for _, router := range routers {
		_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			key := method + " " + route
			if !seen[key] {
				seen[key] = true
				routes = append(routes, RouteInfo{Method: method, Path: route})
			}
			return nil
		})
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path == routes[j].Path {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Path < routes[j].Path
	})

	return routes
}

// NotFoundHandler returns a 404 handler that shows a route listing in
// development mode, or a JSON error in production.
func NotFoundHandler(isDev bool, routers ...chi.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isDev {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
			return
		}
		routes := Collect(routers...)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(renderHTML(r.Method, r.URL.Path, routes)))
	}
}

// DebugRoutesHandler returns a handler that lists all routes as JSON or HTML
// based on the Accept header. Only available in development mode.
func DebugRoutesHandler(isDev bool, routers ...chi.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isDev {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		routes := Collect(routers...)

		// JSON response if Accept header requests it.
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(routes)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderHTML("", "", routes)))
	}
}

// PrintRoutes prints all registered routes to stdout in a table format.
func PrintRoutes(routers ...chi.Router) {
	routes := Collect(routers...)
	if len(routes) == 0 {
		return
	}

	// Calculate column widths
	methodW, pathW := 6, 4
	for _, ri := range routes {
		if len(ri.Method) > methodW {
			methodW = len(ri.Method)
		}
		if len(ri.Path) > pathW {
			pathW = len(ri.Path)
		}
	}

	fmt.Printf("\n   🪓 Registered routes:\n")
	fmt.Printf("   %-*s  %s\n", methodW, "METHOD", "PATH")
	fmt.Printf("   %-*s  %s\n", methodW, strings.Repeat("─", methodW), strings.Repeat("─", pathW))
	for _, ri := range routes {
		fmt.Printf("   %-*s  %s\n", methodW, ri.Method, ri.Path)
	}
	fmt.Println()
}

func renderHTML(method, path string, routes []RouteInfo) string {
	var heading string
	if method != "" && path != "" {
		heading = fmt.Sprintf(`<h2 style="color:#ef4444;margin:0 0 4px">Routing Error</h2>
		<p style="color:#a1a1aa;margin:0 0 24px;font-size:15px">No route matches <strong>[%s]</strong> "%s"</p>`, method, path)
	} else {
		heading = `<h2 style="color:#22d3ee;margin:0 0 24px">Registered Routes</h2>`
	}

	var rows strings.Builder
	for _, ri := range routes {
		color := methodColor(ri.Method)
		rows.WriteString(fmt.Sprintf(`<tr>
			<td style="padding:8px 16px;font-weight:700;color:%s;font-size:13px;letter-spacing:0.5px">%s</td>
			<td style="padding:8px 16px;color:#e4e4e7;font-family:'JetBrains Mono',monospace;font-size:14px">%s</td>
		</tr>`, color, ri.Method, ri.Path))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html><head>
<meta charset="utf-8">
<title>Routes</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700&family=JetBrains+Mono&display=swap');
  * { box-sizing: border-box; }
  body {
    font-family: 'Inter', system-ui, sans-serif;
    background: #09090b; color: #fafafa;
    margin: 0; padding: 40px;
    min-height: 100vh;
  }
  .card {
    max-width: 800px; margin: 0 auto;
    background: #18181b; border: 1px solid #27272a;
    border-radius: 12px; padding: 32px;
    box-shadow: 0 25px 50px -12px rgba(0,0,0,.5);
  }
  table { width: 100%%; border-collapse: collapse; }
  thead th {
    text-align: left; padding: 8px 16px;
    color: #71717a; font-size: 11px;
    text-transform: uppercase; letter-spacing: 1px;
    border-bottom: 1px solid #27272a;
  }
  tbody tr { border-bottom: 1px solid #1e1e22; }
  tbody tr:hover { background: #1f1f23; }
  .badge {
    display: inline-block; padding: 2px 8px;
    border-radius: 4px; font-size: 11px;
    background: #27272a; color: #a1a1aa;
    margin-top: 8px;
  }
</style>
</head><body>
<div class="card">
  %s
  <table>
    <thead><tr><th>Method</th><th>Path</th></tr></thead>
    <tbody>%s</tbody>
  </table>
  <div class="badge">🪓 axe • development mode</div>
</div>
</body></html>`, heading, rows.String())
}

func methodColor(method string) string {
	switch method {
	case "GET":
		return "#22d3ee"
	case "POST":
		return "#4ade80"
	case "PUT", "PATCH":
		return "#facc15"
	case "DELETE":
		return "#ef4444"
	default:
		return "#a1a1aa"
	}
}
