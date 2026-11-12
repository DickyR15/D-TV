package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type hongguoDoHFixtureRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip hongguoDoHFixtureRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestHongguoRegisterDoHRetriesTLSDNSInterceptionPreservingPostIdentity(t *testing.T) {
	const host = "example.com"
	const fallbackIP = "93.184.216.34"
	const payload = "{\"registration\":\"synthetic\"}"
	var directAttempts, dohLookups, recoveredRequests atomic.Int32

	goodServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != host || request.TLS == nil || request.TLS.ServerName != host {
			t.Errorf("DoH retry changed Host or TLS SNI: host=%q tls=%+v", request.Host, request.TLS)
		}
		if request.Method != http.MethodPost || request.URL.Path != "/service/2/device_register/" {
			t.Errorf("unexpected registration request: %s %s", request.Method, request.URL)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil || string(body) != payload {
			t.Errorf("registration body changed: %q err=%v", body, err)
		}
		recoveredRequests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"device_id_str":"1001","install_id_str":"10019"}`)
	}))
	defer goodServer.Close()

	badServer := newUntrustedTLSServer(t, host, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Error("untrusted DNS-RPZ endpoint must fail TLS before the HTTP handler runs")
	}))
	defer badServer.Close()

	trustedRoots := x509.NewCertPool()
	trustedRoots.AddCert(goodServer.Certificate())
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: trustedRoots}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		switch address {
		case host + ":443":
			directAttempts.Add(1)
			return (&net.Dialer{}).DialContext(ctx, network, badServer.Listener.Addr().String())
		case fallbackIP + ":443":
			return (&net.Dialer{}).DialContext(ctx, network, goodServer.Listener.Addr().String())
		default:
			t.Errorf("unexpected dial target %q", address)
			return nil, net.ErrClosed
		}
	}

	resolver := newDNSResolver(base)
	resolver.endpoints = []string{"https://dns.example.test/resolve"}
	resolver.client = &http.Client{Transport: hongguoDoHFixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		dohLookups.Add(1)
		if request.URL.Query().Get("name") != host || request.URL.Query().Get("type") != "A" {
			t.Errorf("unexpected DoH query %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status: "200 OK",
			Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"Status":0,"Answer":[{"type":1,"TTL":120,"data":"93.184.216.34"}]}`)),
			Request: request,
		}, nil
	})}
	transport := &hostDoHTransport{base: base, resolver: resolver, host: host}
	defer transport.CloseIdleConnections()

	request, err := http.NewRequest(http.MethodPost, "https://"+host+"/service/2/device_register/", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatalf("registration did not recover with verified DoH: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "1001") {
		t.Fatalf("unexpected recovered registration response: status=%d body=%q err=%v", response.StatusCode, body, readErr)
	}
	if directAttempts.Load() != 1 || dohLookups.Load() != 1 || recoveredRequests.Load() != 1 {
		t.Fatalf("unexpected retry counts: direct=%d doh=%d recovered=%d", directAttempts.Load(), dohLookups.Load(), recoveredRequests.Load())
	}
}

func TestHongguoRegisterDoHDoesNotRetryHTTPRejection(t *testing.T) {
	const host = "example.com"
	var dohLookups atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "registration rejected", http.StatusForbidden)
	}))
	defer server.Close()

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != host+":443" {
			return nil, net.ErrClosed
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	resolver := newDNSResolver(base)
	resolver.client = &http.Client{Transport: hongguoDoHFixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		dohLookups.Add(1)
		return nil, io.EOF
	})}
	transport := &hostDoHTransport{base: base, resolver: resolver, host: host}
	defer transport.CloseIdleConnections()

	request, _ := http.NewRequest(http.MethodPost, "https://"+host+"/service/2/device_register/", strings.NewReader("{}"))
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden || dohLookups.Load() != 0 {
		t.Fatalf("HTTP rejection incorrectly used DoH: status=%d lookups=%d", response.StatusCode, dohLookups.Load())
	}
}
