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

	"github.com/axe-cute/axe/pkg/worker"
)

// Poller reads unprocessed outbox_events and dispatches them as Asynq tasks.
type Poller struct {
	db        *sql.DB
	client    *asynq.Client
	log       *slog.Logger
	interval  time.Duration
	batchSize int
	driver    string // "postgres", "mysql", "sqlite3"
}

// Config holds poller settings.
type Config struct {
	Interval  time.Duration // polling interval, default 5s
	BatchSize int           // rows per poll, default 50
	// Driver is the database driver name: "postgres", "mysql", or "sqlite3".
	// Used to select the correct SQL placeholder and locking strategy.
	// Defaults to "postgres".
	Driver string
}

// New creates a new Poller.
func New(db *sql.DB, redisAddr string, cfg Config, log *slog.Logger) *Poller {
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 50
	}
	if cfg.Driver == "" {
		cfg.Driver = "postgres"
	}

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	return &Poller{
		db:        db,
		client:    client,
		log:       log,
		interval:  cfg.Interval,
		batchSize: cfg.BatchSize,
		driver:    cfg.Driver,
	}
}

// placeholder returns the correct positional placeholder for the driver.
// PostgreSQL uses $N; MySQL and SQLite use ?.
func (p *Poller) placeholder() string {
	if p.driver == "postgres" {
		return "$1"
	}
	return "?"
}

// lockClause returns the row-level lock hint for the driver.
// FOR UPDATE SKIP LOCKED is supported by PostgreSQL and MySQL 8+.
// SQLite uses file-level locking — this clause is omitted.
func (p *Poller) lockClause() string {
	switch p.driver {
	case "postgres", "mysql":
		return "FOR UPDATE SKIP LOCKED"
	default:
		return "" // sqlite3
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
	// Select unprocessed events.
	// Use driver-specific placeholder and lock clause.
	lockSQL := p.lockClause()
	if lockSQL != "" {
		lockSQL = "\n\t\t" + lockSQL
	}
	query := `
		SELECT id, event_type, aggregate
		FROM outbox_events
		WHERE processed_at IS NULL
		  AND retries < 5
		ORDER BY created_at ASC
		LIMIT ` + p.placeholder() + lockSQL + `
	`
	rows, err := p.db.QueryContext(ctx, query, p.batchSize)
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
		updateQuery := `UPDATE outbox_events SET processed_at = NOW(), retries = retries + 1 WHERE id = ` + p.placeholder()
		if _, err := p.db.ExecContext(ctx, updateQuery, id); err != nil {
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
