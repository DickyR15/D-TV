package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNativeCoverLoadReusesCachedExternalFallbackAfterCacheRecreationBeforeAnyNetwork(t *testing.T) {
	fixture, err := os.ReadFile("../../test/fixtures/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}

	const primaryURL = "https://broken-source.example.test/poster.jpg"
	const fallbackURL = "https://img9.doubanio.com/view/photo/s_ratio_poster/public/cached.jpg"
	var fallbackRequests, metadataRequests, originRequests, proxyRequests, officialRequests int
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "broken-source.example.test":
			originRequests++
			return posterLookupJSONResponse(request, "not found"), nil
		case "images.weserv.nl", "wsrv.nl":
			proxyRequests++
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
		case "fujicreative.co.jp":
			officialRequests++
			return posterLookupJSONResponse(request, "unexpected official poster request"), nil
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
	directory := t.TempDir()
	drama := nativeDrama{
		ID: "duoduo:1192", Source: sourceDuoduo, Title: "奉子成婚",
		Category: "劇集", Cover: primaryURL, FallbackCover: fallbackURL,
	}

	// Seed a verified external fallback as if an earlier app process had
	// downloaded it. Recreate the cache object before calling load(), which is
	// the real entry point used by poster rendering.
	firstCache := newNativeCoverCache(directory, downloader)
	referer := nativeCoverFallbackReferer(downloader, drama, fallbackURL)
	cachedPath, err := firstCache.loadAlternativeAddress(
		context.Background(), drama, fallbackURL, referer, "external", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	fallbackRequests = 0
	originRequests = 0
	proxyRequests = 0
	metadataRequests = 0
	officialRequests = 0

	restartedCache := newNativeCoverCache(directory, downloader)
	gotPath, err := restartedCache.load(context.Background(), drama, false)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != cachedPath {
		t.Fatalf("poster path = %q, want cached fallback %q", gotPath, cachedPath)
	}
	if fallbackRequests != 0 {
		t.Fatalf("cached fallback triggered %d redundant fallback requests", fallbackRequests)
	}
	if originRequests != 0 || proxyRequests != 0 {
		t.Fatalf("cached fallback waited for origin/proxy network attempts: origin=%d proxy=%d", originRequests, proxyRequests)
	}
	if metadataRequests != 0 || officialRequests != 0 {
		t.Fatalf("cached fallback triggered metadata/curated-poster requests: metadata=%d official=%d", metadataRequests, officialRequests)
	}
}

func TestDuoduoCoverFailoverUsesBoundedBudgets(t *testing.T) {
	if got := nativeCoverFetchTimeout(sourceDuoduo); got != 12*time.Second {
		t.Fatalf("Duoduo initial fetch budget = %s, want 12s", got)
	}
	if got := nativeCoverAlternativeTimeout(sourceDuoduo, "provider"); got != 12*time.Second {
		t.Fatalf("Duoduo provider fallback budget = %s, want 12s", got)
	}
	if got := nativeCoverFetchTimeout(sourceHongguo); got != 25*time.Second {
		t.Fatalf("other provider initial fetch budget = %s, want 25s", got)
	}
	if got := nativeCoverAlternativeTimeout(sourceDuoduo, "external"); got != 0 {
		t.Fatalf("external poster budget = %s, want unchanged", got)
	}
}

func TestDuoduoPosterProxyUsesResizedWebPOnGlobalEdgeCache(t *testing.T) {
	const origin = "https://img.example.test/poster.jpg?token=a&size=original"
	proxyAddress := duoduoPosterProxyURL(origin)
	parsed, err := url.Parse(proxyAddress)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "wsrv.nl" {
		t.Fatalf("poster proxy host = %q, want wsrv.nl", parsed.Host)
	}
	query := parsed.Query()
	if query.Get("url") != "img.example.test/poster.jpg?token=a&size=original" {
		t.Fatalf("original poster URL was not preserved: %q", query.Get("url"))
	}
	if query.Get("w") != "440" || query.Get("h") != "660" ||
		query.Get("fit") != "inside" || query.Get("output") != "webp" ||
		query.Get("q") != "80" {
		t.Fatalf("unexpected poster resize parameters: %v", query)
	}
}

func TestDuoduoPosterProxyRunsImmediatelyAfterDirectOrigin(t *testing.T) {
	const origin = "https://img.example.test/poster.jpg"
	attempts := duoduoPosterAttempts(origin, "https://source.example.test/play/1")
	if len(attempts) < 2 {
		t.Fatalf("poster attempts = %#v, want direct + resized proxy", attempts)
	}
	if attempts[0].address != origin {
		t.Fatalf("first poster attempt = %q, want original %q", attempts[0].address, origin)
	}
	proxy, err := url.Parse(attempts[1].address)
	if err != nil || proxy.Host != "wsrv.nl" {
		t.Fatalf("second poster attempt = %q, want wsrv.nl resize proxy", attempts[1].address)
	}
	if attempts[1].referer != "https://wsrv.nl/" {
		t.Fatalf("proxy Referer = %q, want https://wsrv.nl/", attempts[1].referer)
	}
}

func TestDuoduoPosterAttemptsReserveTimeForResizedProxy(t *testing.T) {
	if got := nativeCoverAttemptTimeout(sourceDuoduo, "https://img.example.test/poster.jpg"); got != 2500*time.Millisecond {
		t.Fatalf("origin attempt timeout = %s, want 2.5s", got)
	}
	if got := nativeCoverAttemptTimeout(sourceDuoduo, "https://wsrv.nl/?url=example.test/poster.jpg"); got != 9*time.Second {
		t.Fatalf("resized proxy timeout = %s, want 9s", got)
	}
	if got := nativeCoverAttemptTimeout(sourceHongguo, "https://img.example.test/poster.jpg"); got != 12*time.Second {
		t.Fatalf("other source timeout = %s, want unchanged 12s", got)
	}
	if got := nativeCoverAttemptTimeout(sourceDuoduo, "https://img9.doubanio.com/poster.jpg", "external"); got != 12*time.Second {
		t.Fatalf("Duoduo external fallback timeout = %s, want 12s", got)
	}
}


func TestNativeCoverExternalFallbackCacheSurvivesCacheRecreation(t *testing.T) {
	fixture, err := os.ReadFile("../../test/fixtures/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}

	const fallbackURL = "https://img9.doubanio.com/view/photo/s_ratio_poster/public/persisted.jpg"
	const referer = "https://movie.douban.com/"
	var requests int
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"image/jpeg"}},
			Body: io.NopCloser(bytes.NewReader(fixture)),
			ContentLength: int64(len(fixture)),
			Request: request,
		}, nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	directory := t.TempDir()
	drama := nativeDrama{
		ID: "duoduo:101901", Source: sourceDuoduo, Title: "持久快取測試",
		Category: "電影", Cover: "https://broken-source.example.test/poster.jpg",
		FallbackCover: fallbackURL,
	}

	firstCache := newNativeCoverCache(directory, downloader)
	firstPath, err := firstCache.loadAlternativeAddress(
		context.Background(), drama, fallbackURL, referer, "external", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("initial fallback requests = %d, want 1", requests)
	}

	// Simulate a new app process: a fresh cache instance must discover the
	// already verified .img file from disk and avoid another network request.
	restartedCache := newNativeCoverCache(directory, downloader)
	restartedPath, err := restartedCache.loadAlternativeAddress(
		context.Background(), drama, fallbackURL, referer, "external", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if restartedPath != firstPath {
		t.Fatalf("restarted cache path = %q, want persisted path %q", restartedPath, firstPath)
	}
	if requests != 1 {
		t.Fatalf("restarted cache performed %d total requests, want the original single request", requests)
	}
}


func TestDuoduoPosterAttemptDiagnosticsAreUsefulAndRedacted(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache := newNativeCoverCache(t.TempDir(), engine.downloader)
	drama := nativeDrama{ID: "duoduo:101901", Source: sourceDuoduo, Title: "診斷測試"}

	cache.recordDuoduoPosterAttempt(
		drama,
		duoduoPosterAttempt{
			address: "https://img.example.test/poster.jpg?token=origin-secret",
			referer: "https://source.example.test/play/101901?session=private",
		},
		1, time.Now().Add(-150*time.Millisecond), nil, context.DeadlineExceeded,
	)
	cache.recordDuoduoPosterAttempt(
		drama,
		duoduoPosterAttempt{
			address: "https://wsrv.nl/?url=img.example.test%2Fposter.jpg%3Ftoken%3Dproxy-secret",
			referer: "https://wsrv.nl/",
		},
		2, time.Now().Add(-75*time.Millisecond), []byte("webp-poster"), nil,
	)

	data, err := os.ReadFile(engine.downloader.diagnostics.path)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	for _, secret := range []string{"origin-secret", "proxy-secret", "session=private"} {
		if strings.Contains(log, secret) {
			t.Fatalf("poster diagnostics leaked signed URL or Referer data %q: %s", secret, log)
		}
	}
	for _, marker := range []string{
		"\"event\":\"cover.attempt\"",
		"\"host\":\"img.example.test\"",
		"\"host\":\"wsrv.nl\"",
		"strategy=origin",
		"strategy=cloudflare_edge",
		"elapsed_ms=",
		"result=deadline",
		"result=success",
	} {
		if !strings.Contains(log, marker) {
			t.Errorf("poster diagnostics missing %q: %s", marker, log)
		}
	}
}

func TestYongshengOfficialPosterRequiresExactAnimeYearAndTitle(t *testing.T) {
	const want = "https://i0.hdslb.com/bfs/bangumi/image/1c697e41f5e2524b2610ec895cead5c25f494e87.png"
	accepted := nativeDrama{
		ID: "duoduo:12345", Source: sourceDuoduo, SourceID: "12345",
		Title: "永生", Category: "動畫", OnlineDate: "2022",
	}
	if got, ok := nativePreferredOfficialPosterAddress(accepted); !ok || got != want {
		t.Fatalf("preferred Yongsheng poster = %q, matched=%v; want %q", got, ok, want)
	}

	rejected := []nativeDrama{
		{ID: "duoduo:12345", Source: sourceDuoduo, Title: "永生", Category: "動畫", OnlineDate: "2023"},
		{ID: "duoduo:12345", Source: sourceDuoduo, Title: "永生", Category: "電影", OnlineDate: "2022"},
		{ID: "duoduo:12345", Source: sourceDuoduo, Title: "永生之十年之約", Category: "動畫", OnlineDate: "2023"},
		{ID: "duoduo:12345", Source: sourceDuoduo, Title: "永生", Category: "動畫"},
	}
	for _, drama := range rejected {
		if got, ok := nativePreferredOfficialPosterAddress(drama); ok {
			t.Errorf("unrelated Yongsheng record unexpectedly matched poster %q: %+v", got, drama)
		}
	}
}

func TestNativeCoverRefererUsesOriginalPosterHostForBilibiliAndDouban(t *testing.T) {
	downloader := &Downloader{
		cfg: defaultConfig(),
		providerHosts: map[string]string{sourceDuoduo: "https://323433ssdfd.top"},
	}
	drama := nativeDrama{ID: "duoduo:12345", Source: sourceDuoduo, SourceID: "12345"}
	cases := []struct {
		address string
		want string
	}{
		{"https://i0.hdslb.com/bfs/bangumi/image/poster.png", "https://www.bilibili.com/"},
		{"https://img9.doubanio.com/view/photo/s_ratio_poster/public/poster.jpg", "https://movie.douban.com/"},
	}
	for _, test := range cases {
		if got := nativeCoverRefererForDrama(downloader, drama, test.address); got != test.want {
			t.Errorf("Referer for %s = %q, want %q", test.address, got, test.want)
		}
	}
}

func TestPreferredYongshengPosterOverridesPreviouslyCachedHotlinkPlaceholder(t *testing.T) {
	fixture, err := os.ReadFile("../../test/fixtures/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	const primaryURL = "https://images.example.test/hotlink-placeholder.jpg"
	const officialURL = "https://i0.hdslb.com/bfs/bangumi/image/1c697e41f5e2524b2610ec895cead5c25f494e87.png"
	var primaryRequests, officialRequests int
	var officialReferer string
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "images.example.test":
			primaryRequests++
		case "i0.hdslb.com":
			officialRequests++
			officialReferer = request.Header.Get("Referer")
		default:
			return nil, errors.New("unexpected poster host: " + request.URL.Hostname())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"image/jpeg"}},
			Body: io.NopCloser(bytes.NewReader(fixture)),
			ContentLength: int64(len(fixture)),
			Request: request,
		}, nil
	})}
	downloader := &Downloader{
		client: client, cfg: defaultConfig(),
		providerHosts: map[string]string{sourceDuoduo: "https://323433ssdfd.top"},
	}
	cache := newNativeCoverCache(t.TempDir(), downloader)
	drama := nativeDrama{
		ID: "duoduo:12345", Source: sourceDuoduo, SourceID: "12345",
		Title: "永生", Category: "動畫", OnlineDate: "2022", Cover: primaryURL,
	}
	placeholderPath, err := cache.loadAddress(context.Background(), drama, false)
	if err != nil {
		t.Fatal(err)
	}
	if primaryRequests != 1 {
		t.Fatalf("primary image requests = %d, want 1", primaryRequests)
	}
	officialPath, err := cache.load(context.Background(), drama, false)
	if err != nil {
		t.Fatal(err)
	}
	if officialPath == placeholderPath {
		t.Fatal("cached primary hotlink placeholder was returned instead of official Yongsheng poster")
	}
	if officialRequests != 1 || officialReferer != "https://www.bilibili.com/" {
		t.Fatalf("official poster requests=%d Referer=%q, want one request from Bilibili", officialRequests, officialReferer)
	}
}
