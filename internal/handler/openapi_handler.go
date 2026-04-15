// Package handler — openapi_handler.go
// Serves the OpenAPI 3.1 spec and Swagger UI for interactive documentation.
//
// Routes (register in main.go):
//
//	GET /openapi.yaml  → raw OpenAPI 3.1 YAML spec
//	GET /docs          → Swagger UI (CDN-hosted)
//	GET /docs/redoc    → ReDoc (lightweight alternative)
package handler

import (
	"net/http"

	axedocs "github.com/axe-cute/axe/docs"
)

// OpenAPIHandler serves API documentation.
type OpenAPIHandler struct{}

// NewOpenAPIHandler creates a new OpenAPIHandler.
func NewOpenAPIHandler() *OpenAPIHandler {
	return &OpenAPIHandler{}
}

// Spec serves the raw OpenAPI 3.1 YAML.
// GET /openapi.yaml
func (h *OpenAPIHandler) Spec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(axedocs.Spec)
}

// SwaggerUI serves the Swagger UI HTML page (CDN-hosted assets).
// GET /docs
func (h *OpenAPIHandler) SwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerUIHTML))
}

// Redoc serves the ReDoc UI (lightweight alternative to Swagger UI).
// GET /docs/redoc
func (h *OpenAPIHandler) Redoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(redocHTML))
}

// ── HTML templates ────────────────────────────────────────────────────────────

const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>axe API — Swagger UI</title>
  <meta name="description" content="axe REST API interactive documentation">
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    * { box-sizing: border-box; }
    body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; }
    #swagger-ui .topbar { background: #1a1a2e; }
    #swagger-ui .topbar-wrapper img { display: none; }
    #swagger-ui .topbar-wrapper::before {
      content: '⚡ axe API';
      color: #fff;
      font-size: 1.2rem;
      font-weight: 700;
      letter-spacing: 0.05em;
    }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: '/openapi.yaml',
      dom_id: '#swagger-ui',
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: 'BaseLayout',
      deepLinking: true,
      displayRequestDuration: true,
      persistAuthorization: true,
      tryItOutEnabled: true,
    });
  </script>
</body>
</html>`

const redocHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>axe API — ReDoc</title>
  <meta name="description" content="axe REST API documentation">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700&display=swap" rel="stylesheet">
  <style>
    body { margin: 0; font-family: 'Inter', sans-serif; }
  </style>
</head>
<body>
  <redoc spec-url="/openapi.yaml" hide-download-button expand-responses="200,201"></redoc>
  <script src="https://cdn.jsdelivr.net/npm/redoc@latest/bundles/redoc.standalone.js"></script>
</body>
</html>`
