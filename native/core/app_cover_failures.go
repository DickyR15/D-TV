package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const nativeCoverFailureCacheFile = "failures-v1.json"
const nativeCoverFailureCacheMaxBytes = 1 << 20

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

func (cache *nativeCoverCache) loadCoverFailures() {
	if cache == nil || cache.directory == "" {
		return
	}
	file, err := os.Open(filepath.Join(cache.directory, nativeCoverFailureCacheFile))
	if err != nil {
		return
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, nativeCoverFailureCacheMaxBytes+1))
	if err != nil || len(body) == 0 || len(body) > nativeCoverFailureCacheMaxBytes {
		return
	}
	var saved map[string]time.Time
	if json.Unmarshal(body, &saved) != nil {
		return
	}
	now := time.Now()
	for key, until := range saved {
		if len(key) != 64 || !until.After(now) || until.After(now.Add(31*24*time.Hour)) {
			continue
		}
		if _, err := hex.DecodeString(key); err == nil {
			cache.failures[key] = until
		}
	}
}

// persistCoverFailuresLocked stores only short-lived URL+Referer hashes and
// expiry timestamps. It never writes the original poster URLs or credentials.
func (cache *nativeCoverCache) persistCoverFailuresLocked() {
	if cache == nil || cache.directory == "" {
		return
	}
	now := time.Now()
	for key, until := range cache.failures {
		if !until.After(now) {
			delete(cache.failures, key)
		}
	}
	path := filepath.Join(cache.directory, nativeCoverFailureCacheFile)
	if len(cache.failures) == 0 {
		_ = os.Remove(path)
		return
	}
	body, err := json.Marshal(cache.failures)
	if err != nil || len(body) > nativeCoverFailureCacheMaxBytes {
		return
	}
	if os.MkdirAll(cache.directory, 0700) != nil {
		return
	}
	_ = nativeDownloadWrite(path, body)
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
	cache.persistCoverFailuresLocked()
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
	cache.persistCoverFailuresLocked()
}

func (cache *nativeCoverCache) clearCoverFailure(address, referer string) {
	key := nativeCoverFailureKey(address, referer)
	cache.mu.Lock()
	if _, found := cache.failures[key]; found {
		delete(cache.failures, key)
		cache.persistCoverFailuresLocked()
	}
	cache.mu.Unlock()
}
