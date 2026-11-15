package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const dnsAlternateSubnet = "8.8.8.0/24"

type dnsCacheEntry struct {
	addresses []string
	expires   time.Time
}

type dnsResolver struct {
	client    *http.Client
	mu        sync.Mutex
	cache     map[string]dnsCacheEntry
	inFlight  map[string]chan struct{}
	endpoints []string
}

func newDNSResolver(transport *http.Transport) *dnsResolver {
	lookupTransport := transport.Clone()
	lookupTransport.TLSClientConfig = nil
	return &dnsResolver{
		client: &http.Client{Transport: lookupTransport, Timeout: 4 * time.Second},
		cache:  map[string]dnsCacheEntry{}, inFlight: map[string]chan struct{}{},
		endpoints: []string{
			"https://dns.alidns.com/resolve",
			"https://dns.google/resolve",
			"https://cloudflare-dns.com/dns-query",
		},
	}
}

func (resolver *dnsResolver) lookup(ctx context.Context, host string) ([]string, error) {
	entry, err := resolver.lookupEntry(ctx, host, "")
	return entry.addresses, err
}

func (resolver *dnsResolver) lookupEntry(ctx context.Context, host, subnet string) (dnsCacheEntry, error) {
	cacheKey := host + "|" + subnet
	for {
		resolver.mu.Lock()
		if entry, found := resolver.cache[cacheKey]; found && time.Now().Before(entry.expires) {
			resolver.mu.Unlock()
			return entry, nil
		}
		if pending := resolver.inFlight[cacheKey]; pending != nil {
			resolver.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return dnsCacheEntry{}, ctx.Err()
			}
		}
		resolver.inFlight[cacheKey] = make(chan struct{})
		resolver.mu.Unlock()
		break
	}
	entry, err := resolver.query(ctx, host, subnet)
	resolver.mu.Lock()
	if err == nil {
		resolver.cache[cacheKey] = entry
	}
	close(resolver.inFlight[cacheKey])
	delete(resolver.inFlight, cacheKey)
	resolver.mu.Unlock()
	return entry, err
}

func (resolver *dnsResolver) query(ctx context.Context, host, subnet string) (dnsCacheEntry, error) {
	lastErr := errors.New("DoH 未提供可用的公用 IP 位址")
	for _, endpoint := range resolver.endpoints {
		for _, recordType := range []string{"A", "AAAA"} {
			if err := ctx.Err(); err != nil {
				return dnsCacheEntry{}, err
			}
			// EDNS client subnet is IPv4-specific. Query AAAA without it.
			if recordType == "AAAA" && subnet != "" {
				continue
			}
			lookupURL, err := url.Parse(endpoint)
			if err != nil {
				return dnsCacheEntry{}, err
			}
			query := lookupURL.Query()
			query.Set("name", host)
			query.Set("type", recordType)
			if subnet != "" {
				query.Set("edns_client_subnet", subnet)
			}
			lookupURL.RawQuery = query.Encode()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL.String(), nil)
			if err != nil {
				return dnsCacheEntry{}, err
			}
			request.Header.Set("Accept", "application/dns-json")
			response, err := resolver.client.Do(request)
			if err != nil {
				lastErr = publicError(err)
				continue
			}
			var payload struct {
				Status int `json:"Status"`
				Answer []struct {
					Type int    `json:"type"`
					TTL  int    `json:"TTL"`
					Data string `json:"data"`
				} `json:"Answer"`
			}
			err = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&payload)
			response.Body.Close()
			if response.StatusCode != http.StatusOK || err != nil || payload.Status != 0 {
				lastErr = errors.New("DoH 服務沒有返回有效 DNS 記錄")
				continue
			}
			entry := dnsCacheEntry{}
			ttl := 300
			wantedType := 1
			if recordType == "AAAA" {
				wantedType = 28
			}
			var addressErr error
			for _, answer := range payload.Answer {
				if answer.Type != wantedType {
					continue
				}
				address := net.ParseIP(answer.Data)
				if address == nil || (recordType == "A" && address.To4() == nil) ||
					(recordType == "AAAA" && (address.To4() != nil || address.To16() == nil)) {
					addressErr = errors.New("DoH 回傳無效的 IP 位址")
					break
				}
				entry.addresses = append(entry.addresses, address.String())
				if answer.TTL < ttl {
					ttl = answer.TTL
				}
			}
			if addressErr == nil && len(entry.addresses) > 0 {
				addressErr = validateCDNAddresses(entry.addresses)
			}
			if addressErr != nil {
				lastErr = fmt.Errorf("DoH 未提供可用的公用 IP 位址：%w", addressErr)
				continue
			}
			if len(entry.addresses) == 0 {
				lastErr = fmt.Errorf("DoH 沒有 %s 記錄", recordType)
				continue
			}
			if ttl < 1 {
				ttl = 1
			}
			entry.expires = time.Now().Add(time.Duration(ttl) * time.Second)
			return entry, nil
		}
	}
	return dnsCacheEntry{}, lastErr
}
