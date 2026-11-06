package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCDNKeyEOFUsesVerifiedDNSAndKeepsMediaIdentity(t *testing.T) {
	var direct, lookups atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != "tp.tuafjz.cn" || request.TLS.ServerName != "tp.tuafjz.cn" || request.URL.RequestURI() != "/videos/synthetic/crypt.key?signature=a%2Fb" || request.Header.Get("Referer") != "https://huangguoai.com/watch/synthetic/" {
			t.Error("DNS recovery changed SNI, host, signature or referer", request.Host, request.URL, request.TLS.ServerName)
		}
		_, _ = writer.Write([]byte("0123456789abcdef"))
	}))
	defer server.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "tp.tuafjz.cn:443" {
			direct.Add(1)
			return nil, io.EOF
		}
		if address != "93.184.216.34:443" {
			t.Error("unexpected DNS destination", address)
			return nil, io.EOF
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	resolver := newDNSResolver(base)
	resolver.client.Transport = sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		lookups.Add(1)
		if request.URL.Query().Get("name") != "tp.tuafjz.cn" {
			t.Error("unexpected DNS query")
		}
		return sourceFixtureResponse(request, 200, `{"Status":0,"Answer":[{"type":1,"TTL":120,"data":"93.184.216.34"}]}`), nil
	})
	transport := newCDNTransport(base, resolver)
	defer transport.CloseIdleConnections()
	d := &Downloader{client: &http.Client{Transport: transport}}
	for index := 0; index < 2; index++ {
		request, _ := http.NewRequest(http.MethodGet, "https://tp.tuafjz.cn/videos/synthetic/crypt.key?signature=a%2Fb", nil)
		request.Header.Set("Referer", "https://huangguoai.com/watch/synthetic/")
		response, err := d.doMediaRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(body) != "0123456789abcdef" || response.Request.URL.Host != "tp.tuafjz.cn" {
			t.Fatal("media key did not recover", err)
		}
	}
	if direct.Load() != 1 || lookups.Load() != 1 {
		t.Fatal("working CDN route was not reused", direct.Load(), lookups.Load())
	}
	for _, addresses := range [][]string{{"127.0.0.1"}, {"198.18.0.1"}, {"182.173.0.181"}, {"93.184.216.34", "10.0.0.1"}} {
		if validateCDNAddresses(addresses) == nil {
			t.Fatal("unsafe DNS answer was accepted", addresses)
		}
	}
}

func TestCoverDNSRecoveryAcceptsNewImageHosts(t *testing.T) {
	var direct, lookups atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != "new-cover.example.test" || request.TLS.ServerName != "new-cover.example.test" || request.URL.RequestURI() != "/poster/synthetic.img?signature=a%2Fb" || request.Header.Get("Referer") != "https://huangguoai.com/watch/synthetic/" {
			t.Error("DNS recovery changed SNI, host, signature or referer", request.Host, request.URL, request.TLS.ServerName)
		}
		_, _ = writer.Write([]byte("0123456789abcdef"))
	}))
	defer server.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "new-cover.example.test:443" {
			direct.Add(1)
			return nil, io.EOF
		}
		if address != "93.184.216.34:443" {
			t.Error("unexpected DNS destination", address)
			return nil, io.EOF
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	resolver := newDNSResolver(base)
	resolver.client.Transport = sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		lookups.Add(1)
		if request.URL.Query().Get("name") != "new-cover.example.test" {
			t.Error("unexpected DNS query")
		}
		return sourceFixtureResponse(request, 200, `{"Status":0,"Answer":[{"type":1,"TTL":120,"data":"93.184.216.34"}]}`), nil
	})
	transport := newCDNTransport(base, resolver)
	defer transport.CloseIdleConnections()
	d := &Downloader{client: &http.Client{Transport: transport}}
	for index := 0; index < 2; index++ {
		request, _ := http.NewRequestWithContext(context.WithValue(context.Background(), nativeCoverNetworkKey{}, true), http.MethodGet, "https://new-cover.example.test/poster/synthetic.img?signature=a%2Fb", nil)
		request.Header.Set("Referer", "https://huangguoai.com/watch/synthetic/")
		response, err := d.doMediaRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(body) != "0123456789abcdef" || response.Request.URL.Host != "new-cover.example.test" {
			t.Fatal("media key did not recover", err)
		}
	}
	if direct.Load() != 1 || lookups.Load() != 1 {
		t.Fatal("working CDN route was not reused", direct.Load(), lookups.Load())
	}
	for _, addresses := range [][]string{{"127.0.0.1"}, {"198.18.0.1"}, {"93.184.216.34", "10.0.0.1"}} {
		if validateCDNAddresses(addresses) == nil {
			t.Fatal("unsafe DNS answer was accepted", addresses)
		}
	}
}

func TestCoverDNSRecoverySupportsIPv6OnlyImageHost(t *testing.T) {
	var direct, ipv6Lookups atomic.Int32
	const host = "ipv6-cover.example.test"
	const ipv6 = "2001:4860:4860::8888"
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != host || request.TLS.ServerName != host || request.URL.Path != "/poster/synthetic.jpg" {
			t.Error("IPv6 DNS recovery changed image host, SNI or path", request.Host, request.TLS.ServerName, request.URL)
		}
		_, _ = writer.Write([]byte("0123456789abcdef"))
	}))
	defer server.Close()

	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == host+":443" {
			direct.Add(1)
			return nil, io.EOF
		}
		if address != net.JoinHostPort(ipv6, "443") {
			t.Error("unexpected IPv6 DNS destination", address)
			return nil, io.EOF
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}

	resolver := newDNSResolver(base)
	resolver.client.Transport = sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Query().Get("type") {
		case "A":
			return sourceFixtureResponse(request, http.StatusOK, `{"Status":0,"Answer":[]}`), nil
		case "AAAA":
			ipv6Lookups.Add(1)
			return sourceFixtureResponse(request, http.StatusOK,
				`{"Status":0,"Answer":[{"type":28,"TTL":120,"data":"2001:4860:4860::8888"}]}`), nil
		default:
			t.Error("unexpected DNS record type", request.URL.Query().Get("type"))
			return sourceFixtureResponse(request, http.StatusBadRequest, `{}`), nil
		}
	})
	transport := newCDNTransport(base, resolver)
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(
		context.WithValue(context.Background(), nativeCoverNetworkKey{}, true),
		http.MethodGet, "https://"+host+"/poster/synthetic.jpg", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatal("IPv6-only cover host did not recover through DNS-over-HTTPS:", err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || string(body) != "0123456789abcdef" {
		t.Fatalf("unexpected image response body %q, err=%v", body, readErr)
	}
	if direct.Load() != 1 || ipv6Lookups.Load() == 0 {
		t.Fatalf("IPv6 fallback was not used: direct=%d AAAA lookups=%d", direct.Load(), ipv6Lookups.Load())
	}
}

func TestHostProtocolFallbackTransportIsScopedToOneHostname(t *testing.T) {
	var baseCalls, fallbackCalls atomic.Int32
	base := sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		baseCalls.Add(1)
		return sourceFixtureResponse(request, http.StatusOK, "base"), nil
	})
	fallback := sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		fallbackCalls.Add(1)
		return sourceFixtureResponse(request, http.StatusOK, "fallback"), nil
	})
	transport := &hostProtocolFallbackTransport{
		base: base, fallback: fallback, host: "djapi.999888456.xyz",
	}

	for _, address := range []string{
		"https://djapi.999888456.xyz/api/hongguo/play",
		"https://example.test/ordinary",
	} {
		request, err := http.NewRequest(http.MethodGet, address, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	if baseCalls.Load() != 1 || fallbackCalls.Load() != 1 {
		t.Fatal("protocol fallback escaped its configured hostname", baseCalls.Load(), fallbackCalls.Load())
	}
}

func TestHTTP1NoALPNTransportDisablesALPNAndHTTP2(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
	}
	base.ForceAttemptHTTP2 = true

	transport := newHTTP1NoALPNTransport(base)
	defer transport.CloseIdleConnections()
	if transport.ForceAttemptHTTP2 || transport.TLSClientConfig == nil ||
		len(transport.TLSClientConfig.NextProtos) != 0 ||
		transport.TLSNextProto == nil || len(transport.TLSNextProto) != 0 ||
		transport.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Fatal("HTTP/1.1 fallback still advertises ALPN/HTTP2 or weakened TLS")
	}
}


func TestHTTP1NoALPNTransportCompletesTLSWithoutNegotiatedProtocol(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.TLS == nil {
			t.Error("fallback request did not use TLS")
		} else if request.TLS.NegotiatedProtocol != "" {
			t.Errorf("fallback unexpectedly negotiated ALPN protocol %q", request.TLS.NegotiatedProtocol)
		}
		_, _ = io.WriteString(writer, "ok")
	}))
	server.EnableHTTP2 = false
	server.StartTLS()
	defer server.Close()

	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	fallback := newHTTP1NoALPNTransport(base)
	defer fallback.CloseIdleConnections()

	response, err := (&http.Client{Transport: fallback}).Get(server.URL)
	if err != nil {
		t.Fatalf("HTTP/1.1 fallback failed against a TLS server that does not negotiate ALPN: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ProtoMajor != 1 ||
		response.TLS == nil || response.TLS.NegotiatedProtocol != "" {
		t.Fatalf("unexpected fallback response: status=%d proto=%s tls=%+v", response.StatusCode, response.Proto, response.TLS)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "ok" {
		t.Fatalf("unexpected response body %q, err=%v", body, err)
	}
}


func TestCDNTransportRetriesUnknownAuthorityThroughVerifiedDoHForMediaHosts(t *testing.T) {
	const host = "example.com"
	const fallbackIP = "93.184.216.34"
	var directAttempts, dohLookups, recoveredRequests atomic.Int32

	goodServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || request.TLS.ServerName != host || request.URL.Path != "/video/index.m3u8" {
			t.Errorf("DoH retry changed TLS SNI or media path: tls=%+v path=%q", request.TLS, request.URL.Path)
		}
		if request.Host == "" || request.Host == fallbackIP {
			t.Errorf("DoH retry did not preserve the original media Host header: %q", request.Host)
		}
		recoveredRequests.Add(1)
		_, _ = io.WriteString(writer, "verified media playlist")
	}))
	defer goodServer.Close()

	badServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Error("untrusted certificate must be rejected before sending an HTTP request")
	}))
	defer badServer.Close()

	_, port, err := net.SplitHostPort(goodServer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	originalAddress := net.JoinHostPort(host, port)
	fallbackAddress := net.JoinHostPort(fallbackIP, port)
	trustedRoots := x509.NewCertPool()
	trustedRoots.AddCert(goodServer.Certificate())

	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: trustedRoots}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == originalAddress {
			if directAttempts.Add(1) != 1 {
				t.Errorf("unexpected repeat of the system-DNS route: %s", address)
				return nil, io.EOF
			}
			return (&net.Dialer{}).DialContext(ctx, network, badServer.Listener.Addr().String())
		}
		if address == fallbackAddress {
			return (&net.Dialer{}).DialContext(ctx, network, goodServer.Listener.Addr().String())
		}
		t.Errorf("unexpected media dial destination: %s", address)
		return nil, io.EOF
	}

	resolver := newDNSResolver(base)
	resolver.endpoints = []string{"https://dns.example.test/resolve"}
	resolver.client = &http.Client{
		Transport: sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
			dohLookups.Add(1)
			if request.URL.Query().Get("name") != host || request.URL.Query().Get("type") != "A" {
				t.Errorf("unexpected DoH lookup: %s", request.URL.String())
			}
			return sourceFixtureResponse(request, http.StatusOK,
				`{"Status":0,"Answer":[{"type":1,"TTL":120,"data":"93.184.216.34"}]}`), nil
		}),
	}

	transport := newCDNTransport(base, resolver)
	transport.fallbackOnUnknownAuthority = true
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}

	for attempt := 0; attempt < 2; attempt++ {
		request, requestErr := http.NewRequestWithContext(
			context.WithValue(context.Background(), nativeMediaRequestKey{}, true),
			http.MethodGet, "https://"+host+":"+port+"/video/index.m3u8", nil,
		)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			t.Fatalf("media request %d did not recover using verified DoH: %v", attempt+1, requestErr)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK || string(body) != "verified media playlist" {
			t.Fatalf("unexpected recovered media response: status=%d body=%q err=%v", response.StatusCode, body, readErr)
		}
	}

	if directAttempts.Load() != 1 || dohLookups.Load() != 1 || recoveredRequests.Load() != 2 {
		t.Fatalf("verified DoH route was not cached: system_dns=%d doh=%d recovered=%d",
			directAttempts.Load(), dohLookups.Load(), recoveredRequests.Load())
	}
	if base.TLSClientConfig == nil || base.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("DNS recovery must not disable normal TLS certificate verification")
	}
}
