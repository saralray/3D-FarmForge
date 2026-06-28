package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Leveled, dependency-free logger ported from server/logger.js. Emits text
// (default) or one JSON object per line (LOG_FORMAT=json). Config: LOG_LEVEL
// (debug|info|warn|error), LOG_FORMAT (text|json), LOG_SERVICE (default "web").

var logLevels = map[string]int{"debug": 10, "info": 20, "warn": 30, "error": 40}

var (
	logService   = envOr("LOG_SERVICE", "web")
	logThreshold = logLevels[strings.ToLower(envOr("LOG_LEVEL", "info"))]
	logAsJSON    = strings.ToLower(envOr("LOG_FORMAT", "text")) == "json"
)

func init() {
	if _, ok := logLevels[strings.ToLower(envOr("LOG_LEVEL", "info"))]; !ok {
		logThreshold = logLevels["info"]
	}
}

func logEmit(level, message string, fields map[string]any) {
	if logLevels[level] < logThreshold {
		return
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if logAsJSON {
		obj := map[string]any{"time": now, "level": level, "service": logService, "msg": message}
		for k, v := range fields {
			obj[k] = v
		}
		if b, err := json.Marshal(obj); err == nil {
			fmt.Fprintln(os.Stderr, string(b))
		} else {
			b, _ := json.Marshal(map[string]any{"time": now, "level": level, "service": logService, "msg": message})
			fmt.Fprintln(os.Stderr, string(b))
		}
		return
	}
	suffix := ""
	if len(fields) > 0 {
		var parts []string
		for k, v := range fields {
			if v == nil {
				continue
			}
			if s, ok := v.(string); ok {
				parts = append(parts, k+"="+s)
			} else {
				b, _ := json.Marshal(v)
				parts = append(parts, k+"="+string(b))
			}
		}
		if len(parts) > 0 {
			suffix = " " + strings.Join(parts, " ")
		}
	}
	fmt.Fprintf(os.Stderr, "%s %s [%s] %s%s\n", now, strings.ToUpper(level), logService, message, suffix)
}

func logDebug(msg string, f map[string]any) { logEmit("debug", msg, f) }
func logInfo(msg string, f map[string]any)  { logEmit("info", msg, f) }
func logWarn(msg string, f map[string]any)  { logEmit("warn", msg, f) }
func logError(msg string, f map[string]any) { logEmit("error", msg, f) }

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

var _ = logDebug // not all levels are used yet during the phased port
