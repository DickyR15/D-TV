package core

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestNativeStreamDialContextPrefersIPv4(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dial := nativeStreamDialContext(&net.Dialer{Timeout: nativeStreamDialTimeout})
	conn, err := dial(ctx, "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("IPv4 target should connect on the preferred path: %v", err)
	}
	defer conn.Close()

	remote, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok || remote.IP.To4() == nil {
		t.Fatalf("preferred connection should be IPv4, got %v", conn.RemoteAddr())
	}
	select {
	case acceptedConn := <-accepted:
		_ = acceptedConn.Close()
	case <-time.After(time.Second):
		t.Fatal("listener did not accept the IPv4 connection")
	}
}

func TestNativeStreamDialContextFallsBackToIPv6(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback is unavailable on this runner: %v", err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dial := nativeStreamDialContext(&net.Dialer{Timeout: nativeStreamDialTimeout})
	conn, err := dial(ctx, "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("IPv6-only target should connect through fallback: %v", err)
	}
	defer conn.Close()

	remote, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok || remote.IP.To4() != nil {
		t.Fatalf("fallback connection should be IPv6, got %v", conn.RemoteAddr())
	}
	select {
	case acceptedConn := <-accepted:
		_ = acceptedConn.Close()
	case <-time.After(time.Second):
		t.Fatal("listener did not accept the IPv6 connection")
	}
}
