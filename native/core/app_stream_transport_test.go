package core

import (
	"context"
	"net"
	"sync"
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
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// localhost resolves to both families on standard CI runners. The IPv4
	// loopback port has no listener, so this exercises the IPv6 fallback.
	conn, err := dial(ctx, "tcp", net.JoinHostPort("localhost", port))
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

func TestNativeDialResolvedIPsRacesCandidates(t *testing.T) {
	ips := []net.IPAddr{
		{IP: net.ParseIP("192.0.2.1")},
		{IP: net.ParseIP("192.0.2.2")},
	}
	var mu sync.Mutex
	started := map[string]bool{}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		started[address] = true
		mu.Unlock()
		if address == "192.0.2.1:443" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
		client, server := net.Pipe()
		go func() { _ = server.Close() }()
		return client, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	startedAt := time.Now()
	conn, err := nativeDialResolvedIPs(ctx, "tcp4", "443", ips, dial)
	if err != nil {
		t.Fatalf("fast candidate should win the race: %v", err)
	}
	_ = conn.Close()
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("dial waited for a stalled sibling address: %s", elapsed)
	}
	mu.Lock()
	defer mu.Unlock()
	if !started["192.0.2.1:443"] || !started["192.0.2.2:443"] {
		t.Fatalf("dial did not try all IPv4 addresses concurrently: %+v", started)
	}
}
