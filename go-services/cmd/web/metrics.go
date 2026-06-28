package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// In-process Prometheus metrics for the web tier (printfarm_web_*), exposed at
// GET /metrics. Hand-rolled port of server/metrics.js; same exposition format,
// same low-cardinality route labels.

var processStartSeconds = float64(time.Now().UnixNano()) / 1e9

var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

var knownMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true,
}

type histogram struct {
	buckets []float64
	sum     float64
	count   float64
}

var (
	metricsMu      sync.Mutex
	requestCounts  = map[string]float64{}
	durationByRoot = map[string]*histogram{}
	inFlight       int
)

var resourceRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{0,30}$`)
var staticExtRe = regexp.MustCompile(`\.[a-zA-Z0-9]{1,8}$`)

func classifyRoute(pathname string) string {
	switch pathname {
	case "/healthz", "/readyz", "/metrics":
		return pathname[1:]
	}
	if strings.HasPrefix(pathname, "/api/v1") {
		return "api_v1"
	}
	if strings.HasPrefix(pathname, "/__printer_proxy") {
		return "printer_proxy"
	}
	if strings.HasPrefix(pathname, "/__printer_webcam") || strings.HasPrefix(pathname, "/webcam") {
		return "webcam"
	}
	if strings.HasPrefix(pathname, "/api/") {
		parts := strings.Split(pathname, "/")
		resource := "root"
		if len(parts) > 2 && parts[2] != "" {
			resource = parts[2]
		}
		if resourceRe.MatchString(resource) {
			return "api_" + resource
		}
		return "api_other"
	}
	if staticExtRe.MatchString(pathname) {
		return "static"
	}
	return "app"
}

func normalizeMethod(method string) string {
	upper := strings.ToUpper(method)
	if upper == "" {
		upper = "GET"
	}
	if knownMethods[upper] {
		return upper
	}
	return "OTHER"
}

func recordRequestStart() {
	metricsMu.Lock()
	inFlight++
	metricsMu.Unlock()
}

func recordRequestEnd(method string, statusCode int, route string, durationMs float64) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	if inFlight > 0 {
		inFlight--
	}
	key := normalizeMethod(method) + "|" + strconv.Itoa(statusCode) + "|" + route
	requestCounts[key]++

	hist := durationByRoot[route]
	if hist == nil {
		hist = &histogram{buckets: make([]float64, len(durationBuckets))}
		durationByRoot[route] = hist
	}
	seconds := durationMs / 1000
	if seconds < 0 {
		seconds = 0
	}
	hist.sum += seconds
	hist.count++
	for i, b := range durationBuckets {
		if seconds <= b {
			hist.buckets[i]++
		}
	}
}

func renderMetrics() string {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	var b strings.Builder

	b.WriteString("# HELP printfarm_web_http_requests_total Total HTTP requests handled by the web server.\n")
	b.WriteString("# TYPE printfarm_web_http_requests_total counter\n")
	for key, value := range requestCounts {
		seg := strings.SplitN(key, "|", 3)
		fmt.Fprintf(&b, "printfarm_web_http_requests_total{method=%q,status=%q,route=%q} %s\n",
			seg[0], seg[1], seg[2], numStr(value))
	}

	b.WriteString("# HELP printfarm_web_http_request_duration_seconds HTTP request latency by route.\n")
	b.WriteString("# TYPE printfarm_web_http_request_duration_seconds histogram\n")
	for route, hist := range durationByRoot {
		for i, le := range durationBuckets {
			fmt.Fprintf(&b, "printfarm_web_http_request_duration_seconds_bucket{route=%q,le=%q} %s\n",
				route, numStr(le), numStr(hist.buckets[i]))
		}
		fmt.Fprintf(&b, "printfarm_web_http_request_duration_seconds_bucket{route=%q,le=\"+Inf\"} %s\n", route, numStr(hist.count))
		fmt.Fprintf(&b, "printfarm_web_http_request_duration_seconds_sum{route=%q} %s\n", route, numStr(hist.sum))
		fmt.Fprintf(&b, "printfarm_web_http_request_duration_seconds_count{route=%q} %s\n", route, numStr(hist.count))
	}

	b.WriteString("# HELP printfarm_web_http_requests_in_flight HTTP requests currently being served.\n")
	b.WriteString("# TYPE printfarm_web_http_requests_in_flight gauge\n")
	fmt.Fprintf(&b, "printfarm_web_http_requests_in_flight %d\n", inFlight)

	b.WriteString("# HELP printfarm_web_start_time_seconds Unix time the web process started.\n")
	b.WriteString("# TYPE printfarm_web_start_time_seconds gauge\n")
	fmt.Fprintf(&b, "printfarm_web_start_time_seconds %s\n", numStr(processStartSeconds))

	b.WriteString("# HELP printfarm_web_resident_memory_bytes Resident set size of the web process.\n")
	b.WriteString("# TYPE printfarm_web_resident_memory_bytes gauge\n")
	fmt.Fprintf(&b, "printfarm_web_resident_memory_bytes %d\n", residentMemoryBytes())

	return b.String()
}

func numStr(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func residentMemoryBytes() int64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	res, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return res * 4096
}
