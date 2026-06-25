package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Optional Redis layer for the web tier. When REDIS_URL is unset every call is a
// no-op and readiness simply omits the redis check (matching the Node server,
// where Redis is optional and a Redis outage is "degraded", never "not ready").
var webRedis *redis.Client

func initRedis() {
	url := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if url == "" {
		return
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		logWarn("redis: failed to parse REDIS_URL", map[string]any{"err": err.Error()})
		return
	}
	opt.DialTimeout = 3 * time.Second
	opt.ReadTimeout = 3 * time.Second
	opt.WriteTimeout = 3 * time.Second
	webRedis = redis.NewClient(opt)
}

func redisEnabled() bool { return webRedis != nil }

func redisPing(ctx context.Context) bool {
	if webRedis == nil {
		return false
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return webRedis.Ping(c).Err() == nil
}
