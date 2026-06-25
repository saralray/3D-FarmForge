package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// serveStatic serves the built SPA from distDir, falling back to index.html for
// client-side routes (the SPA fallback), mirroring server/app.js serveStatic.
func serveStatic(w http.ResponseWriter, req *http.Request) {
	pathname := req.URL.Path
	if pathname == "/" {
		pathname = "/index.html"
	}
	// filepath.Clean on a rooted path neutralizes any ../ traversal.
	clean := filepath.Join(distDir, filepath.Clean("/"+pathname))
	absDist, _ := filepath.Abs(distDir)
	absClean, _ := filepath.Abs(clean)
	indexPath := filepath.Join(distDir, "index.html")

	if !strings.HasPrefix(absClean, absDist) {
		clean = indexPath
	} else if info, err := os.Stat(clean); err != nil || info.IsDir() {
		clean = indexPath
	}

	http.ServeFile(w, req, clean)
}
