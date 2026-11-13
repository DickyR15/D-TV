package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const nativeCoverMaxBytes = 20 << 20
const nativeCoverCacheBytes = 256 << 20
const nativeCoverCacheEntries = 2000
const nativeCoverTTL = 30 * 24 * time.Hour

const nativeCoverDuoduoFetchBudget = 12 * time.Second
const nativeCoverDuoduoFallbackBudget = 12 * time.Second
const nativeCoverDuoduoProxyHedgeDelay = 650 * time.Millisecond

func nativeCoverFetchTimeout(source string) time.Duration {
	if source == sourceDuoduo {
		return nativeCoverDuoduoFetchBudget
	}
	return 25 * time.Second
}

func nativeCoverAlternativeTimeout(source, cacheClass string) time.Duration {
	if source == sourceDuoduo && cacheClass == "provider" {
		return nativeCoverDuoduoFallbackBudget
	}
	return 0
}
func nativeCoverAttemptTimeout(source, address string, cacheClass ...string) time.Duration {
	// External artwork (for example a Douban fallback) is not routed through
	// Duoduo's provider proxy and must retain the normal download window.
	if source != sourceDuoduo || len(cacheClass) > 0 && cacheClass[0] != "provider" {
		return 12 * time.Second
	}
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err == nil && strings.EqualFold(parsed.Hostname(), "wsrv.nl") {
		return 6 * time.Second
	}
	return 2500 * time.Millisecond
}

const nativeCoverRepairConcurrency = 4

type nativeCoverEntry struct {
	size      int64
	updatedAt time.Time
	usedAt    time.Time
}

type nativeCoverCall struct {
	done chan struct{}
	path string
	err  error
}

type nativeCoverCache struct {
	directory   string
	downloader  *Downloader
	mu          sync.Mutex
	entries     map[string]nativeCoverEntry
	pending     map[string]*nativeCoverCall
	failures    map[string]time.Time
	slots       chan struct{}
	repairSlots chan struct{}
	repairs     map[string]*nativeCoverRepair
	size        int64
	limit       int64
}

func newNativeCoverCache(directory string, downloader *Downloader) *nativeCoverCache {
	cache := &nativeCoverCache{
		directory: filepath.Join(directory, "covers-v1"), downloader: downloader,
		entries: map[string]nativeCoverEntry{}, pending: map[string]*nativeCoverCall{},
		failures: map[string]time.Time{},
		// Match the six-request Flutter scheduler for the first visible grid.
		slots: make(chan struct{}, 6), limit: nativeCoverCacheBytes,
		repairSlots: make(chan struct{}, nativeCoverRepairConcurrency), repairs: map[string]*nativeCoverRepair{},
	}
	files, _ := os.ReadDir(cache.directory)
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".decode-") && !file.IsDir() {
			if info, err := file.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
				_ = os.Remove(filepath.Join(cache.directory, file.Name()))
			}
		}
		key, valid := strings.CutSuffix(file.Name(), ".img")
		if !valid || len(key) != 64 || file.IsDir() {
			continue
		}
		if _, err := hex.DecodeString(key); err != nil {
			continue
		}
		info, err := file.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > nativeCoverMaxBytes {
			continue
		}
		cache.entries[key] = nativeCoverEntry{size: info.Size(), updatedAt: info.ModTime(), usedAt: info.ModTime()}
		cache.size += info.Size()
	}
	cache.loadCoverFailures()
	cache.prune("")
	return cache
}

func nativeCoverOriginReferer(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed == nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "movie.douban.com" || host == "www.douban.com" ||
		strings.HasSuffix(host, ".doubanio.com"):
		return "https://movie.douban.com/"
	case host == "bilibili.com" || strings.HasSuffix(host, ".bilibili.com") ||
		host == "hdslb.com" || strings.HasSuffix(host, ".hdslb.com"):
		return "https://www.bilibili.com/"
	case host == "bgm.tv" || strings.HasSuffix(host, ".bgm.tv"):
		return "https://bgm.tv/"
	case host == "k.sinaimg.cn" || strings.HasSuffix(host, ".sinaimg.cn"):
		return "https://news.sina.com.cn/"
	case host == "live.ykimg.com" || strings.HasSuffix(host, ".ykimg.com"):
		return "https://www.youku.com/"
	default:
		return ""
	}
}

func nativeCoverReferer(downloader *Downloader, source, address string) string {
	if referer := nativeCoverOriginReferer(address); referer != "" {
		return referer
	}
	if source == sourceCloudFront {
		if parsed, err := url.Parse(address); err == nil && (strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".zdmhyg.cn") || strings.EqualFold(parsed.Hostname(), "pic.tuafjz.cn")) {
			return downloader.providerBaseURL(sourceHuangguoAI) + "/"
		}
	}
	if source == sourceHuangdou {
		if parsed, err := url.Parse(address); err == nil && (strings.EqualFold(parsed.Hostname(), "tideember.cc") || strings.EqualFold(parsed.Hostname(), "xqjurgek.top")) {
			return parsed.Scheme + "://" + parsed.Host + "/home"
		}
		downloader.providerMu.Lock()
		host := downloader.providerHosts[source]
		downloader.providerMu.Unlock()
		if host == "" {
			host = downloader.providerBaseURL(source)
		}
		return host + "/home"
	}
	return downloader.providerBaseURL(source) + "/"
}

// Duoduo's image server can require the exact detail page as its Referer.
// The website shows posters at /play/{id}; a homepage-only Referer may be
// rejected even though the same image is visible from the official page.
func nativeCoverRefererForDrama(downloader *Downloader, drama nativeDrama, address string) string {
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	if source == sourceDuoduo {
		// Bilibili's CDN requires its own site Referer to avoid returning its
		// "image hotlink disabled" placeholder. Other original catalogue
		// images keep Duoduo's exact work page Referer; external fallback
		// images continue to use their own host-specific Referer below.
		if parsed, err := url.Parse(strings.TrimSpace(address)); err == nil {
			host := strings.ToLower(parsed.Hostname())
			if host == "bilibili.com" || strings.HasSuffix(host, ".bilibili.com") ||
				host == "hdslb.com" || strings.HasSuffix(host, ".hdslb.com") {
				return "https://www.bilibili.com/"
			}
		}
		if _, sourceID, valid := splitProviderDramaID(drama.ID); valid && webProviderNumericID.MatchString(sourceID) {
			base := strings.TrimRight(downloader.providerBaseURL(source), "/")
			if base == "" {
				base = strings.TrimRight(duoduoPublicEntryURL, "/")
			}
			return base + "/play/" + url.PathEscape(sourceID)
		}
	}
	return nativeCoverReferer(downloader, source, address)
}

type duoduoPosterAttempt struct {
	address string
	referer string
}

// recordDuoduoPosterAttempt captures origin-vs-edge latency without writing
// signed image URLs, query strings, or Referer values into exported logs.
func (cache *nativeCoverCache) recordDuoduoPosterAttempt(
	drama nativeDrama,
	attempt duoduoPosterAttempt,
	number int,
	started time.Time,
	data []byte,
	err error,
) {
	if cache == nil || cache.downloader == nil {
		return
	}
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	if source != sourceDuoduo {
		return
	}
	parsed, parseErr := url.Parse(strings.TrimSpace(attempt.address))
	host := ""
	if parseErr == nil {
		host = strings.ToLower(parsed.Hostname())
	}
	strategy := "origin"
	if host == "wsrv.nl" {
		strategy = "cloudflare_edge"
	}
	result := "success"
	level := "info"
	if errors.Is(err, context.Canceled) {
		// A slower request cancelled after the competing route returned a
		// verified image is a measured hedge loss, not a host failure.
		result = "hedge_cancelled"
	} else if err != nil {
		level = "warn"
		result = nativeStreamError(err)
		if result == "io_error" {
			message := strings.ToLower(err.Error())
			if strings.Contains(message, "status") || strings.Contains(message, "content-type") ||
				strings.Contains(message, "image") {
				result = "http_or_invalid_image"
			}
		}
	}
	cache.downloader.recordDiagnostic(diagnosticEvent{
		Level: level, Event: "cover.attempt", Source: source, DramaID: drama.ID,
		Host: host, Attempt: number,
		Message: fmt.Sprintf("strategy=%s elapsed_ms=%d bytes=%d result=%s",
			strategy, time.Since(started).Milliseconds(), len(data), result),
	})
}

// Baidu's gimg endpoint sometimes wraps a real poster URL inside its path,
// for example /gimg/app=2001&fmt=jpeg&src=https%3A%2F%2Fi0.hdslb.com%2F...png.
// Keep the original wrapper as the first choice, but expose the embedded
// source as a separate fallback so a broken proxy does not hide the real file.
func duoduoBaiduPosterSourceURL(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || !validNativeCoverURL(parsed) {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasPrefix(host, "gimg") || !strings.HasSuffix(host, ".baidu.com") {
		return ""
	}
	values := url.Values{}
	if parsed.RawQuery != "" {
		queryValues, _ := url.ParseQuery(parsed.RawQuery)
		for key, entries := range queryValues {
			for _, value := range entries {
				values.Add(key, value)
			}
		}
	}
	pathValues, pathErr := url.ParseQuery(strings.TrimPrefix(parsed.Path, "/gimg/"))
	if pathErr == nil {
		for key, entries := range pathValues {
			for _, value := range entries {
				values.Add(key, value)
			}
		}
	}
	if values == nil {
		return ""
	}
	candidate := strings.TrimSpace(values.Get("src"))
	if strings.HasPrefix(candidate, "//") {
		candidate = "https:" + candidate
	} else if candidate != "" && !strings.Contains(candidate, "://") {
		// Baidu's gimg wrapper commonly stores a bare hostname in src,
		// e.g. src=img.example.com/path/poster.webp.
		candidate = "https://" + strings.TrimLeft(candidate, "/")
	}
	source, err := url.Parse(candidate)
	if err != nil || !validNativeCoverURL(source) || strings.EqualFold(source.Hostname(), host) {
		return ""
	}
	return source.String()
}

// Only public DNS hostnames are sent to the third-party image proxy. This
// avoids turning poster fallback into a proxy for localhost or literal IPs.
// The full escaped path and query are retained because poster URLs can be
// signed or can themselves be image-proxy wrappers.
func duoduoPosterProxyURL(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || !validNativeCoverURL(parsed) {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || !strings.Contains(host, ".") || strings.Contains(host, "..") ||
		net.ParseIP(host) != nil || host == "localhost" ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") {
		return ""
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return ""
	}
	parsed.Fragment = ""
	target := strings.TrimPrefix(parsed.String(), parsed.Scheme+"://")

	// wsrv.nl resizes and caches images behind Cloudflare's global network.
	// Keep a portrait-safe bounding box and return WebP at poster-card size:
	// this avoids downloading multi-megapixel originals just to render a 440px
	// catalogue tile. The original provider URL remains the first attempt.
	proxy, err := url.Parse("https://wsrv.nl/")
	if err != nil {
		return ""
	}
	query := proxy.Query()
	query.Set("url", target)
	query.Set("w", "440")
	query.Set("h", "660")
	query.Set("fit", "inside")
	query.Set("output", "webp")
	query.Set("q", "80")
	proxy.RawQuery = query.Encode()
	return proxy.String()
}

func duoduoPosterAttemptsForDrama(address string, downloader *Downloader, drama nativeDrama) []duoduoPosterAttempt {
	referer := nativeCoverRefererForDrama(downloader, drama, address)
	attempts := duoduoPosterAttempts(address, referer)

	// Keep the former root-page Referer as a compatibility retry for image
	// mirrors that only allow the site's origin-level hotlink policy.
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	rootReferer := nativeCoverReferer(downloader, source, address)
	if rootReferer != "" && rootReferer != referer {
		attempts = append(attempts, duoduoPosterAttempts(address, rootReferer)... )
	}

	seen := make(map[string]bool, len(attempts))
	unique := make([]duoduoPosterAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		key := attempt.address + "\x00" + attempt.referer
		if attempt.address == "" || seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, attempt)
	}
	return unique
}

// downloadDuoduoPosterPair gives the origin a short head start, then races its
// resized wsrv.nl edge copy against it. The first fully validated image wins.
// Attempt timings are recorded on the user's own network, so Taiwan-specific
// latency naturally influences every poster without hard-coding a region.
func (cache *nativeCoverCache) downloadDuoduoPosterPair(
	ctx context.Context,
	drama nativeDrama,
	attempts []duoduoPosterAttempt,
) ([]byte, int, error) {
	if cache == nil || len(attempts) < 2 {
		return nil, 0, nil
	}
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	proxyURL, err := url.Parse(strings.TrimSpace(attempts[1].address))
	if source != sourceDuoduo || err != nil || !strings.EqualFold(proxyURL.Hostname(), "wsrv.nl") {
		return nil, 0, nil
	}

	eligible := [2]bool{
		!cache.coverOnCooldown(attempts[0].address, attempts[0].referer),
		!cache.coverOnCooldown(attempts[1].address, attempts[1].referer),
	}
	nextIndex := 2
	if !eligible[0] && !eligible[1] {
		return nil, nextIndex, errors.New("多多海報原始網址與代理網址均在冷卻中")
	}

	type attemptResult struct {
		data []byte
		err  error
	}
	pairCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan attemptResult, 2)
	launched := [2]bool{}
	launchedCount := 0
	completed := 0
	launch := func(index int) {
		if index < 0 || index > 1 || !eligible[index] || launched[index] {
			return
		}
		launched[index] = true
		launchedCount++
		attempt := attempts[index]
		go func() {
			started := time.Now()
			attemptCtx, attemptCancel := context.WithTimeout(
				pairCtx,
				nativeCoverAttemptTimeout(source, attempt.address, "provider"),
			)
			data, requestErr := cache.download(attemptCtx, attempt.address, attempt.referer)
			attemptCancel()
			cache.recordDuoduoPosterAttempt(drama, attempt, index+1, started, data, requestErr)
			if requestErr == nil {
				cache.clearCoverFailure(attempt.address, attempt.referer)
			} else if !errors.Is(requestErr, context.Canceled) {
				cache.noteCoverFailure(attempt.address, attempt.referer, requestErr)
			}
			results <- attemptResult{data: data, err: requestErr}
		}()
	}

	if eligible[0] {
		launch(0)
	} else {
		// The origin is already cooling down; do not make the user wait.
		launch(1)
	}
	var hedgeTimer *time.Timer
	var hedge <-chan time.Time
	if eligible[0] && eligible[1] {
		hedgeTimer = time.NewTimer(nativeCoverDuoduoProxyHedgeDelay)
		hedge = hedgeTimer.C
	}
	stopHedge := func() {
		if hedgeTimer != nil {
			hedgeTimer.Stop()
			hedgeTimer = nil
		}
		hedge = nil
	}

	var failures []error
	for {
		if completed == launchedCount && (!eligible[1] || launched[1]) {
			stopHedge()
			if len(failures) == 0 {
				return nil, nextIndex, errors.New("多多海報下載失敗")
			}
			return nil, nextIndex, errors.Join(failures...)
		}
		select {
		case <-ctx.Done():
			stopHedge()
			return nil, nextIndex, ctx.Err()
		case <-hedge:
			launch(1)
			hedge = nil
		case result := <-results:
			completed++
			if result.err == nil {
				stopHedge()
				cancel()
				return result.data, nextIndex, nil
			}
			failures = append(failures, result.err)
			// A fast origin failure should trigger the edge attempt immediately;
			// otherwise the edge starts after the short hedge window.
			if !launched[1] && eligible[1] {
				stopHedge()
				launch(1)
			}
		}
	}
}

func duoduoPosterAttempts(address, referer string) []duoduoPosterAttempt {
	var attempts []duoduoPosterAttempt
	seen := map[string]bool{}
	add := func(candidate, candidateReferer string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		identity := candidate + "\x00" + candidateReferer
		if seen[identity] {
			return
		}
		seen[identity] = true
		attempts = append(attempts, duoduoPosterAttempt{
			address: candidate,
			referer: firstNonEmpty(candidateReferer, referer),
		})
	}
	add(address, referer)
	// Give the original poster a short direct attempt, then try the globally
	// cached, resized WebP variant before slower legacy Referer permutations.
	if proxyURL := duoduoPosterProxyURL(address); proxyURL != "" {
		add(proxyURL, "https://wsrv.nl/")
	}
	if sourceURL := duoduoBaiduPosterSourceURL(address); sourceURL != "" {
		add(address, "https://www.baidu.com/")
		add(sourceURL, "https://www.bilibili.com/")
		add(sourceURL, "https://www.baidu.com/")
		if proxyURL := duoduoPosterProxyURL(sourceURL); proxyURL != "" {
			add(proxyURL, "https://wsrv.nl/")
		}
	}
	return attempts
}

func validNativeCoverURL(address *url.URL) bool {
	if address == nil ||
		(address.Scheme != "https" && address.Scheme != "http") ||
		address.Hostname() == "" || address.User != nil {
		return false
	}
	// api.zxki.cn exposes a paid image hotlink-bypass API that requires a
	// private API key. It is not a public image CDN and must not be treated as
	// an ordinary poster URL or called without credentials.
	if strings.EqualFold(address.Hostname(), "api.zxki.cn") {
		return false
	}
	return true
}

func (cache *nativeCoverCache) loadAddress(ctx context.Context, drama nativeDrama, force bool) (string, error) {
	drama.Cover = repairLegacyCoverURL(drama)
	address, err := url.Parse(drama.Cover)
	if err != nil || !validNativeCoverURL(address) {
		return "", errors.New("海報網址無效")
	}
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	referer := nativeCoverRefererForDrama(cache.downloader, drama, drama.Cover)
	if force {
		// An explicit retry must bypass a previous failure cooldown. For Duoduo,
		// clear every equivalent URL/Referer attempt, not just the first URL.
		attempts := []duoduoPosterAttempt{{address: drama.Cover, referer: referer}}
		if source == sourceDuoduo {
			attempts = duoduoPosterAttemptsForDrama(drama.Cover, cache.downloader, drama)
		}
		for _, attempt := range attempts {
			cache.clearCoverFailure(attempt.address, attempt.referer)
		}
	}
	digest := sha256.Sum256([]byte(source + "\x00" + drama.Cover + "\x00" + referer))
	key := hex.EncodeToString(digest[:])
	path := filepath.Join(cache.directory, key+".img")
	cache.mu.Lock()
	fallback := ""
	if entry, found := cache.entries[key]; found {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() == entry.size {
			entry.usedAt = time.Now()
			cache.entries[key] = entry
			age := time.Since(entry.updatedAt)
			if !force && age >= 0 && age < nativeCoverTTL {
				cache.mu.Unlock()
				return path, nil
			}
			if !force {
				fallback = path
			}
		} else {
			delete(cache.entries, key)
			cache.size -= entry.size
		}
	}
	call := cache.pending[key]
	if call == nil {
		if len(cache.pending) >= 256 {
			cache.mu.Unlock()
			return "", errors.New("海报正在加载，请稍后重试")
		}
		call = &nativeCoverCall{done: make(chan struct{})}
		cache.pending[key] = call
		go cache.fetch(key, drama, referer, fallback, call)
	}
	cache.mu.Unlock()
	// If an expired but verified image exists, show it immediately. The
	// refresh goroutine above keeps running and replaces the disk cache in the
	// background; users never wait for a slow origin just to see a poster.
	if fallback != "" && !force {
		return fallback, nil
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-call.done:
		return call.path, call.err
	}
}

func (cache *nativeCoverCache) fetch(key string, drama nativeDrama, referer, fallback string, call *nativeCoverCall) {
	coverSource := canonicalProviderSource(drama.Source)
	if coverSource == "" {
		coverSource = sourceFromDramaID(drama.ID)
	}
	// Duoduo image mirrors can spend a long time on dead hosts or Referer
	// retries. Bound the initial poster path so the repair pipeline can start
	// sooner; other providers retain the existing 25-second budget.
	ctx, cancel := context.WithTimeout(context.Background(), nativeCoverFetchTimeout(coverSource))
	defer cancel()
	var data []byte
	var err error
	select {
	case cache.slots <- struct{}{}:
		attempts := []duoduoPosterAttempt{{address: drama.Cover, referer: referer}}
		if coverSource == sourceDuoduo {
			attempts = duoduoPosterAttemptsForDrama(drama.Cover, cache.downloader, drama)
		}
		startIndex := 0
		if coverSource == sourceDuoduo {
			data, startIndex, err = cache.downloadDuoduoPosterPair(ctx, drama, attempts)
		}
		if startIndex == 0 || err != nil {
			for index := startIndex; index < len(attempts); index++ {
				if ctx.Err() != nil {
					err = ctx.Err()
					break
				}
				attempt := attempts[index]
				if cache.coverOnCooldown(attempt.address, attempt.referer) {
					err = errors.New("海報網址暫時冷卻，略過重複請求")
					continue
				}
				started := time.Now()
				attemptCtx, attemptCancel := context.WithTimeout(
					ctx, nativeCoverAttemptTimeout(coverSource, attempt.address),
				)
				data, err = cache.download(attemptCtx, attempt.address, attempt.referer)
				attemptCancel()
				cache.recordDuoduoPosterAttempt(drama, attempt, index+1, started, data, err)
				if err == nil {
					cache.clearCoverFailure(attempt.address, attempt.referer)
					break
				}
				cache.noteCoverFailure(attempt.address, attempt.referer, err)
			}
		}

		// Provider-detail alternatives are checked once by load() -> repair().
		// Do not repeat the same detail API request here after the primary
		// catalogue poster fails; that used to add one full network round trip
		// before the actual fallback pipeline could even start.

		<-cache.slots
	case <-ctx.Done():
		err = ctx.Err()
	}
	path := filepath.Join(cache.directory, key+".img")
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err == nil {
		err = writeNativeCacheFile(path, data)
		if err == nil {
			cache.size -= cache.entries[key].size
			now := time.Now()
			cache.entries[key] = nativeCoverEntry{size: int64(len(data)), updatedAt: now, usedAt: now}
			cache.size += int64(len(data))
			cache.prune(key)
			call.path = path
		}
	}
	if err != nil && fallback != "" && !isPermanentMissingCover(err) {
		if _, statErr := os.Stat(fallback); statErr == nil {
			call.path, err = fallback, nil
		}
	}
	if isPermanentMissingCover(err) {
		// A confirmed 404/410 means this exact poster URL has been removed.
		// Do not keep showing an expired image from the same URL's cache.
		_ = os.Remove(path)
		cache.size -= cache.entries[key].size
		delete(cache.entries, key)
	}
	call.err = err
	delete(cache.pending, key)
	close(call.done)
	if err != nil {
		cache.downloader.recordDiagnostic(diagnosticEvent{Event: "cover_failed", Source: drama.Source, DramaID: drama.ID, Message: err.Error()})
	}
}

func (cache *nativeCoverCache) download(ctx context.Context, address, referer string) ([]byte, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(context.WithValue(attemptCtx, nativeCoverNetworkKey{}, true), http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Referer", referer)
	request.Header.Set("Accept", "image/webp,image/jpeg,image/png,image/gif,*/*;q=0.5")
	request.Header.Set("Sec-Fetch-Mode", "no-cors")
	request.Header.Set("Sec-Fetch-Dest", "image")
	client := *cache.downloader.client
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !validNativeCoverURL(request.URL) {
			return errors.New("海報重新導向位址無效")
		}
		request.Header.Set("Referer", referer)
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return nil, &nativeCoverHTTPStatusError{StatusCode: response.StatusCode}
	}
	if !nativeCoverResponseComplete(response) {
		return nil, &nativeCoverHTTPStatusError{StatusCode: response.StatusCode}
	}
	if response.ContentLength > nativeCoverMaxBytes {
		return nil, errors.New("海報檔案過大")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, nativeCoverMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > nativeCoverMaxBytes {
		return nil, errors.New("海報檔案過大")
	}
	data = nativeDecodeCover(data)
	if !nativeIsCoverImage(data) {
		return nil, errors.New("站源未返回有效海报")
	}
	return data, nil
}

func nativeCoverResponseComplete(response *http.Response) bool {
	if response == nil {
		return false
	}
	if response.StatusCode == http.StatusOK {
		return true
	}
	if response.StatusCode != http.StatusPartialContent {
		return false
	}
	value := strings.TrimSpace(response.Header.Get("Content-Range"))
	if !strings.HasPrefix(strings.ToLower(value), "bytes ") {
		return false
	}
	rangeAndTotal := strings.TrimSpace(value[len("bytes "):])
	byteRange, totalValue, found := strings.Cut(rangeAndTotal, "/")
	if !found {
		return false
	}
	startValue, endValue, found := strings.Cut(strings.TrimSpace(byteRange), "-")
	if !found {
		return false
	}
	start, startErr := strconv.ParseInt(strings.TrimSpace(startValue), 10, 64)
	end, endErr := strconv.ParseInt(strings.TrimSpace(endValue), 10, 64)
	total, totalErr := strconv.ParseInt(strings.TrimSpace(totalValue), 10, 64)
	return startErr == nil && endErr == nil && totalErr == nil &&
		start == 0 && total > 0 && end >= 0 && end+1 == total
}

func nativeDecodeCover(data []byte) []byte {
	if nativeIsCoverImage(data) {
		return data
	}
	// Some poster CDNs return a gzip-wrapped image body while still labelling
	// it as image/jpeg or image/png. Go only transparently decompresses HTTP
	// responses when Content-Encoding is set; unwrap a raw gzip body as a
	// bounded fallback, then validate the actual image signature.
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err == nil {
			decoded, readErr := io.ReadAll(io.LimitReader(reader, nativeCoverMaxBytes+1))
			closeErr := reader.Close()
			if readErr == nil && closeErr == nil &&
				len(decoded) > 0 && len(decoded) <= nativeCoverMaxBytes &&
				nativeIsCoverImage(decoded) {
				return decoded
			}
		}
	}
	encrypted := data
	if bytes.HasPrefix(encrypted, []byte("Salted__")) && len(encrypted) > 16 {
		encrypted = encrypted[16:]
	}
	if len(encrypted) > 0 {
		block, err := aes.NewCipher([]byte("f5d965df75336270"))
		if err == nil {
			plain := make([]byte, (len(encrypted)+aes.BlockSize-1)/aes.BlockSize*aes.BlockSize)
			copy(plain, encrypted)
			cipher.NewCBCDecrypter(block, []byte("97b60394abc2fbe1")).CryptBlocks(plain, plain)
			plain = plain[:len(encrypted)]
			if unpadded, err := pkcs7Unpad(plain, aes.BlockSize); err == nil {
				plain = unpadded
			}
			if nativeIsCoverImage(plain) {
				return plain
			}
		}
	}
	plain := append([]byte(nil), data...)
	key := []byte("2019ysapp7527")
	for index := 0; index < min(100, len(plain)); index++ {
		plain[index] ^= key[index%len(key)]
	}
	if nativeIsCoverImage(plain) {
		return plain
	}
	return data
}

func nativeIsCoverImage(data []byte) bool {
	if isHEICImage(data) || isAVIFImage(data) {
		return true
	}
	if bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) ||
		bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) || bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")) {
		return true
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return true
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		return true
	}
	if len(data) >= 16 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "avif", "avis", "heic", "heix", "hevc", "hevx", "mif1":
			return true
		}
	}
	return false
}

func (cache *nativeCoverCache) prune(keep string) {
	if cache.size <= cache.limit && len(cache.entries) <= nativeCoverCacheEntries {
		return
	}
	keys := make([]string, 0, len(cache.entries))
	for key := range cache.entries {
		if key != keep {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return cache.entries[keys[i]].usedAt.Before(cache.entries[keys[j]].usedAt) })
	for _, key := range keys {
		if cache.size <= cache.limit && len(cache.entries) <= nativeCoverCacheEntries {
			break
		}
		if err := os.Remove(filepath.Join(cache.directory, key+".img")); err == nil || errors.Is(err, os.ErrNotExist) {
			cache.size -= cache.entries[key].size
			delete(cache.entries, key)
		}
	}
}
