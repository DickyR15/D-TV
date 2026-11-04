package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGuipianParseCards(t *testing.T) {
	body := `<ul class="row video-list video-film-list"><a class="video-link" href="/nv/123.html" title="測試鬼片"><div data-original="/upload/poster.jpg"></div><div class="video-duration">HD</div></a></ul>`
	got := parseGuipianCards(body)
	if len(got) != 1 {
		t.Fatalf("got %d cards, want 1", len(got))
	}
	if got[0].Source != sourceGuipian || got[0].SourceID != "123" || got[0].Title != "測試鬼片" {
		t.Fatalf("unexpected drama: %#v", got[0])
	}
	if got[0].Cover != guipianSiteBaseURL+"/upload/poster.jpg" {
		t.Fatalf("unexpected cover: %#v", got[0].Cover)
	}
}

func TestGuipianPosterCandidatesSupportLazyAndResponsiveArtwork(t *testing.T) {
	markup := "<div class=\"video-cover\" style=\"background-image:url('//cdn.example.test/background.jpg')\">" +
		"<img class=\"lazy\" src=\"/assets/placeholder.gif\" data-original=\"/upload/original.jpg?token=1&amp;mode=poster\" " +
		"data-src=\"/upload/lazy.jpg\" srcset=\"//cdn.example.test/small.jpg 1x, //cdn.example.test/large.jpg 2x\"></div>"
	got := guipianPosterCandidates(markup)
	want := []string{
		guipianSiteBaseURL + "/upload/original.jpg?token=1&mode=poster",
		guipianSiteBaseURL + "/upload/lazy.jpg",
		guipianSiteBaseURL + "/assets/placeholder.gif",
		"https://cdn.example.test/small.jpg",
		"https://cdn.example.test/large.jpg",
		"https://cdn.example.test/background.jpg",
	}
	if len(got) != len(want) {
		t.Fatalf("poster candidates = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("poster candidate %d = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestGuipianDetailPosterCandidatesPreferDetailArtworkAndOpenGraph(t *testing.T) {
	body := "<meta property=\"og:image\" content=\"https://cdn.example.test/og-poster.jpg\">" +
		"<div class=\"detail-img\"><img src=\"/assets/placeholder.gif\" data-src=\"/upload/detail-poster.webp\"></div>" +
		"<div class=\"recommend-cover\"><img src=\"/upload/other-title.jpg\"></div>"
	got := guipianDetailPosterCandidates(body)
	if len(got) < 2 {
		t.Fatalf("detail poster candidates = %#v", got)
	}
	if got[0] != guipianSiteBaseURL+"/upload/detail-poster.webp" {
		t.Fatalf("detail poster priority = %q", got[0])
	}
	if got[len(got)-1] != "https://cdn.example.test/og-poster.jpg" {
		t.Fatalf("Open Graph poster should be retained as a fallback: %#v", got)
	}
}

func TestGuipianCategoryKindIsRetainedForPosterMatching(t *testing.T) {
	for category, want := range map[string]string{
		"3": "電影", "31": "電影", "12": "劇集", "18": "劇集", "23": "動漫", "26": "動漫",
	} {
		if got := guipianCategoryKind(category); got != want {
			t.Errorf("category %s kind = %q, want %q", category, got, want)
		}
	}
	if got := bangumiPosterSubjectTypes(nativeDrama{Category: "影视", Tags: []string{"電影"}}); len(got) != 1 || got[0] != 6 {
		t.Fatalf("film classification did not select movie posters: %#v", got)
	}
	if got := bangumiPosterSubjectTypes(nativeDrama{Category: "影视", Tags: []string{"劇集"}}); len(got) != 1 || got[0] != 6 {
		t.Fatalf("series classification did not select TV posters: %#v", got)
	}
	if got := bangumiPosterSubjectTypes(nativeDrama{Category: "影视", Tags: []string{"動漫"}}); len(got) != 1 || got[0] != 2 {
		t.Fatalf("animation classification did not select anime posters: %#v", got)
	}
}

func TestGuipianCoverMetadataDoesNotRequirePlayablePlaylist(t *testing.T) {
	body := "<h1 class=\"media-title\">奪金戰</h1>" +
		"<div class=\"detail-img\"><img src=\"/assets/placeholder.gif\" data-src=\"/upload/duojinzhan.webp\"></div>" +
		"<li>年份：<span>2018</span></li>"
	drama, err := parseGuipianCoverMetadata("123", body)
	if err != nil {
		t.Fatal(err)
	}
	if drama.Title != "奪金戰" || drama.OnlineDate != "2018" {
		t.Fatalf("detail metadata = %+v", drama)
	}
	if drama.Cover != guipianSiteBaseURL+"/upload/duojinzhan.webp" {
		t.Fatalf("poster = %q", drama.Cover)
	}
	normalized := nativeNormalize(drama)
	if normalized.OnlineDate != "2018" {
		t.Fatalf("normalized release year = %q", normalized.OnlineDate)
	}
}

func TestGuipianPlayURLValidation(t *testing.T) {
	if got := guipianPlayURL("/play/123-1-1.html"); got != guipianSiteBaseURL+"/play/123-1-1.html" {
		t.Fatalf("unexpected normalized URL: %q", got)
	}
	if got := guipianPlayURL("https://example.com/play/123-1-1.html"); got != "" {
		t.Fatalf("foreign host accepted: %q", got)
	}
}

func TestGuipianPlaylistEpisodes(t *testing.T) {
	body := `<li class="swiper-slide ewave-tab" data-target="#ewave-playlist-1">线路一</li><div id="ewave-playlist-1"><ul><li><a title='第1集' href='/play/123-1-1.html'>第1集</a></li><li><a title='第2集' href='/play/123-1-2.html'>第2集</a></li></ul></div><section>`
	name, chapters, err := guipianPickPlaylist(body)
	if err != nil {
		t.Fatal(err)
	}
	if name != "线路一" || len(chapters) != 2 {
		t.Fatalf("unexpected playlist: name=%q chapters=%d", name, len(chapters))
	}
	if chapters[0].PageURL != guipianSiteBaseURL+"/play/123-1-1.html" {
		t.Fatalf("unexpected episode URL: %#v", chapters[0])
	}
}

func TestGuipianCategoryValidation(t *testing.T) {
	if !validGuipianCategory("") || !validGuipianCategory("3") || !validGuipianCategory("11") {
		t.Fatal("valid category rejected")
	}
	if validGuipianCategory("999") || validGuipianCategory("../3") {
		t.Fatal("invalid category accepted")
	}
}


func guipianLiveFetch(ctx context.Context, client *http.Client, address, referer, byteRange string) (int, string, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return 0, "", nil, err
	}
	request.Header.Set("User-Agent", guipianUserAgent)
	request.Header.Set("Referer", referer)
	request.Header.Set("Accept", "*/*")
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, "", nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		return response.StatusCode, response.Header.Get("Content-Type"), nil, err
	}
	return response.StatusCode, response.Header.Get("Content-Type"), body, nil
}

func guipianFirstPlaylistURI(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

func guipianLiveProbeAsset(ctx context.Context, client *http.Client, address, referer string, depth int) error {
	if depth > 3 {
		return errors.New("HLS playlist nesting exceeded probe limit")
	}
	status, contentType, body, err := guipianLiveFetch(ctx, client, address, referer, "bytes=0-2047")
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusPartialContent {
		return fmt.Errorf("media HTTP %d", status)
	}
	if len(body) == 0 || strings.Contains(strings.ToLower(contentType), "text/html") {
		return fmt.Errorf("media response is empty or HTML (type %s)", contentType)
	}
	if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff")), "#EXTM3U") {
		child := guipianFirstPlaylistURI(string(body))
		if child == "" {
			return errors.New("HLS playlist has no child URI")
		}
		base, parseErr := url.Parse(address)
		if parseErr != nil {
			return parseErr
		}
		next, parseErr := base.Parse(child)
		if parseErr != nil {
			return parseErr
		}
		return guipianLiveProbeAsset(ctx, client, next.String(), referer, depth+1)
	}
	return nil
}

func TestGuipianLivePlayback(t *testing.T) {
	if os.Getenv("DTV_LIVE_GUIPIAN") != "1" {
		t.Skip("set DTV_LIVE_GUIPIAN=1 to probe the public Guipian source")
	}
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	downloader := engine.downloader

	rss, err := downloader.guipianGet(ctx, "/xml/rss.xml")
	if err != nil {
		t.Fatalf("fetch live RSS: %v", err)
	}
	var searchTitle string
	for _, item := range reGuipianRSSItem.FindAllStringSubmatch(rss, -1) {
		title := reGuipianRSSTitle.FindStringSubmatch(item[1])
		if len(title) > 1 {
			searchTitle = guipianClean(title[1])
			if searchTitle != "" {
				break
			}
		}
	}
	if searchTitle == "" {
		t.Fatal("live RSS contains no parseable title for search validation")
	}
	searchResults, _, err := downloader.searchGuipian(ctx, 1, searchTitle)
	if err != nil {
		t.Fatalf("live search for %q: %v", searchTitle, err)
	}
	if len(searchResults) == 0 {
		t.Fatalf("live search returned no result for RSS title %q", searchTitle)
	}
	t.Logf("search OK: %q -> %s", searchTitle, searchResults[0].ID)

	items, _, err := downloader.fetchGuipianCatalogPage(ctx, 1, "3", "")
	if err != nil {
		t.Fatalf("fetch live horror catalogue: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("live horror catalogue returned no titles")
	}
	t.Logf("catalogue OK: %d titles returned", len(items))

	// A separate drama category guards against accidentally treating every
	// title as a one-episode movie.
	seriesItems, _, seriesErr := downloader.fetchGuipianCatalogPage(ctx, 1, "12", "")
	if seriesErr != nil {
		t.Fatalf("fetch live series catalogue: %v", seriesErr)
	}
	seriesOK := false
	var seriesErrors []string
	for index, item := range seriesItems {
		if index >= 8 || ctx.Err() != nil || seriesOK {
			break
		}
		series, episodes, detailErr := downloader.fetchGuipianDetail(ctx, item.SourceID)
		if detailErr != nil {
			seriesErrors = append(seriesErrors, item.SourceID+": "+detailErr.Error())
			continue
		}
		if len(episodes) > 1 {
			seriesOK = true
			t.Logf("multi-episode detail OK: %s (%d episodes)", series.Title, len(episodes))
		}
	}
	if !seriesOK {
		t.Fatalf("live series catalogue did not produce a multi-episode detail: %s", strings.Join(seriesErrors, " | "))
	}

	posterOK := false
	playbackOK := false
	var detailErrors []string
	for index, item := range items {
		if index >= 8 || ctx.Err() != nil || (posterOK && playbackOK) {
			break
		}
		drama, chapters, detailErr := downloader.fetchGuipianDetail(ctx, item.SourceID)
		if detailErr != nil {
			detailErrors = append(detailErrors, item.SourceID+": "+detailErr.Error())
			continue
		}
		if drama.Title == "" || len(chapters) == 0 {
			detailErrors = append(detailErrors, item.SourceID+": detail has no title or episodes")
			continue
		}
		t.Logf("detail OK: %s (%d episodes)", drama.Title, len(chapters))

		coverAddresses := nativeCoverAddressesFromDrama(drama)
		if !posterOK && len(coverAddresses) > 0 {
			coverURL := coverAddresses[0]
			status, contentType, body, posterErr := guipianLiveFetch(ctx, downloader.client, coverURL, guipianSiteBaseURL+"/", "")
			if posterErr == nil && status >= 200 && status < 300 && len(body) > 0 && !strings.Contains(strings.ToLower(contentType), "text/html") {
				posterOK = true
				t.Logf("poster OK: %s (HTTP %d, %s, %d bytes)", coverURL, status, contentType, len(body))
			} else if posterErr != nil {
				detailErrors = append(detailErrors, item.SourceID+" poster: "+posterErr.Error())
			} else {
				detailErrors = append(detailErrors, item.SourceID+fmt.Sprintf(" poster: HTTP %d type=%s bytes=%d", status, contentType, len(body)))
			}
		}

		for episodeIndex, chapter := range chapters {
			if episodeIndex >= 4 || ctx.Err() != nil || playbackOK {
				break
			}
			task := Task{DramaID: drama.ID, DramaTitle: drama.Title, Chapter: chapter, Index: episodeIndex, Total: len(chapters)}
			media, mediaErr := downloader.resolveGuipianMedia(ctx, task)
			if mediaErr != nil {
				detailErrors = append(detailErrors, item.SourceID+" episode "+chapter.Title+": "+mediaErr.Error())
				continue
			}
			if err := guipianLiveProbeAsset(ctx, downloader.streamClient, media.URL, media.Referer, 0); err != nil {
				detailErrors = append(detailErrors, item.SourceID+" episode "+chapter.Title+": "+err.Error())
				continue
			}
			playbackOK = true
			t.Logf("playback OK: %s / %s -> %s", drama.Title, chapter.Title, media.URL)
			break
		}
	}
	if !posterOK || !playbackOK {
		t.Fatalf("live source validation incomplete: poster=%t playback=%t; details: %s", posterOK, playbackOK, strings.Join(detailErrors, " | "))
	}
}
