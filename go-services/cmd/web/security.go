package main

import (
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Security headers ported from server/app.js setSecurityHeaders. CSP/HSTS live in
// the app (not nginx) deliberately. Runtime-tunable via CONTENT_SECURITY_POLICY
// (override/"off"), CSP_REPORT_ONLY, HSTS_MAX_AGE.

const defaultCSP = "default-src 'self'; base-uri 'self'; object-src 'none'; " +
	"frame-ancestors 'none'; form-action 'self'; img-src 'self' data: blob:; " +
	"font-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; " +
	"connect-src 'self'; frame-src 'self'; worker-src 'self' blob:; manifest-src 'self'"

// webcamCSP is the relaxed policy for proxied Snapmaker webcam assets (inline
// player script + jsdelivr CDN). Applied to webcam responses only.
const webcamCSP = "default-src 'self'; base-uri 'self'; object-src 'none'; " +
	"img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; " +
	"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; connect-src 'self'; " +
	"media-src 'self' blob: data:; worker-src 'self' blob:"

var (
	cspValue   = resolveCSP()
	cspHeader  = resolveCSPHeader()
	hstsMaxAge = resolveHSTS()
)

func resolveCSP() string {
	raw, ok := os.LookupEnv("CONTENT_SECURITY_POLICY")
	if !ok || raw == "" {
		return defaultCSP
	}
	if strings.ToLower(raw) == "off" {
		return ""
	}
	return raw
}

func resolveCSPHeader() string {
	if strings.ToLower(os.Getenv("CSP_REPORT_ONLY")) == "true" {
		return "Content-Security-Policy-Report-Only"
	}
	return "Content-Security-Policy"
}

func resolveHSTS() int {
	if raw, ok := os.LookupEnv("HSTS_MAX_AGE"); ok {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			return v
		}
	}
	return 15552000
}

func setSecurityHeaders(req *http.Request, h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	if cspValue != "" {
		h.Set(cspHeader, cspValue)
	}
	if hstsMaxAge > 0 && req.Header.Get("X-Forwarded-Proto") == "https" {
		h.Set("Strict-Transport-Security", "max-age="+strconv.Itoa(hstsMaxAge)+"; includeSubDomains")
	}
}
