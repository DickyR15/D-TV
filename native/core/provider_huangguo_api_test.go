package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func huangguoEdgeTarget(t *testing.T, request *http.Request) *url.URL {
	t.Helper()
	var envelope struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}
	if request.Method != http.MethodPost || request.URL.Path != "/api/edge/request" || json.NewDecoder(request.Body).Decode(&envelope) != nil || envelope.Method != http.MethodGet {
		t.Errorf("request bypassed the edge envelope: %s %s", request.Method, request.URL)
		return &url.URL{}
	}
	target, err := url.Parse(envelope.Path)
	if err != nil {
		t.Errorf("invalid envelope path %q", envelope.Path)
		return &url.URL{}
	}
	return target
}

func huangguoEdgeReply(data any) string {
	body, _ := json.Marshal(map[string]any{"status": 1, "msg": "", "data": data})
	return string(body)
}

func TestHuangguoAIJSONLDFallbackCarriesPosterCandidates(t *testing.T) {
	body := `<script type="application/ld+json">{
		"@context":"https://schema.org",
		"@type":"VideoObject",
		"name":"JSON-LD 海報作品",
		"url":"/detail/905/",
		"image":["/images/poster-primary.webp","/images/poster-backup.jpg"],
		"thumbnailUrl":"/images/poster-thumb.jpg"
	}</script>`
	items := parseHuangguoAIJSONLDDramas(body, "AI 短劇", "https://huangguoai.com/")
	if len(items) != 1 {
		t.Fatalf("JSON-LD catalogue items = %d, want 1", len(items))
	}
	want := []string{
		"https://huangguoai.com/images/poster-primary.webp",
		"https://huangguoai.com/images/poster-backup.jpg",
		"https://huangguoai.com/images/poster-thumb.jpg",
	}
	if items[0].Cover != want[0] || items[0].CoverURL != want[0] ||
		len(items[0].CoverCandidates) != 2 ||
		items[0].CoverCandidates[0] != want[1] ||
		items[0].CoverCandidates[1] != want[2] {
		t.Fatalf("JSON-LD poster candidates = %#v", items[0])
	}
}

func TestHuangguoAIDetailUsesEdgeAPI(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() != "huangguoai.com" {
			return nil, errors.New("only synthetic huangguoai requests are allowed")
		}
		target := huangguoEdgeTarget(t, request)
		switch target.Path {
		case "/api/videos/12":
			return sourceFixtureResponse(request, 200, huangguoEdgeReply(map[string]any{
				"id": 12, "title": "合成劇", "description": "合成簡介", "cover": "/covers/12.jpg", "episode_count": 2, "is_finished": false,
				"episodes": []any{map[string]any{"ep_num": 2, "title": "第2集"}, map[string]any{"ep_num": 1, "title": ""}},
			})), nil
		}
		return sourceFixtureResponse(request, 200, `{"status":0,"msg":"合成劇集不存在"}`), nil
	})
	drama, chapters, err := d.fetchHuangguoAIDetail(context.Background(), "12")
	if err != nil || drama.ID != "huangguoai:12" || drama.Title != "合成劇" || drama.Desc != "合成簡介" || drama.ReleaseStatus != "ongoing" || drama.Cover != "https://huangguoai.com/covers/12.jpg" {
		t.Fatal("detail metadata was not read from the API", err, drama)
	}
	if len(chapters) != 2 || chapters[0].ID != "huangguoai:12:auto-1" || chapters[0].Title != "第1集" || chapters[1].ID != "huangguoai:12:ep-2" || chapters[1].PageURL != "https://huangguoai.com/video/12/ep-2/" {
		t.Fatal("chapter identities changed", chapters)
	}
	if chapterEpisodeNumber(chapters[0], 0) != 1 || chapterEpisodeNumber(chapters[1], 0) != 2 {
		t.Fatal("episode numbers could not be recovered from the API chapter metadata", chapters)
	}
	if _, _, err := d.fetchHuangguoAIDetail(context.Background(), "13"); err == nil || !strings.Contains(err.Error(), "合成劇集不存在") {
		t.Fatal("upstream failure message was discarded", err)
	}
}

func TestHuangguoAISearchUsesSitePagination(t *testing.T) {
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		target := huangguoEdgeTarget(t, request)
		if target.Path != "/api/search" || target.Query().Get("q") != "合成 关键词" {
			return nil, errors.New("only synthetic search requests are allowed")
		}
		page, _ := strconv.Atoi(target.Query().Get("page"))
		return sourceFixtureResponse(request, 200, huangguoEdgeReply(map[string]any{
			"items": []any{map[string]any{"id": 50 + page, "title": "合成結果" + strconv.Itoa(page), "episode_count": 3}},
			"pagination": map[string]any{"page": page, "pages": 2, "size": 20, "total": 2},
		})), nil
	})
	first, err := engine.nativeCatalog(context.Background(), nativeInput{Source: sourceHuangguoAI, Query: "合成 关键词", Page: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != "huangguoai:51" || !first.HasMore || first.LocalSearch {
		t.Fatal("search did not reach the site", err, first)
	}
	last, err := engine.nativeCatalog(context.Background(), nativeInput{Source: sourceHuangguoAI, Query: "合成 关键词", Page: 2})
	if err != nil || len(last.Items) != 1 || last.Items[0].ID != "huangguoai:52" || last.HasMore {
		t.Fatal("search pagination ignored the site total", err, last)
	}
}

func TestHuangguoAICategoryAndRankingUseEdgeAPI(t *testing.T) {
	transport := func(request *http.Request) (*http.Response, error) {
		target := huangguoEdgeTarget(t, request)
		switch target.Path {
		case "/api/videos/category/ai-duanju":
			return sourceFixtureResponse(request, 200, huangguoEdgeReply(map[string]any{
				"items": []any{map[string]any{"id": 71, "title": "分類劇", "episode_count": 4}},
			})), nil
		case "/api/ranks/hot":
			return sourceFixtureResponse(request, 200, huangguoEdgeReply(map[string]any{
				"items": []any{map[string]any{"video_id": 71, "title": "熱播劇", "rank": 1, "metric_label": "播放", "metric_value": 12000}},
			})), nil
		default:
			return nil, errors.New("unexpected synthetic endpoint: "+target.Path)
		}
	}
	d := sourceFixtureDownloader(t, transport)
	items, more, err := d.fetchHuangguoAICatalogPage(context.Background(), 1, "ai-duanju")
	if err != nil || len(items) != 1 || items[0].SourceID != "71" || more {
		t.Fatal("category API did not return its items", err, items, more)
	}
	page, err := d.fetchRankingPage(context.Background(), rankingBoard{ID: "huangguo-hot", Source: sourceHuangguoAI, Name: "熱播榜", path: "hot"}, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Drama.ID != "huangguoai:71" || page.Items[0].Metric != "播放 1.2萬" {
		t.Fatal("ranking API did not return its items", err, page)
	}
}

func TestHuangguoAIRejectsInvalidSourceIDBeforeRequest(t *testing.T) {
	requests := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected network request")
	})
	if _, _, err := d.fetchHuangguoAIDetail(context.Background(), "../invalid"); err == nil || requests != 0 {
		t.Fatal("invalid source ID reached the site API", err, requests)
	}
}

func TestHuangguoPosterParserKeepsLazySrcsetAndVideoPoster(t *testing.T) {
	fragment := `<div class="video-card" data-cover="/poster/card.webp"
		style="background-image:url('/poster/background.jpg')">
		<img src="/loading.gif" data-original="/poster/original.webp"
			data-src="/poster/lazy.jpg"
			data-srcset="/poster/small.jpg 320w, /poster/large.jpg 1200w">
		<video poster="/poster/video-poster.webp"></video>
	</div>`
	got := huangguoPosterCandidates(fragment, "https://huangguoai.com")
	for _, want := range []string{
		"https://huangguoai.com/poster/original.webp",
		"https://huangguoai.com/poster/lazy.jpg",
		"https://huangguoai.com/poster/small.jpg",
		"https://huangguoai.com/poster/large.jpg",
		"https://huangguoai.com/poster/video-poster.webp",
		"https://huangguoai.com/poster/card.webp",
		"https://huangguoai.com/poster/background.jpg",
	} {
		if !containsString(got, want) {
			t.Fatalf("Huangguo poster candidate %q missing from %#v", want, got)
		}
	}
}

func TestHuangguoAIDetailKeepsJSONLDPosterBackups(t *testing.T) {
	pageURL := "https://huangguoai.com/detail/905/"
	body := `<script type="application/ld+json">{
		"@type":"VideoObject",
		"url":"https://huangguoai.com/detail/905/",
		"name":"AI 詳情海報測試",
		"image":["/images/main.webp","/images/backup.jpg"],
		"thumbnailUrl":"/images/thumb.jpg"
	}</script>`
	patch := Drama{
		ID: providerDramaID(sourceHuangguoAI, "905"),
		Source: sourceHuangguoAI,
		SourceID: "905",
		Title: "AI 詳情海報測試",
	}
	got, err := parseHuangguoSortDetail(body, pageURL, patch)
	if err != nil {
		t.Fatalf("parseHuangguoSortDetail failed: %v", err)
	}
	want := []string{
		"https://huangguoai.com/images/main.webp",
		"https://huangguoai.com/images/backup.jpg",
		"https://huangguoai.com/images/thumb.jpg",
	}
	if got.Cover != want[0] || len(got.CoverCandidates) != 2 ||
		got.CoverCandidates[0] != want[1] || got.CoverCandidates[1] != want[2] {
		t.Fatalf("Huangguo AI detail poster candidates = %#v", got)
	}
}
