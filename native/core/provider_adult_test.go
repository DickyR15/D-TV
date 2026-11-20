package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func adultCategoryURLMust(source, category string) string {
	address, err := adultCategoryURL(source, category)
	if err != nil {
		return ""
	}
	return address
}

func TestAdultProviderRegistrationAndCategories(t *testing.T) {
	for _, input := range []string{"missav.ws", "www.missav.ai"} {
		if got := canonicalProviderSource(input); got != sourceMissAV {
			t.Fatalf("canonicalProviderSource(%q) = %q", input, got)
		}
		if !isHuangguoProviderSource(sourceMissAV) {
			t.Fatal("MissAV is not registered")
		}
		if got := providerSourceForURL("https://" + input + "/"); got != sourceMissAV {
			t.Fatalf("providerSourceForURL(%q) = %q", input, got)
		}
	}
	if got := providerSourceForURL("https://missav.live/"); got != sourceMissAV {
		t.Fatalf("current MissAV domain = %q", got)
	}
	if got := canonicalProviderSource("supjav"); got != sourceSupJav {
		t.Fatalf("canonicalProviderSource(%q) = %q, want active SupJav", "supjav", got)
	}
	if got := providerSourceForURL("https://supjav.blog/"); got != sourceSupJav {
		t.Fatalf("providerSourceForURL(%q) = %q, want SupJav", "https://supjav.blog/", got)
	}
	if got := providerSourceForURL("https://supjav.com/"); got != "" {
		t.Fatalf("legacy SupJav entry domain should remain disabled, got %q", got)
	}
	if !validNativeCategory(sourceMissAV, "") || !validAdultProviderCategory(sourceMissAV, "") ||
		validAdultProviderCategory(sourceMissAV, "../bad") {
		t.Fatal("MissAV category validation changed")
	}
	if len(adultProviderCategories(sourceMissAV)) == 0 {
		t.Fatal("MissAV categories are missing")
	}
}

func TestAdultMissAVDynamicGenreParser(t *testing.T) {
	body := `<nav>
		<a href="/cn/genres/chinese-subtitle">中文字幕類</a>
		<a href="/dm298/cn/genres/uncensored-leak">無碼流出</a>
		<a href="/cn/genres/chinese-subtitle">中文字幕</a>
		<a href="/cn/videos/not-a-genre">不應收錄</a>
		<a href="https://evil.example/cn/genres/fake">外站分類</a>
	</nav>`
	got := adultParseMissAVCategories(body, missAVBaseURL)
	if len(got) != 2 {
		t.Fatalf("MissAV genres = %#v, want two trusted categories", got)
	}
	byID := map[string]string{}
	for _, category := range got {
		byID[category.ID] = category.Name
	}
	if byID["chinese-subtitle"] != "中文字幕" || byID["uncensored-leak"] != "無碼流出" {
		t.Fatalf("MissAV dynamic genres = %#v", byID)
	}
}

func TestAdultProviderDramaIDsAndDetailURLs(t *testing.T) {
	missav := providerDramaID(sourceMissAV, "sone-543")
	source, sourceID, ok := splitProviderDramaID(missav)
	if !ok || source != sourceMissAV || sourceID != "sone-543" {
		t.Fatalf("split MISS AV ID = %q/%q/%v", source, sourceID, ok)
	}
	if got := adultDetailURL(sourceMissAV, "sone-543"); got != "https://missav.live/cn/sone-543" {
		t.Fatalf("MISS AV detail URL = %q", got)
	}
	if got := adultCategoryURLMust(sourceMissAV, "vr"); got != "https://missav.live/cn/genres/vr" {
		t.Fatalf("MISS AV genre URL = %q", got)
	}
	if adultDetailURL(sourceMissAV, "today-hot") != "" {
		t.Fatal("adult detail URL builder accepted a category")
	}
}

func TestAdultCardParsingAndLazyCoverFallback(t *testing.T) {
	missav := "<div class=\"thumbnail group\"><a href=\"/cn/sone-543\"><img data-src=\"/covers/sample.webp\" alt=\"SAMPLE-543\"></a><div class=\"my-2\">SAMPLE-543</div></div>"
	items := adultParseCards(missav, missAVBaseURL, sourceMissAV)
	if len(items) != 1 {
		t.Fatalf("MISS AV cards = %d, want 1", len(items))
	}
	if items[0].ID != providerDramaID(sourceMissAV, "sone-543") ||
		items[0].Title != "SAMPLE-543" ||
		items[0].Cover != "https://missav.live/covers/sample.webp" {
		t.Fatalf("unexpected MISS AV card: %#v", items[0])
	}
}

func TestAdultMissAVPosterSelectionSkipsFlagsAndSiteLogos(t *testing.T) {
	base := adultProviderBaseURL(sourceMissAV)
	body := `<div class="thumbnail">
		<img class="country-flag" src="/images/flags/china-flag.png" alt="China flag" width="32" height="20">
		<img class="site-logo" src="/images/d-tv-logo.webp" alt="site logo" width="120" height="32">
		<a href="/cn/sone-543" title="SAMPLE-543">
			<img class="poster" data-src="/covers/sample.webp" alt="video poster" width="225" height="338">
		</a>
	</div>`
	items := adultParseCards(body, base, sourceMissAV)
	if len(items) != 1 {
		t.Fatalf("MissAV cards = %d, want 1", len(items))
	}
	want := strings.TrimRight(base, "/") + "/covers/sample.webp"
	if items[0].Cover != want {
		t.Fatalf("MissAV selected a flag/logo instead of the poster: got %q want %q", items[0].Cover, want)
	}
}

func TestAdultImageCandidatesPreferPortraitPosterOverWideArtwork(t *testing.T) {
	base := adultProviderBaseURL(sourceMissAV)
	body := `<div>
		<img class="backdrop" src="/images/wide-art.webp" width="640" height="360">
		<img class="poster" data-src="/images/real-poster.webp" width="225" height="338">
	</div>`
	covers := adultImageCandidates(body, base, sourceMissAV)
	want := strings.TrimRight(base, "/") + "/images/real-poster.webp"
	if len(covers) != 1 || covers[0] != want {
		t.Fatalf("poster ranking = %v, want only the valid portrait poster %q", covers, want)
	}
}

func TestAdultMissAVDetailPathParserRejectsNonVideoRoutes(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/cn/sone-543", want: "sone-543"},
		{path: "/dm298/cn/sone-543", want: "sone-543"},
		{path: "/cn/genres/uncensored-leak"},
		{path: "/dm298/cn/genres/uncensored-leak"},
		{path: "/cn/search/sample"},
		{path: "/cn/sone-543/extra"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			got, _ := adultSourceIDFromHref(test.path, missAVBaseURL, sourceMissAV)
			if got != test.want {
				t.Fatalf("adultSourceIDFromHref(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestAdultMissAVCardParserFallsBackWhenCardClassesChange(t *testing.T) {
	body := `<section class="catalog-grid">
		<article class="video-tile">
			<a href="/dm298/cn/sone-543">
				<img src="/covers/placeholder.jpg" data-original="/covers/real-poster.webp" alt="SAMPLE-543">
			</a>
		</article>
		<a href="/cn/genres/uncensored-leak">分類導覽</a>
	</section>`
	items := adultParseCards(body, missAVBaseURL, sourceMissAV)
	if len(items) != 1 {
		t.Fatalf("MissAV flexible card parser returned %d items, want one: %#v", len(items), items)
	}
	if items[0].SourceID != "sone-543" || items[0].Title != "SAMPLE-543" {
		t.Fatalf("unexpected flexible MissAV card: %#v", items[0])
	}
	if items[0].Cover != "https://missav.live/covers/real-poster.webp" {
		t.Fatalf("lazy poster candidate = %q, want original artwork URL", items[0].Cover)
	}
}

func TestAdultMissAVCardParserAcceptsBackgroundAndLazyImageAttributes(t *testing.T) {
	body := `<article class="item">
		<a href="/cn/abc-123"><span class="poster" data-bg="/covers/background.jpg"></span></a>
		<h3>ABC-123 title</h3>
	</article>
	<article class="item">
		<a href="/cn/def-456"><img src="/covers/blank.gif" data-lazy-src="/covers/lazy.jpg" alt="DEF-456"></a>
	</article>`
	items := adultParseCards(body, missAVBaseURL, sourceMissAV)
	if len(items) != 2 {
		t.Fatalf("MissAV image-attribute parser returned %d items, want two: %#v", len(items), items)
	}
	if items[0].Cover != "https://missav.live/covers/background.jpg" {
		t.Fatalf("background poster = %q", items[0].Cover)
	}
	if items[1].Cover != "https://missav.live/covers/lazy.jpg" {
		t.Fatalf("lazy poster = %q", items[1].Cover)
	}
}

func TestAdultMissAVPaginationFallsBackToFullPageCardCount(t *testing.T) {
	if !adultCatalogHasMorePages("<html></html>", 1, 20) {
		t.Fatal("a full MissAV catalogue page should keep pagination available when links are missing")
	}
	if adultCatalogHasMorePages("<html></html>", 1, 19) {
		t.Fatal("a short final MissAV page without a paginator should close")
	}
}

func TestAdultMissAVPaginationUsesPublishedPageLinks(t *testing.T) {
	tests := []struct {
		name string
		body string
		page int
		want bool
	}{
		{
			name: "next page remains available with fewer than twenty cards",
			body: `<nav><a href="/cn/new?page=1">1</a><a href="/cn/new?page=2">2</a></nav>`,
			page: 1,
			want: true,
		},
		{
			name: "search pagination recognizes a later page",
			body: `<nav><a href="/cn/search/test?page=3">3</a></nav>`,
			page: 2,
			want: true,
		},
		{
			name: "last advertised page closes pagination",
			body: `<nav><a href="/cn/new?page=1">1</a><a href="/cn/new?page=2">2</a></nav>`,
			page: 2,
			want: false,
		},
		{
			name: "genre links without a page number do not extend pagination",
			body: `<nav><a href="/cn/genres/uncensored">分類</a><a href="/cn/new">最新</a></nav>`,
			page: 1,
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := adultHasMorePages(test.body, test.page); got != test.want {
				t.Fatalf("adultHasMorePages(page=%d) = %v, want %v", test.page, got, test.want)
			}
		})
	}
}

func TestAdultMissAVCardParserKeepsMoreThanOneHundredEntries(t *testing.T) {
	var body strings.Builder
	for index := 0; index < 125; index++ {
		slug := fmt.Sprintf("sample-%03d", index)
		fmt.Fprintf(&body,
			`<div class="thumbnail group"><a href="/cn/%s"><img data-src="/covers/%s.webp" alt="%s"></a><div class="my-2">%s</div></div>`,
			slug, slug, slug, slug,
		)
	}
	items := adultParseCards(body.String(), missAVBaseURL, sourceMissAV)
	if len(items) != 125 {
		t.Fatalf("MISS AV parser returned %d cards, want all 125", len(items))
	}
	if items[124].SourceID != "sample-124" {
		t.Fatalf("last parsed card = %q, want sample-124", items[124].SourceID)
	}
}

func TestAdultPackerBase62AlphabetBound(t *testing.T) {
	if got := adultEncodeBase(61, 62); got != "Z" {
		t.Fatalf("base-62 digit 61 = %q, want Z", got)
	}
	packedWithUnsupportedRadix := "eval(function(p,a,c,k,e,d){return p;}('0',64,1,'x'.split('|'))"
	if got := adultUnpackPacker(packedWithUnsupportedRadix); got != "" {
		t.Fatalf("base-64 Packer payload should be rejected, got %q", got)
	}
}

func TestAdultPlaybackURLsResolveRelativeToTheActualPlayerPage(t *testing.T) {
	got := adultFindM3U8(
		"<script>var player={source:'/media/master.m3u8?token=abc'};</script>",
		"https://missav.live/cn/simw-014",
	)
	if got != "https://missav.live/media/master.m3u8?token=abc" {
		t.Fatalf("relative player URL = %q", got)
	}
	if got := adultFindM3U8("<script>source='https://cdn.example/master.m3u8'</script>"); got != "https://cdn.example/master.m3u8" {
		t.Fatalf("absolute player URL without a page base = %q", got)
	}
}

func TestAdultMissAVPrefersCurrentSurritPlaylist(t *testing.T) {
	body := `<script>const playlist = "https://surrit.com/01234567-89ab-cdef-0123-456789abcdef/playlist.m3u8?token=fixture";</script>`
	got := adultFindMissAVMedia(body, "https://missav.live/cn/sample-123")
	want := "https://surrit.com/01234567-89ab-cdef-0123-456789abcdef/playlist.m3u8?token=fixture"
	if got != want {
		t.Fatalf("MissAV Surrit media URL = %q, want %q", got, want)
	}
}

func TestAdultProviderRejectsUntrustedURLsAndParsesPlaybackHints(t *testing.T) {
	if adultSafeURL(missAVBaseURL, "javascript:alert(1)", sourceMissAV) != "" {
		t.Fatal("MISS AV accepted a script URL")
	}
	if got := adultFindM3U8("<script>source=\"https://cdn.example.test/master.m3u8?token=abc\"</script>"); got != "https://cdn.example.test/master.m3u8?token=abc" {
		t.Fatalf("M3U8 extraction = %q", got)
	}
}


func TestGetHuangguoChaptersDispatchesAdultProvider(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() != "madou.club" || request.URL.Path != "/sample-video" {
			return nil, fmt.Errorf("unexpected madou request: %s", request.URL)
		}
		body := "<html><head><meta property=\"og:title\" content=\"madou 診斷測試作品\"></head><body></body></html>"
		return sourceFixtureResponse(request, http.StatusOK, body), nil
	})
	title, chapters, err := d.GetHuangguoChapters(context.Background(), sourceMadou, "sample-video")
	if err != nil {
		t.Fatalf("GetHuangguoChapters(madou) failed: %v", err)
	}
	if title != "madou 診斷測試作品" || len(chapters) != 1 ||
		chapters[0].Source != sourceMadou || chapters[0].Title != "正片" {
		t.Fatalf("madou health-check detail = %q, %#v", title, chapters)
	}
}
