package core

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestHongguoModuleContractThroughPlaybackBoundary(t *testing.T) {
	const series = "7710822671759461438"
	const video = "7710822671759461439"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request: %s", request.URL.Host)
		return nil, errors.New("unexpected request")
	})
	backend := hongguoModuleBackend{load: func(ctx context.Context, request hongguoPlaybackRequest) (hongguoModuleResult, error) {
		if request.seriesID != series || request.videoID != video {
			t.Fatalf("wrong request: %+v", request)
		}
		return hongguoModuleResult{
			Version: hongguoModuleContractVersion, SeriesID: series, VideoID: video,
			Duration: 90 * time.Second,
			Streams: []hongguoModuleStream{
				{URL: "https://example.test/preview.mp4", Quality: 1080, Preview: true},
				{URL: "https://example.test/low.mp4", Quality: 180},
				{URL: "https://example.test/full.mp4", Quality: 720},
				{URL: "https://example.test/full.mp4", Quality: 1080},
				{URL: "https://backup.example.test/full.mp4", Quality: 720},
				{URL: "https://example.test/unknown.mp4"},
				{URL: "file:///tmp/video.mp4", Quality: 2160},
			},
		}, nil
	}}
	media, err := d.resolveHongguoMediaWithBackend(context.Background(), Task{
		DramaID: "hongguo:" + series,
		Chapter: Chapter{VideoURL: "hongguo-cenc://" + video},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if media.URL != "https://example.test/full.mp4" || media.Quality != 1080 ||
		media.Duration != 90*time.Second || len(media.Variants) != 3 ||
		media.Variants[1].Quality != 720 || media.Variants[2].Quality != 0 {
		t.Fatalf("incorrect media: %+v", media)
	}
}

func TestHongguoModuleRejectsInvalidResults(t *testing.T) {
	request := hongguoPlaybackRequest{seriesID: "7710822671759461438", videoID: "7710822671759461439"}
	base := hongguoModuleResult{
		Version: hongguoModuleContractVersion, SeriesID: request.seriesID, VideoID: request.videoID,
		Streams: []hongguoModuleStream{{URL: "https://example.test/full.mp4", Quality: 1080}},
	}
	for _, tc := range []struct {
		name string
		edit func(*hongguoModuleResult)
	}{
		{"version", func(r *hongguoModuleResult) { r.Version++ }},
		{"episode", func(r *hongguoModuleResult) { r.VideoID = "1" }},
		{"series", func(r *hongguoModuleResult) { r.SeriesID = "1" }},
		{"protected", func(r *hongguoModuleResult) { r.Protected = true }},
		{"preview", func(r *hongguoModuleResult) { r.Streams = []hongguoModuleStream{{URL: "https://example.test/preview.mp4", Quality: 1080, Preview: true}} }},
		{"low", func(r *hongguoModuleResult) { r.Streams = []hongguoModuleStream{{URL: "https://example.test/low.mp4", Quality: 360}} }},
		{"empty", func(r *hongguoModuleResult) { r.Streams = nil }},
		{"non-https", func(r *hongguoModuleResult) { r.Streams = []hongguoModuleStream{{URL: "http://example.test/full.mp4", Quality: 1080}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := base
			tc.edit(&result)
			if _, err := hongguoModuleMedia(request, result); err == nil {
				t.Fatal("invalid module result accepted")
			}
		})
	}
}

func TestHongguoModuleCancelledBeforeLoad(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	backend := hongguoModuleBackend{load: func(context.Context, hongguoPlaybackRequest) (hongguoModuleResult, error) {
		t.Fatal("cancelled request reached provider")
		return hongguoModuleResult{}, nil
	}}
	if _, err := backend.resolve(ctx, hongguoPlaybackRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
