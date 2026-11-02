package core

import (
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
		if !ok || provider.Name != name || provider.API == "" {
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
	chapters := macCMSParseChapters(sourceMacCMSDytt, "78296", row)
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
