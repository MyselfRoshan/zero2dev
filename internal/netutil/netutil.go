// Package netutil has the small network helpers several packages share: finding a free local
// port.
package netutil

import (
	"net"
	"strconv"
)

// PortFree reports whether 127.0.0.1:port can be listened on now.
func PortFree(port int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// NextFreePort is the first port after from (wrapping round to 1024) that taken does not
// claim (taken may be nil) and that free reports as free (free nil: PortFree). ok is false when
// none is left.
func NextFreePort(from int, taken func(int) bool, free func(int) bool) (port int, ok bool) {
	if free == nil {
		free = PortFree
	}
	for i := 1; i <= 65535; i++ {
		p := from + i
		if p > 65535 {
			p = 1024 + (p-65536)%(65536-1024)
		}
		if p == from {
			break
		}
		if (taken == nil || !taken(p)) && free(p) {
			return p, true
		}
	}
	return 0, false
}
