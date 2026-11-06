package core

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestNativeCoverLoadReusesCachedExternalFallbackBeforeProviderRefresh(t *testing.T) {
	fixture, err := os.ReadFile("../../test/fixtures/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}

	const primaryURL = "https://broken-source.example.test/poster.jpg"
	const fallbackURL = "https://img9.doubanio.com/view/photo/s_ratio_poster/public/cached.jpg"
	var fallbackRequests, metadataRequests int
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "broken-source.example.test", "images.weserv.nl":
			return posterLookupJSONResponse(request, "not found"), nil
		case "img9.doubanio.com":
			fallbackRequests++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{"image/jpeg"}},
				Body: io.NopCloser(bytes.NewReader(fixture)),
				ContentLength: int64(len(fixture)),
				Request: request,
			}, nil
		default:
			metadataRequests++
			return posterLookupJSONResponse(request, "unexpected metadata request"), nil
		}
	})}
	downloader := &Downloader{
		client: client,
		cfg: defaultConfig(),
		providerHosts: map[string]string{sourceDuoduo: "https://323433ssdfd.top"},
	}
	cache := newNativeCoverCache(t.TempDir(), downloader)
	drama := nativeDrama{
		ID: "duoduo:101901", Source: sourceDuoduo, Title: "測試海報",
		Category: "電影", Cover: primaryURL, FallbackCover: fallbackURL,
	}

	// Seed the persistent cover-file cache as if an earlier launch had already
	// downloaded and verified this exact external fallback.
	referer := nativeCoverFallbackReferer(downloader, drama, fallbackURL)
	cachedPath, err := cache.loadAlternativeAddress(
		context.Background(), drama, fallbackURL, referer, "external", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	fallbackRequests = 0

	gotPath, err := cache.load(context.Background(), drama, false)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != cachedPath {
		t.Fatalf("poster path = %q, want cached fallback %q", gotPath, cachedPath)
	}
	if fallbackRequests != 0 {
		t.Fatalf("cached fallback triggered %d redundant network requests", fallbackRequests)
	}
	if metadataRequests != 0 {
		t.Fatalf("cached fallback waited for %d unnecessary metadata requests", metadataRequests)
	}
}

func TestDuoduoCoverFailoverUsesBoundedBudgets(t *testing.T) {
	if got := nativeCoverFetchTimeout(sourceDuoduo); got != 12*time.Second {
		t.Fatalf("Duoduo initial fetch budget = %s, want 12s", got)
	}
	if got := nativeCoverAlternativeTimeout(sourceDuoduo, "provider"); got != 10*time.Second {
		t.Fatalf("Duoduo provider fallback budget = %s, want 10s", got)
	}
	if got := nativeCoverFetchTimeout(sourceHongguo); got != 25*time.Second {
		t.Fatalf("other provider initial fetch budget = %s, want 25s", got)
	}
	if got := nativeCoverAlternativeTimeout(sourceDuoduo, "external"); got != 0 {
		t.Fatalf("external poster budget = %s, want unchanged", got)
	}
}
