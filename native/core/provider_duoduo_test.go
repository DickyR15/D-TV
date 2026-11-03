package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestDuoduoParsesOnlyPublicDirectPlaybackLines(t *testing.T) {
	rows := []map[string]any{
		{
			"site_key": "vip", "site_name": "VIP 線路", "decode_status": 1,
			"vod_play_url": "第1集$https://vip.example.test/play?id=1",
		},
		{
			"site_key": "direct", "site_name": "直連線路", "decode_status": 0,
			"vod_play_url": "第1集$https://media.example.test/1.m3u8?token=a#第2集$https://media.example.test/2.mp4",
		},
		{
			"site_key": "unsupported", "site_name": "非直連線路", "decode_status": 0,
			"vod_play_url": "第1集$https://player.example.test/watch/1",
		},
	}
	lines := duoduoParseAggregateLines(rows)
	if len(lines) != 1 || lines[0].Key != "direct" || len(lines[0].Episodes) != 2 {
		t.Fatalf("unexpected direct playback lines: %+v", lines)
	}
	if lines[0].Episodes[0].URL != "https://media.example.test/1.m3u8?token=a" ||
		lines[0].Episodes[1].URL != "https://media.example.test/2.mp4" {
		t.Fatalf("direct media URLs were not retained: %+v", lines[0].Episodes)
	}
}

func TestDuoduoPosterUsesOfficialPlayPageRefererBeforeHomepage(t *testing.T) {
	downloader := &Downloader{
		providerHosts: map[string]string{sourceDuoduo: "https://323433ssdfd.top"},
	}
	drama := nativeDrama{ID: "duoduo:54635", Source: sourceDuoduo, Title: "測試影片"}
	const poster = "https://img.example.test/poster.jpg"
	const wantPage = "https://323433ssdfd.top/play/54635"

	if got := nativeCoverRefererForDrama(downloader, drama, poster); got != wantPage {
		t.Fatalf("Duoduo poster Referer = %q, want official play page %q", got, wantPage)
	}

	attempts := duoduoPosterAttemptsForDrama(poster, downloader, drama)
	if len(attempts) < 2 || attempts[0].address != poster || attempts[0].referer != wantPage {
		t.Fatalf("official play-page Referer was not the first poster attempt: %+v", attempts)
	}
	root := "https://323433ssdfd.top/"
	foundRootRetry := false
	for _, attempt := range attempts[1:] {
		if attempt.address == poster && attempt.referer == root {
			foundRootRetry = true
			break
		}
	}
	if !foundRootRetry {
		t.Fatalf("homepage Referer compatibility retry was lost: %+v", attempts)
	}
}
func TestDuoduoPosterFallbackFields(t *testing.T) {
	cases := []struct {
		name string
		row  map[string]any
		want string
	}{
		{
			name: "primary poster wins",
			row: map[string]any{
				"vod_pic": "https://img.example.test/main.jpg",
				"vod_pic_thumb": "https://img.example.test/thumb.jpg",
			},
			want: "https://img.example.test/main.jpg",
		},
		{
			name: "thumbnail fills missing primary poster",
			row: map[string]any{
				"vod_pic": "",
				"vod_pic_thumb": "https://img.example.test/thumb.jpg",
			},
			want: "https://img.example.test/thumb.jpg",
		},
		{
			name: "relative slide image fills missing poster",
			row: map[string]any{
				"vod_pic": "javascript:bad",
				"vod_pic_slide": "/images/slide.jpg",
			},
			want: "https://duoduozhuiju.com/images/slide.jpg",
		},
		{
			name: "poster object url is accepted",
			row: map[string]any{
				"vod_pic": map[string]any{"url": "https://img.example.test/object.jpg"},
			},
			want: "https://img.example.test/object.jpg",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.row["vod_id"] = 78296
			tc.row["vod_name"] = "測試影片"
			drama := duoduoDramaFromRow(tc.row)
			if drama.Cover != tc.want || drama.CoverURL != tc.want {
				t.Fatalf("poster fallback = %v / %v, want %q", drama.Cover, drama.CoverURL, tc.want)
			}
		})
	}
}

func TestDuoduoPosterRecoveryHandlesBaiduWrappersAndUnknownCDNs(t *testing.T) {
	const wrapped = "https://gimg0.baidu.com/gimg/app=2001&n=0&g=0n&fmt=jpeg&src=https%3A%2F%2Fi0.hdslb.com%2Fbfs%2Fbangumi%2Fimage%2F0af10a0c3258186e96fde4406b384c13dd643d8f.png"
	const source = "https://i0.hdslb.com/bfs/bangumi/image/0af10a0c3258186e96fde4406b384c13dd643d8f.png"
	if got := duoduoBaiduPosterSourceURL(wrapped); got != source {
		t.Fatalf("Baidu poster wrapper source = %q, want %q", got, source)
	}
	proxy := duoduoPosterProxyURL("https://poster-cdn.example.test/images/cover.jpg?token=a&size=large")
	if proxy == "" ||
		!strings.Contains(proxy, "poster-cdn.example.test") ||
		!strings.Contains(proxy, "token%3Da%26size%3Dlarge") {
		t.Fatalf("unknown public CDN did not retain its full URL in proxy fallback: %q", proxy)
	}
	if duoduoPosterProxyURL("http://127.0.0.1/private.png") != "" ||
		duoduoPosterProxyURL("http://localhost/private.png") != "" {
		t.Fatal("poster proxy must reject localhost and literal private addresses")
	}
	attempts := duoduoPosterAttempts(wrapped, "https://323433ssdfd.top/")
	seenSource := false
	seenBaiduReferer := false
	seenBilibiliReferer := false
	for _, attempt := range attempts {
		seenSource = seenSource || attempt.address == source
		seenBaiduReferer = seenBaiduReferer || attempt.address == wrapped && attempt.referer == "https://www.baidu.com/"
		seenBilibiliReferer = seenBilibiliReferer || attempt.address == source && attempt.referer == "https://www.bilibili.com/"
	}
	if !seenSource || !seenBaiduReferer || !seenBilibiliReferer {
		t.Fatalf("Baidu poster recovery lacks wrapper/source/referrer attempts: %+v", attempts)
	}
}

func TestDuoduoKeepsAlternatePosterFieldsForRetry(t *testing.T) {
	row := map[string]any{
		"vod_id": 78296,
		"vod_name": "測試影片",
		"vod_pic": "https://broken.example.test/main.jpg",
		"vod_pic_thumb": "https://working.example.test/thumb.jpg",
		"vod_pic_slide": "https://working.example.test/slide.jpg",
	}
	addresses := duoduoPosterAddressesFromRow(row)
	if len(addresses) != 3 ||
		addresses[0] != "https://broken.example.test/main.jpg" ||
		addresses[1] != "https://working.example.test/thumb.jpg" ||
		addresses[2] != "https://working.example.test/slide.jpg" {
		t.Fatalf("alternate poster URLs were lost or reordered: %+v", addresses)
	}
}

func TestNativeDecodeCoverUnwrapsGzipImageBodies(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(image); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	decoded := nativeDecodeCover(compressed.Bytes())
	if !bytes.Equal(decoded, image) || !nativeIsCoverImage(decoded) {
		t.Fatalf("gzip-wrapped PNG was not restored: %x", decoded)
	}

	var htmlCompressed bytes.Buffer
	htmlWriter := gzip.NewWriter(&htmlCompressed)
	if _, err := htmlWriter.Write([]byte("<html>403 Forbidden</html>")); err != nil {
		t.Fatal(err)
	}
	if err := htmlWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if nativeIsCoverImage(nativeDecodeCover(htmlCompressed.Bytes())) {
		t.Fatal("gzip-wrapped HTML response must not be accepted as a poster image")
	}
}

func TestNativeCoverPartialResponseMustContainWholeImage(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		contentRange string
		want       bool
	}{
		{name: "ordinary success", statusCode: http.StatusOK, want: true},
		{name: "complete partial response", statusCode: http.StatusPartialContent, contentRange: "bytes 0-99/100", want: true},
		{name: "truncated partial response", statusCode: http.StatusPartialContent, contentRange: "bytes 0-49/100", want: false},
		{name: "nonzero range start", statusCode: http.StatusPartialContent, contentRange: "bytes 50-99/100", want: false},
		{name: "unknown total", statusCode: http.StatusPartialContent, contentRange: "bytes 0-99/*", want: false},
		{name: "not found", statusCode: http.StatusNotFound, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: tc.statusCode,
				Header: http.Header{},
			}
			if tc.contentRange != "" {
				response.Header.Set("Content-Range", tc.contentRange)
			}
			if got := nativeCoverResponseComplete(response); got != tc.want {
				t.Fatalf("nativeCoverResponseComplete() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDuoduoMapsMovieMetadataAndCategories(t *testing.T) {
	drama := duoduoDramaFromRow(map[string]any{
		"vod_id": 78296, "vod_name": "流浪地球（特別版）",
		"type_name": "电影", "vod_year": "2019",
		"vod_pic": "//images.example.test/poster.jpg",
		"vod_content": "&lt;p&gt;科幻電影&lt;/p&gt;",
		"vod_remarks": "正片", "vod_douban_score": "7.5", "vod_douban_id": 20502443,
		"vod_hits": 12345, "vod_class": []any{"科幻", "冒險"},
		"vod_area": []any{"中國大陸"},
	})
	if drama.ID != "duoduo:78296" || drama.Source != sourceDuoduo ||
		drama.Title != "流浪地球（特別版）" || drama.CategoryName != "电影" || drama.DoubanID != "20502443" ||
		drama.Cover != "https://images.example.test/poster.jpg" ||
		!strings.Contains(drama.Desc, "<p>科幻電影</p>") {
		t.Fatalf("Duoduo metadata mapping is incomplete: %+v", drama)
	}
	if !validNativeCategory(sourceDuoduo, "1") || !validNativeCategory(sourceDuoduo, "4") ||
		validNativeCategory(sourceDuoduo, "999") || validNativeCategory(sourceDuoduo, "../1") {
		t.Fatal("Duoduo category validation accepted invalid type IDs")
	}

	uncategorized := duoduoDramaFromRow(map[string]any{
		"vod_id": 78297, "vod_name": "未提供官方類別的影片",
	})
	if uncategorized.CategoryName != "" {
		t.Fatalf("missing Duoduo type_name must stay uncategorized, got %q", uncategorized.CategoryName)
	}
	categories := duoduoCategoriesFromHome(map[string]any{
		"categories": []any{
			map[string]any{"type_id": 1, "type_name": "电影"},
			map[string]any{"type_id": 2, "type_name": "剧集"},
			map[string]any{"type_id": 3, "type_name": "动漫"},
			map[string]any{"type_id": 4, "type_name": "综艺"},
		},
	})
	if len(categories) != 4 {
		t.Fatalf("Duoduo categories should mirror the four official homepage categories, got %+v", categories)
	}
	for _, category := range categories {
		if category.Name == "影視" || category.Name == "影视" {
			t.Fatalf("D TV invented a Duoduo category not present on the official site: %+v", category)
		}
	}
	if canonicalProviderSource("duoduozhuiju.com") != sourceDuoduo ||
		canonicalProviderSource("323433ssdfd.top") != sourceDuoduo {
		t.Fatal("Duoduo entry or mirror host did not resolve to the unified source")
	}
	for _, retired := range []string{"baofeng", "jisu", "360", "huya"} {
		if isHuangguoProviderSource(retired) {
			t.Fatalf("retired MacCMS source %q is still registered", retired)
		}
	}
}

func TestDuoduoCatalogAndSearchUseNativeAPI(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/index/home":
			if request.Header.Get("X-Client") != duoduoClientID ||
				request.Header.Get("web-sign") != duoduoWebSign {
				t.Fatal("Duoduo public web client headers are missing")
			}
			return sourceFixtureResponse(request, http.StatusOK,
				`{"code":200,"data":{"categories":[{"type_id":1,"type_name":"电影"},{"type_id":2,"type_name":"剧集"}]}}`), nil
		case "/api.php/web/filter/vod":
			if request.URL.Query().Get("type_id") != "" ||
				request.URL.Query().Get("type_name") != "电影" ||
				request.URL.Query().Get("sort") != "hits" ||
				request.URL.Query().Get("pageSize") != "24" {
				t.Fatalf("unexpected Duoduo filter parameters: %s", request.URL.RawQuery)
			}
			return sourceFixtureResponse(request, http.StatusOK,
				`{"code":200,"data":[{"vod_id":78296,"vod_name":"流浪地球","type_name":"电影","vod_year":"2019"}]}`), nil
		case "/api.php/web/search/index":
			if request.URL.Query().Get("wd") != "流浪地球" ||
				request.URL.Query().Get("limit") != "15" {
				t.Fatalf("unexpected Duoduo search parameters: %s", request.URL.RawQuery)
			}
			return sourceFixtureResponse(request, http.StatusOK,
				`{"code":200,"data":[{"vod_id":78296,"vod_name":"流浪地球","type_name":"电影","vod_year":"2019"}]}`), nil
		default:
			t.Fatalf("unexpected Duoduo API endpoint: %s", request.URL.String())
			return nil, nil
		}
	})
	items, more, err := d.fetchDuoduoCatalogPage(context.Background(), 1, "1", "")
	if err != nil || more || len(items) != 1 || items[0].ID != "duoduo:78296" {
		t.Fatalf("Duoduo movie category failed: items=%+v more=%v err=%v", items, more, err)
	}
	items, more, err = d.fetchDuoduoCatalogPage(context.Background(), 1, "", "流浪地球")
	if err != nil || more || len(items) != 1 || items[0].Title != "流浪地球" {
		t.Fatalf("Duoduo search failed: items=%+v more=%v err=%v", items, more, err)
	}
}

func TestDuoduoMirrorFailoverAndPreferredHost(t *testing.T) {
	var first, second int
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case duoduoAPIHosts[0]:
			first++
			return sourceFixtureResponse(request, http.StatusServiceUnavailable, "busy"), nil
		case duoduoAPIHosts[1]:
			second++
			return sourceFixtureResponse(request, http.StatusOK, `{"code":200,"data":{"ok":true}}`), nil
		default:
			t.Fatalf("unexpected mirror host: %s", request.URL.Host)
			return nil, nil
		}
	})
	_, base, err := d.fetchDuoduoAPI(context.Background(), "/api.php/web/index/home", nil)
	if err != nil || base != "https://"+duoduoAPIHosts[1] || first != 1 || second != 1 {
		t.Fatalf("mirror failover failed: base=%q first=%d second=%d err=%v", base, first, second, err)
	}
	d.providerMu.Lock()
	preferred := d.providerHosts[sourceDuoduo]
	d.providerMu.Unlock()
	if preferred != base {
		t.Fatalf("successful mirror was not remembered: %q != %q", preferred, base)
	}
}

func TestDuoduoDetailAndPlaybackMergesLinesIntoOneEpisodeList(t *testing.T) {
	directA := "https://media.example.test/movie/index.m3u8"
	directB := "https://backup.example.test/movie/index.mp4"
	episode201 := "https://media.example.test/movie/episode-201.m3u8"
	aggregateCalls := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/vod/get_detail":
			return sourceFixtureResponse(request, http.StatusOK,
				`{"code":200,"data":[{"vod_id":78296,"vod_name":"測試長篇劇集","type_name":"劇集","vod_year":"2026","vod_pic":"https://images.example.test/poster.jpg","vod_play_from":"direct","vod_play_url":"第1集$https://media.example.test/movie/index.m3u8"}],"vodplayer":[{"from":"direct","decode_status":0,"show":"直連","sort":1}]}`), nil
		case "/api.php/web/internal/search_aggregate":
			aggregateCalls++
			body := fmt.Sprintf(`{"code":200,"data":[{"site_key":"direct","site_name":"直連","decode_status":0,"sort":1,"vod_play_url":"第1集$%s"},{"site_key":"backup","site_name":"備援","decode_status":0,"sort":2,"vod_play_url":"第1集$%s"},{"site_key":"long-line","site_name":"長集數線路","decode_status":0,"sort":3,"vod_play_url":"第201集$%s"}]}`, directA, directB, episode201)
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		default:
			t.Fatalf("unexpected Duoduo detail/playback endpoint: %s", request.URL.String())
			return nil, nil
		}
	})
	drama, chapters, err := d.fetchDuoduoDetail(context.Background(), "78296")
	if err != nil || len(chapters) != 2 || drama.ID != "duoduo:78296" {
		t.Fatalf("Duoduo detail did not merge duplicate lines: drama=%+v chapters=%+v err=%v", drama, chapters, err)
	}
	if drama.TotalEpisode != 201 || drama.EpisodeCount != 201 || drama.Episodes != 201 || !drama.EpisodesVerified {
		t.Fatalf("episode total must use the highest episode number across all lines: %+v", drama)
	}
	if chapters[0].EpisodeString(1) != "1" || chapters[0].VideoURL != directA ||
		chapters[1].EpisodeString(2) != "201" || chapters[1].VideoURL != episode201 {
		t.Fatalf("canonical episodes are not in numeric order: %+v", chapters)
	}
	if chapters[0].PlayLine != "" || chapters[1].PlayLine != "" {
		t.Fatalf("automatic playback should not expose duplicate per-line episode selectors: %+v", chapters)
	}
	if chapters[0].ID == chapters[1].ID {
		t.Fatalf("canonical chapter IDs must be unique: %+v", chapters)
	}
	if len(chapters[0].PlaybackVariants) != 1 || chapters[0].PlaybackVariants[0] != directB ||
		len(chapters[1].PlaybackVariants) != 0 {
		t.Fatalf("same-episode alternatives were not attached to the canonical episode: %+v", chapters)
	}
	if count := duoduoChapterEpisodeCount(chapters); count != 201 {
		t.Fatalf("canonical episode count = %d, want highest available number 201", count)
	}

	media, resolveErr := d.resolveDuoduoMedia(context.Background(), Task{
		DramaID: drama.ID,
		Chapter: chapters[0],
	})
	if resolveErr != nil || media.URL != directA || !media.StandardHTTP ||
		len(media.Variants) != 1 || media.Variants[0].URL != directB {
		t.Fatalf("episode 1 did not retain its automatic cross-line fallback: media=%+v err=%v", media, resolveErr)
	}
	choice := nativePlaybackChoices(media, 0)
	if len(choice.media) != 2 || choice.media[0].URL != directA || choice.media[1].URL != directB {
		t.Fatalf("automatic player routes do not contain both same-episode lines: %+v", choice.media)
	}
	if aggregateCalls != 1 {
		t.Fatalf("direct URL playback re-fetched aggregate API %d times; want only the detail-time request", aggregateCalls)
	}
}

func TestDuoduoDetailDirectURLSurvivesAggregateEndpointFailure(t *testing.T) {
	const direct = "https://media.example.test/movie/direct.m3u8"
	aggregateCalls := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/vod/get_detail":
			return sourceFixtureResponse(request, http.StatusOK,
				`{"code":200,"data":[{"vod_id":78296,"vod_name":"測試電影","type_name":"电影","vod_play_from":"direct","vod_play_url":"第1集$https://media.example.test/movie/direct.m3u8"}],"vodplayer":[{"from":"direct","decode_status":0,"show":"直連","sort":1}]}`), nil
		case "/api.php/web/internal/search_aggregate":
			aggregateCalls++
			return sourceFixtureResponse(request, http.StatusServiceUnavailable, "aggregate temporarily unavailable"), nil
		default:
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
			return nil, nil
		}
	})

	drama, chapters, err := d.fetchDuoduoDetail(context.Background(), "78296")
	if err != nil || len(chapters) != 1 {
		t.Fatalf("detail payload should provide the direct chapter: drama=%+v chapters=%+v err=%v", drama, chapters, err)
	}
	if chapters[0].VideoURL != direct || chapters[0].PlayLine != "直連" {
		t.Fatalf("detail API direct URL was not retained: %+v", chapters[0])
	}
	if aggregateCalls == 0 {
		t.Fatal("full aggregate list should be queried to discover additional playback sources")
	}

	media, err := d.resolveDuoduoMedia(context.Background(), Task{
		DramaID: drama.ID,
		Chapter: chapters[0],
	})
	if err != nil || media.URL != direct || !media.StandardHTTP {
		t.Fatalf("stored direct URL should resolve without aggregate API: media=%+v err=%v", media, err)
	}
}


func TestDuoduoHomeFeaturedVideosAreIncludedOnFirstPage(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/index/home":
			return sourceFixtureResponse(request, http.StatusOK,
				`{"code":200,"data":{"categories":[{"type_id":1,"type_name":"电影","videos":[{"vod_id":100,"vod_name":"首頁電影A"},{"vod_id":101,"vod_name":"首頁電影B"}]},{"type_id":2,"type_name":"剧集","videos":[{"vod_id":200,"vod_name":"首頁劇集"}]}],"recommend":[{"vod_id":300,"vod_name":"首頁推薦"}]}}`), nil
		case "/api.php/web/filter/vod":
			if request.URL.Query().Get("page") != "1" ||
				request.URL.Query().Get("pageSize") != "24" {
				t.Fatalf("unexpected Duoduo catalog query: %s", request.URL.RawQuery)
			}
			return sourceFixtureResponse(request, http.StatusOK,
				`{"code":200,"data":[{"vod_id":100,"vod_name":"首頁電影A"},{"vod_id":400,"vod_name":"一般片庫影片"}]}`), nil
		default:
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
			return nil, nil
		}
	})

	items, more, err := d.fetchDuoduoCatalogPage(context.Background(), 1, "", "")
	if err != nil || more {
		t.Fatalf("Duoduo first page failed: items=%+v more=%v err=%v", items, more, err)
	}
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		ids[item.SourceID] = true
	}
	for _, id := range []string{"100", "101", "200", "300", "400"} {
		if !ids[id] {
			t.Errorf("homepage/catalog title %s was not included", id)
		}
	}
	if len(items) != 5 {
		t.Fatalf("duplicate featured titles were not de-duplicated: got %d items (%+v)", len(items), items)
	}
}

func TestDuoduoCategoryHomeSupplementOnlyIncludesSelectedCategory(t *testing.T) {
	data := map[string]any{
		"categories": []any{
			map[string]any{
				"type_id": 1, "videos": []any{
					map[string]any{"vod_id": 101, "vod_name": "電影"},
				},
			},
			map[string]any{
				"type_id": 2, "videos": []any{
					map[string]any{"vod_id": 201, "vod_name": "劇集"},
				},
			},
		},
		"recommend": []any{
			map[string]any{"vod_id": 301, "vod_name": "首頁推薦"},
		},
	}
	items := duoduoHomeSupplement(data, "1")
	if len(items) != 1 || items[0].SourceID != "101" {
		t.Fatalf("category supplement mixed categories or recommendations: %+v", items)
	}
	all := duoduoHomeSupplement(data, "")
	if len(all) != 3 {
		t.Fatalf("all-category home supplement = %d, want 3", len(all))
	}
}

func TestDuoduoPosterRecoveryGetsDoubanIDWhenCatalogOmitsIt(t *testing.T) {
	const cover = "https://images.example.test/poster.jpg"
	const doubanID = "36524559"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api.php/web/vod/get_detail" {
			t.Fatalf("poster recovery called an unexpected endpoint: %s", request.URL.String())
			return nil, nil
		}
		return sourceFixtureResponse(request, http.StatusOK,
			`{"code":200,"data":[{"vod_id":1625,"vod_name":"鬼灭之刃 剧场版 无限城篇 第一章 猗窝座再来","vod_pic":"https://dbzy5.com/poster.jpg","vod_douban_id":36524559}]}`), nil
	})
	addresses, gotDoubanID, err := d.nativeCoverRecoveryMetadata(context.Background(), nativeDrama{
		ID: "duoduo:1625", Source: sourceDuoduo, SourceID: "1625",
		Title: "鬼灭之刃 剧场版 无限城篇 第一章 猗窝座再来",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotDoubanID != doubanID {
		t.Fatalf("detail Douban ID = %q, want %q", gotDoubanID, doubanID)
	}
	if len(addresses) == 0 || addresses[0] != "https://dbzy5.com/poster.jpg" {
		t.Fatalf("detail poster addresses = %v, want original cover %q", addresses, cover)
	}
}

func TestDuoduoPosterMetadataDoesNotRequirePlayableLines(t *testing.T) {
	const cover = "https://images.example.test/poster.jpg"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api.php/web/vod/get_detail" {
			t.Fatalf("poster lookup called an unexpected endpoint: %s", request.URL.String())
			return nil, nil
		}
		return sourceFixtureResponse(request, http.StatusOK,
			`{"code":200,"data":[{"vod_id":78296,"vod_name":"沒有直連線路的影片","vod_pic":"https://images.example.test/poster.jpg","vod_play_from":"player","vod_play_url":"正片$https://player.example.test/watch/1"}]}`), nil
	})
	address, err := d.nativeCoverAddress(context.Background(), nativeDrama{
		ID: "duoduo:78296", Source: sourceDuoduo, SourceID: "78296",
	})
	if err != nil || address != cover {
		t.Fatalf("poster metadata should be available without playable lines: address=%q err=%v", address, err)
	}
}

func TestDuoduoPosterProxySupportsPublicCDNsAndRejectsUnsafeHosts(t *testing.T) {
	for _, address := range []string{
		"https://img.ffzy888.com/poster/1.jpg?token=a%2Fb",
		"https://images.example.test/poster.jpg",
		"https://gimg0.baidu.com/gimg/app=2001&src=https%3A%2F%2Fi0.hdslb.com%2Fposter.png",
	} {
		proxy := duoduoPosterProxyURL(address)
		if !strings.HasPrefix(proxy, "https://images.weserv.nl/?url=") {
			t.Fatalf("public poster host was not routed through image proxy: address=%q proxy=%q", address, proxy)
		}
	}
	for _, address := range []string{
		"http://127.0.0.1/private.png",
		"http://localhost/private.png",
		"http://poster.example.test:8080/poster.jpg",
		"http://router.local/private.png",
	} {
		if got := duoduoPosterProxyURL(address); got != "" {
			t.Fatalf("unsafe poster host should not be proxied: address=%q proxy=%q", address, got)
		}
	}
}

func TestDuoduoProviderBaseURLRemembersWorkingMirror(t *testing.T) {
	d := &Downloader{providerHosts: map[string]string{
		sourceDuoduo: "https://duoduosdf12223234334.top",
	}}
	if got := d.providerBaseURL(sourceDuoduo); got != "https://duoduosdf12223234334.top" {
		t.Fatalf("Duoduo referer base did not follow the active mirror: %q", got)
	}
}


func TestDuoduoCatalogPaginationUsesTypeNameAndStopsOnShortPage(t *testing.T) {
	homeCalls := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/index/home":
			homeCalls++
			return sourceFixtureResponse(request, http.StatusOK,
				`{"code":200,"data":{"categories":[{"type_id":1,"type_name":"电影","videos":[]}]}}`), nil
		case "/api.php/web/filter/vod":
			query := request.URL.Query()
			if query.Get("type_id") != "" || query.Get("type_name") != "电影" ||
				query.Get("pageSize") != "24" || query.Get("sort") != "hits" {
				t.Fatalf("unexpected Duoduo category filters: %s", request.URL.RawQuery)
			}
			count := 5
			if query.Get("page") == "1" {
				count = 18
			} else if query.Get("page") != "2" {
				t.Fatalf("unexpected Duoduo page number: %s", query.Get("page"))
			}
			rows := make([]map[string]any, 0, count)
			for index := 0; index < count; index++ {
				id := 1000 + (queryPageNumber(query.Get("page"))-1)*100 + index
				rows = append(rows, map[string]any{
					"vod_id": id, "vod_name": fmt.Sprintf("測試影片%d", id),
					"type_name": "电影",
				})
			}
			body, err := json.Marshal(map[string]any{"code": 200, "data": rows})
			if err != nil {
				t.Fatal(err)
			}
			return sourceFixtureResponse(request, http.StatusOK, string(body)), nil
		default:
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
			return nil, nil
		}
	})
	first, more, err := d.fetchDuoduoCatalogPage(context.Background(), 1, "1", "")
	if err != nil || len(first) != 18 || !more {
		t.Fatalf("first Duoduo page = %d items, more=%v err=%v", len(first), more, err)
	}
	second, more, err := d.fetchDuoduoCatalogPage(context.Background(), 2, "1", "")
	if err != nil || len(second) != 5 || more {
		t.Fatalf("second Duoduo page = %d items, more=%v err=%v", len(second), more, err)
	}
	if homeCalls != 1 {
		t.Fatalf("homepage metadata was fetched %d times for two pages; want once", homeCalls)
	}
}

func queryPageNumber(value string) int {
	page, err := strconv.Atoi(value)
	if err != nil || page < 1 {
		return 1
	}
	return page
}


func TestDuoduoMovieLanguagesRemainOneEpisode(t *testing.T) {
  chapters := []Chapter{
    {
      ID: "duoduo:161235:1", Source: sourceDuoduo, Title: "普通话版",
      VideoURL: "https://media.example.test/movie/mandarin.mp4",
      PlaybackVariants: []string{"https://backup.example.test/movie/mandarin.mp4"},
      CurrentEpisode: rawEpisode(1),
    },
    {
      ID: "duoduo:161235:2", Source: sourceDuoduo, Title: "粤语版",
      VideoURL: "https://media.example.test/movie/cantonese.mp4",
      PlaybackVariants: []string{"https://backup.example.test/movie/cantonese.mp4"},
      CurrentEpisode: rawEpisode(2),
    },
  }
  got := duoduoMovieChapter("161235", chapters)
  if len(got) != 1 {
    t.Fatalf("movie versions produced %d chapters, want exactly one: %+v", len(got), got)
  }
  if got[0].Title != "正片" || got[0].EpisodeString(1) != "1" ||
    got[0].ID != providerChapterID(sourceDuoduo, "161235", "1") {
    t.Fatalf("movie chapter identity is not canonical: %+v", got[0])
  }
  want := []string{
    "https://backup.example.test/movie/mandarin.mp4",
    "https://media.example.test/movie/cantonese.mp4",
    "https://backup.example.test/movie/cantonese.mp4",
  }
  if !reflect.DeepEqual(got[0].PlaybackVariants, want) {
    t.Fatalf("movie versions were not preserved as same-episode fallbacks: got=%v want=%v", got[0].PlaybackVariants, want)
  }
}

func TestDuoduoMovieCategoryRecognizesSimplifiedAndTraditionalLabels(t *testing.T) {
  for _, category := range []string{"电影", "電影", "movie", "影片"} {
    if !duoduoIsMovieCategory(category) {
      t.Errorf("movie category %q was not recognized", category)
    }
  }
  if duoduoIsMovieCategory("剧集") || duoduoIsMovieCategory("影視") {
    t.Fatal("series or obsolete generic media label was treated as a movie category")
  }
}

func TestMigrateNativeDramaRemovesObsoleteDuoduoCategory(t *testing.T) {
  migrated := migrateNativeDrama(nativeDrama{
    ID: "duoduo:161235", Source: sourceDuoduo, Category: "影視",
  })
  if migrated.Category != "" {
    t.Fatalf("obsolete Duoduo category survived cache migration: %q", migrated.Category)
  }
  current := migrateNativeDrama(nativeDrama{
    ID: "duoduo:161235", Source: sourceDuoduo, Category: "电影",
  })
  if current.Category != "电影" {
    t.Fatalf("official Duoduo category was removed: %q", current.Category)
  }
}
