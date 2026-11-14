package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func adultProviderTestDocument(t *testing.T, source string) *html.Node {
	t.Helper()
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse provider fixture: %v", err)
	}
	return document
}

func TestMissavCardsParseIdentityAndArtwork(t *testing.T) {
	document := adultProviderTestDocument(t, `<!doctype html><html><body>
<a href="/dm635/zh/dldss-581" title="DLDSS-581 測試作品">
  <img data-src="//images.example.test/dldss-581.webp" alt="測試作品">
</a>
<a href="https://other.example.test/dldss-999" title="外站連結">
  <img src="https://other.example.test/poster.jpg">
</a>
<a href="/dm635/zh/actress" title="女優分類"></a>
</body></html>`)
	items := parseMissavCards(document, "https://missav.ws/dm635/zh/release")
	if len(items) != 1 {
		t.Fatalf("expected one MissAV card, got %d: %+v", len(items), items)
	}
	if items[0].ID != "missav:dldss-581" || items[0].SourceID != "dldss-581" {
		t.Fatalf("MissAV identity mismatch: %+v", items[0])
	}
	if items[0].Title != "DLDSS-581 測試作品" {
		t.Fatalf("MissAV title mismatch: %q", items[0].Title)
	}
	if items[0].Cover != "https://images.example.test/dldss-581.webp" {
		t.Fatalf("MissAV poster mismatch: %q", items[0].Cover)
	}
}

func TestSupjavCardsParseNumericIDAndLazyPoster(t *testing.T) {
	document := adultProviderTestDocument(t, `<!doctype html><html><body>
<div class="posts clearfix"><div class="post">
  <a class="img" href="/187772.html" title="Supjav 測試影片">
    <img data-original="/uploads/187772.jpg" alt="測試影片">
  </a>
  <h3><a rel="bookmark" href="/187772.html">測試影片</a></h3>
</div></div>
<a href="https://other.example.test/12345.html" title="外站影片"></a>
</body></html>`)
	items := parseSupjavCards(document, "https://supjav.com/zh/")
	if len(items) != 1 {
		t.Fatalf("expected one Supjav card, got %d: %+v", len(items), items)
	}
	if items[0].ID != "supjav:187772" || items[0].SourceID != "187772" {
		t.Fatalf("Supjav identity mismatch: %+v", items[0])
	}
	if items[0].Title != "Supjav 測試影片" {
		t.Fatalf("Supjav title mismatch: %q", items[0].Title)
	}
	if items[0].Cover != "https://supjav.com/uploads/187772.jpg" {
		t.Fatalf("Supjav poster mismatch: %q", items[0].Cover)
	}
}

func TestAdultProviderDetailIDsAndPlaybackParsing(t *testing.T) {
	if got := missavIDFromURL("/dm635/zh/DLDSS-569", "https://missav.ws/zh/release"); got != "dldss-569" {
		t.Fatalf("MissAV URL ID = %q", got)
	}
	if missavIDFromURL("https://other.example/dldss-569", "https://missav.ws/zh/") != "" {
		t.Fatal("MissAV accepted a foreign-host detail URL")
	}
	if got := supjavIDFromURL("/187772.html", "https://supjav.com/zh/"); got != "187772" {
		t.Fatalf("Supjav URL ID = %q", got)
	}
	if supjavIDFromURL("https://other.example/187772.html", "https://supjav.com/zh/") != "" {
		t.Fatal("Supjav accepted a foreign-host detail URL")
	}

	player := adultProviderTestDocument(t, `<!doctype html><html><body>
<a data-link="TV-ENCODED-LINK">TV</a>
<a data-link="PC-ENCODED-LINK">PC</a>
</body></html>`)
	if got := supjavTVLink(player); got != "TV-ENCODED-LINK" {
		t.Fatalf("Supjav TV route = %q", got)
	}

	mediaPage := adultProviderTestDocument(t, `<!doctype html><html><body>
<video><source src="https://media.example.test/stream/index.m3u8?token=abc"></video>
<script>const backup = "https://media.example.test/backup.mp4";</script>
</body></html>`)
	media := adultMediaCandidates(mediaPage, "", "https://missav.ws/zh/dldss-569")
	if len(media) != 2 ||
		media[0] != "https://media.example.test/stream/index.m3u8?token=abc" ||
		media[1] != "https://media.example.test/backup.mp4" {
		t.Fatalf("adult direct media extraction or fallback order = %#v", media)
	}
}

func TestAdultProviderCanonicalURLs(t *testing.T) {
	d := &Downloader{cfg: defaultConfig()}
	if got, want := d.missavDetailAddress("DLDSS-581"), "https://missav.ws/dldss-581/"; got != want {
		t.Fatalf("MissAV detail URL = %q, want %q", got, want)
	}
	if got, want := d.missavCatalogAddress(1, "DLDSS-581"), "https://missav.ws/search/DLDSS-581"; got != want {
		t.Fatalf("MissAV search URL = %q, want %q", got, want)
	}
	if got, want := d.missavCatalogAddress(2, ""), "https://missav.ws/zh/release?page=2"; got != want {
		t.Fatalf("MissAV page 2 URL = %q, want %q", got, want)
	}
	if got, want := d.supjavCatalogAddress(1, "SSIS-001"), "https://supjav.com/zh/?s=SSIS-001"; got != want {
		t.Fatalf("Supjav search URL = %q, want %q", got, want)
	}
	if got, want := d.supjavCatalogAddress(2, ""), "https://supjav.com/zh/page/2/"; got != want {
		t.Fatalf("Supjav page 2 URL = %q, want %q", got, want)
	}
}

func TestSupjavRequestsTaiwanTraditionalChinese(t *testing.T) {
	var gotLanguage string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		gotLanguage = request.Header.Get("Accept-Language")
		return sourceFixtureResponse(request, http.StatusOK, "<html><body></body></html>"), nil
	})
	if _, _, err := d.fetchSupjavCatalogPage(context.Background(), 1, "", ""); err != nil {
		t.Fatalf("Supjav fixture request failed: %v", err)
	}
	if !strings.Contains(gotLanguage, "zh-TW") || !strings.Contains(gotLanguage, "zh-Hant") {
		t.Fatalf("Supjav Accept-Language = %q, want zh-TW and zh-Hant", gotLanguage)
	}
}


func TestMissavPlaybackResolverExtractsDirectMP4(t *testing.T) {
	const detailURL = "https://missav.ws/dldss-581/"
	var pageSeen bool
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "missav.ws" || request.URL.Path != "/dldss-581/" {
			return nil, fmt.Errorf("unexpected MissAV request: %s", request.URL)
		}
		if request.Header.Get("Referer") != "https://missav.ws/zh/" {
			t.Errorf("MissAV detail Referer = %q", request.Header.Get("Referer"))
		}
		pageSeen = true
		return sourceFixtureResponse(request, http.StatusOK, `<html><body>
<video><source src="https://media.example.test/dldss-581.mp4"></video>
</body></html>`), nil
	})

	media, err := d.resolveProviderMedia(context.Background(), Task{
		DramaID: "missav:dldss-581",
		Chapter: Chapter{ID: "missav:dldss-581:1", Source: sourceMissav, PageURL: detailURL},
	})
	if err != nil {
		t.Fatalf("resolve MissAV playback: %v", err)
	}
	if !pageSeen {
		t.Fatal("MissAV detail page was not requested")
	}
	if media.URL != "https://media.example.test/dldss-581.mp4" {
		t.Fatalf("MissAV media URL = %q", media.URL)
	}
	if media.Referer != detailURL {
		t.Fatalf("MissAV media Referer = %q, want %q", media.Referer, detailURL)
	}
}

func TestSupjavPlaybackResolverUsesTVEndpointAndLoadsPlaylist(t *testing.T) {
	const (
		detailURL   = "https://supjav.com/187772.html"
		encodedLink = "TV-ENCODED-LINK"
		reversed    = "KNIL-DEDOCNE-VT"
		playlistURL = "https://media.example.test/master.m3u8?token=fixture"
	)
	var pageSeen, endpointSeen, playlistSeen bool
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "supjav.com":
			if request.URL.Path != "/187772.html" {
				return nil, fmt.Errorf("unexpected Supjav detail path: %s", request.URL.Path)
			}
			language := request.Header.Get("Accept-Language")
			if !strings.Contains(language, "zh-TW") || !strings.Contains(language, "zh-Hant") {
				t.Errorf("Supjav Accept-Language = %q, want zh-TW and zh-Hant", language)
			}
			if request.Header.Get("Referer") != "https://supjav.com/zh/" {
				t.Errorf("Supjav detail Referer = %q", request.Header.Get("Referer"))
			}
			pageSeen = true
			return sourceFixtureResponse(request, http.StatusOK,
				"<html><body><a data-link=\"" + encodedLink + "\">TV</a></body></html>"), nil
		case "lk1.supremejav.com":
			if request.URL.Path != "/supjav.php" {
				return nil, fmt.Errorf("unexpected Supjav playback path: %s", request.URL.Path)
			}
			if got := request.URL.Query().Get("c"); got != reversed {
				t.Errorf("Supjav playback token = %q, want reversed token %q", got, reversed)
			}
			if request.Header.Get("Referer") != "https://supjav.com/zh/" {
				t.Errorf("Supjav playback Referer = %q", request.Header.Get("Referer"))
			}
			endpointSeen = true
			return sourceFixtureResponse(request, http.StatusOK,
				"<script>var urlPlay = \"" + playlistURL + "\";</script>"), nil
		case "media.example.test":
			if request.URL.Path != "/master.m3u8" || request.URL.Query().Get("token") != "fixture" {
				return nil, fmt.Errorf("unexpected Supjav playlist URL: %s", request.URL)
			}
			if request.Header.Get("Referer") != "https://supjav.com/zh/" {
				t.Errorf("Supjav playlist Referer = %q", request.Header.Get("Referer"))
			}
			playlistSeen = true
			return sourceFixtureResponse(request, http.StatusOK,
				"#EXTM3U\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST\n"), nil
		default:
			return nil, fmt.Errorf("unexpected Supjav host: %s", request.URL.Host)
		}
	})

	media, err := d.resolveProviderMedia(context.Background(), Task{
		DramaID: "supjav:187772",
		Chapter: Chapter{ID: "supjav:187772:1", Source: sourceSupjav, PageURL: detailURL},
	})
	if err != nil {
		t.Fatalf("resolve Supjav playback: %v", err)
	}
	if !pageSeen || !endpointSeen || !playlistSeen {
		t.Fatalf("Supjav playback stages: detail=%t endpoint=%t playlist=%t", pageSeen, endpointSeen, playlistSeen)
	}
	if media.URL != playlistURL {
		t.Fatalf("Supjav media URL = %q, want %q", media.URL, playlistURL)
	}
	if !strings.HasPrefix(media.Playlist, "#EXTM3U") {
		t.Fatalf("Supjav playlist was not retained: %q", media.Playlist)
	}
}


func TestMissavAndSupjavAreRecognizedAdvancedProviders(t *testing.T) {
	for _, testCase := range []struct {
		source string
		host   string
	}{
		{source: sourceMissav, host: "https://missav.ws/zh/release"},
		{source: sourceSupjav, host: "https://supjav.com/zh/"},
	} {
		if !isHuangguoProviderSource(testCase.source) {
			t.Fatalf("advanced source %q is not enabled in full edition", testCase.source)
		}
		if got := providerSourceForURL(testCase.host); got != testCase.source {
			t.Fatalf("provider host %q maps to %q, want %q", testCase.host, got, testCase.source)
		}
		if !validNativeCategory(testCase.source, "") || validNativeCategory(testCase.source, "unknown") {
			t.Fatalf("unexpected category validation for %q", testCase.source)
		}
	}
	if got := (&Downloader{cfg: defaultConfig()}).providerBaseURL(sourceMissav); got != missavDefaultBaseURL {
		t.Fatalf("MissAV base URL = %q", got)
	}
	if got := (&Downloader{cfg: defaultConfig()}).providerBaseURL(sourceSupjav); got != supjavDefaultBaseURL {
		t.Fatalf("Supjav base URL = %q", got)
	}
}
