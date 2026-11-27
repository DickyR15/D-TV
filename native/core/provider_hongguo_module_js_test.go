package core

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestHongguoJSModuleThroughPlaybackBoundary(t *testing.T) {
	backend, err := newHongguoJSModuleBackend(`
		function resolveMedia(seriesId, videoId) {
			if (typeof fetch !== "undefined" || typeof require !== "undefined") throw Error("unexpected host binding");
			return JSON.stringify({version: 1, seriesId, videoId, durationSeconds: 90,
				protected: false, streams: [
					{url: "https://example.test/sample.mp4", quality: 720},
					{url: "https://example.test/high.mp4", quality: 1080},
					{url: "https://example.test/preview.mp4", quality: 2160, preview: true}
				]});
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Fatal("fixture unexpectedly reached network")
		return nil, errors.New("unexpected request")
	})
	media, err := d.resolveHongguoMediaWithBackend(context.Background(), Task{
		DramaID: "hongguo:7710822671759461438",
		Chapter: Chapter{VideoURL: "hongguo-cenc://7710822671759461439"},
	}, backend)
	if err != nil || media.Quality != 1080 || len(media.Variants) != 2 ||
		media.Duration != 90*time.Second {
		t.Fatalf("unexpected playback result: %+v %v", media, err)
	}
}

func TestHongguoJSModuleInterruptsLoop(t *testing.T) {
	backend, err := newHongguoJSModuleBackend("function resolveMedia() { while (true) {} }")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = backend.resolve(ctx, hongguoPlaybackRequest{seriesID: "1", videoID: "2"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
}

func TestHongguoJSModuleRejectsMalformedResults(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"durationSeconds":-1}`,
		`{"version":1,"durationSeconds":86401}`,
		`{"version":1,"extra":true}`,
		`{"version":1} {}`,
	} {
		if _, err := decodeHongguoJSModuleResult(raw); err == nil {
			t.Fatalf("invalid result accepted: %s", raw)
		}
	}
}
