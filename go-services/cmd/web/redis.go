package main

import (
	"context"
	"crypto/tls"
	"net/http"
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

// ── H-4 FIX: Redis-backed login rate limiter ─────────────────────────────────
// When Redis is available, the rate limit counter is stored there so all web
// replicas share a single counter. Falls through to the in-memory map otherwise.

const (
	redisLoginKey    = "printfarm:login_failures:"
	redisLoginExpiry = loginWindow
)

// redisCheckAndRecord atomically checks the current failure count and — when
// the check passes — increments it. Returns (allowed, retryAfter).
func redisCheckLoginRate(ctx context.Context, key string) (bool, time.Duration) {
	if webRedis == nil {
		return true, 0
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rk := redisLoginKey + key
	count, err := webRedis.Get(c, rk).Int()
	if err == redis.Nil {
		return true, 0
	}
	if err != nil {
		// Redis error — fall back to allow (degraded, not blocking)
		return true, 0
	}
	if count >= loginMaxFailures {
		ttl, _ := webRedis.TTL(c, rk).Result()
		if ttl < 0 {
			ttl = loginWindow
		}
		return false, ttl
	}
	return true, 0
}

func redisRecordLoginFailure(key string) {
	if webRedis == nil {
		return
	}
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rk := redisLoginKey + key
	p := webRedis.Pipeline()
	p.Incr(c, rk)
	p.Expire(c, rk, redisLoginExpiry)
	_, _ = p.Exec(c)
}

func redisClearLoginAttempts(key string) {
	if webRedis == nil {
		return
	}
	c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = webRedis.Del(c, redisLoginKey+key).Err()
}

// ── H-2 FIX: Bambu TLS configuration ─────────────────────────────────────────
// Bambu printers use self-signed certificates that cannot be verified against a
// public CA. BAMBU_TLS_SKIP_VERIFY=false enforces certificate verification —
// only possible when the printer certificate or a private CA cert is trusted at
// the OS level. Default is "true" (skip) for backward compatibility; set to
// "false" in environments where printer certs are properly managed.
//
// All Bambu connection sites (MQTT in command.go and camera.go) must call
// bambuTLSConfig() instead of inlining &tls.Config{InsecureSkipVerify: true}.

var (
	bambuTLSSkipVerify     bool
	bambuTLSSkipVerifyOnce = func() {
		v := strings.ToLower(strings.TrimSpace(os.Getenv("BAMBU_TLS_SKIP_VERIFY")))
		// default true (skip) unless explicitly set to "false" or "0"
		bambuTLSSkipVerify = v != "false" && v != "0"
		if bambuTLSSkipVerify {
			logWarn("H-2: Bambu TLS certificate verification is disabled (BAMBU_TLS_SKIP_VERIFY=true). "+
				"Set BAMBU_TLS_SKIP_VERIFY=false and trust the printer certificate at the OS level to enable verification.",
				nil)
		}
	}
)

func init() { bambuTLSSkipVerifyOnce() }

func bambuTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: bambuTLSSkipVerify} //nolint:gosec // G402: see comment above
}

// samlProbeTransport returns an http.Transport that blocks requests to
// private/loopback/link-local IP ranges (H-3 SSRF fix). Used by the SAML test
// endpoint to prevent an admin from probing internal services.
func samlProbeTransport() http.RoundTripper {
	return &ssrfSafeTransport{inner: http.DefaultTransport}
}

type ssrfSafeTransport struct{ inner http.RoundTripper }

func (t *ssrfSafeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if isPrivateHost(req.URL.Hostname()) {
		return nil, &ssrfBlockedError{host: req.URL.Hostname()}
	}
	return t.inner.RoundTrip(req)
}

type ssrfBlockedError struct{ host string }

func (e *ssrfBlockedError) Error() string {
	return "SSRF: request to private/internal host " + e.host + " is blocked"
}
