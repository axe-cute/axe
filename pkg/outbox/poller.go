// Package outbox implements the Transactional Outbox pattern for axe.
// The poller periodically reads unprocessed rows from outbox_events table
// and dispatches them as Asynq tasks.
//
// Architecture:
//
//	DB write → insert to outbox_events (same TX)
//	Poller   → read unprocessed → enqueue to Asynq → mark processed
//	Worker   → consume from Asynq → call downstream (webhook, email, etc.)
package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/axe-cute/axe/pkg/worker"
)

// ── Metrics ───────────────────────────────────────────────────────────────────

var (
	outboxDeadLettersTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "axe",
		Subsystem: "outbox",
		Name:      "dead_letters_total",
		Help:      "Total outbox events that exceeded max retries.",
	})
	outboxEnqueuedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "axe",
		Subsystem: "outbox",
		Name:      "enqueued_total",
		Help:      "Total outbox events successfully enqueued.",
	})
	outboxEnqueueFailedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "axe",
		Subsystem: "outbox",
		Name:      "enqueue_failed_total",
		Help:      "Total outbox enqueue failures.",
	})
)

// Enqueuer abstracts task enqueueing so the poller can be tested without Redis.
// *asynq.Client satisfies this interface.
type Enqueuer interface {
	Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

// Poller reads unprocessed outbox_events and dispatches them as Asynq tasks.
type Poller struct {
	db         *sql.DB
	enqueuer   Enqueuer
	log        *slog.Logger
	interval   time.Duration
	batchSize  int
	maxRetries int
	driver     string // "postgres", "mysql", "sqlite3"
}

// Config holds poller settings.
type Config struct {
	Interval  time.Duration // polling interval, default 5s
	BatchSize int           // rows per poll, default 50
	// MaxRetries is the maximum number of enqueue attempts before an event
	// is considered a dead letter. Default: 5.
	MaxRetries int
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
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 5
	}
	if cfg.Driver == "" {
		cfg.Driver = "postgres"
	}

	return &Poller{
		db:         db,
		enqueuer:   enqueuer,
		log:        log,
		interval:   cfg.Interval,
		batchSize:  cfg.BatchSize,
		maxRetries: cfg.MaxRetries,
		driver:     cfg.Driver,
	}
}

// NewWithRedis creates a Poller that connects to Redis for task enqueueing.
// This is the most common constructor — equivalent to New(db, asynq.NewClient(...), cfg, log).
func NewWithRedis(db *sql.DB, redisAddr string, cfg Config, log *slog.Logger) *Poller {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	return New(db, client, cfg, log)
}

// ph returns the correct positional placeholder for parameter n.
// PostgreSQL uses $N; MySQL and SQLite use ?.
func (p *Poller) ph(n int) string {
	if p.driver == "postgres" {
		return fmt.Sprintf("$%d", n)
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
	// Exclude events in backoff (retry_after in the future) and dead letters.
	lockSQL := p.lockClause()
	if lockSQL != "" {
		lockSQL = "\n\t\t" + lockSQL
	}

	// retry_after NULL = eligible immediately (backwards-compatible).
	nowFn := "NOW()"
	if p.driver == "sqlite3" {
		nowFn = "CURRENT_TIMESTAMP"
	}

	query := `
		SELECT id, event_type, aggregate, retries
		FROM outbox_events
		WHERE processed_at IS NULL
		  AND retries < ` + p.ph(1) + `
		  AND (retry_after IS NULL OR retry_after <= ` + nowFn + `)
		ORDER BY created_at ASC
		LIMIT ` + p.ph(2) + lockSQL + `
	`
	rows, err := p.db.QueryContext(ctx, query, p.maxRetries, p.batchSize)
	if err != nil {
		return err
	}

	// Collect all rows first, then close cursor before writing.
	// This avoids table-lock contention (critical for SQLite, good practice for all drivers).
	type event struct {
		id, eventType, aggregate string
		retries                  int
	}
	var events []event
	for rows.Next() {
		var e event
		if err := rows.Scan(&e.id, &e.eventType, &e.aggregate, &e.retries); err != nil {
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
			outboxEnqueueFailedTotal.Inc()

			// Increment retry count + set exponential backoff for next attempt.
			newRetries := e.retries + 1
			p.handleRetryOrDeadLetter(e.id, e.eventType, e.aggregate, newRetries, nowFn)
			continue
		}

		// Mark as processed — use driver-specific timestamp function.
		// Use a short independent context so in-flight marks complete even if
		// the parent poll context is cancelled (prevents duplicate delivery).
		updateQuery := `UPDATE outbox_events SET processed_at = ` + nowFn + `, retries = retries + 1 WHERE id = ` + p.ph(1)
		markCtx, markCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if _, err := p.db.ExecContext(markCtx, updateQuery, e.id); err != nil {
			p.log.Error("mark outbox processed", "event_id", e.id, "error", err)
			markCancel()
			continue
		}
		markCancel()

		enqueued++
		outboxEnqueuedTotal.Inc()
	}

	if enqueued > 0 {
		p.log.Info("outbox poll", "enqueued", enqueued)
	}
	return nil
}

// handleRetryOrDeadLetter increments the retry counter and either sets exponential
// backoff for the next attempt or logs a dead letter if max retries exceeded.
func (p *Poller) handleRetryOrDeadLetter(eventID, eventType, aggregate string, newRetries int, nowFn string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if newRetries >= p.maxRetries {
		// Dead letter — event will no longer be picked up by the poller.
		outboxDeadLettersTotal.Inc()
		p.log.Warn("outbox: event exceeded max retries (dead letter)",
			"event_id", eventID,
			"event_type", eventType,
			"aggregate", aggregate,
			"retries", newRetries,
			"max_retries", p.maxRetries,
		)

		updateQuery := `UPDATE outbox_events SET retries = ` + p.ph(1) + ` WHERE id = ` + p.ph(2)
		if _, err := p.db.ExecContext(ctx, updateQuery, newRetries, eventID); err != nil {
			p.log.Error("mark outbox dead letter", "event_id", eventID, "error", err)
		}
		return
	}

	// Exponential backoff: min(interval * 2^retries, 5 minutes)
	backoff := time.Duration(math.Min(
		float64(p.interval)*math.Pow(2, float64(newRetries)),
		float64(5*time.Minute),
	))

	p.log.Info("outbox: scheduling retry with backoff",
		"event_id", eventID,
		"retry", newRetries,
		"backoff", backoff,
	)

	// Set retries and retry_after for backoff.
	// retry_after = NOW() + backoff_seconds
	backoffSec := int(backoff.Seconds())
	var updateQuery string
	switch p.driver {
	case "postgres":
		updateQuery = fmt.Sprintf(
			`UPDATE outbox_events SET retries = %s, retry_after = NOW() + INTERVAL '%d seconds' WHERE id = %s`,
			p.ph(1), backoffSec, p.ph(2),
		)
	case "mysql":
		updateQuery = fmt.Sprintf(
			`UPDATE outbox_events SET retries = %s, retry_after = DATE_ADD(NOW(), INTERVAL %d SECOND) WHERE id = %s`,
			p.ph(1), backoffSec, p.ph(2),
		)
	default: // sqlite3
		updateQuery = fmt.Sprintf(
			`UPDATE outbox_events SET retries = %s, retry_after = datetime('now', '+%d seconds') WHERE id = %s`,
			p.ph(1), backoffSec, p.ph(2),
		)
	}

	if _, err := p.db.ExecContext(ctx, updateQuery, newRetries, eventID); err != nil {
		p.log.Error("set outbox retry_after", "event_id", eventID, "error", err)
	}
}
