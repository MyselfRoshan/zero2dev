package netutil

import (
	"net"
	"strconv"
	"testing"
)

func TestPortFree(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen")
	}
	port := l.Addr().(*net.TCPAddr).Port
	if PortFree(port) {
		t.Fatalf("port %d is in use but reported free", port)
	}
	l.Close()
	if !PortFree(port) {
		t.Fatalf("port %d was released but is not free", port)
	}
	_ = strconv.Itoa
}

func TestNextFreePort(t *testing.T) {
	taken := map[int]bool{8001: true, 8002: true}
	free := func(p int) bool { return p != 8003 }
	got, ok := NextFreePort(8000, func(p int) bool { return taken[p] }, free)
	if !ok || got != 8004 {
		t.Fatalf("got %d %v, want 8004", got, ok)
	}
	// wraps round to 1024 after 65535
	got, ok = NextFreePort(65535, nil, func(p int) bool { return true })
	if !ok || got != 1024 {
		t.Fatalf("wrap: got %d %v, want 1024", got, ok)
	}
	if _, ok := NextFreePort(5000, nil, func(int) bool { return false }); ok {
		t.Fatal("no free port must be reported")
	}
}
