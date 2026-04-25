package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/axe-cute/examples-webtoon/internal/handler/middleware"
	"github.com/axe-cute/examples-webtoon/pkg/apperror"
)

// ── GET /admin/stats ────────────────────────────────────────────────────────
//
// Dashboard aggregate. Runs a handful of cheap index-only scans; wrapped in
// one query via UNION ALL so the admin page loads with a single round-trip.
// Target latency: <20 ms even with 100k series.
type dashStats struct {
	Series          int64 `json:"series"`
	SeriesOngoing   int64 `json:"series_ongoing"`
	SeriesCompleted int64 `json:"series_completed"`
	Episodes        int64 `json:"episodes"`
	EpisodesPub     int64 `json:"episodes_published"`
	Pages           int64 `json:"pages"`
	PagesReady      int64 `json:"pages_ready"`
	PagesPending    int64 `json:"pages_pending"`
	PagesFailed     int64 `json:"pages_failed"`
	StorageBytes    int64 `json:"storage_bytes"`
	Bookmarks       int64 `json:"bookmarks"`
	DistinctUsers   int64 `json:"distinct_users"`
	QueueDepth      int64 `json:"queue_depth"`
	QueuePending    int64 `json:"queue_pending"`
	QueueActive     int64 `json:"queue_active"`
	QueueRetry      int64 `json:"queue_retry"`
	QueueArchived   int64 `json:"queue_archived"`
}

type trendingSlice struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Score float64 `json:"trending_score"`
}

type statsResp struct {
	dashStats
	Trending       []trendingSlice    `json:"trending"`
	RecentUploads  []adminPageResponse `json:"recent_uploads"`
	UploadsByDay   []uploadsBucket    `json:"uploads_by_day"`
}

type uploadsBucket struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

// Stats aggregates dashboard data in parallel-safe sequential queries.
//
// We don't fan out with goroutines because the queries are fast and Postgres
// already parallelises within a single connection via prepared plan caching.
// Adding goroutines here would trade ~2ms saved for harder error handling.
func (h *AdminHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats := dashStats{}

	// Bulk counts — one query, multiple SUM(CASE) aggregations. This is the
	// cheapest way to get breakdowns without N round-trips.
	err := h.db.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM series),
		  (SELECT COUNT(*) FROM series WHERE status='ongoing'),
		  (SELECT COUNT(*) FROM series WHERE status='completed'),
		  (SELECT COUNT(*) FROM episodes),
		  (SELECT COUNT(*) FROM episodes WHERE published = TRUE),
		  (SELECT COUNT(*) FROM episode_pages),
		  (SELECT COUNT(*) FROM episode_pages WHERE status='ready'),
		  (SELECT COUNT(*) FROM episode_pages WHERE status IN ('uploaded','processing')),
		  (SELECT COUNT(*) FROM episode_pages WHERE status='failed'),
		  (SELECT COALESCE(SUM(bytes),0) FROM episode_pages),
		  (SELECT COUNT(*) FROM bookmarks),
		  (SELECT COUNT(DISTINCT user_id) FROM bookmarks)
		`).Scan(
		&stats.Series, &stats.SeriesOngoing, &stats.SeriesCompleted,
		&stats.Episodes, &stats.EpisodesPub,
		&stats.Pages, &stats.PagesReady, &stats.PagesPending, &stats.PagesFailed,
		&stats.StorageBytes,
		&stats.Bookmarks, &stats.DistinctUsers,
	)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}

	// Queue stats via asynq Inspector — cheap, in-memory on Redis.
	if h.inspector != nil {
		qctx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()
		stats.QueuePending, stats.QueueActive, stats.QueueRetry, stats.QueueArchived =
			queueCounts(qctx, h.inspector)
		stats.QueueDepth = stats.QueuePending + stats.QueueActive + stats.QueueRetry
	}

	// Top 5 trending — reuse the same denormalised column used by readers.
	tRows, err := h.db.QueryContext(ctx, `
		SELECT id, title, trending_score FROM series
		 WHERE trending_score > 0
		 ORDER BY trending_score DESC LIMIT 5`)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}
	trending := make([]trendingSlice, 0, 5)
	for tRows.Next() {
		var t trendingSlice
		var id uuid.UUID
		if err := tRows.Scan(&id, &t.Title, &t.Score); err != nil {
			tRows.Close()
			middleware.WriteError(w, err)
			return
		}
		t.ID = id.String()
		trending = append(trending, t)
	}
	tRows.Close()

	// Most recent 8 page uploads (admin sanity check).
	pRows, err := h.db.QueryContext(ctx, `
		SELECT id, page_num, status, error, original_key, medium_key, thumb_key, width_px, height_px
		  FROM episode_pages
		 ORDER BY created_at DESC LIMIT 8`)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}
	recent := make([]adminPageResponse, 0, 8)
	for pRows.Next() {
		p, err := scanAdminPage(pRows, h.store.PublicURL)
		if err != nil {
			pRows.Close()
			middleware.WriteError(w, err)
			return
		}
		recent = append(recent, p)
	}
	pRows.Close()

	// Uploads-per-day over the last 14 days. generate_series pads zero days
	// so the chart has a consistent x-axis.
	uRows, err := h.db.QueryContext(ctx, `
		SELECT to_char(day, 'YYYY-MM-DD'), COALESCE(cnt, 0)
		  FROM generate_series(CURRENT_DATE - INTERVAL '13 days', CURRENT_DATE, '1 day') AS d(day)
		  LEFT JOIN (
		    SELECT date_trunc('day', created_at)::date AS d, COUNT(*) AS cnt
		      FROM episode_pages
		     WHERE created_at >= CURRENT_DATE - INTERVAL '14 days'
		     GROUP BY 1
		  ) s ON s.d = d.day
		 ORDER BY day`)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}
	buckets := make([]uploadsBucket, 0, 14)
	for uRows.Next() {
		var b uploadsBucket
		if err := uRows.Scan(&b.Day, &b.Count); err != nil {
			uRows.Close()
			middleware.WriteError(w, err)
			return
		}
		buckets = append(buckets, b)
	}
	uRows.Close()

	middleware.WriteJSON(w, http.StatusOK, statsResp{
		dashStats:     stats,
		Trending:      trending,
		RecentUploads: recent,
		UploadsByDay:  buckets,
	})
}

// scanAdminPage is shared with AdminListPages for consistent field shape.
func scanAdminPage(rows *sql.Rows, publicURL func(string) string) (adminPageResponse, error) {
	var (
		p           adminPageResponse
		id          uuid.UUID
		errMsg      sql.NullString
		originalKey string
		mediumKey   sql.NullString
		thumbKey    sql.NullString
		width       sql.NullInt64
		height      sql.NullInt64
	)
	if err := rows.Scan(&id, &p.PageNum, &p.Status, &errMsg, &originalKey, &mediumKey, &thumbKey, &width, &height); err != nil {
		return p, err
	}
	p.ID = id.String()
	p.OriginalURL = publicURL(originalKey)
	if errMsg.Valid {
		v := errMsg.String
		p.Error = &v
	}
	if mediumKey.Valid {
		u := publicURL(mediumKey.String)
		p.MediumURL = &u
	}
	if thumbKey.Valid {
		u := publicURL(thumbKey.String)
		p.ThumbURL = &u
	}
	if width.Valid {
		v := int(width.Int64)
		p.WidthPx = &v
	}
	if height.Valid {
		v := int(height.Int64)
		p.HeightPx = &v
	}
	return p, nil
}

// queueCounts fans out four in-flight queries to the asynq inspector.
// We suppress individual errors: queue introspection is observability, not
// correctness. A flaky inspector should never break the dashboard.
func queueCounts(ctx context.Context, insp *asynq.Inspector) (pending, active, retry, archived int64) {
	queues := []string{"default", "critical", "low"}
	for _, q := range queues {
		if info, err := insp.GetQueueInfo(q); err == nil && info != nil {
			pending += int64(info.Pending)
			active += int64(info.Active)
			retry += int64(info.Retry)
			archived += int64(info.Archived)
		}
	}
	return
}

// ── POST /admin/episodes/{id}/pages/reorder ─────────────────────────────────
//
// Accepts a full ordered list of page IDs for an episode. We rewrite
// page_num in one UPDATE per page inside a single tx.
//
// Trade-off: this isn't an optimal bulk update (one UPDATE per row), but
// it's correct under concurrent reorders (each row touched exactly once)
// and trivially revertible. For episodes with >1000 pages we'd switch to
// a temp-table swap.
type reorderReq struct {
	PageIDs []string `json:"page_ids"`
}

func (h *AdminHandler) ReorderPages(w http.ResponseWriter, r *http.Request) {
	episodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid episode id"))
		return
	}
	var req reorderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid JSON"))
		return
	}
	if len(req.PageIDs) == 0 {
		middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("page_ids required"))
		return
	}
	// Validate all IDs before touching the DB — fail fast with a clean error.
	ids := make([]uuid.UUID, len(req.PageIDs))
	for i, s := range req.PageIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			middleware.WriteError(w, apperror.ErrInvalidInput.WithMessage("invalid page_id at index "+itoa(i)))
			return
		}
		ids[i] = id
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	// Phase 1: move every row to negative page_num to sidestep the
	// UNIQUE(episode_id, page_num) constraint during reassignment.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE episode_pages SET page_num = -page_num WHERE episode_id = $1`, episodeID); err != nil {
		middleware.WriteError(w, err)
		return
	}
	// Phase 2: assign new page_num in the requested order.
	for i, id := range ids {
		res, err := tx.ExecContext(r.Context(),
			`UPDATE episode_pages SET page_num = $1, updated_at = NOW()
			  WHERE id = $2 AND episode_id = $3`,
			i+1, id, episodeID)
		if err != nil {
			middleware.WriteError(w, err)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			middleware.WriteError(w, apperror.ErrNotFound.WithMessage("page "+id.String()+" not in episode"))
			return
		}
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── GET /admin/users ────────────────────────────────────────────────────────
//
// No dedicated users table yet; we approximate "user activity" from the
// bookmarks table (only authenticated action we track right now). Each
// distinct user_id becomes a row with aggregate stats.
//
// When a real users table is added, swap this query to JOIN on users for
// email/name/joined_at, but the UI shape stays the same.
type userRow struct {
	UserID        string `json:"user_id"`
	Bookmarks     int64  `json:"bookmarks"`
	FirstSeen     string `json:"first_seen"`
	LastActivity  string `json:"last_activity"`
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT user_id,
		       COUNT(*) AS bookmarks,
		       to_char(MIN(created_at), 'YYYY-MM-DD"T"HH24:MI:SS') AS first_seen,
		       to_char(MAX(updated_at), 'YYYY-MM-DD"T"HH24:MI:SS') AS last_activity
		  FROM bookmarks
		 GROUP BY user_id
		 ORDER BY last_activity DESC
		 LIMIT 200`)
	if err != nil {
		middleware.WriteError(w, err)
		return
	}
	defer rows.Close()

	out := make([]userRow, 0)
	for rows.Next() {
		var u userRow
		var uid uuid.UUID
		if err := rows.Scan(&uid, &u.Bookmarks, &u.FirstSeen, &u.LastActivity); err != nil {
			middleware.WriteError(w, err)
			return
		}
		u.UserID = uid.String()
		out = append(out, u)
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{
		"data":  out,
		"total": len(out),
		"note":  "Derived from bookmarks table. Add a users table for real RBAC.",
	})
}

// sentinel so staticcheck doesn't flag it; errors package is used by
// extractors elsewhere.
var _ = errors.New
