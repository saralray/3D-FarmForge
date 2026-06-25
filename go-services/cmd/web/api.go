package main

import "net/http"

// handleAPI is the entry point for the /api/*, /api/v1, and printer-proxy
// surface. It is being ported group by group (see WEB_PORT_PLAN.md); until a
// route matches it returns false so the request falls through to static serving.
// While the Node web service remains the live server, this Go server is run only
// for parity testing, so unported routes simply aren't exercised here.
func handleAPI(w http.ResponseWriter, req *http.Request) bool {
	_ = w
	_ = req
	return false
}
