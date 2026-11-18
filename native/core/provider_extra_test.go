package core

import (
	"strings"
	"testing"
)

func TestExtraAdultImportedModuleCards(t *testing.T) {
	tests := []struct {
		name, source, base, html, wantID, wantTitle string
	}{
		{
			name: "SupJav", source: sourceSupJav, base: supjavBaseURL,
			html: `<div class="box-item"><a href="/video/sample-123"><img src="https://img.supjav.blog/sample.jpg" alt="SupJav 海報"><div class="detail">SupJav 測試作品</div></a></div>`,
			wantID: "video/sample-123", wantTitle: "SupJav 測試作品",
		},
		{
			name: "PPP", source: sourcePPP, base: pppBaseURL,
			html: `<div class="item card-video"><a href="https://ppp.porn/v/abc123/"><img data-src="https://cdnlab.live/abc.jpg" alt="PPP 測試作品"><figcaption class="card-video__duration">6:57</figcaption></a></div>`,
			wantID: "abc123", wantTitle: "PPP 測試作品",
		},
		{
			name: "Madou", source: sourceMadou, base: madouBaseURL,
			html: `<article class="excerpt"><a class="thumbnail" href="https://madou.club/sample-123.html"><img data-src="https://madou.club/poster.jpg"></a><h2>麻豆測試作品</h2></article>`,
			wantID: "sample-123.html", wantTitle: "麻豆測試作品",
		},
		{
			name: "91Porn", source: source91Porna, base: ninetyOneBaseURL,
			html: `<div class="video-item"><a href="/comic/index/avdetail?video_key=key_123"><img data-src="https://91porna.com/poster.jpg" alt="91 測試作品"></a><span>12:34</span></div>`,
			wantID: "key_123", wantTitle: "91 測試作品",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			segments := extraAdultSegments(test.html, test.source)
			if len(segments) != 1 { t.Fatalf("expected one card segment, got %d", len(segments)) }
			item, ok := extraAdultParseCard(test.source, segments[0], test.base)
			if !ok { t.Fatal("card parser rejected valid module fixture") }
			if item.SourceID != test.wantID { t.Fatalf("source ID = %q, want %q", item.SourceID, test.wantID) }
			if item.Title != test.wantTitle { t.Fatalf("title = %q, want %q", item.Title, test.wantTitle) }
			if item.Cover == "" { t.Fatal("poster URL was not parsed") }
		})
	}
}

func TestExtraAdultSupJavBlogIsPrimaryHost(t *testing.T) {
	if got := extraAdultBaseURL(sourceSupJav); got != "https://supjav.blog" { t.Fatalf("SupJav base URL = %q", got) }
	if got := providerSourceForURL("https://supjav.blog/video/sample-123"); got != sourceSupJav { t.Fatalf("SupJav host mapped to %q", got) }
	if got := extraAdultDetailURL(sourceSupJav, "video/sample-123"); got != "https://supjav.blog/video/sample-123" { t.Fatalf("SupJav detail URL = %q", got) }
}

func TestExtraAdultPlaybackMarkupPatterns(t *testing.T) {
	body := `<iframe src="https://upload18.com/embed/123"></iframe><script>setVideoHLS("https://cdn.supjav.blog/video/index.m3u8?token=x")</script>`
	if got := extraAdultFrame("https://supjav.blog/video/example", body); got != "https://upload18.com/embed/123" { t.Fatalf("iframe URL = %q", got) }
	if got := extraAdultHLS(body, "https://supjav.blog/video/example"); !strings.Contains(got, "/index.m3u8?token=x") { t.Fatalf("HLS URL = %q", got) }
}

func TestExtraAdultCategoryAndSearchPaths(t *testing.T) {
	got, err := extraAdultCategoryPath(sourceSupJav, 1, "uncensored")
	if err != nil || got != "/uncensored?page=1" { t.Fatalf("SupJav category path = %q, err=%v", got, err) }
	got, paged, err := extraAdultSearchPath(sourcePPP, 2, "taiwan")
	if err != nil || !paged || got != "/search/?q=taiwan&page=2" { t.Fatalf("PPP search path = %q, paged=%v, err=%v", got, paged, err) }
}

func TestExtraAdult91PornaUsesCurrentPublicRoutes(t *testing.T) {
	if !extraAdultValidCategory(source91Porna, "play") {
		t.Fatal("91Porn current playing category is not registered")
	}
	got, err := extraAdultCategoryPath(source91Porna, 1, "")
	if err != nil || got != "/comic/index/video?category=play&page=1" {
		t.Fatalf("91Porn default category path = %q, err=%v", got, err)
	}
	if got := extraAdultDetailURL(source91Porna, "key_123"); got != "https://91porna.com/comic/index/detail?video_key=key_123" {
		t.Fatalf("91Porn detail URL = %q", got)
	}
	html := `<div class="video-item"><a href="/comic/index/detail?video_key=key_123"><img data-src="/poster.jpg" alt="91 current detail route"></a><span>12:34</span></div>`
	segments := extraAdultSegments(html, source91Porna)
	if len(segments) != 1 {
		t.Fatalf("91Porn current card segments = %d, want 1", len(segments))
	}
	item, ok := extraAdultParseCard(source91Porna, segments[0], ninetyOneBaseURL)
	if !ok || item.SourceID != "key_123" || item.Cover != "https://91porna.com/poster.jpg" {
		t.Fatalf("91Porn current card parse = %#v, ok=%v", item, ok)
	}
}
