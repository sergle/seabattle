// Package netutil holds small networking helpers for the startup banner.
package netutil

import "net"

// LANIP returns the host's primary outbound LAN IPv4 address (the source IP the
// kernel would use to reach the internet), or "" if it can't be determined.
// No packets are actually sent — a UDP socket only resolves the route.
func LANIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}
