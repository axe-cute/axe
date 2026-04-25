package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/axe-cute/examples-webtoon/internal/handler/middleware"
)

// AuditLogger records every admin mutation for compliance + replay.
//
// We wrap the admin chi.Router so every POST/PUT/PATCH/DELETE with a
// 2xx/3xx response is persisted. GETs are ignored (reads aren't mutations
// worth auditing, and the table would balloon).
//
// Design trade-offs:
//   - We buffer the request body to extract subject IDs from paths/bodies.
//     For typical admin payloads (<100 KB) this is fine; uploads bypass
//     the API entirely (presigned PUT) so bodies here are small.
//   - Writes go through a goroutine to avoid blocking the response.
//     If the DB is down, we log and drop — audit completeness is nice-to-
//     have, not critical to request correctness.
type AuditLogger struct {
	db  *sql.DB
	log *slog.Logger
}

// NewAuditLogger constructs the middleware factory.
func NewAuditLogger(db *sql.DB, log *slog.Logger) *AuditLogger {
	return &AuditLogger{db: db, log: log}
}

// Middleware returns a chi middleware that records non-GET requests. It
// short-circuits if the caller isn't an admin — we only care about admin
// mutations for compliance. Regular users creating bookmarks, etc., aren't
// audited (those are normal product activity, not privileged actions).
func (a *AuditLogger) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Fast path: GETs never audit.
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			// Only log privileged mutations. Skip entirely for non-admin
			// (or unauthenticated) calls so the table stays signal-dense.
			claims := middleware.ClaimsFromCtx(r.Context())
			if claims == nil || claims.Role != "admin" {
				next.ServeHTTP(w, r)
				return
			}

			// Capture small request body for subject extraction. We cap at 1MB
			// so malformed huge payloads can't exhaust memory.
			var bodyBytes []byte
			if r.Body != nil && r.ContentLength > 0 && r.ContentLength < 1_000_000 {
				bodyBytes, _ = io.ReadAll(io.LimitReader(r.Body, 1_000_000))
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}

			// Wrap writer to capture status + response preview. We tee
			// the first 2KB of the response so the audit can pull the newly
			// created resource ID out of POST bodies, where it wouldn't
			// appear in the URL or request payload.
			ww := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(ww, r)

			// Fire-and-forget insert. Don't block the response on audit I/O.
			go a.record(r, ww.status, bodyBytes, ww.buf.Bytes())
		})
	}
}

func (a *AuditLogger) record(r *http.Request, status int, body, respBody []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	claims := middleware.ClaimsFromCtx(r.Context())
	var actorID *uuid.UUID
	if claims != nil {
		if id, err := uuid.Parse(claims.UserID); err == nil {
			actorID = &id
		}
		// Claims.Subject is the user_id (per jwtauth.GenerateTokenPair).
		// We don't carry email in the JWT today; when a real users table
		// lands, enrich actor_email via a cheap lookup on actor_id.
	}
	actorEmail := "" // reserved for future enrichment

	action := actionFromPath(r.Method, r.URL.Path)
	subjectType, subjectID := extractSubject(r.URL.Path, body)
	// On create (POST with 2xx), the new resource's ID lives only in the
	// response body. Sniff it so the audit row has a usable subject_id.
	if subjectID == nil && r.Method == http.MethodPost && status >= 200 && status < 300 && len(respBody) > 0 {
		if st, id := subjectFromResponse(respBody, r.URL.Path); id != nil {
			subjectID = id
			if subjectType == "" {
				subjectType = st
			}
		}
	}

	// metadata: request path + query + body preview (truncated)
	md := map[string]any{
		"method":      r.Method,
		"path":        r.URL.Path,
		"query":       r.URL.RawQuery,
	}
	if len(body) > 0 && len(body) < 2000 {
		// Store a JSON preview if the body is valid JSON; otherwise skip.
		var preview map[string]any
		if err := json.Unmarshal(body, &preview); err == nil {
			// Remove obvious PII / huge fields.
			delete(preview, "password")
			delete(preview, "pages") // page arrays are long + duplicative of subject_id
			md["body"] = preview
		}
	}
	mdJSON, _ := json.Marshal(md)

	var ip *string
	if host := clientIP(r); host != "" {
		ip = &host
	}

	_, err := a.db.ExecContext(ctx, `
		INSERT INTO admin_actions
		  (actor_id, actor_email, action, subject_type, subject_id, status, metadata, ip)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		actorID, nullString(actorEmail), action,
		nullString(subjectType), subjectID, status, mdJSON, ip,
	)
	if err != nil {
		a.log.Warn("audit insert failed", "error", err, "action", action)
	}
}

// pluralToSingular handles English plurals we actually have. English
// pluralisation is ambiguous in general; we hardcode the resources this
// app exposes so "serieses" → "series" works correctly.
var pluralToSingular = map[string]string{
	"serieses":  "series",
	"series":    "series", // both forms accepted
	"episodes":  "episode",
	"bookmarks": "bookmark",
	"users":     "user",
	"pages":     "page",
}

func singularize(s string) string {
	if v, ok := pluralToSingular[s]; ok {
		return v
	}
	return strings.TrimSuffix(s, "s")
}

// actionFromPath produces a stable verb.subject label from the method + URL.
// Examples:
//
//	POST   /api/v1/serieses/          → series.create
//	PUT    /api/v1/serieses/{id}      → series.update
//	DELETE /api/v1/serieses/{id}      → series.delete
//	POST   /api/v1/admin/uploads/presign → uploads.presign
//	POST   /api/v1/admin/episodes/{id}/pages          → page.create
//	POST   /api/v1/admin/episodes/{id}/pages/reorder  → page.reorder
func actionFromPath(method, path string) string {
	p := strings.TrimPrefix(path, "/api/v1")
	p = strings.TrimPrefix(p, "/admin")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) == 0 || parts[0] == "" {
		return strings.ToLower(method)
	}
	verb := map[string]string{
		http.MethodPost:   "create",
		http.MethodPut:    "update",
		http.MethodPatch:  "patch",
		http.MethodDelete: "delete",
	}[method]
	if verb == "" {
		verb = strings.ToLower(method)
	}
	// Detect the "deepest" resource name in the path for nested routes:
	// /episodes/{id}/pages                 → subject = pages
	// /episodes/{id}/pages/reorder         → subject = pages, verb = reorder
	// /episodes/{id}/pages/{pageID}        → subject = pages
	// /serieses/{id}                       → subject = serieses
	var subject string
	var trailing string // trailing literal segment (e.g. "reorder", "presign")
	for i, seg := range parts {
		if isUUIDLike(seg) {
			continue
		}
		// if previous segment is a subject and this is a literal, it's a
		// trailing action verb (not another subject).
		if subject != "" && i > 0 && !isUUIDLike(parts[i-1]) {
			trailing = seg
			break
		}
		subject = seg
	}
	if subject == "" {
		return strings.ToLower(method)
	}
	sub := singularize(subject)
	if trailing != "" {
		return sub + "." + trailing
	}
	return sub + "." + verb
}

// isUUIDLike reports whether a path segment looks like a UUID (cheap check;
// we don't need full parse here).
func isUUIDLike(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

// extractSubject looks for an entity id in the URL path segments, then
// falls back to well-known fields in the request body.
//
// Path scan walks in order so the *first* (resource, id) pair wins:
// /serieses/{sid}/episodes/{eid}  → (series, sid) — the outer resource
// is the subject of an admin action.
func extractSubject(path string, body []byte) (subjectType string, subjectID *uuid.UUID) {
	p := strings.TrimPrefix(path, "/api/v1")
	p = strings.TrimPrefix(p, "/admin")
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")

	for i := 0; i+1 < len(parts); i++ {
		if id, err := uuid.Parse(parts[i+1]); err == nil {
			return singularize(parts[i]), &id
		}
	}

	// fallback: body has "episode_id" or similar
	if len(body) > 0 {
		var m map[string]any
		if err := json.Unmarshal(body, &m); err == nil {
			for _, k := range []string{"episode_id", "series_id", "page_id", "id"} {
				if v, ok := m[k].(string); ok {
					if id, err := uuid.Parse(v); err == nil {
						return strings.TrimSuffix(k, "_id"), &id
					}
				}
			}
		}
	}
	return "", nil
}

// statusWriter captures the status code + a bounded preview of the
// response body. Caps at 2KB so large responses (page lists, search) don't
// balloon goroutine memory — creates return small JSON anyway.
type statusWriter struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.buf.Len() < 2048 {
		// Cap copy to remaining buffer capacity.
		remaining := 2048 - w.buf.Len()
		if len(p) <= remaining {
			w.buf.Write(p)
		} else {
			w.buf.Write(p[:remaining])
		}
	}
	return w.ResponseWriter.Write(p)
}

// subjectFromResponse extracts (resource_type, id) from a JSON response.
// Used on POST 2xx when the URL doesn't contain the new ID yet.
func subjectFromResponse(body []byte, path string) (string, *uuid.UUID) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return "", nil
	}
	idStr, ok := m["id"].(string)
	if !ok {
		return "", nil
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return "", nil
	}
	// Resource type comes from the path: /serieses/ → series, /episodes/ → episode.
	p := strings.TrimPrefix(path, "/api/v1")
	p = strings.TrimPrefix(p, "/admin")
	p = strings.TrimPrefix(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) > 0 {
		return singularize(parts[0]), &id
	}
	return "", &id
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.Index(fwd, ","); i > 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ── Audit query endpoint ────────────────────────────────────────────────────

type auditRow struct {
	ID          string          `json:"id"`
	ActorID     *string         `json:"actor_id,omitempty"`
	ActorEmail  *string         `json:"actor_email,omitempty"`
	Action      string          `json:"action"`
	SubjectType *string         `json:"subject_type,omitempty"`
	SubjectID   *string         `json:"subject_id,omitempty"`
	Status      int             `json:"status"`
	Metadata    json.RawMessage `json:"metadata"`
	IP          *string         `json:"ip,omitempty"`
	CreatedAt   string          `json:"created_at"`
}

// ListAudit returns recent admin_actions with simple filters.
//
// Query params:
//
//	actor=<uuid>    — only this actor
//	subject=<uuid>  — only this subject
//	action=<prefix> — LIKE 'prefix%'
//	limit=<n>       — default 50, max 200
//	before=<rfc3339> — cursor for "load more"
func (h *AdminHandler) ListAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := parseIntDefault(q.Get("limit"), 50)
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	args := []any{}
	conds := []string{}
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, strings.Replace(cond, "?", "$"+itoa(len(args)), 1))
	}

	if v := q.Get("actor"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			add("actor_id = ?", id)
		}
	}
	if v := q.Get("subject"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			add("subject_id = ?", id)
		}
	}
	if v := q.Get("action"); v != "" {
		add("action LIKE ?", v+"%")
	}
	if v := q.Get("before"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("created_at < ?", t)
		}
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, limit)

	sqlStr := `
		SELECT id, actor_id, actor_email, action, subject_type, subject_id,
		       status, metadata, host(ip)::text, created_at
		  FROM admin_actions ` + where + `
		 ORDER BY created_at DESC
		 LIMIT $` + itoa(len(args))

	rows, err := h.db.QueryContext(r.Context(), sqlStr, args...)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}
	defer rows.Close()

	out := make([]auditRow, 0, limit)
	for rows.Next() {
		var (
			id            uuid.UUID
			actorID       *uuid.UUID
			actorEmail    sql.NullString
			action        string
			subjectType   sql.NullString
			subjectID     *uuid.UUID
			status        int
			md            []byte
			ipStr         sql.NullString
			createdAt     time.Time
		)
		if err := rows.Scan(&id, &actorID, &actorEmail, &action, &subjectType, &subjectID, &status, &md, &ipStr, &createdAt); err != nil {
			middleware.WriteError(w, err)
			return
		}
		row := auditRow{
			ID:        id.String(),
			Action:    action,
			Status:    status,
			Metadata:  md,
			CreatedAt: createdAt.Format(time.RFC3339),
		}
		if actorID != nil {
			s := actorID.String()
			row.ActorID = &s
		}
		if actorEmail.Valid {
			row.ActorEmail = &actorEmail.String
		}
		if subjectType.Valid {
			row.SubjectType = &subjectType.String
		}
		if subjectID != nil {
			s := subjectID.String()
			row.SubjectID = &s
		}
		if ipStr.Valid {
			row.IP = &ipStr.String
		}
		out = append(out, row)
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"data": out, "total": len(out), "limit": limit})
}

// ── tiny helpers (avoid dragging in strconv.Itoa where locality matters) ───

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}


