func TestIkanbotAutomaticRouteSelectionPrefersHighestQuality(t *testing.T) {
	const pageURL = "https://admin-api.aikanbot.com/play/1023900"
	chapter := Chapter{
		ID: providerChapterID(sourceIkanBot, "1023900", "1"),
		Source: sourceIkanBot,
		VideoURL: "https://cdn.example.test/720/episode.mp4",
		PlayLine: "720P",
		Referer: pageURL,
		PageURL: pageURL,
		PlaybackVariants: []string{
			"1080:https://cdn.example.test/1080/episode.mp4",
			"480:https://cdn.example.test/480/episode.mp4",
		},
	}
	media, err := (&Downloader{}).resolveIkanbotMedia(context.Background(), Task{
		DramaID: providerDramaID(sourceIkanBot, "1023900"),
		Chapter: chapter,
		Index: 1,
	})
	if err != nil {
		t.Fatalf("resolveIkanbotMedia failed: %v", err)
	}
	choice := nativePlaybackChoices(media, 0)
	want := []string{
		"https://cdn.example.test/1080/episode.mp4",
		"https://cdn.example.test/720/episode.mp4",
	}
	if len(choice.media) != len(want) {
		t.Fatalf("automatic routes = %d, want %d (sub-720P backup intentionally filtered)", len(choice.media), len(want))
	}
	for index, address := range want {
		if choice.media[index].URL != address {
			t.Fatalf("automatic route %d = %q, want %q", index, choice.media[index].URL, address)
		}
	}
}package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	htmlnet "golang.org/x/net/html"
)

func TestIkanbotDisplayEpisodeCountSeparatesMovieChoicesFromTVEpisodes(t *testing.T) {
	if got := ikanbotDisplayEpisodeCount(true, 31); got != 0 {
		t.Fatalf("movie display episode count = %d, want 0", got)
	}
	if got := ikanbotDisplayEpisodeCount(false, 31); got != 31 {
		t.Fatalf("TV display episode count = %d, want 31", got)
	}
	if got := ikanbotDisplayEpisodeCount(false, -1); got != 0 {
		t.Fatalf("negative episode count = %d, want 0", got)
	}
}

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
	if media.URL != playlistURL || strings.TrimSpace(media.Playlist) != strings.TrimSpace(playlist) {
		t.Fatalf("signed iKanBot HLS playlist was not prepared: %#v", media)
	}
}


func TestIkanbotAutomaticRouteSelectionPrefersHighestQuality(t *testing.T) {
	const pageURL = "https://admin-api.aikanbot.com/play/1023900"
	chapter := Chapter{
		ID: providerChapterID(sourceIkanBot, "1023900", "1"),
		Source: sourceIkanBot,
		VideoURL: "https://cdn.example.test/720/episode.mp4",
		PlayLine: "720P",
		Referer: pageURL,
		PageURL: pageURL,
		PlaybackVariants: []string{
			"1080:https://cdn.example.test/1080/episode.mp4",
			"480:https://cdn.example.test/480/episode.mp4",
		},
	}
	media, err := (&Downloader{}).resolveIkanbotMedia(context.Background(), Task{
		DramaID: providerDramaID(sourceIkanBot, "1023900"),
		Chapter: chapter,
		Index: 1,
	})
	if err != nil {
		t.Fatalf("resolveIkanbotMedia failed: %v", err)
	}
	choice := nativePlaybackChoices(media, 0)
	want := []string{
		"https://cdn.example.test/1080/episode.mp4",
		"https://cdn.example.test/720/episode.mp4",
		"https://cdn.example.test/480/episode.mp4",
	}
	if len(choice.media) != len(want) {
		t.Fatalf("automatic routes = %d, want %d", len(choice.media), len(want))
	}
	for index, address := range want {
		if choice.media[index].URL != address {
			t.Fatalf("automatic route %d = %q, want %q", index, choice.media[index].URL, address)
		}
	}
}

func TestIkanbotQualityLabelsDoNotBecomeEpisodeNumbers(t *testing.T) {
	for _, label := range []string{"720", "720P", "1080", "1080p", "480", "720集", "1080集", "720P高清", "高清720", "藍光1080"} {
		if !ikanbotQualityOnlyLabel(label) {
			t.Fatalf("quality label %q was not recognized", label)
		}
		if got := ikanbotEpisodeOrder(label, 1); got != 1 {
			t.Fatalf("quality label %q became episode %d; want fallback episode 1", label, got)
		}
	}
	for _, test := range []struct { name string; want int }{
		{"第2集", 2}, {"EP03", 3}, {"Episode 4", 4}, {"第12話", 12},
	} {
		if got := ikanbotEpisodeOrder(test.name, 1); got != test.want {
			t.Fatalf("episode label %q = %d, want %d", test.name, got, test.want)
		}
	}
}

func TestIkanbotBuildChaptersKeepsEpisodeOneWhenLineLabelIs720(t *testing.T) {
	lines := []ikanbotLine{
		{Flag: "720P", Episodes: []ikanbotEpisode{{Name: "720", URL: "https://cdn-a.example.test/720/1.m3u8", Order: 1}}},
		{Flag: "1080P", Episodes: []ikanbotEpisode{{Name: "720", URL: "https://cdn-b.example.test/1080/1.m3u8", Order: 1}}},
	}
	chapters := ikanbotBuildChapters("1023899", "https://admin-api.aikanbot.com/play/1023899", lines)
	if len(chapters) != 1 {
		t.Fatalf("chapters = %d, want one episode: %#v", len(chapters), chapters)
	}
	if string(chapters[0].CurrentEpisode) != "1" || chapters[0].Title != "1" {
		t.Fatalf("720 route label was not normalized to episode 1: %#v", chapters[0])
	}
	if chapters[0].PlayLine != "1080P" || len(chapters[0].PlaybackVariants) != 1 ||
		!strings.Contains(chapters[0].PlaybackVariants[0], "cdn-a.example.test") {
		t.Fatalf("highest quality primary and automatic backup were not selected: %#v", chapters[0])
	}
}

func TestIkanbotMovieQualityLabelsMergeIntoOneEpisodeWithFallback(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"list": []any{
				map[string]any{"resData": `[{"flag":"720P","url":"720集$https://cdn-low.example.test/movie.mp4"}]`},
				map[string]any{"resData": `[{"flag":"1080P","url":"1080集$https://cdn-high.example.test/movie.mp4"}]`},
			},
		},
	}
	lines := ikanbotParseLines(payload)
	chapters := ikanbotBuildChapters("1023900", "https://admin-api.aikanbot.com/play/1023900", lines)
	if len(chapters) != 1 {
		t.Fatalf("quality-only movie lines produced %d episodes, want one: %#v", len(chapters), chapters)
	}
	chapter := chapters[0]
	if chapter.Title != "1" || string(chapter.CurrentEpisode) != "1" {
		t.Fatalf("movie quality label was treated as an episode number: %#v", chapter)
	}
	if chapter.PlayLine != "1080P" || chapter.VideoURL != "https://cdn-high.example.test/movie.mp4" {
		t.Fatalf("highest-quality source was not selected as primary: %#v", chapter)
	}
	if len(chapter.PlaybackVariants) != 1 || chapter.PlaybackVariants[0] != "720:https://cdn-low.example.test/movie.mp4" {
		t.Fatalf("lower-quality source was not preserved as fallback: %#v", chapter.PlaybackVariants)
	}
	d := &Downloader{}
	media, err := d.resolveIkanbotMedia(context.Background(), Task{
		DramaID: providerDramaID(sourceIkanBot, "1023900"),
		Chapter: chapter,
	})
	if err != nil {
		t.Fatalf("resolveIkanbotMedia failed: %v", err)
	}
	choice := nativePlaybackChoices(media, 0)
	if len(choice.media) != 2 ||
		choice.media[0].URL != "https://cdn-high.example.test/movie.mp4" ||
		choice.media[1].URL != "https://cdn-low.example.test/movie.mp4" {
		t.Fatalf("automatic playback did not keep high-quality-first fallback order: %#v", choice.media)
	}
}

func TestIkanbotAutoSelectsHighestHLSRendition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1800000,RESOLUTION=1280x720\n720/index.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080\n1080/index.m3u8\n"))
		case "/1080/index.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\nsegment-1080.ts\n#EXT-X-ENDLIST\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d := &Downloader{client: server.Client()}
	media, err := d.prepareWebProviderMedia(context.Background(), providerMedia{
		URL: server.URL + "/master.m3u8",
		Referer: server.URL + "/",
		Quality: 1080,
		StandardHTTP: true,
	}, "愛看機器人")
	if err != nil {
		t.Fatalf("prepareWebProviderMedia failed: %v", err)
	}
	media = d.ikanbotPreferHighestHLS(context.Background(), media)
	if !strings.HasSuffix(media.URL, "/1080/index.m3u8") ||
		!strings.Contains(media.Playlist, "segment-1080.ts") ||
		media.Quality != 1080 {
		t.Fatalf("Auto did not lock the 1080P rendition: URL=%s quality=%d playlist=%s",
			media.URL, media.Quality, media.Playlist)
	}
}

func TestIkanbotImageCandidatesSupportsLazyAndSrcset(t *testing.T) {
	body := "<div style=\"background-image:url('/poster/background.webp')\"><img src=\"data:image/gif;base64,placeholder\" data-original=\"/poster/original.jpg\" data-srcset=\"/poster/small.jpg 1x, /poster/large.jpg 2x\"></div>"
	doc, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil { t.Fatal(err) }
	var node *htmlnet.Node
	for _, candidate := range providerHTMLNodes(doc, func(n *htmlnet.Node) bool {
		return n.Type == htmlnet.ElementNode && n.Data == "img"
	}) { node = candidate; break }
	got := ikanbotImageCandidates(node, "https://admin-api.aikanbot.com/play/1023899")
	want := []string{
		"https://admin-api.aikanbot.com/poster/original.jpg",
		"https://admin-api.aikanbot.com/poster/small.jpg",
		"https://admin-api.aikanbot.com/poster/large.jpg",
	}
	if len(got) != len(want) { t.Fatalf("image candidates = %#v, want %#v", got, want) }
	for index := range want {
		if got[index] != want[index] { t.Fatalf("candidate %d=%q, want %q", index, got[index], want[index]) }
	}
}



func TestIkanbotMovieChaptersUseLanguageChoicesAndMergeCDNFallbacks(t *testing.T) {
	lines := []ikanbotLine{
		{Flag: "1080P", Episodes: []ikanbotEpisode{{Name: "國語", URL: "https://cdn.example.test/mandarin-1080.mp4", Order: 1}}},
		{Flag: "1080P", Episodes: []ikanbotEpisode{{Name: "日語版", URL: "https://cdn.example.test/japanese.mp4", Order: 1}}},
		{Flag: "720P", Episodes: []ikanbotEpisode{{Name: "国语", URL: "https://cdn.example.test/mandarin-720.mp4", Order: 1}}},
		{Flag: "來源甲", Episodes: []ikanbotEpisode{{Name: "第1集", URL: "https://cdn-a.example.test/movie.mp4", Order: 1}}},
		{Flag: "來源乙", Episodes: []ikanbotEpisode{{Name: "第1集", URL: "https://cdn-b.example.test/movie.mp4", Order: 1}}},
	}
	chapters := ikanbotBuildChapters("1023901", "https://admin-api.aikanbot.com/play/1023901", lines, true)
	if len(chapters) != 3 {
		t.Fatalf("movie choices = %d, want two audio choices plus one automatic route group: %#v", len(chapters), chapters)
	}
	if chapters[0].Title != "國語版" || chapters[1].Title != "日語版" || chapters[2].Title != "電影版" {
		t.Fatalf("language choices or default movie label are incorrect: %#v", chapters)
	}
	if chapters[0].VideoURL != "https://cdn.example.test/mandarin-1080.mp4" ||
		chapters[0].PlayLine != "1080P" || len(chapters[0].PlaybackVariants) != 1 ||
		chapters[0].PlaybackVariants[0] != "720:https://cdn.example.test/mandarin-720.mp4" {
		t.Fatalf("same-language alternate route was not retained as a fallback: %#v", chapters[0])
	}
	if chapters[2].VideoURL != "https://cdn-a.example.test/movie.mp4" ||
		len(chapters[2].PlaybackVariants) != 1 ||
		chapters[2].PlaybackVariants[0] != "0:https://cdn-b.example.test/movie.mp4" {
		t.Fatalf("unlabelled source lines were not merged as automatic fallbacks: %#v", chapters[2])
	}
}

func TestIkanbotMovieMTypeAndAudioLanguageRecognition(t *testing.T) {
	for _, value := range []string{"1", " movie ", "FILM"} {
		if !ikanbotIsMovieMType(value) {
			t.Errorf("movie mtype %q was not recognized", value)
		}
	}
	for _, value := range []string{"", "2", "tv"} {
		if ikanbotIsMovieMType(value) {
			t.Errorf("non-movie mtype %q was classified as movie", value)
		}
	}
	cases := map[string]string{
		"國語版": "國語版",
		"国语": "國語版",
		"粵語": "粵語版",
		"Cantonese": "粵語版",
		"日語": "日語版",
		"English": "英語版",
		"韓文": "韓語版",
		"原聲": "原聲版",
		"雙語": "雙語版",
	}
	for input, want := range cases {
		if got := ikanbotMovieLanguageLabel(input, "1080P"); got != want {
			t.Errorf("language label for %q = %q, want %q", input, got, want)
		}
	}
}

func TestGetHuangguoChaptersDispatchesIkanbotMovieAsSingleSelectableVersions(t *testing.T) {
	token := strings.Repeat("a", 80)
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/play/1023901":
			body := fmt.Sprintf(`<html><h1 id="video_title">電影測試作品</h1><input id="current_id" value="1023901"><input id="e_token" value="%s"><input id="mtype" value="1"></html>`, token)
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		case "/api/getResN":
			body := `{"state":1,"data":{"list":[{"resData":"[{\"flag\":\"1080P\",\"url\":\"國語$https://cdn.example.test/mandarin.mp4\"}]"},{"resData":"[{\"flag\":\"1080P\",\"url\":\"日語$https://cdn.example.test/japanese.mp4\"}]"}]}}`
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		default:
			return nil, fmt.Errorf("unexpected iKanBot movie request: %s", request.URL)
		}
	})
	title, chapters, err := d.GetHuangguoChapters(context.Background(), sourceIkanBot, "1023901")
	if err != nil {
		t.Fatalf("GetHuangguoChapters(iKanBot movie) failed: %v", err)
	}
	if title != "電影測試作品" || len(chapters) != 2 ||
		chapters[0].Title != "國語版" || chapters[1].Title != "日語版" {
		t.Fatalf("movie mtype did not return language-labelled choices: title=%q chapters=%#v", title, chapters)
	}
	if chapters[0].Source != sourceIkanBot || chapters[1].Source != sourceIkanBot {
		t.Fatalf("movie chapters lost source identity: %#v", chapters)
	}
}


func TestIkanbotMovieMergesSameLanguageAcrossDifferentSources(t *testing.T) {
	lines := []ikanbotLine{
		{Flag: "來源甲", Episodes: []ikanbotEpisode{{Name: "國語", URL: "https://source-a.example.test/movie.mp4", Order: 1}}},
		{Flag: "來源乙", Episodes: []ikanbotEpisode{{Name: "國語", URL: "https://source-b.example.test/movie.mp4", Order: 1}}},
	}
	chapters := ikanbotBuildChapters("1023902", "https://admin-api.aikanbot.com/play/1023902", lines, true)
	if len(chapters) != 1 || chapters[0].Title != "國語版" ||
		chapters[0].VideoURL != "https://source-a.example.test/movie.mp4" ||
		len(chapters[0].PlaybackVariants) != 1 ||
		chapters[0].PlaybackVariants[0] != "0:https://source-b.example.test/movie.mp4" {
		t.Fatalf("same-language CDN lines were not merged into one language choice: %#v", chapters)
	}
}


func TestIkanbotMovieMergesRepeatedLanguageRowsAsFallbacks(t *testing.T) {
	lines := []ikanbotLine{
		{Flag: "國語", Episodes: []ikanbotEpisode{{Name: "國語", URL: "https://source-a.example.test/movie.mp4", Order: 1}}},
		{Flag: "國語", Episodes: []ikanbotEpisode{{Name: "國語", URL: "https://source-b.example.test/movie.mp4", Order: 1}}},
	}
	chapters := ikanbotBuildChapters("1023903", "https://admin-api.aikanbot.com/play/1023903", lines, true)
	if len(chapters) != 1 || chapters[0].Title != "國語版" ||
		chapters[0].VideoURL != "https://source-a.example.test/movie.mp4" ||
		len(chapters[0].PlaybackVariants) != 1 ||
		chapters[0].PlaybackVariants[0] != "0:https://source-b.example.test/movie.mp4" {
		t.Fatalf("repeated language rows were not merged as fallbacks: %#v", chapters)
	}
}


func TestIkanbotMovieQualityOnlyRowsBecomeOneMovieChoice(t *testing.T) {
	lines := []ikanbotLine{
		{Flag: "1080P", Episodes: []ikanbotEpisode{{Name: "1080P", URL: "https://cdn-high.example.test/movie.mp4", Order: 1}}},
		{Flag: "720P", Episodes: []ikanbotEpisode{{Name: "720P", URL: "https://cdn-low.example.test/movie.mp4", Order: 1}}},
	}
	chapters := ikanbotBuildChapters("1023904", "https://admin-api.aikanbot.com/play/1023904", lines, true)
	if len(chapters) != 1 {
		t.Fatalf("quality-only movie rows = %d choices, want one: %#v", len(chapters), chapters)
	}
	if chapters[0].Title != "電影版" || chapters[0].VideoURL != "https://cdn-high.example.test/movie.mp4" {
		t.Fatalf("quality-only movie choice is incorrect: %#v", chapters[0])
	}
	if len(chapters[0].PlaybackVariants) != 1 ||
		chapters[0].PlaybackVariants[0] != "720:https://cdn-low.example.test/movie.mp4" {
		t.Fatalf("lower-quality movie route was not kept as fallback: %#v", chapters[0].PlaybackVariants)
	}
}

func TestIkanbotMovieMergesAllNamedAndQualityCDNsIntoOneChoice(t *testing.T) {
	lines := []ikanbotLine{
		{Flag: "來源甲 1080P", Episodes: []ikanbotEpisode{{Name: "第1集", URL: "https://source-a.example.test/1080.mp4", Order: 1}}},
		{Flag: "來源甲 720P", Episodes: []ikanbotEpisode{{Name: "第1集", URL: "https://source-a.example.test/720.mp4", Order: 1}}},
		{Flag: "來源乙 1080P", Episodes: []ikanbotEpisode{{Name: "第1集", URL: "https://source-b.example.test/1080.mp4", Order: 1}}},
	}
	chapters := ikanbotBuildChapters("1023905", "https://admin-api.aikanbot.com/play/1023905", lines, true)
	if len(chapters) != 1 || chapters[0].Title != "電影版" ||
		chapters[0].VideoURL != "https://source-a.example.test/1080.mp4" {
		t.Fatalf("all unlabelled movie CDN lines should form one selection: %#v", chapters)
	}
	if len(chapters[0].PlaybackVariants) != 2 ||
		chapters[0].PlaybackVariants[0] != "1080:https://source-b.example.test/1080.mp4" ||
		chapters[0].PlaybackVariants[1] != "720:https://source-a.example.test/720.mp4" {
		t.Fatalf("automatic movie fallback order is incorrect: %#v", chapters[0].PlaybackVariants)
	}
}


func TestIkanbotMovieLanguageTracksMergeAcrossCDNQualityVariants(t *testing.T) {
	lines := []ikanbotLine{
		{Flag: "來源甲 1080P", Episodes: []ikanbotEpisode{{Name: "國語", URL: "https://source-a.example.test/mandarin-1080.mp4", Order: 1}}},
		{Flag: "來源甲 720P", Episodes: []ikanbotEpisode{{Name: "國語", URL: "https://source-a.example.test/mandarin-720.mp4", Order: 1}}},
		{Flag: "來源乙 1080P", Episodes: []ikanbotEpisode{{Name: "國語", URL: "https://source-b.example.test/mandarin-1080.mp4", Order: 1}}},
	}
	chapters := ikanbotBuildChapters("1023906", "https://admin-api.aikanbot.com/play/1023906", lines, true)
	if len(chapters) != 1 || chapters[0].Title != "國語版" {
		t.Fatalf("all Mandarin CDN rows should be one language choice: %#v", chapters)
	}
	if chapters[0].VideoURL != "https://source-a.example.test/mandarin-1080.mp4" ||
		len(chapters[0].PlaybackVariants) != 2 ||
		chapters[0].PlaybackVariants[0] != "1080:https://source-b.example.test/mandarin-1080.mp4" ||
		chapters[0].PlaybackVariants[1] != "720:https://source-a.example.test/mandarin-720.mp4" {
		t.Fatalf("language fallback order is incorrect: %#v", chapters[0])
	}
}


func TestIkanbotMovieThirtyOneSourceRowsUseOnePrimaryAndThirtyFallbacks(t *testing.T) {
	lines := make([]ikanbotLine, 31)
	for index := range lines {
		lines[index] = ikanbotLine{
			Flag: fmt.Sprintf("source-%02d", index+1),
			Episodes: []ikanbotEpisode{{
				Name: "正片",
				URL: fmt.Sprintf("https://cdn.example.test/source-%02d/movie.m3u8", index+1),
				Order: 1,
			}},
		}
	}
	chapters := ikanbotBuildChapters("1023907", "https://admin-api.aikanbot.com/play/1023907", lines, true)
	if len(chapters) != 1 {
		t.Fatalf("31 CDN rows should become one movie selection, got %d: %#v", len(chapters), chapters)
	}
	if chapters[0].Title != "電影版" ||
		chapters[0].VideoURL != "https://cdn.example.test/source-01/movie.m3u8" ||
		len(chapters[0].PlaybackVariants) != 30 ||
		chapters[0].PlaybackVariants[0] != "0:https://cdn.example.test/source-02/movie.m3u8" ||
		chapters[0].PlaybackVariants[29] != "0:https://cdn.example.test/source-31/movie.m3u8" {
		t.Fatalf("31-row fallback plan did not preserve preferred order: %#v", chapters[0])
	}
}
