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

const nativePlaybackRouteProbeTimeout = 6 * time.Second
const nativePlaybackRouteProbeLimit = 4

type nativePlaybackRouteProbeResult struct {
	index   int
	host    string
	latency time.Duration
	status  int
	bytes   int
	err     error
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
	probeCount := min(topQualityCount, nativePlaybackRouteProbeLimit)
	if probeCount < 2 {
		return choice
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
	successful := 0
	for range probeCount {
		result := <-resultsChannel
		results = append(results, result)
		if result.err == nil {
			successful++
		}
	}

	// Only change the order when at least one candidate proved it can return
	// media data. If probes are all blocked by a provider, keep the old order.
	if successful > 0 && ctx.Err() == nil {
		sort.SliceStable(results, func(i, j int) bool {
			left, right := results[i], results[j]
			if (left.err == nil) != (right.err == nil) {
				return left.err == nil
			}
			if left.err == nil && left.latency != right.latency {
				return left.latency < right.latency
			}
			return left.index < right.index
		})
		ranked := append([]providerMedia(nil), choice.media...)
		for position, result := range results {
			ranked[position] = choice.media[result.index]
		}
		choice.media = ranked
	}

	rankedPosition := make(map[int]int, len(results))
	if successful > 0 && ctx.Err() == nil {
		for position, result := range results {
			rankedPosition[result.index] = position
		}
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
