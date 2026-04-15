// Package docs exposes the embedded OpenAPI specification for axe.
// The spec is compiled into the binary at build time via go:embed.
package docs

import _ "embed"

// Spec is the raw OpenAPI 3.1 YAML specification.
// Served at GET /openapi.yaml and consumed by Swagger UI / ReDoc.
//
//go:embed openapi.yaml
var Spec []byte
