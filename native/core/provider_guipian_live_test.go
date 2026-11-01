package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TestGuipianLivePlaybackThroughLocalProxy is deliberately opt-in. The
// dedicated PR workflow sets DTV_LIVE_GUIPIAN=1 so this test always probes
// the real public source rather than relying on HTML or playlist fixtures.
func TestGuipianLivePlaybackThroughLocalProxy(t *testing.T) {
	if os.Getenv("DTV_LIVE_GUIPIAN") != "1" {
		t.Skip("set DTV_LIVE_GUIPIAN=1 to probe the public Guipian source")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := engine.downloader.fetchGuipianCatalogPage(ctx, 1, "3", "")
	if err != nil {
		t.Fatalf("live Guipian category request failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("live Guipian category returned no titles")
	}
	t.Logf("live category returned %d titles; first title=%q", len(items), items[0].Title)

	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()
	localClient := &http.Client{Timeout: 25 * time.Second}
	var attempts []string

	for _, item := range items[:min(len(items), 5)] {
		drama, chapters, detailErr := engine.downloader.fetchGuipianDetail(ctx, item.SourceID)
		if detailErr != nil {
			attempts = append(attempts, fmt.Sprintf("%s detail: %v", item.SourceID, detailErr))
			continue
		}
		if len(chapters) == 0 {
			attempts = append(attempts, fmt.Sprintf("%s detail returned no episodes", item.SourceID))
			continue
		}
		t.Logf("live detail %q returned %d episodes", drama.Title, len(chapters))

		for _, chapter := range chapters[:min(len(chapters), 2)] {
			media, resolveErr := engine.downloader.resolveGuipianMediaForPlatform(ctx, Task{
				DramaID: drama.ID,
				Chapter: chapter,
			}, "android")
			if resolveErr != nil {
				attempts = append(attempts, fmt.Sprintf("%s/%s resolve: %v", item.SourceID, chapter.Title, resolveErr))
				continue
			}
			if !media.StandardHTTP || media.credentials == nil || media.Referer == "" {
				attempts = append(attempts, fmt.Sprintf("%s/%s missing Android transport or playback headers", item.SourceID, chapter.Title))
				continue
			}
			localURL, token := stream.nativeOpen(media)
			probeErr := probeGuipianLiveStream(ctx, localClient, localURL, media.URL)
			stream.nativeRelease(token)
			if probeErr == nil {
				t.Logf("live playback proxy passed: title=%q episode=%q upstreamHost=%s refererHost=%s",
					drama.Title, chapter.Title, guipianLiveHost(media.URL), guipianLiveHost(media.Referer))
				return
			}
			attempts = append(attempts, fmt.Sprintf("%s/%s proxy: %v", item.SourceID, chapter.Title, probeErr))
		}
	}
	t.Fatalf("no live Guipian episode passed playback preparation and local proxy checks: %s", strings.Join(attempts, "; "))
}

func probeGuipianLiveStream(ctx context.Context, client *http.Client, localURL, upstreamURL string) error {
	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		return err
	}
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return probeGuipianLiveHLS(ctx, client, localURL)
	}
	_, _, err = readGuipianLiveAsset(ctx, client, localURL, 64<<10, "bytes=0-65535")
	return err
}

func probeGuipianLiveHLS(ctx context.Context, client *http.Client, localURL string) error {
	root, _, err := readGuipianLiveAsset(ctx, client, localURL, (4<<20)+1, "")
	if err != nil {
		return fmt.Errorf("master playlist through local proxy: %w", err)
	}
	rootText := string(root)
	if !strings.HasPrefix(strings.TrimSpace(rootText), "#EXTM3U") {
		return fmt.Errorf("local proxy did not return an HLS playlist")
	}
	slash := strings.LastIndex(localURL, "/")
	if slash < 0 {
		return fmt.Errorf("invalid local stream URL")
	}
	localPrefix := localURL[:slash+1]
	variants := guipianLiveVariantURLs(rootText)
	if len(variants) == 0 {
		return probeGuipianLiveMediaPlaylist(ctx, client, rootText, localPrefix)
	}
	var lastErr error
	for _, variant := range variants[:min(len(variants), 3)] {
		if !strings.HasPrefix(variant, localPrefix) {
			lastErr = fmt.Errorf("master playlist child URL was not rewritten to the local proxy")
			continue
		}
		child, _, childErr := readGuipianLiveAsset(ctx, client, variant, (4<<20)+1, "")
		if childErr != nil {
			lastErr = fmt.Errorf("child playlist request: %w", childErr)
			continue
		}
		childText := string(child)
		if !strings.HasPrefix(strings.TrimSpace(childText), "#EXTM3U") {
			lastErr = fmt.Errorf("child URL did not return an HLS playlist")
			continue
		}
		if childErr = probeGuipianLiveMediaPlaylist(ctx, client, childText, localPrefix); childErr == nil {
			return nil
		}
		lastErr = childErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("master playlist did not contain a usable variant")
	}
	return lastErr
}

func guipianLiveVariantURLs(playlist string) []string {
	var variants []string
	waiting := false
	for _, line := range strings.Split(playlist, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			waiting = true
			continue
		}
		if waiting && line != "" && !strings.HasPrefix(line, "#") {
			variants = append(variants, line)
			waiting = false
		}
	}
	return variants
}

func probeGuipianLiveMediaPlaylist(ctx context.Context, client *http.Client, playlist, localPrefix string) error {
	if !strings.HasPrefix(strings.TrimSpace(playlist), "#EXTM3U") {
		return fmt.Errorf("child media playlist is invalid")
	}
	for _, line := range strings.Split(playlist, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXT-X-KEY:") {
			continue
		}
		match := nativePlaylistURI.FindStringSubmatch(line)
		if len(match) < 2 || strings.HasPrefix(strings.ToLower(match[1]), "data:") {
			break
		}
		if !strings.HasPrefix(match[1], localPrefix) {
			return fmt.Errorf("HLS key URI was not rewritten to the local proxy")
		}
		key, _, err := readGuipianLiveAsset(ctx, client, match[1], 1024, "")
		if err != nil {
			return fmt.Errorf("HLS key request: %w", err)
		}
		if len(key) == 0 {
			return fmt.Errorf("HLS key response was empty")
		}
		break
	}
	segmentURL := ""
	for _, line := range strings.Split(playlist, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			segmentURL = line
			break
		}
	}
	if segmentURL == "" {
		for _, line := range strings.Split(playlist, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#EXT-X-MAP:") {
				match := nativePlaylistURI.FindStringSubmatch(line)
				if len(match) > 1 {
					segmentURL = match[1]
					break
				}
			}
		}
	}
	if segmentURL == "" {
		return fmt.Errorf("media playlist contained no segment or initialization map")
	}
	if !strings.HasPrefix(segmentURL, localPrefix) {
		return fmt.Errorf("HLS segment URI was not rewritten to the local proxy")
	}
	segment, _, err := readGuipianLiveAsset(ctx, client, segmentURL, 64<<10, "")
	if err != nil {
		return fmt.Errorf("HLS segment request: %w", err)
	}
	if len(segment) == 0 {
		return fmt.Errorf("HLS segment response was empty")
	}
	return nil
}

func readGuipianLiveAsset(ctx context.Context, client *http.Client, address string, limit int64, rangeHeader string) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, 0, err
	}
	if rangeHeader != "" {
		request.Header.Set("Range", rangeHeader)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, limit))
	if readErr != nil {
		return nil, response.StatusCode, readErr
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return body, response.StatusCode, fmt.Errorf("local proxy returned HTTP %d", response.StatusCode)
	}
	if len(body) == 0 {
		return body, response.StatusCode, fmt.Errorf("local proxy returned an empty body")
	}
	return body, response.StatusCode, nil
}

func guipianLiveHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
