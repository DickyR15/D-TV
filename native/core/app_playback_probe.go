package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const nativePlaybackRouteProbeTimeout = 4 * time.Second
const nativePlaybackRouteProbeLimit = 4

type nativePlaybackRouteProbeResult struct {
	index   int
	host    string
	latency time.Duration
	status  int
	bytes   int
	err     error
}

// Put same-quality CDN fallbacks on distinct hosts before retrying a slow origin.
func diversifyNativePlaybackRoutesByHost(routes []providerMedia) []providerMedia {
	seen := make(map[string]bool, len(routes))
	unique := make([]providerMedia, 0, len(routes))
	repeated := make([]providerMedia, 0, len(routes))
	for _, media := range routes {
		host := ""
		if parsed, err := url.Parse(media.URL); err == nil {
			host = strings.ToLower(parsed.Host)
		}
		if host == "" || seen[host] {
			repeated = append(repeated, media)
			continue
		}
		seen[host] = true
		unique = append(unique, media)
	}
	return append(unique, repeated...)
}

// rankNativePlaybackRoutes performs a small, concurrent HTTP probe of the
// highest-quality alternatives before playback starts. It only reorders
// candidates within that quality, preserving quality preference and fallback
// availability. This measures media response, not ICMP reachability.
func (engine *nativeEngine) rankNativePlaybackRoutes(
	ctx context.Context,
	choice nativePlaybackChoice,
	task Task,
) nativePlaybackChoice {
	if engine.downloader == nil || engine.downloader.client == nil || len(choice.media) < 2 {
		return choice
	}

	bestQuality := choice.media[0].Quality
	topQualityCount := 0
	for _, media := range choice.media {
		if media.Quality != bestQuality {
			break
		}
		topQualityCount++
	}
	if topQualityCount < 2 {
		return choice
	}

	// Keep recently failed CDN hosts behind unpenalized hosts even after a
	// fresh online resolve returns the same set of signed URLs.
	lowerQuality := append([]providerMedia(nil), choice.media[topQualityCount:]...)
	topRoutes := diversifyNativePlaybackRoutesByHost(choice.media[:topQualityCount])
	healthyRoutes := make([]providerMedia, 0, len(topRoutes))
	coolingRoutes := make([]providerMedia, 0, len(topRoutes))
	for _, media := range topRoutes {
		if engine.downloader.nativeStreamHostCoolingDown(media.URL) {
			coolingRoutes = append(coolingRoutes, media)
		} else {
			healthyRoutes = append(healthyRoutes, media)
		}
	}
	choice.media = append(append(healthyRoutes, coolingRoutes...), lowerQuality...)
	probeCount := min(len(healthyRoutes), nativePlaybackRouteProbeLimit)
	if probeCount < 2 {
		probeCount = min(topQualityCount, nativePlaybackRouteProbeLimit)
	}

	wasCooling := make(map[int]bool, probeCount)
	for index := 0; index < probeCount; index++ {
		wasCooling[index] = engine.downloader.nativeStreamHostCoolingDown(choice.media[index].URL)
	}

	resultsChannel := make(chan nativePlaybackRouteProbeResult, probeCount)
	for index := 0; index < probeCount; index++ {
		index := index
		media := choice.media[index]
		go func() {
			latency, status, size, host, err := engine.downloader.probeNativePlaybackRoute(ctx, media)
			resultsChannel <- nativePlaybackRouteProbeResult{
				index: index, host: host, latency: latency, status: status, bytes: size, err: err,
			}
		}()
	}
	results := make([]nativePlaybackRouteProbeResult, 0, probeCount)
	resultsByIndex := make(map[int]nativePlaybackRouteProbeResult, probeCount)
	for range probeCount {
		result := <-resultsChannel
		results = append(results, result)
		resultsByIndex[result.index] = result
		if result.err == nil {
			// A successful media probe means this host has recovered.
			engine.downloader.clearNativeStreamHostFailure(choice.media[result.index].URL)
			wasCooling[result.index] = false
			continue
		}
		reason := nativeStreamError(result.err)
		if result.status == http.StatusTooManyRequests || result.status >= http.StatusInternalServerError {
			reason = "upstream_http"
		}
		engine.downloader.rememberNativeStreamHostFailure(choice.media[result.index].URL, reason)
		wasCooling[result.index] = engine.downloader.nativeStreamHostCoolingDown(choice.media[result.index].URL)
	}

	// Rank the entire preferred-quality tier, not just the routes we probed.
	// A candidate that just timed out must not immediately remain first; an
	// untested alternative is safer to try next. Never move a lower quality up.
	rankedPosition := make(map[int]int, topQualityCount)
	if ctx.Err() == nil {
		order := make([]int, topQualityCount)
		for index := range order {
			order[index] = index
		}
		routeClass := func(index int) int {
			result, probed := resultsByIndex[index]
			if probed && result.err == nil {
				return 0 // proven responsive
			}
			if !probed {
				return 1 // not yet tested; prefer over a known failure
			}
			if wasCooling[index] {
				return 3 // transient connection/transport failure
			}
			return 2 // non-transient response failure, such as an HTTP denial
		}
		sort.SliceStable(order, func(i, j int) bool {
			leftIndex, rightIndex := order[i], order[j]
			leftClass, rightClass := routeClass(leftIndex), routeClass(rightIndex)
			if leftClass != rightClass {
				return leftClass < rightClass
			}
			left, leftProbed := resultsByIndex[leftIndex]
			right, rightProbed := resultsByIndex[rightIndex]
			if leftClass == 0 && leftProbed && rightProbed {
				if wasCooling[leftIndex] != wasCooling[rightIndex] {
					return !wasCooling[leftIndex]
				}
				if left.latency != right.latency {
					return left.latency < right.latency
				}
			}
			return leftIndex < rightIndex
		})
		rankedTop := append([]providerMedia(nil), choice.media[:topQualityCount]...)
		for position, originalIndex := range order {
			rankedTop[position] = choice.media[originalIndex]
			rankedPosition[originalIndex] = position
		}
		choice.media = append(rankedTop, lowerQuality...)
	}

	for _, result := range results {
		state := "ok"
		reason := ""
		if result.err != nil {
			state = "failed"
			reason = nativeStreamError(result.err)
		}
		ranked := result.index
		if value, ok := rankedPosition[result.index]; ok {
			ranked = value
		}
		engine.downloader.recordDiagnostic(diagnosticEvent{
			Level:      "info",
			Event:      "playback.route_probe",
			Source:     sourceFromDramaID(task.DramaID),
			DramaID:    task.DramaID,
			DramaTitle: task.DramaTitle,
			Episode:    task.Index,
			Quality:    choice.media[ranked].Quality,
			Host:       result.host,
			Message: fmt.Sprintf(
				"state=%s original_route=%d ranked_route=%d media_ms=%d http=%d bytes=%d error=%s",
				state, result.index+1, ranked+1, result.latency.Milliseconds(),
				result.status, result.bytes, reason,
			),
		})
	}
	return choice
}

func (downloader *Downloader) probeNativePlaybackRoute(
	parent context.Context,
	media providerMedia,
) (time.Duration, int, int, string, error) {
	if !isProviderHTTPMediaURL(media.URL) {
		return 0, 0, 0, "", fmt.Errorf("invalid media URL")
	}
	parsed, err := url.Parse(media.URL)
	if err != nil {
		return 0, 0, 0, "", err
	}
	host := parsed.Hostname()
	ctx, cancel := context.WithTimeout(
		providerMediaContext(parent, media.credentials),
		nativePlaybackRouteProbeTimeout,
	)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, media.URL, nil)
	if err != nil {
		return 0, 0, 0, host, err
	}
	request.Header.Set("Referer", media.Referer)
	if !strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		request.Header.Set("Range", "bytes=0-1023")
	}
	start := time.Now()
	response, err := downloader.doMediaRequest(request)
	if err != nil {
		return time.Since(start), 0, 0, host, err
	}
	defer response.Body.Close()
	latency := time.Since(start)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return latency, response.StatusCode, 0, host, fmt.Errorf("media HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	if err != nil {
		return time.Since(start), response.StatusCode, len(body), host, err
	}
	if len(body) == 0 {
		return time.Since(start), response.StatusCode, 0, host, fmt.Errorf("empty media response")
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	prefix := strings.ToLower(strings.TrimSpace(string(body)))
	if strings.Contains(contentType, "text/html") ||
		strings.Contains(contentType, "application/json") ||
		strings.HasPrefix(prefix, "<!doctype html") ||
		strings.HasPrefix(prefix, "<html") {
		return time.Since(start), response.StatusCode, len(body), host, fmt.Errorf("non-media response")
	}
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") &&
		!strings.HasPrefix(strings.TrimSpace(string(body)), "#EXTM3U") {
		return time.Since(start), response.StatusCode, len(body), host, fmt.Errorf("invalid HLS playlist")
	}
	return time.Since(start), response.StatusCode, len(body), host, nil
}
