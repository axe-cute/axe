package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/axe-cute/examples-webtoon/internal/handler/middleware"
	"github.com/axe-cute/examples-webtoon/internal/jobs"
	"github.com/axe-cute/examples-webtoon/pkg/apperror"
	"github.com/axe-cute/examples-webtoon/pkg/jwtauth"
	"github.com/axe-cute/examples-webtoon/pkg/storage"
)

// AdminHandler exposes editorial endpoints gated by role=admin.
//
// Flow for uploading an episode:
//  1. Admin POSTs /admin/uploads/presign with {episode_id, filename, content_type, size}
//     → server returns {put_url, key, page_num}
//  2. Browser PUTs the file directly to `put_url` (bytes bypass our API entirely)
//  3. Admin POSTs /admin/episodes/{id}/pages with {keys: [...]} to register
//     → server enqueues one transform task per page; status becomes 'uploaded'
//  4. asynq worker (jobs.TransformPage) reads original, writes variants,
//     flips status to 'ready'. GET /episodes/{id}/pages starts returning it.
type AdminHandler struct {
	db        *sql.DB
	store     *storage.Client
	asynq     *asynq.Client
	inspector *asynq.Inspector // optional: powers queue widget on dashboard
	audit     *AuditLogger     // optional: records mutations
	jwtSvc    *jwtauth.Service
	blocklist middleware.Blocklist
	log       *slog.Logger
}

// NewAdminHandler wires the admin surface. The jwtSvc/blocklist are used
// by JWTAuth middleware mounted on Routes().
func NewAdminHandler(
	db *sql.DB,
	store *storage.Client,
	asynqClient *asynq.Client,
	jwtSvc *jwtauth.Service,
	bl middleware.Blocklist,
	log *slog.Logger,
) *AdminHandler {
	return &AdminHandler{
		db:        db,
		store:     store,
		asynq:     asynqClient,
		jwtSvc:    jwtSvc,
		blocklist: bl,
		log:       log,
	}
}

// WithInspector enables queue stats on the dashboard. Pass nil to disable.
func (h *AdminHandler) WithInspector(i *asynq.Inspector) *AdminHandler {
	h.inspector = i
	return h
}

// WithAudit wires the audit middleware into admin routes.
func (h *AdminHandler) WithAudit(a *AuditLogger) *AdminHandler {
	h.audit = a
	return h
}

// Routes returns a chi router with all /admin endpoints, pre-wrapped in
// JWT + RequireRole("admin") + audit middleware.
func (h *AdminHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.JWTAuth(h.jwtSvc, h.blocklist))
	r.Use(middleware.RequireRole("admin"))
	if h.audit != nil {
		r.Use(h.audit.Middleware())
	}

	// Dashboard
	r.Get("/stats", h.Stats)

	// Uploads
	r.Post("/uploads/presign", h.Presign)

	// Episode pages
	r.Post("/episodes/{id}/pages", h.RegisterPages)
	r.Get("/episodes/{id}/pages", h.AdminListPages)
	r.Post("/episodes/{id}/pages/reorder", h.ReorderPages)
	r.Delete("/pages/{page_id}", h.DeletePage)

	// Users (read-only)
	r.Get("/users", h.ListUsers)

	// Audit trail
	r.Get("/audit", h.ListAudit)
	return r
}

// ── Presign upload ──────────────────────────────────────────────────────────

type presignRequest struct {
	EpisodeID   string `json:"episode_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type presignResponse struct {
	PutURL      string `json:"put_url"`
	Key         string `json:"key"`
	ContentType string `json:"content_type"`
	ExpiresIn   int    `json:"expires_in"` // seconds
}

// allowedImageTypes binds the uploadable MIME set. Extend cautiously —
// every new format means a new decoder in jobs/transform.go.
var allowedImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

const (
	presignTTL     = 10 * time.Minute
	maxUploadBytes = 20 * 1024 * 1024 // 20 MB per page
)

// Presign issues a time-limited PUT URL so the browser can upload directly
// to object storage. We bind content_type into the signature so the
// browser's PUT must match — prevents upload of arbitrary types.
func (h *AdminHandler) Presign(w http.ResponseWriter, r *http.Request) {
	var req presignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid JSON body"))
		return
	}

	episodeID, err := uuid.Parse(req.EpisodeID)
	if err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid episode_id"))
		return
	}
	ext, ok := allowedImageTypes[req.ContentType]
	if !ok {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("content_type must be image/jpeg, image/png, or image/webp"))
		return
	}
	if req.Size <= 0 || req.Size > maxUploadBytes {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage(fmt.Sprintf("size must be 1..%d bytes", maxUploadBytes)))
		return
	}

	// Confirm the episode exists — better to 404 here than after the
	// browser has already uploaded bytes.
	var exists bool
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM episodes WHERE id = $1)`, episodeID,
	).Scan(&exists); err != nil {
		middleware.WriteError(w, fmt.Errorf("check episode: %w", err))
		return
	}
	if !exists {
		middleware.WriteError(w, apperror.ErrNotFound.WithMessage("episode not found"))
		return
	}

	// Per-upload UUID in the key prevents collisions when the same filename
	// is uploaded twice and allows safe retries.
	key := path.Join("originals", episodeID.String(), uuid.NewString()+ext)

	putURL, err := h.store.PresignPut(r.Context(), key, req.ContentType, presignTTL)
	if err != nil {
		middleware.WriteError(w, fmt.Errorf("presign: %w", err))
		return
	}

	middleware.WriteJSON(w, http.StatusOK, presignResponse{
		PutURL:      putURL,
		Key:         key,
		ContentType: req.ContentType,
		ExpiresIn:   int(presignTTL.Seconds()),
	})
}

// ── Register uploaded pages ─────────────────────────────────────────────────

type registerPagesRequest struct {
	Pages []struct {
		Key         string `json:"key"`
		ContentType string `json:"content_type"`
		Bytes       int64  `json:"bytes"`
	} `json:"pages"`
}

type pageRow struct {
	ID       string `json:"id"`
	PageNum  int    `json:"page_num"`
	Status   string `json:"status"`
	Key      string `json:"original_key"`
}

// RegisterPages creates episode_pages rows for each uploaded key and
// enqueues a transform job per page. Pages are numbered in the order
// they arrive in the request body — admin UI submits them pre-sorted.
//
// Transaction semantics: we insert the whole batch in one tx so the
// episode either gets all its pages or none (partial uploads are
// frustrating to fix in the admin UI).
func (h *AdminHandler) RegisterPages(w http.ResponseWriter, r *http.Request) {
	episodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid episode id"))
		return
	}

	var req registerPagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid JSON body"))
		return
	}
	if len(req.Pages) == 0 {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("no pages provided"))
		return
	}

	// Find the next page_num for this episode so we can append without
	// conflicting with existing pages. This is the simple, correct
	// approach; for a fancier admin UI you'd allow explicit reordering.
	var startAt int
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(page_num), 0) FROM episode_pages WHERE episode_id = $1`,
		episodeID,
	).Scan(&startAt); err != nil {
		middleware.WriteError(w, fmt.Errorf("max page_num: %w", err))
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, fmt.Errorf("begin tx: %w", err))
		return
	}
	defer func() { _ = tx.Rollback() }()

	inserted := make([]pageRow, 0, len(req.Pages))
	for i, p := range req.Pages {
		pageNum := startAt + i + 1
		id := uuid.New()
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO episode_pages (id, episode_id, page_num, original_key, content_type, bytes, status)
			VALUES ($1, $2, $3, $4, $5, $6, 'uploaded')`,
			id, episodeID, pageNum, p.Key, p.ContentType, p.Bytes,
		); err != nil {
			middleware.WriteError(w, fmt.Errorf("insert page %d: %w", pageNum, err))
			return
		}
		inserted = append(inserted, pageRow{
			ID:      id.String(),
			PageNum: pageNum,
			Status:  "uploaded",
			Key:     p.Key,
		})
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, fmt.Errorf("commit: %w", err))
		return
	}

	// Enqueue transform tasks *after* commit. If asynq is down we still
	// keep the rows (status='uploaded') and the admin UI shows a retry
	// button. Better UX than rolling back on queue failure.
	enqueued, failed := h.enqueueTransforms(r.Context(), inserted)

	middleware.WriteJSON(w, http.StatusCreated, map[string]any{
		"data":           inserted,
		"enqueued":       enqueued,
		"enqueue_failed": failed,
	})
}

func (h *AdminHandler) enqueueTransforms(ctx context.Context, rows []pageRow) (ok int, failed int) {
	for _, p := range rows {
		id, _ := uuid.Parse(p.ID)
		task, err := jobs.NewTransformPageTask(id)
		if err != nil {
			failed++
			continue
		}
		if _, err := h.asynq.EnqueueContext(ctx, task); err != nil {
			h.log.Warn("enqueue transform failed", "page_id", id, "error", err)
			failed++
			continue
		}
		ok++
	}
	return
}

// ── Admin: list pages (all statuses) ────────────────────────────────────────

type adminPageResponse struct {
	ID         string  `json:"id"`
	PageNum    int     `json:"page_num"`
	Status     string  `json:"status"`
	Error      *string `json:"error,omitempty"`
	OriginalURL string `json:"original_url"`
	MediumURL  *string `json:"medium_url,omitempty"`
	ThumbURL   *string `json:"thumb_url,omitempty"`
	WidthPx    *int    `json:"width_px,omitempty"`
	HeightPx   *int    `json:"height_px,omitempty"`
}

// AdminListPages returns all pages including failed/processing — used by
// the admin dashboard. The public endpoint (handler.EpisodeHandler) only
// returns status='ready'.
func (h *AdminHandler) AdminListPages(w http.ResponseWriter, r *http.Request) {
	episodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid episode id"))
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, page_num, status, error, original_key, medium_key, thumb_key, width_px, height_px
		  FROM episode_pages
		 WHERE episode_id = $1
		 ORDER BY page_num ASC`, episodeID)
	if err != nil {
		middleware.WriteError(w, fmt.Errorf("list pages: %w", err))
		return
	}
	defer rows.Close()

	out := make([]adminPageResponse, 0)
	for rows.Next() {
		var (
			id          uuid.UUID
			pageNum     int
			status      string
			errMsg      sql.NullString
			originalKey string
			mediumKey   sql.NullString
			thumbKey    sql.NullString
			width       sql.NullInt64
			height      sql.NullInt64
		)
		if err := rows.Scan(&id, &pageNum, &status, &errMsg, &originalKey, &mediumKey, &thumbKey, &width, &height); err != nil {
			middleware.WriteError(w, fmt.Errorf("scan page: %w", err))
			return
		}
		resp := adminPageResponse{
			ID:          id.String(),
			PageNum:     pageNum,
			Status:      status,
			OriginalURL: h.store.PublicURL(originalKey),
		}
		if errMsg.Valid {
			v := errMsg.String
			resp.Error = &v
		}
		if mediumKey.Valid {
			u := h.store.PublicURL(mediumKey.String)
			resp.MediumURL = &u
		}
		if thumbKey.Valid {
			u := h.store.PublicURL(thumbKey.String)
			resp.ThumbURL = &u
		}
		if width.Valid {
			v := int(width.Int64)
			resp.WidthPx = &v
		}
		if height.Valid {
			v := int(height.Int64)
			resp.HeightPx = &v
		}
		out = append(out, resp)
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"data": out, "total": len(out)})
}

// ── Admin: delete page ──────────────────────────────────────────────────────

// DeletePage removes a page row and best-effort removes all its storage
// keys (original + variants). Storage deletes are best-effort because
// object stores are eventually consistent and the row cleanup is what
// matters for correctness.
func (h *AdminHandler) DeletePage(w http.ResponseWriter, r *http.Request) {
	pageID, err := uuid.Parse(chi.URLParam(r, "page_id"))
	if err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid page id"))
		return
	}

	var originalKey, mediumKey, thumbKey sql.NullString
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT original_key, medium_key, thumb_key FROM episode_pages WHERE id = $1`,
		pageID,
	).Scan(&originalKey, &mediumKey, &thumbKey); err != nil {
		if err == sql.ErrNoRows {
			middleware.WriteError(w, apperror.ErrNotFound.WithMessage("page not found"))
			return
		}
		middleware.WriteError(w, fmt.Errorf("load page: %w", err))
		return
	}

	if _, err := h.db.ExecContext(r.Context(),
		`DELETE FROM episode_pages WHERE id = $1`, pageID); err != nil {
		middleware.WriteError(w, fmt.Errorf("delete page: %w", err))
		return
	}

	// Best-effort storage cleanup; failures are logged but don't block
	// the 204. The row is gone, so orphaned objects cost storage fees
	// only — a nightly reconcile job would sweep them.
	for _, k := range []sql.NullString{originalKey, mediumKey, thumbKey} {
		if k.Valid && k.String != "" {
			if err := h.store.Delete(r.Context(), k.String); err != nil {
				h.log.Warn("orphan object (manual cleanup)", "key", k.String, "error", err)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
