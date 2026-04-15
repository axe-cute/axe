// Package outbox implements the Transactional Outbox pattern for axe.
// The poller periodically reads unprocessed rows from outbox_events table
// and dispatches them as Asynq tasks.
//
// Architecture:
//   DB write → insert to outbox_events (same TX)
//   Poller   → read unprocessed → enqueue to Asynq → mark processed
//   Worker   → consume from Asynq → call downstream (webhook, email, etc.)
package outbox

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"

	"github.com/axe-go/axe/pkg/worker"
)

// Poller reads unprocessed outbox_events and dispatches them as Asynq tasks.
type Poller struct {
	db       *sql.DB
	client   *asynq.Client
	log      *slog.Logger
	interval time.Duration
	batchSize int
}

// Config holds poller settings.
type Config struct {
	Interval  time.Duration // polling interval, default 5s
	BatchSize int           // rows per poll, default 50
}

// New creates a new Poller.
func New(db *sql.DB, redisAddr string, cfg Config, log *slog.Logger) *Poller {
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 50
	}

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	return &Poller{
		db:        db,
		client:    client,
		log:       log,
		interval:  cfg.Interval,
		batchSize: cfg.BatchSize,
	}
}

// Start begins polling for outbox events. Blocks until ctx is cancelled.
func (p *Poller) Start(ctx context.Context) {
	p.log.Info("outbox poller starting", "interval", p.interval, "batch_size", p.batchSize)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	defer p.client.Close()

	for {
		select {
		case <-ctx.Done():
			p.log.Info("outbox poller stopping")
			return
		case <-ticker.C:
			if err := p.poll(ctx); err != nil {
				p.log.Error("outbox poll error", "error", err)
			}
		}
	}
}

// poll reads a batch of unprocessed events and enqueues them.
func (p *Poller) poll(ctx context.Context) error {
	// Select unprocessed events with a row-level lock to prevent double processing
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, event_type, aggregate
		FROM outbox_events
		WHERE processed_at IS NULL
		  AND retries < 5
		ORDER BY created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, p.batchSize)
	if err != nil {
		return err
	}
	defer rows.Close()

	var enqueued int
	for rows.Next() {
		var id, eventType, aggregate string
		if err := rows.Scan(&id, &eventType, &aggregate); err != nil {
			p.log.Error("scan outbox row", "error", err)
			continue
		}

		task, err := worker.NewOutboxEventTask(id, eventType, aggregate)
		if err != nil {
			p.log.Error("create outbox task", "event_id", id, "error", err)
			continue
		}

		if _, err := p.client.Enqueue(task, asynq.Queue("default")); err != nil {
			p.log.Error("enqueue outbox task", "event_id", id, "error", err)
			continue
		}

		// Mark as processed
		if _, err := p.db.ExecContext(ctx, `
			UPDATE outbox_events
			SET processed_at = NOW(), retries = retries + 1
			WHERE id = $1
		`, id); err != nil {
			p.log.Error("mark outbox processed", "event_id", id, "error", err)
			continue
		}

		enqueued++
	}

	if enqueued > 0 {
		p.log.Info("outbox poll", "enqueued", enqueued)
	}
	return rows.Err()
}
