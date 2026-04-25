// Package jobs contains periodic background work: trending recompute, view
// counter flush. Registered with the asynq scheduler from main.go.
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/axe-cute/examples-webtoon/pkg/views"
)

const (
	// TaskFlushTrending drains Redis view counters and updates series.trending_score.
	TaskFlushTrending = "webtoon:trending:flush"

	// decay is the exponential moving-average weight for old score.
	// new_score = decay*old_score + (1 - decay)*scaled(delta_views)
	// decay=0.85 means "last flush contributes ~15% to the score".
	decay = 0.85

	// viewScale normalises view deltas into the score domain.
	viewScale = 1.0
)

// FlushTrendingHandler returns an asynq handler that drains the views
// counter and updates trending_score on each series with new activity.
//
// Scheduled every ~30s in development (see schedule.go). In production,
// 1–5min is more appropriate.
func FlushTrendingHandler(db *sql.DB, counter *views.Counter, log *slog.Logger) asynq.HandlerFunc {
	return func(ctx context.Context, _ *asynq.Task) error {
		return FlushTrending(ctx, db, counter, log)
	}
}

// FlushTrending is the pure function behind the asynq handler. Exposed so
// tests and one-shot admin commands can invoke it directly.
func FlushTrending(ctx context.Context, db *sql.DB, counter *views.Counter, log *slog.Logger) error {
	deltas, err := counter.DrainSeries(ctx)
	if err != nil {
		return fmt.Errorf("drain: %w", err)
	}

	// Apply decay to every row so rankings cool naturally even if traffic
	// pauses. Single bounded UPDATE.
	if _, err := db.ExecContext(ctx,
		`UPDATE series SET trending_score = trending_score * $1`, decay); err != nil {
		return fmt.Errorf("decay: %w", err)
	}

	if len(deltas) == 0 {
		log.Debug("trending flush: no new views")
		return nil
	}

	// Bump scores for series with new activity in a single transaction.
	// For small N (hot series per flush window), a loop of prepared
	// UPDATEs is simpler than array-unnest and fast enough. For N > ~1k
	// switch to a temp-table JOIN or pgx-native array binding.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`UPDATE series SET trending_score = trending_score + $1 WHERE id = $2`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for id, n := range deltas {
		bump := (1 - decay) * viewScale * float64(n)
		if _, err := stmt.ExecContext(ctx, bump, id); err != nil {
			return fmt.Errorf("bump %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	log.Info("trending flush", "series_updated", len(deltas), "total_views", sumDeltas(deltas))
	return nil
}

// NewFlushTrendingTask creates an asynq task payload for the scheduler.
func NewFlushTrendingTask() *asynq.Task {
	payload, _ := json.Marshal(struct{}{})
	return asynq.NewTask(TaskFlushTrending, payload)
}

func sumDeltas(m map[uuid.UUID]int64) int64 {
	var s int64
	for _, v := range m {
		s += v
	}
	return s
}
