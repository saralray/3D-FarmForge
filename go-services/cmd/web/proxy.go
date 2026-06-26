package main

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// proxy.go ports handlePrinterProxy from server/app.js: the raw HTTP passthrough
// to printer hardware that backs /__printer_proxy/ (control API, e.g. Moonraker)
// and /__printer_webcam/ (camera HTTP endpoint). The friendly /webcam/<id-or-name>
// URL resolves a printer then delegates to the webcam proxy.
//
// Bambu cameras are NOT HTTP — the A1/P1 port-6000 JPEG socket and the H2 RTSP
// hub are ported in Phase 7. Until then a Bambu webcam request is handled by the
// Phase-7 stub (handleBambuWebcam), so the HTTP-passthrough path here is exercised
// only by generic / Snapmaker profiles.

const proxyPrefix = "/__printer_proxy/"
const webcamPrefix = "/__printer_webcam/"

// bambuProfiles mirrors BAMBU_PROFILES.
var bambuProfiles = map[string]bool{
	"bambulab_a1_mini": true,
	"bambulab_h2s":     true,
	"bambulab_h2d":     true,
	"bambulab_h2c":     true,
}

// liveMjpegProfiles mirrors LIVE_MJPEG_PROFILES (used by /webcam/<id> to pick
// stream vs snapshot).
var liveMjpegProfiles = map[string]bool{
	"snapmaker_u1": true,
	"bambulab_h2s": true,
	"bambulab_h2d": true,
	"bambulab_h2c": true,
}

// proxyClient has no timeout: a webcam response can be an endless MJPEG stream.
// Client disconnects abort the upstream request via the request context.
var proxyClient = &http.Client{}

// handlePrinterProxy mirrors the Node function. makeTarget builds the upstream
// URL from the printer and the (encoded) proxy path. Returns true once handled;
// false when the path doesn't match the prefix (so the caller can fall through).
func handlePrinterProxy(w http.ResponseWriter, req *http.Request, prefix string, makeTarget func(pc *printerConn, proxyPath string) string) bool {
	pathname := req.URL.Path
	if !strings.HasPrefix(pathname, prefix) {
		return false
	}
	ctx := req.Context()

	parts := splitNonEmpty(pathname[len(prefix):])
	printerID := ""
	if len(parts) > 0 {
		printerID, _ = decodeURIComponent(parts[0])
		parts = parts[1:]
	}
	if printerID == "" {
		sendJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing printer proxy target"}, "")
		return true
	}

	printer, err := getPrinterConn(ctx, printerID)
	if err != nil {
		internalError(w, "getPrinterConn", err)
		return true
	}
	if printer == nil {
		sendJSON(w, http.StatusNotFound, map[string]any{"error": "Printer not found"}, "")
		return true
	}

	isWebcam := prefix == webcamPrefix

	// Bambu's camera isn't HTTP — deferred to the Phase-7 hub.
	if isWebcam && bambuProfiles[printer.Profile] {
		handleBambuWebcam(ctx, w, req, printer, parts)
		return true
	}

	proxyPath := "/" + strings.Join(encodeSegments(parts), "/")
	if req.URL.RawQuery != "" {
		proxyPath += "?" + req.URL.RawQuery
	}

	var body io.Reader
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		body = req.Body
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, makeTarget(printer, proxyPath), body)
	if err != nil {
		internalError(w, "proxy build request", err)
		return true
	}
	applyProxyHeaders(upstreamReq, req, printer)

	resp, err := proxyClient.Do(upstreamReq)
	if err != nil {
		// A client navigating away cancels the request context — expected.
		if ctx.Err() != nil {
			return true
		}
		// Mirror Node's top-level catch for an upstream failure (500 + message).
		sendJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()}, "")
		return true
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if isWebcam {
		writeWebcamResponse(w, resp, contentType)
		return true
	}

	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
	return true
}

// applyProxyHeaders reproduces the Node header assembly: the printer's api-key
// header, then any extra headers, then every inbound header except host /
// connection / content-length (which a later spread overrides — here inbound
// wins, matching JS object-spread precedence).
func applyProxyHeaders(out *http.Request, in *http.Request, printer *printerConn) {
	for k, v := range parseHeaderString(printer.APIKeyHeader) {
		out.Header.Set(k, v)
	}
	for k, vals := range in.Header {
		lk := strings.ToLower(k)
		if lk == "host" || lk == "connection" || lk == "content-length" {
			continue
		}
		out.Header[k] = append([]string(nil), vals...)
	}
}

func writeWebcamResponse(w http.ResponseWriter, resp *http.Response, contentType string) {
	h := w.Header()
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", webcamCSP)
	h.Del("Content-Security-Policy-Report-Only")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")

	// The embeddable player HTML letterboxes inside the iframe; buffer just the
	// HTML and inject a style override so the media fills the frame. Streams
	// (MJPEG/JPEG) stay piped.
	if strings.Contains(contentType, "text/html") {
		raw, _ := io.ReadAll(resp.Body)
		html := string(raw)
		styleTag := "<style>html,body{margin:0;height:100%;overflow:hidden;background:#000}" +
			"video,canvas,img{position:fixed!important;inset:0!important;width:100%!important;" +
			"height:100%!important;object-fit:cover!important}</style>"
		var patched string
		if strings.Contains(html, "</head>") {
			patched = strings.Replace(html, "</head>", styleTag+"</head>", 1)
		} else {
			patched = html + styleTag
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.WriteString(w, patched)
		return
	}

	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// handleWebcamStream backs GET /webcam/<id-or-name>: resolve the printer, pick
// stream vs snapshot for its profile, and delegate to the webcam proxy.
func handleWebcamStream(w http.ResponseWriter, req *http.Request) bool {
	pathname := req.URL.Path
	if !strings.HasPrefix(pathname, "/webcam/") {
		return false
	}
	rest := strings.TrimSuffix(pathname[len("/webcam/"):], "/")
	if rest == "" || strings.Contains(rest, "/") {
		return false
	}
	ctx := req.Context()
	identifier, _ := decodeURIComponent(rest)
	printer, err := getPrinterConnByIdOrName(ctx, identifier)
	if err != nil {
		internalError(w, "getPrinterConnByIdOrName", err)
		return true
	}
	if printer == nil {
		sendJSON(w, http.StatusNotFound, map[string]any{"error": "Printer not found"}, "")
		return true
	}

	camPath := "snapshot.jpg"
	if liveMjpegProfiles[printer.Profile] {
		camPath = "stream.mjpg"
	}
	// Rewrite to the canonical webcam-proxy path and delegate.
	req2 := req.Clone(ctx)
	req2.URL = &url.URL{Path: webcamPrefix + url.PathEscape(printer.ID) + "/" + camPath}
	return handlePrinterProxy(w, req2, webcamPrefix, webcamTarget)
}

func proxyTarget(pc *printerConn, proxyPath string) string  { return pc.URL + proxyPath }
func webcamTarget(pc *printerConn, proxyPath string) string { return pc.URL + "/webcam" + proxyPath }

// ── helpers ──────────────────────────────────────────────────────────────────

func splitNonEmpty(s string) []string {
	parts := strings.Split(s, "/")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func encodeSegments(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = encodeURIComponent(p)
	}
	return out
}

// encodeURIComponent mirrors JS encodeURIComponent: percent-encode everything
// except the unreserved set A-Za-z0-9 and -_.!~*'().
func encodeURIComponent(s string) string {
	const unreserved = "-_.!~*'()"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			strings.IndexByte(unreserved, c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte("0123456789ABCDEF"[c>>4])
			b.WriteByte("0123456789ABCDEF"[c&0xf])
		}
	}
	return b.String()
}

// parseHeaderString mirrors the Node helper: "Name: value" → {Name: value};
// a bare value → {X-API-Key: value}; empty → {}.
func parseHeaderString(headerValue string) map[string]string {
	idx := strings.IndexByte(headerValue, ':')
	if idx == -1 {
		v := strings.TrimSpace(headerValue)
		if v == "" {
			return map[string]string{}
		}
		return map[string]string{"X-API-Key": v}
	}
	name := strings.TrimSpace(headerValue[:idx])
	value := strings.TrimSpace(headerValue[idx+1:])
	if name == "" || value == "" {
		return map[string]string{}
	}
	return map[string]string{name: value}
}

// handleBambuWebcam is the Phase-7 entry point for Bambu cameras (port-6000 JPEG
// snapshot and H2 RTSP hub). Until the hub is ported it reports the camera as
// unavailable, matching how the UI treats a failed capture (it just shows
// "Webcam offline").
func handleBambuWebcam(ctx context.Context, w http.ResponseWriter, req *http.Request, printer *printerConn, parts []string) {
	_ = ctx
	_ = req
	_ = printer
	_ = parts
	sendJSON(w, http.StatusBadGateway, map[string]any{"error": "Bambu camera not yet available"}, "")
}
