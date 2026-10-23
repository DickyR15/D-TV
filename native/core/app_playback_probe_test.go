package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNativePlaybackRouteProbePrefersResponsiveSameQualityBackup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Referer") != "https://player.example.test/watch" {
			t.Errorf("route probe lost referer: %q", request.Header.Get("Referer"))
		}
		switch request.URL.Path {
		case "/slow.mp4":
			time.Sleep(160 * time.Millisecond)
		case "/failed.mp4":
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(writer, "synthetic-media-bytes")
	}))
	defer server.Close()

	media := providerMedia{
		URL: server.URL + "/slow.mp4", Referer: "https://player.example.test/watch", Quality: 1080,
		Variants: []providerMedia{
			{URL: server.URL + "/fast.mp4", Referer: "https://player.example.test/watch", Quality: 1080},
			{URL: server.URL + "/failed.mp4", Referer: "https://player.example.test/watch", Quality: 1080},
			{URL: server.URL + "/720p.mp4", Referer: "https://player.example.test/watch", Quality: 720},
		},
	}
	choice := nativePlaybackChoices(media, 0)
	engine := &nativeEngine{downloader: &Downloader{client: server.Client()}}
	ranked := engine.rankNativePlaybackRoutes(context.Background(), choice, Task{
		DramaID: "hongguo:fixture",
		DramaTitle: "synthetic playback",
		Index: 1,
	})
	if len(ranked.media) != 4 {
		t.Fatalf("route count changed: %d", len(ranked.media))
	}
	if ranked.media[0].URL != server.URL+"/fast.mp4" {
		t.Fatalf("expected the responsive backup first, got %s", ranked.media[0].URL)
	}
	if ranked.media[1].URL != server.URL+"/slow.mp4" {
		t.Fatalf("expected the slower same-quality main route second, got %s", ranked.media[1].URL)
	}
	if ranked.media[2].URL != server.URL+"/failed.mp4" {
		t.Fatalf("failed candidate should remain after responsive candidates, got %s", ranked.media[2].URL)
	}
	if ranked.media[3].Quality != 720 {
		t.Fatalf("route probing should not downgrade preferred quality: %+v", ranked.media)
	}
}
