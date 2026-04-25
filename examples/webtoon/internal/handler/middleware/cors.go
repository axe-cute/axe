package middleware

import (
	"net/http"
	"strings"
)

// CORS returns a middleware that adds permissive CORS headers for the given
// allowed origins. Use "*" to allow any origin (dev only; credentials disabled
// when wildcard is in effect).
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowAll := false
	allowSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		o = strings.TrimSpace(o)
		if o == "*" {
			allowAll = true
		}
		if o != "" {
			allowSet[o] = true
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := false
			switch {
			case allowAll && origin != "":
				w.Header().Set("Access-Control-Allow-Origin", origin)
				allowed = true
			case origin != "" && allowSet[origin]:
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				allowed = true
			}

			if allowed {
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
				w.Header().Set("Access-Control-Max-Age", "600")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
