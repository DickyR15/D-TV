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

func TestHongguoJSModuleReusesSessionAndSeparatesInstances(t *testing.T) {
	const script = `
		let calls = 0;
		function resolveMedia(seriesId, videoId) {
			return JSON.stringify({version:1, seriesId, videoId, durationSeconds: ++calls,
				streams:[{url:"https://example.test/sample.mp4", quality:1080}]});
		}
	`
	a, err := newHongguoJSModuleBackend(script)
	if err != nil { t.Fatal(err) }
	b, err := newHongguoJSModuleBackend(script)
	if err != nil { t.Fatal(err) }
	request := hongguoPlaybackRequest{seriesID:"1", videoID:"2"}
	for _, tc := range []struct {
		backend hongguoModuleBackend
		want time.Duration
	}{{a, time.Second}, {a, 2*time.Second}, {b, time.Second}, {a, 3*time.Second}} {
		media, err := tc.backend.resolve(context.Background(), request)
		if err != nil || media.Duration != tc.want {
			t.Fatalf("duration=%v want=%v err=%v", media.Duration, tc.want, err)
		}
	}
}

func TestHongguoJSModuleResetsAfterInterrupt(t *testing.T) {
	backend, err := newHongguoJSModuleBackend(`
		let calls = 0;
		function resolveMedia(seriesId, videoId) {
			calls++;
			if (videoId === "3") { while (true) {} }
			return JSON.stringify({version:1, seriesId, videoId, durationSeconds:calls,
				streams:[{url:"https://example.test/sample.mp4",quality:1080}]});
		}
	`)
	if err != nil { t.Fatal(err) }
	request := hongguoPlaybackRequest{seriesID:"1", videoID:"2"}
	if _, err := backend.resolve(context.Background(), request); err != nil { t.Fatal(err) }
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := backend.resolve(ctx, hongguoPlaybackRequest{seriesID:"1",videoID:"3"}); !errors.Is(err,context.DeadlineExceeded) {
		t.Fatalf("expected timeout, got %v", err)
	}
	media, err := backend.resolve(context.Background(), request)
	if err != nil || media.Duration != time.Second {
		t.Fatalf("session was not reset: %v %v", media.Duration, err)
	}
}

func TestHongguoJSModuleSessionSerializesConcurrentCalls(t *testing.T) {
	backend, err := newHongguoJSModuleBackend(`
		let calls = 0;
		function resolveMedia(seriesId,videoId) {
			return JSON.stringify({version:1,seriesId,videoId,durationSeconds:++calls,
				streams:[{url:"https://example.test/sample.mp4",quality:1080}]});
		}
	`)
	if err != nil { t.Fatal(err) }
	type outcome struct { duration time.Duration; err error }
	results := make(chan outcome, 8)
	for i:=0;i<8;i++ {
		go func() {
			m, err := backend.resolve(context.Background(), hongguoPlaybackRequest{seriesID:"1",videoID:"2"})
			results <- outcome{m.Duration,err}
		}()
	}
	seen := map[time.Duration]bool{}
	for i:=0;i<8;i++ {
		got := <-results
		if got.err != nil || seen[got.duration] || got.duration < time.Second || got.duration > 8*time.Second {
			t.Fatalf("unexpected concurrent result: %+v", got)
		}
		seen[got.duration] = true
	}
}
