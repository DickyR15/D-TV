package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type nativeCoverHTTPStatusError struct{ StatusCode int }

func (err *nativeCoverHTTPStatusError) Error() string {
	return fmt.Sprintf("海報伺服器回應 HTTP %d", err.StatusCode)
}

func nativeCoverFailureCooldown(err error) time.Duration {
	var statusErr *nativeCoverHTTPStatusError
	if errors.As(err, &statusErr) {
		switch statusErr.StatusCode {
		case 404, 410:
			return 30 * time.Minute
		case 401, 403:
			return 5 * time.Minute
		case 429:
			return 2 * time.Minute
		default:
			if statusErr.StatusCode >= 500 && statusErr.StatusCode <= 599 {
				return 90 * time.Second
			}
		}
	}
	return 90 * time.Second
}

func nativeCoverFailureKey(address, referer string) string {
	sum := sha256.Sum256([]byte(address + "\x00" + referer))
	return hex.EncodeToString(sum[:])
}

func (cache *nativeCoverCache) coverOnCooldown(address, referer string) bool {
	key := nativeCoverFailureKey(address, referer)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	until, found := cache.failures[key]
	if !found {
		return false
	}
	if time.Now().Before(until) {
		return true
	}
	delete(cache.failures, key)
	return false
}

func (cache *nativeCoverCache) noteCoverFailure(address, referer string, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	key := nativeCoverFailureKey(address, referer)
	now := time.Now()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.failures == nil {
		cache.failures = make(map[string]time.Time)
	}
	if len(cache.failures) >= 4096 {
		for candidate, until := range cache.failures {
			if !now.Before(until) {
				delete(cache.failures, candidate)
			}
		}
		if len(cache.failures) >= 4096 {
			var oldestKey string
			var oldest time.Time
			for candidate, until := range cache.failures {
				if oldestKey == "" || until.Before(oldest) {
					oldestKey, oldest = candidate, until
				}
			}
			delete(cache.failures, oldestKey)
		}
	}
	cache.failures[key] = now.Add(nativeCoverFailureCooldown(err))
}

func (cache *nativeCoverCache) clearCoverFailure(address, referer string) {
	key := nativeCoverFailureKey(address, referer)
	cache.mu.Lock()
	delete(cache.failures, key)
	cache.mu.Unlock()
}
