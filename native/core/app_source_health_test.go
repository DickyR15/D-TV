package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSourceProbePlayableRouteFallsBackWhenPreferredCDNIsDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bad.m3u8":
			http.Error(w, "TLS policy denied this edge", http.StatusForbidden)
		case "/good.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\nsegment.ts\n#EXT-X-ENDLIST\n"))
		case "/segment.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("transport-stream-segment"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := server.Client()
	d := &Downloader{client: client, streamClient: client}
	media := providerMedia{
		URL: server.URL + "/bad.m3u8",
		Referer: server.URL + "/",
		StandardHTTP: true,
		Variants: []providerMedia{{
			URL: server.URL + "/good.m3u8",
			Referer: server.URL + "/",
			StandardHTTP: true,
		}},
	}
	selected, keyURL, segmentURL, segmentBytes, err := d.sourceProbePlayableRoute(context.Background(), media)
	if err != nil {
		t.Fatalf("source health should select the working same-episode fallback: %v", err)
	}
	if !strings.HasSuffix(selected.URL, "/good.m3u8") ||
		keyURL != "" ||
		!strings.HasSuffix(segmentURL, "/segment.ts") ||
		len(segmentBytes) == 0 {
		t.Fatalf("unexpected selected route: url=%q key=%q segment=%q", selected.URL, keyURL, segmentURL)
	}
}

func TestSourceProbePlayableRouteReportsFailureWhenAllRoutesFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()
	client := server.Client()
	d := &Downloader{client: client, streamClient: client}
	media := providerMedia{
		URL: server.URL + "/one.m3u8",
		Referer: server.URL + "/",
		StandardHTTP: true,
		Variants: []providerMedia{{URL: server.URL + "/two.m3u8", Referer: server.URL + "/", StandardHTTP: true}},
	}
	if _, _, _, _, err := d.sourceProbePlayableRoute(context.Background(), media); err == nil {
		t.Fatal("all-denied routes should fail the playback health check")
	}
}
