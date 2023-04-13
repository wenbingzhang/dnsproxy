package server

import (
	"net"

	"github.com/miekg/dns"
)

// getNetProtocol returns true if the client is connecting over TCP.
func getNetProtocol(w dns.ResponseWriter) string {
	_, ok := w.RemoteAddr().(*net.TCPAddr)
	if ok {
		return "tcp"
	}
	return "udp"
}
