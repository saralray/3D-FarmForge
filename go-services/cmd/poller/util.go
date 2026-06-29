package main

import (
	"crypto/tls"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
)

func parseFloat(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func addrPort(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// bambuTLSConfig returns the TLS config for Bambu printer connections.
// H-2 FIX: controlled by BAMBU_TLS_SKIP_VERIFY (default "true" for backward
// compat with Bambu's self-signed certificates). Set to "false" when the
// printer certificate is trusted at the OS level or via a private CA.
func bambuTLSConfig() *tls.Config {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("BAMBU_TLS_SKIP_VERIFY")))
	skip := v != "false" && v != "0"
	if skip {
		log.Println("H-2: Bambu TLS certificate verification is disabled (BAMBU_TLS_SKIP_VERIFY=true). " +
			"Set BAMBU_TLS_SKIP_VERIFY=false and trust the printer certificate to enable verification.")
	}
	return &tls.Config{InsecureSkipVerify: skip} //nolint:gosec // G402: documented above
}
