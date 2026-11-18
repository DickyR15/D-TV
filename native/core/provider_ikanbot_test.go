package core

import (
	"net/url"
	"strings"
	"testing"

	htmlnet "golang.org/x/net/html"
)

func TestIkanbotParseResDataSupportsArrayObjectAndEscapedJSON(t *testing.T) {
	tests := []struct {
		name string
		raw string
		wantFlag string
		wantURL string
	}{
		{
			name: "array",
			raw: `[{"flag":"1080P","url":"第1集$https://cdn.example.test/1.m3u8"}]`,
			wantFlag: "1080P",
			wantURL: "https://cdn.example.test/1.m3u8",
		},
		{
			name: "object",
			raw: `{"flag":"720P","url":"第2集$https://cdn.example.test/2.m3u8"}`,
			wantFlag: "720P",
			wantURL: "https://cdn.example.test/2.m3u8",
		},
		{
			name: "escaped array",
			raw: `[{\"flag\":\"480P\",\"url\":\"第3集$https://cdn.example.test/3.m3u8\"}]`,
			wantFlag: "480P",
			wantURL: "https://cdn.example.test/3.m3u8",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := ikanbotParseResData(test.raw)
			if len(entries) != 1 {
				t.Fatalf("parsed entries = %d, want 1: %#v", len(entries), entries)
			}
			if entries[0].Flag != test.wantFlag || !strings.Contains(entries[0].URL, test.wantURL) {
				t.Fatalf("parsed entry = %#v, want flag=%q URL containing %q", entries[0], test.wantFlag, test.wantURL)
			}
		})
	}
}

func TestIkanbotParseLinesAcceptsObjectResDataAndPreservesAllEpisodes(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"list": []any{
				map[string]any{
					"resData": `[{"flag":"1080P","url":"第1集$https://cdn.example.test/1.m3u8#第2集$https://cdn.example.test/2.m3u8"}]`,
				},
				map[string]any{
					"resData": map[string]any{
						"flag": "720P",
						"url": "第3集$https://cdn.example.test/3.m3u8",
					},
				},
			},
		},
	}
	lines := ikanbotParseLines(payload)
	if len(lines) != 2 {
		t.Fatalf("parsed lines = %d, want 2: %#v", len(lines), lines)
	}
	total := 0
	for _, line := range lines {
		total += len(line.Episodes)
	}
	if total != 3 {
		t.Fatalf("parsed episode total = %d, want 3", total)
	}
}

func TestIkanbotEpisodeOrderPrefersEpisodeNumberOverYear(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{name: "2024 年 第 03 集", want: 3},
		{name: "第 12 話", want: 12},
		{name: "Episode 7", want: 7},
		{name: "EP08", want: 8},
	}
	for _, test := range tests {
		if got := ikanbotEpisodeOrder(test.name, 99); got != test.want {
			t.Fatalf("episode order for %q = %d, want %d", test.name, got, test.want)
		}
	}
}

func TestIkanbotBuildChaptersMergesEpisodesAcrossLines(t *testing.T) {
	lines := []ikanbotLine{
		{
			Flag: "1080P",
			Episodes: []ikanbotEpisode{
				{Name: "第1集", URL: "https://media.example.test/1080/1.m3u8", Order: 1},
				{Name: "第2集", URL: "https://media.example.test/1080/2.m3u8", Order: 2},
			},
		},
		{
			Flag: "720P",
			Episodes: []ikanbotEpisode{
				{Name: "第1集", URL: "https://media.example.test/720/1.m3u8", Order: 1},
				{Name: "第2集", URL: "https://media.example.test/720/2.m3u8", Order: 2},
				{Name: "第3集", URL: "https://media.example.test/720/3.m3u8", Order: 3},
			},
		},
	}
	chapters := ikanbotBuildChapters("1023900", "https://v.ikanbot.com/play/1023900", lines)
	if len(chapters) != 3 {
		t.Fatalf("merged IkanBot chapters = %d, want 3", len(chapters))
	}
	if chapters[0].EpisodeString(0) != "1" ||
		chapters[1].EpisodeString(0) != "2" ||
		chapters[2].EpisodeString(0) != "3" {
		t.Fatalf("episode order was not preserved: %#v", chapters)
	}
	if chapters[2].VideoURL != "https://media.example.test/720/3.m3u8" ||
		chapters[2].PlayLine != "720P" {
		t.Fatalf("episode found only on secondary line was not playable: %#v", chapters[2])
	}
	if len(chapters[0].PlaybackVariants) != 1 ||
		!strings.Contains(chapters[0].PlaybackVariants[0], "https://media.example.test/720/1.m3u8") {
		t.Fatalf("alternate line for episode 1 was lost: %#v", chapters[0].PlaybackVariants)
	}
}

func TestIkanbotUsesCurrentOfficialHostAndCategoryRoutes(t *testing.T) {
	if ikanbotBaseURL != "https://admin-api.aikanbot.com" {
		t.Fatalf("IkanBot base URL = %q", ikanbotBaseURL)
	}
	got, err := ikanbotCategoryPath(1, "movie-热门")
	if err != nil || got != "/hot/index-movie-热门.html" {
		t.Fatalf("IkanBot page-one route = %q, err=%v", got, err)
	}
	got, err = ikanbotCategoryPath(2, "movie-热门")
	if err != nil || got != "/hot/index-movie-热门-p-2.html" {
		t.Fatalf("IkanBot page-two route = %q, err=%v", got, err)
	}
}

func TestIkanbotParsesCurrentCardLinksWithoutLegacyItemClass(t *testing.T) {
	body := `<div class="module-list">
		<div class="module-item">
			<a href="/play/1023899" title="黑球">
				<img data-original="//img.example.test/black-ball.webp" alt="黑球海報">
				<span>黑球</span>
			</a>
		</div>
		<div class="module-item">
			<a href="https://v.ikanbot.com/play/1023900">
				<img data-src="/covers/second.webp" alt="第二部作品">
				<p>第二部作品</p>
			</a>
		</div>
		<div class="module-item">
			<a href="https://evil.example/play/999">
				<img src="/not-trusted.webp" alt="外站作品">
			</a>
		</div>
	</div>`
	doc, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	items := ikanbotParseCards(doc, "https://v.ikanbot.com/hot/index-movie-热门.html", "movie-热门")
	if len(items) != 2 {
		t.Fatalf("IkanBot parsed %d cards, want 2: %#v", len(items), items)
	}
	if items[0].SourceID != "1023899" || items[0].Title != "黑球" ||
		items[0].Cover != "https://img.example.test/black-ball.webp" {
		t.Fatalf("first IkanBot card = %#v", items[0])
	}
	if items[1].SourceID != "1023900" || items[1].Title != "第二部作品" ||
		items[1].Cover != "https://v.ikanbot.com/covers/second.webp" {
		t.Fatalf("second IkanBot card = %#v", items[1])
	}
}

func TestIkanbotImageSupportsLazyImageAttributes(t *testing.T) {
	body := `<img data-lazy-src="/covers/lazy.webp" src="data:image/gif;base64,placeholder">`
	doc, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var image *htmlnet.Node
	for _, node := range providerHTMLNodes(doc, func(n *htmlnet.Node) bool {
		return n.Type == htmlnet.ElementNode && n.Data == "img"
	}) {
		image = node
		break
	}
	if got := ikanbotImage(image, ikanbotBaseURL+"/play/1023899"); got != ikanbotBaseURL+"/covers/lazy.webp" {
		t.Fatalf("IkanBot lazy cover = %q", got)
	}
}

func TestIkanbotPlaybackAPIUsesResolvedPageHost(t *testing.T) {
	for _, host := range []string{"admin-api.aikanbot.com", "v.ikanbot.com", "v.aikanbot.com", "www.ikanbot.com"} {
		got, err := ikanbotPlaybackAPIURL(
			"https://"+host+"/play/1023900",
			"1023900",
			"2",
			"722ae90e3a9be1a774fa099083c3cd15",
		)
		if err != nil {
			t.Fatalf("IkanBot API URL for %s: %v", host, err)
		}
		parsed, err := url.Parse(got)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		if parsed.Host != host || parsed.Path != "/api/getResN" ||
			query.Get("videoId") != "1023900" ||
			query.Get("mtype") != "2" ||
			query.Get("token") != "722ae90e3a9be1a774fa099083c3cd15" {
			t.Fatalf("unexpected IkanBot API URL: %s", got)
		}
	}
}

func TestIkanbotPlaybackAPIRejectsUntrustedPageHosts(t *testing.T) {
	for _, page := range []string{
		"http://v.ikanbot.com/play/853970",
		"https://evil.example/play/853970",
		"https://img1.ikanbot.com/play/853970",
	} {
		if _, err := ikanbotPlaybackAPIURL(page, "853970", "2", "signed-token"); err == nil {
			t.Fatalf("IkanBot API builder accepted untrusted page %q", page)
		}
	}
}

func TestIkanbotPlaybackUsesSourceVideoIDButSignsCurrentID(t *testing.T) {
	currentID := "853970"
	sourceID := "1023900"
	token := "mre0530ce88964487488y67d38c0a1uj7fd15cb8"
	signed := ikanbotSignToken(currentID, token)
	if signed == "" {
		t.Fatal("IkanBot current_id token signature was empty")
	}
	address, err := ikanbotPlaybackAPIURL(
		"https://v.ikanbot.com/play/"+sourceID,
		sourceID,
		"2",
		signed,
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("videoId") != sourceID {
		t.Fatalf("API videoId = %q, want source ID %q", parsed.Query().Get("videoId"), sourceID)
	}
	if parsed.Query().Get("token") != signed {
		t.Fatal("API did not use the signature derived from current_id")
	}
}


func TestIkanbotHLSURLRecognizesSignedPlaylistURLs(t *testing.T) {
	for _, address := range []string{
		"https://cdn.example.test/playlist.m3u8",
		"https://cdn.example.test/playlist.m3u8?token=abc%2F123&expires=456",
	} {
		if !ikanbotIsHLSURL(address) {
			t.Fatalf("IkanBot did not recognize HLS URL: %s", address)
		}
	}
	for _, address := range []string{
		"https://cdn.example.test/video.mp4?token=abc",
		"https://cdn.example.test/path/playlist.m3u8.txt?token=abc",
		"not a URL",
	} {
		if ikanbotIsHLSURL(address) {
			t.Fatalf("IkanBot incorrectly classified non-HLS URL: %s", address)
		}
	}
}


func TestIkanbotAdminAPIHostIsRecognized(t *testing.T) {
	const address = "https://admin-api.aikanbot.com/play/1023899"
	if got := providerSourceForURL(address); got != sourceIkanBot {
		t.Fatalf("current iKanBot host mapped to %q", got)
	}
	if got := canonicalProviderSource("admin-api.aikanbot.com"); got != sourceIkanBot {
		t.Fatalf("canonical current iKanBot host = %q", got)
	}
}

func TestGetHuangguoChaptersDispatchesIkanbot(t *testing.T) {
	token := strings.Repeat("a", 80)
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/play/1023899":
			body := fmt.Sprintf("<html><h1 id=\"video_title\">愛看測試作品</h1><input id=\"current_id\" value=\"1023899\"><input id=\"e_token\" value=\"%s\"><input id=\"mtype\" value=\"2\"></html>", token)
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		case "/api/getResN":
			body := "{\"state\":1,\"data\":{\"list\":[{\"resData\":\"[{\\\"flag\\\":\\\"1080P\\\",\\\"url\\\":\\\"第1集$https://cdn.example.test/1.m3u8?token=abc\\\"}]\"}]}}"
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		default:
			return nil, fmt.Errorf("unexpected iKanBot request: %s", request.URL)
		}
	})
	title, chapters, err := d.GetHuangguoChapters(context.Background(), sourceIkanBot, "1023899")
	if err != nil {
		t.Fatalf("GetHuangguoChapters(iKanBot) failed: %v", err)
	}
	if title != "愛看測試作品" || len(chapters) != 1 ||
		chapters[0].Source != sourceIkanBot ||
		chapters[0].VideoURL != "https://cdn.example.test/1.m3u8?token=abc" {
		t.Fatalf("iKanBot health-check detail = %q, %#v", title, chapters)
	}
}

func TestIkanbotResolveMediaLoadsSignedHLSPlaylist(t *testing.T) {
	const (
		playlistURL = "https://cdn.example.test/index.m3u8?token=abc%2F123"
		refererURL  = "https://admin-api.aikanbot.com/play/1023899"
	)
	playlist := "#EXTM3U\n#EXTINF:6.0,\nsegment-1.ts?token=abc\n#EXT-X-ENDLIST\n"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != playlistURL {
			return nil, fmt.Errorf("unexpected media request: %s", request.URL)
		}
		if request.Header.Get("Referer") != refererURL ||
			request.Header.Get("User-Agent") != ikanbotUserAgent {
			return nil, fmt.Errorf("iKanBot media headers were not preserved: %#v", request.Header)
		}
		return sourceFixtureResponse(request, http.StatusOK, playlist), nil
	})
	chapter := Chapter{
		ID: providerChapterID(sourceIkanBot, "1023899", "1"),
		Source: sourceIkanBot,
		PageURL: refererURL,
		Referer: refererURL,
		VideoURL: playlistURL,
	}
	media, err := d.resolveIkanbotMedia(context.Background(), Task{
		DramaID: providerDramaID(sourceIkanBot, "1023899"),
		Chapter: chapter,
		Index: 1,
	})
	if err != nil {
		t.Fatalf("resolveIkanbotMedia failed: %v", err)
	}
	if media.URL != playlistURL || media.Playlist != playlist {
		t.Fatalf("signed iKanBot HLS playlist was not prepared: %#v", media)
	}
}
