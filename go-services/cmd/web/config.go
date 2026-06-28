package main

import (
	"os"
	"strconv"
	"strings"
)

var (
	webPort            = envInt("PORT", 5173)
	distDir            = envOr("WEB_DIST_DIR", "dist")
	dbConnectTimeoutMs = envInt("DATABASE_CONNECT_TIMEOUT_MS", 5000)
	dbStatementTimeout = envInt("DATABASE_STATEMENT_TIMEOUT_MS", 30000)
	dbIdleTxTimeout    = envInt("DATABASE_IDLE_TX_TIMEOUT_MS", 60000)
	dbPoolMax          = envInt("DATABASE_POOL_MAX", 10)

	logHTTPMode = strings.ToLower(envOr("LOG_HTTP", "sample"))
)

var quietRoutes = map[string]bool{"healthz": true, "readyz": true, "metrics": true}

func envInt(name string, def int) int {
	if raw := os.Getenv(name); raw != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			return v
		}
	}
	return def
}
