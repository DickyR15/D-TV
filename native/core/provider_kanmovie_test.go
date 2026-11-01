package core

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestKanmMovieBackendSourceIdentity(t *testing.T) {
	tests := []struct {
		value string
		host  string
		id    string
		ok    bool
	}{
		{value: "zuidazy.co~132720", host: "zuidazy.co", id: "132720", ok: true},
		{value: "www.subozy.com~160017", host: "subozy.com", id: "160017", ok: true},
		{value: "ikunzy.com~70727", host: "ikunzy.com", id: "70727", ok: true},
		{value: "unknown.example~123", ok: false},
		{value: "zuidazy.co~0", ok: false},
		{value: "zuidazy.co~abc", ok: false},
		{value: "zuidazy.co", ok: false},
	}
	for _, testCase := range tests {
		backend, id, ok := kanmovieSourceIdentity(testCase.value)
		if ok != testCase.ok {
			t.Fatalf("kanmovieSourceIdentity(%q) ok=%v, want %v", testCase.value, ok, testCase.ok)
		}
		if ok && (backend.host != testCase.host || id != testCase.id) {
			t.Fatalf("kanmovieSourceIdentity(%q) = %s/%s, want %s/%s", testCase.value, backend.host, id, testCase.host, testCase.id)
		}
	}
}

func TestKanmMovieCategoriesAndRoutes(t *testing.T) {
	for _, entry := range kanmovieCategories {
		if !validKanmMovieCategory(entry.ID) {
			t.Fatalf("category %q was rejected", entry.ID)
		}
		if _, ok := kanmovieCategoryPath(entry.ID); !ok {
			t.Fatalf("category %q has no page path", entry.ID)
		}
	}
	for _, category := range []string{"../movies", "not-a-category", "movies?page=2"} {
		if validKanmMovieCategory(category) {
			t.Fatalf("unexpectedly accepted category %q", category)
		}
	}
	if path, ok := kanmovieCategoryPath(""); !ok || path != "/today" {
		t.Fatalf("empty category path = %q (%v), want /today", path, ok)
	}
}

func TestParseKanmMovieCardsAndPagination(t *testing.T) {
	document, err := html.Parse(strings.NewReader(`<!doctype html>
<html><body>
<section class="movie-list">
 <article class="movie-item">
  <a href="/detail/zuidazy.co~132720" title="碟仙怪談在线观看">
   <img alt="碟仙怪談" data-src="https://images.example/poster.jpg">
   <span>恐怖片 · 中国大陆 · 2026 · 更新HD</span>
  </a>
 </article>
 <article class="movie-item">
  <a href="/detail/jszyapi.com~156039" title="奪金戰">
   <img alt="奪金戰" src="https://images.example/series.jpg">
   <span>電視劇 · 2026 · 更新至19集</span>
  </a>
 </article>
</section>
<nav><a href="/movies?page=2" rel="next">下一頁</a></nav>
</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	items, more, err := parseKanmMovieCards(document, "https://www.kanmovie.com/movies?page=1", "電影", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || !more {
		t.Fatalf("parsed %d cards, hasMore=%v; want 2, true", len(items), more)
	}
	if items[0].ID != "kanmovie:zuidazy.co~132720" || items[0].Title != "碟仙怪談" {
		t.Fatalf("first card = %#v", items[0])
	}
	if items[0].Cover != "https://images.example/poster.jpg" {
		t.Fatalf("first card cover = %v", items[0].Cover)
	}
	if items[1].ID != "kanmovie:jszyapi.com~156039" || items[1].Title != "奪金戰" {
		t.Fatalf("second card = %#v", items[1])
	}
}

func TestParseKanmMovieAPIAndMultiplePlayLines(t *testing.T) {
	payload := `{
		"code":1,
		"page":1,
		"pagecount":"1",
		"total":1,
		"list":[{
			"vod_id":132720,
			"vod_name":"碟仙怪談",
			"vod_pic":"https://images.example/poster.jpg",
			"vod_blurb":"劇情簡介",
			"vod_play_from":"zuidam3u8$$$备用线路",
			"vod_play_url":"正片$https://cdn.example/main.m3u8$$$正片$https://cdn.example/backup.m3u8"
		}]
	}`
	parsed, err := parseKanmMovieAPI(payload)
	if err != nil {
		t.Fatal(err)
	}
	if kanmovieRawInteger(parsed.PageCount) != 1 || len(parsed.List) != 1 {
		t.Fatalf("unexpected API page: %#v", parsed)
	}
	drama := kanmovieDramaFromRecord(kanmovieBackends[0], parsed.List[0], "電影")
	if drama.ID != "kanmovie:zuidazy.co~132720" || drama.Title != "碟仙怪談" || drama.Desc != "劇情簡介" {
		t.Fatalf("unexpected mapped drama: %#v", drama)
	}
	groups := parseKanmMoviePlayGroups(parsed.List[0])
	if len(groups) != 2 || groups[0].Name != "zuidam3u8" || groups[1].Name != "备用线路" {
		t.Fatalf("unexpected play groups: %#v", groups)
	}
	if len(groups[0].Episodes) != 1 || groups[0].Episodes[0].URL != "https://cdn.example/main.m3u8" {
		t.Fatalf("unexpected primary play line: %#v", groups[0])
	}
}

func TestParseKanmMovieAPIRejectsInvalidPayload(t *testing.T) {
	if _, err := parseKanmMovieAPI("<html>blocked</html>"); err == nil {
		t.Fatal("expected malformed API payload error")
	}
	if _, err := parseKanmMovieAPI(`{"code":0,"msg":"disabled","list":[]}`); err == nil {
		t.Fatal("expected API status error")
	}
	if _, err := parseKanmMovieAPI(`{"code":1,"list":null}`); err == nil {
		t.Fatal("expected missing list error")
	}
}

func TestKanmMovieDetailAndPlaybackPreserveMultipleLines(t *testing.T) {
	const apiResponse = `{
		"code":1,
		"page":1,
		"pagecount":1,
		"total":1,
		"list":[{
			"vod_id":132720,
			"vod_name":"碟仙怪談",
			"vod_pic":"https://images.example/poster.jpg",
			"vod_blurb":"劇情簡介",
			"vod_total":1,
			"vod_play_from":"zuidam3u8$$$backup",
			"vod_play_url":"正片$https://cdn.example/main.m3u8$$$正片$https://cdn.example/backup.m3u8"
		}]
	}`
	downloader := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "api.zuidapi.com" {
			return nil, errors.New("unexpected upstream host: " + request.URL.Host)
		}
		if request.URL.Query().Get("ids") != "132720" {
			return nil, errors.New("unexpected upstream id")
		}
		return sourceFixtureResponse(request, http.StatusOK, apiResponse), nil
	})
	drama, chapters, err := downloader.fetchKanmMovieDetail(context.Background(), "zuidazy.co~132720")
	if err != nil {
		t.Fatal(err)
	}
	if drama.ID != "kanmovie:zuidazy.co~132720" || drama.Title != "碟仙怪談" {
		t.Fatalf("unexpected detail: %#v", drama)
	}
	if len(chapters) != 1 || chapters[0].ID != "kanmovie:zuidazy.co~132720:1" {
		t.Fatalf("unexpected chapters: %#v", chapters)
	}
	task := Task{
		DramaID: drama.ID,
		Chapter: chapters[0],
		Index:   0,
		Total:   len(chapters),
	}
	media, err := downloader.resolveKanmMovieMedia(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if media.URL != "https://cdn.example/main.m3u8" {
		t.Fatalf("primary media URL = %q", media.URL)
	}
	if len(media.Variants) != 1 || media.Variants[0].URL != "https://cdn.example/backup.m3u8" {
		t.Fatalf("expected backup line variant, got %#v", media.Variants)
	}
}

func TestKanmMovieSourceAliasesAndURLCandidatesAreFinite(t *testing.T) {
	if got := canonicalProviderSource("www.kanmovie.com"); got != sourceKanmMovie {
		t.Fatalf("canonical KANMovie source = %q", got)
	}
	if providerSourceForURL("https://www.kanmovie.com/movies") != sourceKanmMovie {
		t.Fatal("KANMovie URL was not recognized")
	}
	downloader := &Downloader{cfg: Config{}, providerHosts: map[string]string{}}
	candidates := downloader.providerURLCandidates("https://www.kanmovie.com/movies")
	if len(candidates) != 1 || candidates[0] != "https://www.kanmovie.com/movies" {
		t.Fatalf("unexpected URL candidates: %#v", candidates)
	}
}
