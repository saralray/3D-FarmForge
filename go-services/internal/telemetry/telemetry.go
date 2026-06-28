// Package telemetry is the poller's optional Redis acceleration layer. Redis is
// strictly optional: when REDIS_URL is unset every call is a no-op, so the poller
// keeps writing telemetry only to Postgres. When set, each printer's live
// telemetry is mirrored to a Redis hash (printer:<id>:live, values JSON-encoded)
// so the web tier can read hot state without hitting Postgres. Hard rule: nothing
// here may raise into the poll loop — every error degrades to a no-op.
package telemetry

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps an optional go-redis client. A nil/disabled Client no-ops.
type Client struct {
	rdb      *redis.Client
	enabled  bool
	warnOnce sync.Once
}

// FromEnv builds a fail-fast client from REDIS_URL with short socket timeouts so a
// dead Redis can't stall a poll cycle. Returns a disabled client when REDIS_URL is
// unset or unparseable.
func FromEnv() *Client {
	url := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if url == "" {
		return &Client{enabled: false}
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		log.Printf("[redis] failed to parse REDIS_URL: %v — telemetry cache disabled", err)
		return &Client{enabled: false}
	}
	opt.DialTimeout = 3 * time.Second
	opt.ReadTimeout = 3 * time.Second
	opt.WriteTimeout = 3 * time.Second
	return &Client{rdb: redis.NewClient(opt), enabled: true}
}

// Enabled reports whether Redis telemetry is active.
func (c *Client) Enabled() bool { return c != nil && c.enabled }

func (c *Client) warn(msg string, err error) {
	c.warnOnce.Do(func() {
		log.Printf("[redis] %s: %v — telemetry cache disabled", msg, err)
	})
}

// Publish writes one printer's telemetry as a Redis hash (printer:<id>:live),
// each value JSON-encoded unless it is already a string, then sets the TTL.
// Best-effort; never returns an error into the caller.
func (c *Client) Publish(printerID string, telemetry map[string]any, ttlSeconds int) {
	if !c.Enabled() || printerID == "" {
		return
	}
	mapping := make(map[string]any, len(telemetry))
	for field, value := range telemetry {
		if s, ok := value.(string); ok {
			mapping[field] = s
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			continue
		}
		mapping[field] = string(encoded)
	}
	if len(mapping) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	key := "printer:" + printerID + ":live"
	if err := c.rdb.HSet(ctx, key, mapping).Err(); err != nil {
		c.warn("telemetry publish failed", err)
		return
	}
	if ttlSeconds > 0 {
		_ = c.rdb.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second).Err()
	}
}

// Close releases the Redis connection pool.
func (c *Client) Close() {
	if c.Enabled() && c.rdb != nil {
		_ = c.rdb.Close()
	}
}
