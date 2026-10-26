package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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


func TestNativePlaybackRouteProbeReturnsAfterFirstHealthyRoute(t *testing.T) {
	const stalledURL = "https://cdn-stalled.example.test/episode.mp4"
	const healthyURL = "https://cdn-healthy.example.test/episode.mp4"
	client := &http.Client{Transport: playbackProbeFixtureTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "cdn-stalled.example.test" {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("synthetic-media-bytes")),
			Request:    request,
		}, nil
	})}
	downloader := &Downloader{client: client}
	engine := &nativeEngine{downloader: downloader}
	choice := nativePlaybackChoices(providerMedia{
		URL: stalledURL, Quality: 1080,
		Variants: []providerMedia{{URL: healthyURL, Quality: 1080}},
	}, 0)

	started := time.Now()
	ranked := engine.rankNativePlaybackRoutes(context.Background(), choice, Task{
		DramaID: "hongguo:fixture", DramaTitle: "synthetic playback", Index: 1,
	})
	elapsed := time.Since(started)
	if elapsed >= time.Second {
		t.Fatalf("route probe waited for a stalled alternative despite a healthy route: %s", elapsed)
	}
	if ranked.media[0].URL != healthyURL {
		t.Fatalf("healthy route should be selected before the stalled alternative: %s", ranked.media[0].URL)
	}
	if downloader.nativeStreamHostCoolingDown(stalledURL) {
		t.Fatal("a probe canceled after another route succeeded must not mark the stalled host as failed")
	}
}


func TestNativePlaybackRouteProbeKeepsCoolingDownHostBehindHealthyRoute(t *testing.T) {
	const failedURL = "https://cdn-stalled.example.test/episode.mp4"
	const healthyURL = "https://cdn-healthy.example.test/episode.mp4"
	client := &http.Client{Transport: playbackProbeFixtureTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "cdn-stalled.example.test" {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("unavailable")),
				Request:    request,
			}, nil
		}
		time.Sleep(15 * time.Millisecond)
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader("synthetic-media-bytes")),
			Request:    request,
		}, nil
	})}
	downloader := &Downloader{client: client}
	downloader.rememberNativeStreamHostFailure(failedURL, "timeout")
	choice := nativePlaybackChoices(providerMedia{
		URL: failedURL, Quality: 1080,
		Variants: []providerMedia{{URL: healthyURL, Quality: 1080}},
	}, 0)
	engine := &nativeEngine{downloader: downloader}
	ranked := engine.rankNativePlaybackRoutes(context.Background(), choice, Task{
		DramaID: "hongguo:fixture", DramaTitle: "synthetic playback", Index: 1,
	})
	if ranked.media[0].URL != healthyURL {
		t.Fatalf("healthy route was not ranked first: %s", ranked.media[0].URL)
	}
	if !downloader.nativeStreamHostCoolingDown(failedURL) {
		t.Fatal("failed host should remain on cooldown")
	}
}

type playbackProbeFixtureTransport func(*http.Request) (*http.Response, error)

func (transport playbackProbeFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}


func TestNativePlaybackRouteProbeRejectsTruncatedMediaBody(t *testing.T) {
	const stalledURL = "https://cdn-prefix-only.example.test/episode.mp4"
	const healthyURL = "https://cdn-healthy.example.test/episode.mp4"
	client := &http.Client{Transport: playbackProbeFixtureTransport(func(request *http.Request) (*http.Response, error) {
		if got, want := request.Header.Get("Range"), "bytes=0-65535"; got != want {
			t.Errorf("route probe range=%q, want %q", got, want)
		}
		if request.URL.Host == "cdn-prefix-only.example.test" {
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Header:     http.Header{"Content-Type": []string{"video/mp4"}},
				Body: io.NopCloser(io.MultiReader(
					strings.NewReader(strings.Repeat("x", nativePlaybackRouteProbeBytes/2)),
					playbackProbeUnexpectedEOFReader{},
				)),
				Request: request,
			}, nil
		}
		time.Sleep(15 * time.Millisecond)
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}},
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", nativePlaybackRouteProbeBytes))),
			Request:    request,
		}, nil
	})}
	downloader := &Downloader{client: client}
	choice := nativePlaybackChoices(providerMedia{
		URL: stalledURL, Quality: 1080,
		Variants: []providerMedia{{URL: healthyURL, Quality: 1080}},
	}, 0)
	engine := &nativeEngine{downloader: downloader}
	ranked := engine.rankNativePlaybackRoutes(context.Background(), choice, Task{
		DramaID: "hongguo:fixture", DramaTitle: "synthetic playback", Index: 1,
	})
	if ranked.media[0].URL != healthyURL {
		t.Fatalf("expected a complete-body route ahead of a truncated prefix, got %s", ranked.media[0].URL)
	}
	if !downloader.nativeStreamHostCoolingDown(stalledURL) {
		t.Fatal("truncated media prefix should cool down its host")
	}
}

type playbackProbeUnexpectedEOFReader struct{}

func (playbackProbeUnexpectedEOFReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestNativePlaybackProbeDiversifiesSameQualityHostsWhenAllFail(t *testing.T) {
	media := providerMedia{
		URL: "https://cdn-a.example.test/one.mp4", Quality: 1080,
		Variants: []providerMedia{
			{URL: "https://cdn-a.example.test/two.mp4", Quality: 1080},
			{URL: "https://cdn-b.example.test/three.mp4", Quality: 1080},
			{URL: "https://cdn-b.example.test/four.mp4", Quality: 1080},
			{URL: "https://cdn-c.example.test/five.mp4", Quality: 1080},
			{URL: "https://cdn-a.example.test/720p.mp4", Quality: 720},
		},
	}
	client := &http.Client{Transport: playbackProbeFixtureTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("unavailable")),
			Request:    request,
		}, nil
	})}
	choice := nativePlaybackChoices(media, 0)
	engine := &nativeEngine{downloader: &Downloader{client: client}}
	ranked := engine.rankNativePlaybackRoutes(context.Background(), choice, Task{DramaID: "hongguo:fixture", Index: 1})
	want := []string{
		"https://cdn-b.example.test/four.mp4",
		"https://cdn-a.example.test/one.mp4",
		"https://cdn-b.example.test/three.mp4",
		"https://cdn-c.example.test/five.mp4",
		"https://cdn-a.example.test/two.mp4",
	}
	if len(ranked.media) != len(want)+1 {
		t.Fatalf("route count changed: %d", len(ranked.media))
	}
	for index, wantURL := range want {
		if ranked.media[index].URL != wantURL {
			t.Fatalf("route %d=%s, want %s", index+1, ranked.media[index].URL, wantURL)
		}
	}
	if ranked.media[len(want)].Quality != 720 {
		t.Fatalf("diversifying hosts must not downgrade preferred quality: %+v", ranked.media)
	}
}

func TestNativePlaybackProbeBudgetDoesNotCooldownSlowHosts(t *testing.T) {
	const slowA = "https://cdn-slow-a.example.test/episode.mp4"
	const slowB = "https://cdn-slow-b.example.test/episode.mp4"
	client := &http.Client{Transport: playbackProbeFixtureTransport(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	downloader := &Downloader{client: client}
	engine := &nativeEngine{downloader: downloader}
	choice := nativePlaybackChoices(providerMedia{
		URL: slowA, Quality: 1080,
		Variants: []providerMedia{{URL: slowB, Quality: 1080}},
	}, 0)

	started := time.Now()
	_ = engine.rankNativePlaybackRoutes(context.Background(), choice, Task{
		DramaID: "hongguo:fixture", DramaTitle: "slow CDN probe", Index: 1,
	})
	elapsed := time.Since(started)
	if elapsed < nativePlaybackRouteProbeTimeout {
		t.Fatalf("probe returned before its own timeout budget elapsed: %s", elapsed)
	}
	if elapsed > 3500*time.Millisecond {
		t.Fatalf("stalled probes delayed playback resolution too long: %s", elapsed)
	}
	for _, address := range []string{slowA, slowB} {
		if downloader.nativeStreamHostCoolingDown(address) {
			t.Errorf("the short probe budget incorrectly cooled down slow host %s", address)
		}
	}
}

func TestNativePlaybackRouteProbeMovesUntestedRoutesAheadOfTimedOutRoutes(t *testing.T) {
	routes := []providerMedia{
		{URL: "https://cdn-a.example.test/one.mp4", Quality: 1080},
		{URL: "https://cdn-b.example.test/two.mp4", Quality: 1080},
		{URL: "https://cdn-c.example.test/three.mp4", Quality: 1080},
		{URL: "https://cdn-d.example.test/four.mp4", Quality: 1080},
		{URL: "https://cdn-e.example.test/five.mp4", Quality: 1080},
		{URL: "https://cdn-f.example.test/720p.mp4", Quality: 720},
	}
	client := &http.Client{Transport: playbackProbeFixtureTransport(func(request *http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	downloader := &Downloader{client: client}
	choice := nativePlaybackChoice{media: routes, qualities: []int{1080, 720}}
	engine := &nativeEngine{downloader: downloader}
	ranked := engine.rankNativePlaybackRoutes(context.Background(), choice, Task{
		DramaID: "hongguo:fixture", DramaTitle: "synthetic playback", Index: 1,
	})
	if ranked.media[0].URL != routes[4].URL {
		t.Fatalf("untested same-quality route should be first after the four probes time out, got %s", ranked.media[0].URL)
	}
	if ranked.media[len(ranked.media)-1].Quality != 720 {
		t.Fatal("route ranking changed quality preference")
	}
	for index := 0; index < 4; index++ {
		if !downloader.nativeStreamHostCoolingDown(routes[index].URL) {
			t.Errorf("timed-out route %d was not cooled down", index+1)
		}
	}
}
