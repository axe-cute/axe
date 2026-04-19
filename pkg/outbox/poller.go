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

// Enqueuer abstracts task enqueueing so the poller can be tested without Redis.
// *asynq.Client satisfies this interface.
type Enqueuer interface {
	Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

// Poller reads unprocessed outbox_events and dispatches them as Asynq tasks.
type Poller struct {
	db        *sql.DB
	enqueuer  Enqueuer
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

// New creates a new Poller with the given Enqueuer (typically an *asynq.Client).
// Use [NewWithRedis] for the common case of connecting to Redis directly.
func New(db *sql.DB, enqueuer Enqueuer, cfg Config, log *slog.Logger) *Poller {
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 50
	}
	if cfg.Driver == "" {
		cfg.Driver = "postgres"
	}

	return &Poller{
		db:        db,
		enqueuer:  enqueuer,
		log:       log,
		interval:  cfg.Interval,
		batchSize: cfg.BatchSize,
		driver:    cfg.Driver,
	}
}

// NewWithRedis creates a Poller that connects to Redis for task enqueueing.
// This is the most common constructor — equivalent to New(db, asynq.NewClient(...), cfg, log).
func NewWithRedis(db *sql.DB, redisAddr string, cfg Config, log *slog.Logger) *Poller {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	return New(db, client, cfg, log)
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
	defer p.enqueuer.Close()

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

	// Collect all rows first, then close cursor before writing.
	// This avoids table-lock contention (critical for SQLite, good practice for all drivers).
	type event struct{ id, eventType, aggregate string }
	var events []event
	for rows.Next() {
		var e event
		if err := rows.Scan(&e.id, &e.eventType, &e.aggregate); err != nil {
			p.log.Error("scan outbox row", "error", err)
			continue
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	// Now process collected events — cursor is closed, no lock contention.
	var enqueued int
	for _, e := range events {
		task, err := worker.NewOutboxEventTask(e.id, e.eventType, e.aggregate)
		if err != nil {
			p.log.Error("create outbox task", "event_id", e.id, "error", err)
			continue
		}

		if _, err := p.enqueuer.Enqueue(task, asynq.Queue("default")); err != nil {
			p.log.Error("enqueue outbox task", "event_id", e.id, "error", err)
			continue
		}

		// Mark as processed — use driver-specific timestamp function.
		// Use a short independent context so in-flight marks complete even if
		// the parent poll context is cancelled (prevents duplicate delivery).
		nowFn := "NOW()"
		if p.driver == "sqlite3" {
			nowFn = "CURRENT_TIMESTAMP"
		}
		updateQuery := `UPDATE outbox_events SET processed_at = ` + nowFn + `, retries = retries + 1 WHERE id = ` + p.placeholder()
		markCtx, markCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if _, err := p.db.ExecContext(markCtx, updateQuery, e.id); err != nil {
			p.log.Error("mark outbox processed", "event_id", e.id, "error", err)
			markCancel()
			continue
		}
		markCancel()

		enqueued++
	}

	if enqueued > 0 {
		p.log.Info("outbox poll", "enqueued", enqueued)
	}
	return nil
}
