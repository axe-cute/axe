package jobs

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"

	"github.com/axe-cute/examples-webtoon/pkg/views"
	"github.com/axe-cute/examples-webtoon/pkg/worker"
)

// Config controls the periodic jobs.
type Config struct {
	// TrendingFlushInterval is how often FlushTrending runs. Demo default 30s;
	// production 1–5min is saner (less DB churn).
	TrendingFlushInterval time.Duration

	// RedisAddr is used to construct an asynq client for enqueuing.
	RedisAddr string
}

// Register wires the trending-flush handler into the asynq worker mux and
// starts a background ticker that enqueues the task periodically.
//
// The returned stop func unregisters the ticker and closes the enqueue client.
// Call it from the shutdown path.
func Register(
	ctx context.Context,
	cfg Config,
	w *worker.Server,
	db *sql.DB,
	counter *views.Counter,
	log *slog.Logger,
) (stop func(), err error) {
	w.Register(TaskFlushTrending, FlushTrendingHandler(db, counter, log))

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.RedisAddr})

	interval := cfg.TrendingFlushInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		defer ticker.Stop()
		log.Info("trending scheduler started", "interval", interval)
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := client.Enqueue(
					NewFlushTrendingTask(),
					asynq.Queue("low"),
					asynq.MaxRetry(2),
					asynq.Timeout(30*time.Second),
				); err != nil {
					log.Warn("enqueue trending flush", "error", err)
				}
			}
		}
	}()

	return func() {
		close(done)
		_ = client.Close()
	}, nil
}
