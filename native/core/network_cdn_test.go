package core

import (
	"context"
	"crypto/tls"
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
	for _, addresses := range [][]string{{"127.0.0.1"}, {"198.18.0.1"}, {"93.184.216.34", "10.0.0.1"}} {
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
