package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type cdnRoute struct {
	address string
	expires time.Time
}

// hostProtocolFallbackTransport keeps a single incompatible upstream on a
// dedicated HTTP/1.1 transport without changing protocol negotiation for
// other providers.
type hostProtocolFallbackTransport struct {
	base     http.RoundTripper
	fallback http.RoundTripper
	host     string
}

func (transport *hostProtocolFallbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if strings.EqualFold(request.URL.Hostname(), transport.host) {
		return transport.fallback.RoundTrip(request)
	}
	return transport.base.RoundTrip(request)
}

func (transport *hostProtocolFallbackTransport) CloseIdleConnections() {
	if closer, ok := transport.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
	if closer, ok := transport.fallback.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func newHTTP1NoALPNTransport(base *http.Transport) *http.Transport {
	transport := base.Clone()
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if base.TLSClientConfig != nil {
		config = base.TLSClientConfig.Clone()
		if config.MinVersion < tls.VersionTLS12 {
			config.MinVersion = tls.VersionTLS12
		}
	}
	// Do not advertise ALPN at all: some older API gateways abort TLS when
	// the client offers h2/http1.1 but the gateway has no matching protocol.
	config.NextProtos = nil
	transport.TLSClientConfig = config
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	return transport
}

// hostDoHTransport retries only one HTTPS host through verified DoH.
// It preserves the request hostname for Host/SNI/certificate verification.
type hostDoHTransport struct {
	base *http.Transport
	resolver *dnsResolver
	router *proxyRouter
	host string
	mu sync.Mutex
	preferred cdnRoute
}

func (transport *hostDoHTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if !strings.EqualFold(request.URL.Hostname(), transport.host) || request.URL.Scheme != "https" {
		return transport.base.RoundTrip(request)
	}
	if transport.router != nil {
		proxy, proxyErr := transport.router.proxy(request)
		if proxyErr != nil || proxy != nil {
			return transport.base.RoundTrip(request)
		}
	}
	transport.mu.Lock()
	preferred := transport.preferred
	transport.mu.Unlock()
	if preferred.address != "" && time.Now().Before(preferred.expires) {
		response, err := transport.roundTripAddress(request, preferred.address)
		if err == nil { return response, nil }
		if request.Context().Err() != nil { return nil, request.Context().Err() }
	}
	response, err := transport.base.RoundTrip(request)
	if err == nil { return response, nil }
	if request.Context().Err() != nil { return nil, request.Context().Err() }
	if !isRecoverableHostDNSFailure(err) {
		return nil, err
	}
	entry, lookupErr := transport.resolver.lookupEntry(request.Context(), request.URL.Hostname(), "")
	if lookupErr != nil {
		return nil, fmt.Errorf("紅果設備註冊 DNS 解析失敗：%w", publicError(errors.Join(err, lookupErr)))
	}
	lastErr := err
	for _, address := range entry.addresses {
		response, retryErr := transport.roundTripAddress(request, address)
		if retryErr == nil {
			transport.mu.Lock()
			transport.preferred = cdnRoute{address: address, expires: entry.expires}
			transport.mu.Unlock()
			return response, nil
		}
		if request.Context().Err() != nil { return nil, request.Context().Err() }
		lastErr = retryErr
	}
	return nil, fmt.Errorf("紅果設備註冊 %s 連線失敗（已嘗試備用 DNS）: %w", request.URL.Hostname(), publicError(lastErr))
}

func isRecoverableHostDNSFailure(err error) bool {
	if err == nil {
		return false
	}
	if isUnknownAuthorityError(err) {
		// DNS-RPZ interception can point the original hostname at a block page.
		// Retry with public DoH, but keep the same hostname and TLS verification.
		return true
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

func (transport *hostDoHTransport) roundTripAddress(request *http.Request, address string) (*http.Response, error) {
	clone := transport.base.Clone()
	previousDial := transport.base.DialContext
	if previousDial == nil {
		previousDial = (&net.Dialer{Timeout: 30*time.Second, KeepAlive: 30*time.Second}).DialContext
	}
	clone.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		port := request.URL.Port()
		if port == "" {
			port = "443"
		}
		return previousDial(ctx, network, net.JoinHostPort(address, port))
	}
	attempt := request.Clone(request.Context())
	if request.Body != nil {
		if request.GetBody == nil {
			return nil, errors.New("紅果註冊請求不支援安全重試")
		}
		body, err := request.GetBody()
		if err != nil { return nil, err }
		attempt.Body = body
	}
	return clone.RoundTrip(attempt)
}

func (transport *hostDoHTransport) CloseIdleConnections() {
	transport.base.CloseIdleConnections()
	if transport.resolver != nil && transport.resolver.client != nil {
		transport.resolver.client.CloseIdleConnections()
	}
}

type cdnTransport struct {
	base                        *http.Transport
	resolver                    *dnsResolver
	fallbackOnUnknownAuthority  bool
	mu                          sync.Mutex
	transports                  map[string]*http.Transport
	preferred                   map[string]cdnRoute
}

func newCDNTransport(base *http.Transport, resolver *dnsResolver) *cdnTransport {
	return &cdnTransport{base: base, resolver: resolver, transports: map[string]*http.Transport{}, preferred: map[string]cdnRoute{}}
}

func (transport *cdnTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || (request.Method != http.MethodGet && request.Method != http.MethodHead) {
		return transport.base.RoundTrip(request)
	}

	protected := protectedCDNHost(request.URL.Hostname()) || request.Context().Value(nativeCoverNetworkKey{}) == true
	mediaRetry := transport.fallbackOnUnknownAuthority && request.Context().Value(nativeMediaRequestKey{}) == true
	if !protected && !mediaRetry {
		return transport.base.RoundTrip(request)
	}

	var proxyURL *url.URL
	var err error
	if transport.base.Proxy != nil {
		proxyURL, err = transport.base.Proxy(request)
		if err != nil {
			return nil, err
		}
	}
	routeKey := request.URL.Host
	if proxyURL != nil {
		routeKey += "|" + proxyURL.String()
	}

	transport.mu.Lock()
	preferred := transport.preferred[routeKey]
	transport.mu.Unlock()
	tried := map[string]bool{}
	var response *http.Response
	var lastErr error
	if preferred.address != "" && time.Now().Before(preferred.expires) {
		tried[preferred.address] = true
		response, err = transport.roundTripAddress(request, proxyURL, routeKey, preferred.address)
		if err == nil {
			return response, nil
		}
		lastErr = err
	} else {
		response, err = transport.base.RoundTrip(request)
		if err == nil {
			return response, nil
		}
		if !protected && !isUnknownAuthorityError(err) {
			return response, err
		}
		lastErr = err
	}
	if request.Context().Err() != nil {
		return nil, request.Context().Err()
	}

	// For media requests, a system resolver may return a DNS-RPZ landing IP
	// instead of the CDN address. Retry only after an unknown-authority TLS
	// failure, using public DoH results and the same normal certificate checks.
	// A successful DoH route is cached so later segments avoid the bad resolver.
	subnets := []string{dnsAlternateSubnet, ""}
	for _, subnet := range subnets {
		entry, lookupErr := transport.resolver.lookupEntry(request.Context(), request.URL.Hostname(), subnet)
		if lookupErr != nil {
			if request.Context().Err() != nil {
				return nil, request.Context().Err()
			}
			continue
		}
		for _, address := range entry.addresses {
			if tried[address] || len(tried) >= 4 {
				continue
			}
			tried[address] = true
			response, err = transport.roundTripAddress(request, proxyURL, routeKey, address)
			if err == nil {
				transport.mu.Lock()
				transport.preferred[routeKey] = cdnRoute{address: address, expires: entry.expires}
				transport.mu.Unlock()
				return response, nil
			}
			if request.Context().Err() != nil {
				return nil, request.Context().Err()
			}
			lastErr = err
		}
	}
	return nil, fmt.Errorf("資源 %s 連線失敗（已嘗試備援 DNS 位址）：%w", request.URL.Hostname(), publicError(lastErr))
}

func isUnknownAuthorityError(err error) bool {
	var authorityError x509.UnknownAuthorityError
	return errors.As(err, &authorityError)
}

func (transport *cdnTransport) roundTripAddress(request *http.Request, proxyURL *url.URL, routeKey, address string) (*http.Response, error) {
	pinned := transport.pinnedTransport(request.URL.Hostname(), proxyURL, routeKey, address)
	upstream := request.Clone(request.Context())
	upstream.Host = request.Host
	if upstream.Host == "" {
		upstream.Host = request.URL.Host
	}
	port := request.URL.Port()
	if port == "" {
		if request.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	// Always bracket IPv6 literals and include the destination port. The
	// original hostname remains in Host and TLS ServerName for virtual hosting.
	upstream.URL.Host = net.JoinHostPort(address, port)
	response, err := pinned.RoundTrip(upstream)
	if response != nil {
		response.Request = request
	}
	return response, err
}

func (transport *cdnTransport) pinnedTransport(host string, proxyURL *url.URL, routeKey, address string) *http.Transport {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	key := routeKey + "|" + address
	if existing := transport.transports[key]; existing != nil {
		return existing
	}
	pinned := transport.base.Clone()
	if pinned.TLSClientConfig == nil {
		pinned.TLSClientConfig = &tls.Config{}
	}
	pinned.TLSClientConfig.ServerName = host
	pinned.Proxy = http.ProxyURL(proxyURL)
	if proxyURL != nil && proxyURL.Scheme == "https" {
		plainProxy := *proxyURL
		plainProxy.Scheme = "http"
		if plainProxy.Port() == "" {
			plainProxy.Host = net.JoinHostPort(proxyURL.Hostname(), "443")
		}
		pinned.Proxy = http.ProxyURL(&plainProxy)
		pinned.DialContext = transport.tlsProxyDialer(proxyURL.Hostname())
	}
	if len(transport.transports) >= 32 {
		for expiredKey, expiredTransport := range transport.transports {
			expiredTransport.CloseIdleConnections()
			delete(transport.transports, expiredKey)
			break
		}
	}
	transport.transports[key] = pinned
	return pinned
}

func (transport *cdnTransport) tlsProxyDialer(host string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		dial := transport.base.DialContext
		if dial == nil {
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			dial = dialer.DialContext
		}
		connection, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		config := &tls.Config{}
		if transport.base.TLSClientConfig != nil {
			config = transport.base.TLSClientConfig.Clone()
		}
		config.ServerName = host
		config.NextProtos = []string{"http/1.1"}
		secured := tls.Client(connection, config)
		if transport.base.TLSHandshakeTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, transport.base.TLSHandshakeTimeout)
			defer cancel()
		}
		if err := secured.HandshakeContext(ctx); err != nil {
			connection.Close()
			return nil, err
		}
		return secured, nil
	}
}

func (transport *cdnTransport) CloseIdleConnections() {
	transport.base.CloseIdleConnections()
	transport.resolver.client.CloseIdleConnections()
	transport.mu.Lock()
	defer transport.mu.Unlock()
	for key, pinned := range transport.transports {
		pinned.CloseIdleConnections()
		delete(transport.transports, key)
	}
	transport.preferred = map[string]cdnRoute{}
}
