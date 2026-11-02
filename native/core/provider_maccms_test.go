package core

import (
	"strings"
	"testing"
)

func TestMacCMSProviderRegistry(t *testing.T) {
	want := map[string]string{
		sourceMacCMSDytt:   "天堂資源",
		sourceMacCMSSubo:   "速播資源",
		sourceMacCMSLzi:    "量子資源",
		sourceMacCMSFfzy:   "非凡資源",
		sourceMacCMSDouban: "豆瓣資源",
		sourceMacCMSZuida:  "最大資源",
	}
	if len(macCMSProviders) != len(want) {
		t.Fatalf("expected %d MacCMS providers, got %d", len(want), len(macCMSProviders))
	}
	for id, name := range want {
		provider, ok := macCMSProviders[id]
		if !ok || provider.Name != name || len(provider.APIs) == 0 || provider.Referer == "" {
			t.Errorf("invalid provider config for %s: %+v", id, provider)
		}
		if !isHuangguoProviderSource(id) || !isMacCMSSource(id) {
			t.Errorf("provider %s was not registered as an available source", id)
		}
	}
}

func TestMacCMSDramaNormalization(t *testing.T) {
	drama := macCMSDramaFromRow(sourceMacCMSDytt, map[string]any{
		"vod_id":      "78296",
		"vod_name":    "流浪地球&amp;特別版",
		"vod_pic":     "//img.example/poster.jpg",
		"vod_content": "劇情簡介",
		"type_name":   "電影",
		"vod_year":    "2026",
		"vod_remarks": "HD",
		"vod_class":   "科幻,冒險",
		"vod_hits":    "1200",
		"vod_score":   "8.2",
	})
	if drama.ID != providerDramaID(sourceMacCMSDytt, "78296") {
		t.Fatalf("unexpected drama ID: %s", drama.ID)
	}
	if drama.Title != "流浪地球&特別版" || drama.CoverURL != "https://img.example/poster.jpg" {
		t.Fatalf("title or cover was not normalized: %+v", drama)
	}
	if drama.ChannelName != "天堂資源" || drama.CategoryName != "電影" || len(drama.Tags) != 2 {
		t.Fatalf("metadata was not normalized: %+v", drama)
	}
}

func TestMacCMSChapterParsingOnlyKeepsDirectMedia(t *testing.T) {
	row := map[string]any{
		"vod_play_from": "m3u8$$$player",
		"vod_play_url": "第1集$https://cdn.example/one.m3u8#第2集$https://cdn.example/two.mp4$$$第1集$https://player.example/watch/123.html",
	}
	downloader := &Downloader{providerHosts: map[string]string{}}
	chapters := downloader.macCMSParseChapters(sourceMacCMSDytt, "78296", row)
	if len(chapters) != 2 {
		t.Fatalf("expected two direct media episodes, got %d: %+v", len(chapters), chapters)
	}
	if chapters[0].Source != sourceMacCMSDytt || chapters[0].VideoURL != "https://cdn.example/one.m3u8" {
		t.Fatalf("first episode was not parsed correctly: %+v", chapters[0])
	}
	if chapters[1].VideoURL != "https://cdn.example/two.mp4" {
		t.Fatalf("second episode was not parsed correctly: %+v", chapters[1])
	}
}

func TestMacCMSDirectMediaURL(t *testing.T) {
	for _, test := range []struct {
		url  string
		want bool
	}{
		{"https://cdn.example/master.m3u8?token=abc", true},
		{"https://cdn.example/movie.mp4", true},
		{"https://player.example/watch/123.html", false},
		{"javascript:alert(1)", false},
		{"https://user:pass@cdn.example/master.m3u8", false},
	} {
		if got := macCMSDirectMediaURL(test.url); got != test.want {
			t.Errorf("macCMSDirectMediaURL(%q) = %v, want %v", test.url, got, test.want)
		}
	}
}

func TestMacCMSRowsAndCategories(t *testing.T) {
	payload := map[string]any{
		"class": []any{
			map[string]any{"type_id": "1", "type_name": "電影"},
			map[string]any{"type_id": "2", "type_name": "劇集"},
		},
		"list": []any{
			map[string]any{"vod_id": "1"},
			map[string]any{"vod_id": "2"},
		},
	}
	if got := len(macCMSRows(payload)); got != 2 {
		t.Fatalf("expected two list rows, got %d", got)
	}
	if got := len(macCMSClasses(payload)); got != 2 {
		t.Fatalf("expected two categories, got %d", got)
	}
}

func TestMacCMSProviderEndpointPriorities(t *testing.T) {
	wantPaths := map[string]string{
		sourceMacCMSDytt:  "/from/dyttm3u8/at/json/",
		sourceMacCMSSubo:  "/from/subm3u8/at/json/",
		sourceMacCMSDouban: "/from/dbm3u8/at/josn/",
	}
	for source, path := range wantPaths {
		provider := macCMSProviders[source]
		if len(provider.APIs) == 0 || !strings.Contains(provider.APIs[0], path) {
			t.Errorf("%s should prefer its dedicated M3U8 API %q; got %+v", source, path, provider.APIs)
		}
	}

	ffzyProvider := macCMSProviders[sourceMacCMSFfzy]
	if len(ffzyProvider.APIs) == 0 || ffzyProvider.APIs[0] != "https://api.ffzyapi.com/api.php/provide/vod/" {
		t.Fatalf("非凡資源 should prefer its verified JSON API root, got %+v", ffzyProvider.APIs)
	}
	lziProvider := macCMSProviders[sourceMacCMSLzi]
	if len(lziProvider.APIs) == 0 || lziProvider.APIs[0] != "https://cj.lziapi.com/api.php/provide/vod/" {
		t.Fatalf("量子 should use its documented generic JSON API, got %+v", lziProvider.APIs)
	}
	maxProvider := macCMSProviders[sourceMacCMSZuida]
	if len(maxProvider.APIs) == 0 || !strings.Contains(maxProvider.APIs[0], "api.zuidapi.com") {
		t.Fatalf("最大資源 must use its own API host first, got %+v", maxProvider.APIs)
	}
	for _, endpoint := range maxProvider.APIs {
		if strings.Contains(endpoint, "zuidazy.me") {
			t.Fatalf("最大點播 endpoint must not be mixed into 最大資源: %s", endpoint)
		}
	}
}

func TestMacCMSPayloadUsableRequiresRowsForCatalogAndDetail(t *testing.T) {
	empty := map[string]any{"code": 1, "list": []any{}}
	rows := map[string]any{"code": 1, "list": []any{map[string]any{"vod_id": 123, "vod_name": "測試"}}}
	for _, action := range []string{"list", "detail"} {
		params := url.Values{"ac": []string{action}}
		if macCMSPayloadUsable(params, empty) {
			t.Errorf("%s response without rows should not be treated as a working API", action)
		}
		if !macCMSPayloadUsable(params, rows) {
			t.Errorf("%s response with rows should be accepted", action)
		}
	}
	if !macCMSPayloadUsable(url.Values{"ac": []string{"class"}}, map[string]any{"class": []any{}}) {
		t.Fatal("non-catalog JSON responses should remain usable")
	}
}

func TestMacCMSPlayLineNamesSupportsCommaSeparatedGroups(t *testing.T) {
	got := macCMSPlayLineNames("liangzi,lzm3u8")
	if len(got) != 2 || got[0] != "liangzi" || got[1] != "lzm3u8" {
		t.Fatalf("comma-separated play groups were not parsed: %#v", got)
	}
	got = macCMSPlayLineNames("liangzi" + strings.Repeat("$", 3) + "lzm3u8")
	if len(got) != 2 || got[0] != "liangzi" || got[1] != "lzm3u8" {
		t.Fatalf("MacCMS three-dollar play groups were not parsed: %#v", got)
	}
}

func TestMacCMSChapterParserMapsCommaSeparatedSourceNames(t *testing.T) {
	row := map[string]any{
		"vod_play_from": "liangzi,lzm3u8",
		"vod_play_url": "第1集$https://cdn.example/cloud.html" + strings.Repeat("$", 3) + "第1集$https://cdn.example/episode.m3u8",
	}
	downloader := &Downloader{providerHosts: map[string]string{}}
	chapters := downloader.macCMSParseChapters(sourceMacCMSLzi, "164761", row)
	if len(chapters) != 1 {
		t.Fatalf("expected one direct M3U8 episode, got %d: %+v", len(chapters), chapters)
	}
	if chapters[0].Title != "第1集" || chapters[0].VideoURL != "https://cdn.example/episode.m3u8" {
		t.Fatalf("second play group was not parsed as a direct episode: %+v", chapters[0])
	}
}
