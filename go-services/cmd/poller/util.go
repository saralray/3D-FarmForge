package main

import (
	"net"
	"strconv"
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
