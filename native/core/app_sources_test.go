package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type sourceFixtureTransport func(*http.Request) (*http.Response, error)

func (transport sourceFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func sourceFixtureResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request}
}

func sourceFixtureDownloader(t *testing.T, transport sourceFixtureTransport) *Downloader {
	t.Helper()
	d := &Downloader{cfg: Config{dataDir: t.TempDir(), Retries: 1, PageSize: 30, MaxPagesPerSort: 1},
		client: &http.Client{Transport: transport}, limiter: newRequestLimiter(3, 0), providerHosts: map[string]string{}}
	d.hongguoClient().state.Registered = true
	return d
}

func sourceFixtureEngine(t *testing.T, transport sourceFixtureTransport) *nativeEngine {
	t.Helper()
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	engine.downloader.client.Transport = transport
	engine.downloader.limiter = newRequestLimiter(3, 0)
	engine.downloader.cfg.Retries = 1
	engine.downloader.hongguoClient().state.Registered = true
	return engine
}

func awaitSourceTask(t *testing.T, engine *nativeEngine, source string) nativeSourceStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := engine.sourceStatus(source)
		if !status.Running {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("source task did not finish")
	return nativeSourceStatus{}
}

func TestSourceUpdateHistoryPageLimitBoundsHongguoWork(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		operation string
		maxPages int
		want     int
	}{
		{name: "red fruit update scans one history page", source: sourceHongguo, operation: "update", maxPages: 50, want: 1},
		{name: "red fruit more loads one history page", source: sourceHongguo, operation: "more", maxPages: 50, want: 1},
		{name: "other provider keeps configured batch update", source: sourceYeguo, operation: "update", maxPages: 50, want: 49},
		{name: "other provider continuation remains one page", source: sourceYeguo, operation: "more", maxPages: 50, want: 1},
		{name: "metadata operation does not page catalog", source: sourceHongguo, operation: "metadata", maxPages: 50, want: 0},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := sourceUpdateHistoryPageLimit(testCase.source, testCase.operation, testCase.maxPages); got != testCase.want {
				t.Fatalf("history page limit = %d, want %d", got, testCase.want)
			}
		})
	}
}


func TestHongguoMetadataCandidatesSelectOnlyMissingFieldsAndIncludeAllBatches(t *testing.T) {
	now := time.Now()
	items := make([]nativeDrama, 24)
	for index := range items {
		items[index] = nativeDrama{
			ID: fmt.Sprintf("hongguo:%d", index+1), Source: sourceHongguo,
			Title: fmt.Sprintf("紅果劇集%d", index+1), Episodes: 12,
			Description: "完整簡介", OnlineDate: "2026-01-01",
		}
	}
	// More than one batch needs enrichment; selection must discover the whole
	// pending set so progress reports the real total before batches are run.
	for index := 0; index < 19; index++ {
		items[index].Episodes = 0
		items[index].Description = ""
	}
	// A title with an episode count but optional fields absent must not cause
	// a detail request just to fill synopsis/date metadata.
	items[19].Description = ""
	items[19].OnlineDate = ""
	// Recent checks and active retry backoffs must not be queried again.
	record := nativeSourceRecord{
		MetadataChecked: map[string]time.Time{items[2].ID: now},
		MetadataRetryAt: map[string]time.Time{items[3].ID: now.Add(time.Minute)},
	}
	candidates := sourceMetadataCandidates(items, sourceHongguo, "metadata", record, now)
	if len(candidates) != 17 {
		t.Fatalf("found %d pending titles, want 17", len(candidates))
	}
	for _, drama := range candidates {
		if drama.ID == items[2].ID || drama.ID == items[3].ID || drama.ID == items[19].ID || drama.ID == items[20].ID {
			t.Fatalf("selected skipped or complete-core-data title: %s", drama.ID)
		}
	}
	if sourceDramaNeedsMetadata(sourceHongguo, "metadata", items[19]) {
		t.Fatal("optional synopsis/date fields alone should not require detail enrichment")
	}
	if sourceDramaNeedsMetadata(sourceHongguo, "metadata", items[20]) {
		t.Fatal("complete Red Fruit metadata was marked as needing enrichment")
	}
	if !sourceDramaNeedsMetadata(sourceHongguo, "metadata", nativeDrama{Source: sourceHongguo, Episodes: 0, OnlineDate: "2026-01-01"}) {
		t.Fatal("missing episode count was not marked as needing enrichment")
	}
}

func TestSourceHistoryPagingUsesSingleRemotePageForHongguo(t *testing.T) {
	ctx := context.WithValue(context.Background(), backgroundCatalogKey{}, true)
	hongguoCtx := sourceHistoryPageContext(ctx, sourceHongguo)
	if got, ok := hongguoCtx.Value(backgroundCatalogKey{}).(bool); !ok || got {
		t.Fatal("Red Fruit history paging kept the expansive background scan")
	}
	otherCtx := sourceHistoryPageContext(ctx, sourceYeguo)
	if got, ok := otherCtx.Value(backgroundCatalogKey{}).(bool); !ok || !got {
		t.Fatal("non-Red-Fruit source unexpectedly lost background paging behavior")
	}
}

func TestSourceUpdatesPreservePagesOtherSourcesAndMetadata(t *testing.T) {
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		target := huangguoEdgeTarget(t, request)
		if !strings.Contains(target.Path, "/api/videos/category/") {
			return nil, errors.New("only catalog API requests are allowed")
		}
		id := "new"
		if target.Query().Get("page") == "3" {
			id = "continued"
		}
		return sourceFixtureResponse(request, 200, huangguoEdgeReply(map[string]any{
			"items": []any{map[string]any{"id": id, "title": "合成短劇", "description": "完整資料", "episode_count": 2}},
		})), nil
	})
	engine.catalogs[sourceHuangguoAI] = []nativeDrama{{ID: "huangguoai:old", Source: sourceHuangguoAI, Title: "原有短剧", Description: "保留资料", Episodes: 2}}
	engine.catalogStates[sourceHuangguoAI] = nativeCatalogState{Page: 2, HasMore: true}
	engine.catalogs[sourceHongguo] = []nativeDrama{{ID: "hongguo:1", Source: sourceHongguo, Title: "其他站源"}}
	if err := engine.updateSource(context.Background(), sourceHuangguoAI, "update"); err != nil {
		t.Fatal(err)
	}
	page := engine.nativeCached(sourceHuangguoAI)
	if len(page.Items) != 3 || page.Page != 3 || page.HasMore {
		t.Fatalf("lost refresh/tail state: %+v", page)
	}
	if len(engine.nativeCached(sourceHongguo).Items) != 1 {
		t.Fatal("updated another source")
	}
	engine.saveCatalogCache(sourceHuangguoAI, &nativeCatalogResult{Items: []nativeDrama{{ID: "huangguoai:old", Title: "新名称"}}, Page: 1, HasMore: true})
	page = engine.nativeCached(sourceHuangguoAI)
	if page.Page != 3 || page.HasMore || len(page.Items) != 3 || page.Items[0].Description != "保留资料" {
		t.Fatal("head refresh discarded cached metadata or pagination", page)
	}
}

func TestYeguoSourceUpdateBatchesConfiguredPagesWithPost(t *testing.T) {
	var pages []string
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "api.yeguo.test" || request.URL.Path != "/api/theater/exploreList" {
			t.Fatalf("unexpected yeguo catalog request: %s", request.URL.String())
		}
		if request.Method != http.MethodPost {
			t.Fatalf("yeguo catalog must use POST for pagination, got %s", request.Method)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		page := request.Form.Get("page")
		pages = append(pages, page)
		id := "90" + page
		body := fmt.Sprintf(`{"status":"1","data":{"list":[{"video_id":%q,"title":"野果分页%s","description":"完整资料","episode_count":"6","serialize_status":"2"}],"page":%s,"limit":20,"total":60,"has_more":%q}}`, id, page, page, map[bool]string{true: "1", false: "0"}[page != "3"])
		return sourceFixtureResponse(request, http.StatusOK, body), nil
	})
	engine.downloader.cfg.MaxPagesPerSort = 3
	engine.downloader.yeguoClient().access = &yeguoAccess{base: "https://api.yeguo.test", identifier: "fixture-trace", loadedAt: time.Now()}

	if err := engine.updateSource(context.Background(), sourceYeguo, "update"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(pages, ",") != "1,2,3" {
		t.Fatalf("yeguo update did not batch configured pages: %v", pages)
	}
	page := engine.nativeCached(sourceYeguo)
	if len(page.Items) != 3 || page.Page != 3 || page.HasMore {
		t.Fatalf("wrong yeguo source cache after update: %+v", page)
	}
}

func TestSourceJobsDeduplicateCancelAndRecoverAfterRestart(t *testing.T) {
	if buildAllSources != "true" {
		t.Skip("full edition source lifecycle")
	}
	var calls atomic.Int32
	started := make(chan struct{})
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	engine.catalogs[sourceHuangguoVideo] = []nativeDrama{{ID: "huangguo-video:video/fixture", Source: sourceHuangguoVideo, Title: "已有缓存"}}
	if _, err := engine.startSourceTask(sourceHuangguoVideo, "update", nativeDrama{}); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := engine.startSourceTask(sourceHuangguoVideo, "check", nativeDrama{}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || engine.sourceStatus(sourceHuangguoVideo).Operation != "update" {
		t.Fatal("duplicate request replaced the running task")
	}
	if count, _ := engine.workLease("", ""); count != 1 {
		t.Fatal("background service cannot see source work")
	}
	engine.cancelSourceTask(sourceHuangguoVideo)
	status := awaitSourceTask(t, engine, sourceHuangguoVideo)
	if status.Count != 1 || status.Stage != "已停止" {
		t.Fatal("cancel discarded cache or did not stop", status)
	}
	engine.changeSourceRecord(sourceHuangguoVideo, func(record *nativeSourceRecord) { record.Running = true })
	restored := &nativeEngine{directory: engine.directory}
	restored.loadSourceRecords()
	if record := restored.sourceRecords[sourceHuangguoVideo]; record.Running || record.Error == "" {
		t.Fatal("interrupted work stayed running")
	}
}

func TestSourceTaskErrorMessageDistinguishesRequestTimeout(t *testing.T) {
	requestMessage := sourceTaskErrorMessage(nil, context.DeadlineExceeded)
	if !strings.Contains(requestMessage, "單一步驟連線逾時") {
		t.Fatalf("request timeout was mislabeled: %q", requestMessage)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	taskMessage := sourceTaskErrorMessage(ctx.Err(), context.DeadlineExceeded)
	if !strings.Contains(taskMessage, "本次片源任務超時") {
		t.Fatalf("task deadline was not reported accurately: %q", taskMessage)
	}
}

func TestSourceHealthRejectsHTML200AndPreservesFailureStage(t *testing.T) {
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/videos":
			return sourceFixtureResponse(request, 200, `<article class="video-card"><a href="/video/fixture" title="合成检测剧">合成检测剧</a></article>`), nil
		case "/video/fixture":
			return sourceFixtureResponse(request, 200, `<h1>合成检测剧</h1><div data-hls="https://media.example.test/master.m3u8"></div>`), nil
		case "/master.m3u8":
			response := sourceFixtureResponse(request, 200, `<html><title>Just a moment</title><script>_cf_chl_opt={}</script>Cloudflare</html>`)
			response.Header.Set("Content-Type", "text/html")
			return response, nil
		default:
			t.Error("unexpected resource, including images", request.URL.Path)
			return nil, errors.New("blocked")
		}
	})
	err := engine.checkSource(context.Background(), sourceHuangguoVideo, nativeDrama{}, true)
	status := engine.sourceStatus(sourceHuangguoVideo)
	if err == nil || status.Health == nil || status.Health.State != "failed" || len(status.Health.Steps) != 3 || status.Health.Steps[2].State != "failed" {
		t.Fatal("HTML 200 was reported as playable", err, status.Health)
	}
	if status.Health.Steps[2].HTTPStatus != 200 || !strings.Contains(status.Health.Steps[2].Message, "瀏覽器驗證") {
		t.Fatal("missing CF failure detail", status.Health.Steps)
	}
}

func TestSourceHealthChecksHLSKeyAndBoundedMedia(t *testing.T) {
	var segments atomic.Int32
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/videos":
			return sourceFixtureResponse(request, 200, `<article class="video-card"><a href="/video/fixture" title="合成检测剧">合成检测剧</a></article>`), nil
		case "/video/fixture":
			return sourceFixtureResponse(request, 200, `<div data-hls="https://huangguo.video/master.m3u8"></div>`), nil
		case "/master.m3u8":
			return sourceFixtureResponse(request, 200, "#EXTM3U\n#EXT-X-STREAM-INF:RESOLUTION=320x480\nmedia.m3u8?signature=a%2Fb\n"), nil
		case "/media.m3u8":
			if request.URL.RawQuery != "signature=a%2Fb" {
				t.Error("signed media URL changed")
			}
			return sourceFixtureResponse(request, 200, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"/api/hls_key/fixture\"\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST"), nil
		case "/api/preview-token":
			return sourceFixtureResponse(request, 200, `{"ok":true,"token":"fixture-preview","expires_in":300}`), nil
		case "/api/hls_key/fixture":
			if request.Header.Get("X-Preview-Token") != "fixture-preview" {
				t.Error("missing preview token")
			}
			return sourceFixtureResponse(request, 200, "0123456789abcdef"), nil
		case "/segment.ts":
			segments.Add(1)
			if request.Header.Get("Range") != "bytes=0-1023" || request.Header.Get("X-Preview-Token") != "" {
				t.Error("unbounded probe or leaked token")
			}
			return sourceFixtureResponse(request, 206, strings.Repeat("s", 2048)), nil
		default:
			t.Error("unexpected resource", request.URL.Path)
			return nil, errors.New("blocked")
		}
	})
	if err := engine.checkSource(context.Background(), sourceHuangguoVideo, nativeDrama{}, true); err != nil {
		t.Fatal(err)
	}
	health := engine.sourceStatus(sourceHuangguoVideo).Health
	// The route selector probes the first segment once to choose a playable
	// fallback, then the final health stage probes it again and records the
	// explicit media-connectivity result. Both requests are range-bounded above.
	if health.State != "ok" || len(health.Steps) != 5 || segments.Load() != 2 {
		t.Fatalf("incomplete health report: segments=%d health=%+v", segments.Load(), health)
	}
	body, err := os.ReadFile(filepath.Join(engine.directory, "sources.json"))
	if err != nil || strings.Contains(string(body), "fixture-preview") {
		t.Fatal("health persistence leaked preview token", err)
	}
	var saved map[string]nativeSourceRecord
	if json.Unmarshal(body, &saved) != nil || saved[sourceHuangguoVideo].Health == nil {
		t.Fatal("health report was not persisted")
	}
}

func TestSourceBackoffUsesRelativeTimeAndRetainsTypedError(t *testing.T) {
	backoff := &requestBackoff{host: "huangguo.video", status: 403, until: time.Now().Add(time.Minute)}
	if !strings.Contains(backoff.Error(), "秒後") || strings.Contains(backoff.Error(), backoff.until.Format("15:04:05")) {
		t.Fatal("retry time depends on native timezone")
	}
	var restored *requestBackoff
	if !errors.As(publicError(fmt.Errorf("playlist: %w", backoff)), &restored) {
		t.Fatal("redaction discarded structured retry time")
	}
}


func TestWaitForPlaybackIdlePausesCatalogDuringPlayback(t *testing.T) {
	engine := &nativeEngine{
		directory: t.TempDir(),
		playbacks: map[string]nativePlaybackChoice{
			"active-session": {created: time.Now()},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
	defer cancel()
	if err := engine.waitForPlaybackIdle(ctx, sourceHongguo, "繼續載入歷史目錄"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("active playback should pause catalog work until canceled, got %v", err)
	}

	engine.mu.Lock()
	delete(engine.playbacks, "active-session")
	engine.mu.Unlock()
	if err := engine.waitForPlaybackIdle(context.Background(), sourceHongguo, "繼續載入歷史目錄"); err != nil {
		t.Fatalf("catalog work should resume after playback releases its session: %v", err)
	}
}

func TestResolveExtraAdultMediaUsesOfficial91PornaPlaybackAPI(t *testing.T) {
	const (
		id = "key_123"
		detailURL = "https://91porna.com/comic/index/avdetail?video_key=key_123"
		mediaURL = "https://media.example.test/master.m3u8?token=abc"
		mediaID = "987654"
	)
	seenDetail, seenPlaybackAPI, seenPlaylist := 0, 0, 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Host == "91porna.com" &&
			request.Method == http.MethodGet && request.URL.Path == "/comic/index/avdetail":
			seenDetail++
			if got := request.Header.Get("User-Agent"); got != adultUserAgent {
				return nil, fmt.Errorf("91Porna detail request UA = %q, want upstream Chrome UA", got)
			}
			return sourceFixtureResponse(request, http.StatusOK,
				"<html><head><title>91Porna API 測試作品</title></head><body><div data-video=\""+mediaID+"\"></div></body></html>"), nil
		case request.URL.Host == "91porna.com" &&
			request.Method == http.MethodPost && request.URL.Path == "/api/videos/"+mediaID+"/playback":
			seenPlaybackAPI++
			if got := request.Header.Get("User-Agent"); got != adultUserAgent {
				return nil, fmt.Errorf("91Porna playback API UA = %q", got)
			}
			if got := request.Header.Get("Referer"); got != detailURL {
				return nil, fmt.Errorf("91Porna playback API Referer = %q", got)
			}
			if got := request.Header.Get("Content-Type"); got != "application/json" {
				return nil, fmt.Errorf("91Porna playback API Content-Type = %q", got)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil || string(body) != "{}" {
				return nil, fmt.Errorf("91Porna playback API body = %q, err=%v", body, err)
			}
			return sourceFixtureResponse(request, http.StatusOK,
				`{"data":{"streams":{"main":"`+mediaURL+`"}}}`), nil
		case request.URL.Host == "media.example.test" && request.Method == http.MethodGet &&
			request.URL.Path == "/master.m3u8":
			seenPlaylist++
			if got := request.Header.Get("Referer"); got != detailURL {
				return nil, fmt.Errorf("91Porna media Referer = %q", got)
			}
			return sourceFixtureResponse(request, http.StatusOK,
				"#EXTM3U\n#EXTINF:6,\nsegment.ts?token=abc\n#EXT-X-ENDLIST\n"), nil
		default:
			return nil, fmt.Errorf("unexpected 91Porna playback request: %s %s", request.Method, request.URL)
		}
	})
	chapter := Chapter{
		ID: providerChapterID(source91Porna, id, detailURL),
		Source: source91Porna, PageURL: detailURL, Referer: detailURL,
		VideoURL: "adult-page://" + detailURL,
	}
	media, err := d.resolveExtraAdultMedia(context.Background(), Task{
		DramaID: providerDramaID(source91Porna, id), Chapter: chapter, Index: 1,
	})
	if err != nil {
		t.Fatalf("resolveExtraAdultMedia(91Porna) failed: %v", err)
	}
	if seenDetail != 1 || seenPlaybackAPI != 1 || seenPlaylist != 1 {
		t.Fatalf("91Porna request counts detail/API/playlist = %d/%d/%d, want 1/1/1",
			seenDetail, seenPlaybackAPI, seenPlaylist)
	}
	if media.URL != mediaURL || !strings.HasPrefix(media.Playlist, "#EXTM3U") {
		t.Fatalf("official 91Porna API media was not prepared: %#v", media)
	}
}

func TestMadouHealthCheckWarnsOnCatalog404ButVerifiesPlaybackFromCachedDrama(t *testing.T) {
	const detailPath = "/sample-123.html"
	const sharePath = "/share/fixture"
	const mediaPath = "/videos/fixture.m3u8"
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Host == "madou.club" && request.URL.Path == "/":
			return sourceFixtureResponse(request, http.StatusNotFound, "not found"), nil
		case request.URL.Host == "madou.club" && request.URL.Path == detailPath:
			body := "<html><head><meta property='og:title' content='madou 健康檢查測試作品'></head><body><iframe id='video' src='https://dash.madou.club" + sharePath + "'></iframe></body></html>"
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		case request.URL.Host == "dash.madou.club" && request.URL.Path == sharePath:
			return sourceFixtureResponse(request, http.StatusOK, "var token='abc'; var m3u8='"+mediaPath+"?token=abc';"), nil
		case request.URL.Host == "dash.madou.club" && request.URL.Path == mediaPath:
			return sourceFixtureResponse(request, http.StatusOK, "#EXTM3U\n#EXTINF:6,\nsegment.ts?token=abc\n#EXT-X-ENDLIST\n"), nil
		case request.URL.Host == "dash.madou.club" && request.URL.Path == "/videos/segment.ts":
			response := sourceFixtureResponse(request, http.StatusPartialContent, strings.Repeat("s", 1024))
			response.Header.Set("Content-Type", "video/mp2t")
			return response, nil
		default:
			return nil, fmt.Errorf("unexpected madou health-check request: %s", request.URL)
		}
	})
	selected := nativeDrama{
		ID: providerDramaID(sourceMadou, "sample-123.html"),
		Source: sourceMadou,
		SourceID: "sample-123.html",
		Title: "madou 健康檢查測試作品",
	}
	engine.mu.Lock()
	engine.catalogs[sourceMadou] = []nativeDrama{selected}
	engine.mu.Unlock()
	if err := engine.checkSource(context.Background(), sourceMadou, selected, true); err != nil {
		t.Fatalf("madou health check should verify playback despite root catalog 404: %v", err)
	}
	health := engine.sourceStatus(sourceMadou).Health
	if health == nil {
		t.Fatal("madou health report was not saved")
	}
	if health.State != "warning" {
		t.Fatalf("madou health state = %q, want warning", health.State)
	}
	if len(health.Steps) != 5 || health.Steps[0].State != "warning" {
		t.Fatalf("madou health steps should report a catalog warning and continue: %#v", health.Steps)
	}
	for i := 1; i < len(health.Steps); i++ {
		if health.Steps[i].State != "ok" {
			t.Fatalf("madou playback check step %q = %q, want ok: %s", health.Steps[i].Name, health.Steps[i].State, health.Steps[i].Message)
		}
	}
}

func TestResolveExtraAdultPPPUsesEmbeddedPlayerAndSignedHLS(t *testing.T) {
	const (
		id = "abc123"
		detailURL = "https://ppp.porn/v/abc123/"
		embedURL = "https://cdnlab.live/embed/abc123"
		mediaURL = "https://hokcap.ppp.porn/stream/token/1760000000/1/12345/12345.m3u8?token=a%2Fb"
	)
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "ppp.porn" || request.URL.Hostname() == "cdnlab.live" {
			if got := request.Header.Get("User-Agent"); got != adultUserAgent {
				return nil, fmt.Errorf("PPP request user-agent = %q", got)
			}
			if request.Context().Value(nativeRPZFallbackKey{}) != true {
				return nil, fmt.Errorf("PPP request did not enable verified RPZ DNS retry: %s", request.URL)
			}
		}
		switch {
		case request.URL.Host == "ppp.porn" && request.URL.Path == "/v/abc123/":
			return sourceFixtureResponse(request, http.StatusOK, "<iframe id=\"player\" src=\"https://cdnlab.live/embed/abc123\"></iframe>"), nil
		case request.URL.Host == "cdnlab.live" && request.URL.Path == "/embed/abc123":
			return sourceFixtureResponse(request, http.StatusOK, "window.player = {\"file\":\""+mediaURL+"\"};"), nil
		case request.URL.Host == "hokcap.ppp.porn" && strings.Contains(request.URL.Path, "/stream/"):
			if request.Header.Get("Referer") != embedURL {
				return nil, fmt.Errorf("PPP playlist Referer = %q, want %q", request.Header.Get("Referer"), embedURL)
			}
			return sourceFixtureResponse(request, http.StatusOK, "#EXTM3U\n#EXTINF:6,\nsegment.ts?token=a%2Fb\n#EXT-X-ENDLIST\n"), nil
		default:
			return nil, fmt.Errorf("unexpected PPP playback request: %s", request.URL)
		}
	})
	chapter := Chapter{
		ID: providerChapterID(sourcePPP, id, detailURL),
		Source: sourcePPP,
		Title: "正片",
		PageURL: detailURL,
		Referer: detailURL,
		VideoURL: "adult-page://" + detailURL,
	}
	media, err := d.resolveExtraAdultMedia(context.Background(), Task{
		DramaID: providerDramaID(sourcePPP, id),
		Chapter: chapter,
		Index: 1,
	})
	if err != nil {
		t.Fatalf("resolveExtraAdultMedia(PPP) failed: %v", err)
	}
	if media.URL != mediaURL || !strings.HasPrefix(media.Playlist, "#EXTM3U") || media.Referer != embedURL {
		t.Fatalf("PPP embedded player did not produce the expected prepared media: %#v", media)
	}
}

func TestExtraAdultRPZFallbackIsEnabledOnlyFor91PornaAndPPP(t *testing.T) {
	for _, test := range []struct {
		source string
		want bool
	}{
		{source91Porna, true},
		{sourcePPP, true},
		{sourceMadou, false},
	} {
		t.Run(test.source, func(t *testing.T) {
			d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
				got := request.Context().Value(nativeRPZFallbackKey{}) == true
				if got != test.want {
					return nil, fmt.Errorf("RPZ fallback context = %v, want %v", got, test.want)
				}
				return sourceFixtureResponse(request, http.StatusOK, "<html></html>"), nil
			})
			_, _, err := extraAdultFetch(context.Background(), d, test.source,
				extraAdultBaseURL(test.source)+"/", extraAdultBaseURL(test.source)+"/")
			if err != nil {
				t.Fatalf("extraAdultFetch failed: %v", err)
			}
		})
	}
}

func TestExtraAdultPPPStreamURLsCollectsOnlyFullPlayerCandidates(t *testing.T) {
	first := "https://hokcap.ppp.porn/stream/a/1760000000/1/12345/12345.m3u8?token=one"
	second := "https://cdnlab.live/stream/b/1760000000/1/67890/67890.m3u8?token=two"
	body := "window.player = {file:'" + first + "', backup:'" + second + "'}; " +
		"<source src='https://cdn-c.example.test/fallback.mp4'>"
	got := extraAdultPPPStreamURLs(body, "https://cdnlab.live/embed/abc123")
	want := []string{first, second}
	if len(got) != len(want) { t.Fatalf("PPP full-stream candidates = %#v, want %#v", got, want) }
	for i := range want {
		if got[i] != want[i] { t.Fatalf("PPP candidate %d=%q, want %q", i, got[i], want[i]) }
	}
}

func TestResolveExtraAdultPPPUsesSecondPlaylistWhenFirstFails(t *testing.T) {
	const pageURL = "https://ppp.porn/v/multi/"
	const firstURL = "https://cdnlab.live/stream/first/1760000000/1/1/1.m3u8"
	const secondURL = "https://cdnlab.live/stream/second/1760000000/1/2/2.m3u8"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Host == "ppp.porn" && request.URL.Path == "/v/multi/":
			return sourceFixtureResponse(request, http.StatusOK,
				"window.player={file:'"+firstURL+"',backup:'"+secondURL+"'};"), nil
		case request.URL.String() == firstURL:
			return sourceFixtureResponse(request, http.StatusServiceUnavailable, "unavailable"), nil
		case request.URL.String() == secondURL:
			return sourceFixtureResponse(request, http.StatusOK, "#EXTM3U\n#EXTINF:6,\nsegment.ts\n#EXT-X-ENDLIST\n"), nil
		default:
			return nil, fmt.Errorf("unexpected PPP request: %s", request.URL)
		}
	})
	chapter := Chapter{
		ID: providerChapterID(sourcePPP, "multi", pageURL),
		Source: sourcePPP, Title: "正片", PageURL: pageURL, Referer: pageURL,
		VideoURL: "adult-page://" + pageURL,
	}
	media, err := d.resolveExtraAdultMedia(context.Background(), Task{
		DramaID: providerDramaID(sourcePPP, "multi"), Chapter: chapter, Index: 1,
	})
	if err != nil { t.Fatalf("PPP should move to the next playlist before returning an error: %v", err) }
	if media.URL != secondURL || !strings.HasPrefix(media.Playlist, "#EXTM3U") {
		t.Fatalf("PPP did not select the playable second route: %#v", media)
	}
}
