// Package views buffers episode/series view counts in Redis, then lets a
// periodic job flush them into Postgres via internal/jobs/trending.go.
//
// Why Redis, not direct UPDATE?
//
//   A popular episode can get thousands of views/minute. A naive
//   "UPDATE episodes SET view_count = view_count + 1 WHERE id = $1"
//   on every read creates row-level lock contention and destroys Postgres
//   throughput. Buffering in Redis (O(1) INCR, single-threaded so no lock)
//   converts N hot writes/sec into 1 bulk UPDATE every flush interval.
//
// Keys used (all under the cache client's prefix):
//
//	ep_views          HASH  { episodeID -> count }
//	series_views      HASH  { seriesID  -> count }
package views

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	keyEpisodeViews = "ep_views"
	keySeriesViews  = "series_views"
)

// Counter is a Redis-backed view counter.
type Counter struct {
	rdb    *redis.Client
	prefix string
}

// New creates a Counter. prefix is prepended to every key (e.g. "webtoon:dev:").
func New(rdb *redis.Client, prefix string) *Counter {
	return &Counter{rdb: rdb, prefix: prefix}
}

func (c *Counter) key(k string) string { return c.prefix + k }

// Incr records a single episode read. Bumps both per-episode and per-series
// counters atomically in a pipeline. Safe to call from hot paths; never blocks
// the caller on a DB round-trip.
func (c *Counter) Incr(ctx context.Context, episodeID, seriesID uuid.UUID) error {
	if c == nil || c.rdb == nil {
		return nil // noop if Redis disabled
	}
	pipe := c.rdb.Pipeline()
	pipe.HIncrBy(ctx, c.key(keyEpisodeViews), episodeID.String(), 1)
	pipe.HIncrBy(ctx, c.key(keySeriesViews), seriesID.String(), 1)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("views.Incr: %w", err)
	}
	return nil
}

// DrainSeries atomically reads and clears the per-series counter HASH.
// Returns a map of seriesID → delta. Callers then UPDATE series.trending_score
// within a single transaction.
//
// Uses HGETALL + DEL in a pipeline; there is a tiny race window where an
// increment between HGETALL and DEL would be lost. For trending rankings this
// is acceptable (we only care about relative ordering). If you need exact
// counts, use GETDEL semantics on per-key counters instead of a HASH.
func (c *Counter) DrainSeries(ctx context.Context) (map[uuid.UUID]int64, error) {
	if c == nil || c.rdb == nil {
		return nil, nil
	}
	pipe := c.rdb.Pipeline()
	getCmd := pipe.HGetAll(ctx, c.key(keySeriesViews))
	pipe.Del(ctx, c.key(keySeriesViews))
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("views.DrainSeries: %w", err)
	}
	raw := getCmd.Val()
	out := make(map[uuid.UUID]int64, len(raw))
	for k, v := range raw {
		id, err := uuid.Parse(k)
		if err != nil {
			continue // drop malformed entries
		}
		var n int64
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
			continue
		}
		out[id] = n
	}
	return out, nil
}

// DrainEpisodes is the symmetric helper for episode-level view counts.
// Not used in this demo (we aggregate at series level for trending), but
// exposed for future use — e.g. episode heatmaps.
func (c *Counter) DrainEpisodes(ctx context.Context) (map[uuid.UUID]int64, error) {
	if c == nil || c.rdb == nil {
		return nil, nil
	}
	pipe := c.rdb.Pipeline()
	getCmd := pipe.HGetAll(ctx, c.key(keyEpisodeViews))
	pipe.Del(ctx, c.key(keyEpisodeViews))
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("views.DrainEpisodes: %w", err)
	}
	raw := getCmd.Val()
	out := make(map[uuid.UUID]int64, len(raw))
	for k, v := range raw {
		id, err := uuid.Parse(k)
		if err != nil {
			continue
		}
		var n int64
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
			continue
		}
		out[id] = n
	}
	return out, nil
}
