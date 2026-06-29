package main

import (
	"net"
)

// isPrivateHost resolves hostname to IPs and returns true if any resolved IP is
// a loopback, private, link-local, or otherwise non-routable address.
// Used by samlProbeTransport (H-3 fix) to block SSRF from the SAML test endpoint.
func isPrivateHost(hostname string) bool {
	// Bare IP address
	if ip := net.ParseIP(hostname); ip != nil {
		return isPrivateIP(ip)
	}
	// Resolve and check all addresses
	addrs, err := net.LookupHost(hostname)
	if err != nil {
		// Resolution failure — treat as private to fail closed
		return true
	}
	for _, addr := range addrs {
		if ip := net.ParseIP(addr); ip != nil && isPrivateIP(ip) {
			return true
		}
	}
	return false
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

var privateIPBlocks []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",  // CGNAT / shared address space (RFC 6598)
		"fc00::/7",       // IPv6 unique local
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"169.254.0.0/16", // IPv4 link-local
	} {
		_, block, _ := net.ParseCIDR(cidr)
		if block != nil {
			privateIPBlocks = append(privateIPBlocks, block)
		}
	}
}
