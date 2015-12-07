package hyperbahn

import (
	"net"
	"strconv"

	"github.com/uber/tchannel-go/hyperbahn/gen-go/hyperbahn"
)

// intToIP4 converts an integer IP representation into a 4-byte net.IP struct
func intToIP4(ip uint32) net.IP {
	return net.IP{
		byte(ip >> 24 & 0xff),
		byte(ip >> 16 & 0xff),
		byte(ip >> 8 & 0xff),
		byte(ip & 0xff),
	}
}

// servicePeerToHostPort converts a Hyperbahn ServicePeer into a hostPort
// string.
func servicePeerToHostPort(peer *hyperbahn.ServicePeer) string {
	host := intToIP4(uint32(*peer.IP.Ipv4)).String()
	port := strconv.Itoa(int(peer.Port))
	return net.JoinHostPort(host, port)
}
