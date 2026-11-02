package core

import (
	"context"
	"fmt"
	"net/http"
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

func TestDuoduoMapsMovieMetadataAndCategories(t *testing.T) {
	drama := duoduoDramaFromRow(map[string]any{
		"vod_id": 78296, "vod_name": "流浪地球（特別版）",
		"type_name": "电影", "vod_year": "2019",
		"vod_pic": "//images.example.test/poster.jpg",
		"vod_content": "&lt;p&gt;科幻電影&lt;/p&gt;",
		"vod_remarks": "正片", "vod_douban_score": "7.5",
		"vod_hits": 12345, "vod_class": []any{"科幻", "冒險"},
		"vod_area": []any{"中國大陸"},
	})
	if drama.ID != "duoduo:78296" || drama.Source != sourceDuoduo ||
		drama.Title != "流浪地球（特別版）" || drama.CategoryName != "电影" ||
		drama.Cover != "https://images.example.test/poster.jpg" ||
		!strings.Contains(drama.Desc, "<p>科幻電影</p>") {
		t.Fatalf("Duoduo metadata mapping is incomplete: %+v", drama)
	}
	if !validNativeCategory(sourceDuoduo, "1") || !validNativeCategory(sourceDuoduo, "4") ||
		validNativeCategory(sourceDuoduo, "999") || validNativeCategory(sourceDuoduo, "../1") {
		t.Fatal("Duoduo category validation accepted invalid type IDs")
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
			if request.URL.Query().Get("type_id") != "1" ||
				request.URL.Query().Get("type_name") != "电影" ||
				request.URL.Query().Get("sort") != "hits" {
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

func TestDuoduoDetailAndPlaybackBuildDirectVariants(t *testing.T) {
	directA := "https://media.example.test/movie/index.m3u8"
	directB := "https://backup.example.test/movie/index.mp4"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/vod/get_detail":
			return sourceFixtureResponse(request, http.StatusOK,
				`{"code":200,"data":[{"vod_id":78296,"vod_name":"流浪地球","type_name":"电影","vod_year":"2019","vod_pic":"https://images.example.test/poster.jpg"}],"vodplayer":[{"from":"direct","decode_status":0,"show":"直連","sort":1}]}`), nil
		case "/api.php/web/internal/search_aggregate":
			body := fmt.Sprintf(`{"code":200,"data":[{"site_key":"direct","site_name":"直連","decode_status":0,"sort":1,"vod_play_url":"第1集$%s"},{"site_key":"backup","site_name":"備援","decode_status":0,"sort":2,"vod_play_url":"第1集$%s"},{"site_key":"next-episode","site_name":"下一集","decode_status":0,"sort":3,"vod_play_url":"第2集$https://wrong.example.test/movie/episode-2.m3u8"}]}`, directA, directB)
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		default:
			t.Fatalf("unexpected Duoduo detail/playback endpoint: %s", request.URL.String())
			return nil, nil
		}
	})
	drama, chapters, err := d.fetchDuoduoDetail(context.Background(), "78296")
	if err != nil || len(chapters) != 1 || drama.ID != "duoduo:78296" ||
		chapters[0].Source != sourceDuoduo || !strings.HasPrefix(chapters[0].VideoURL, "duoduo-play://") {
		t.Fatalf("Duoduo detail failed: drama=%+v chapters=%+v err=%v", drama, chapters, err)
	}
	media, err := d.resolveDuoduoMedia(context.Background(), Task{
		DramaID: drama.ID,
		Chapter: chapters[0],
	})
	if err != nil || media.URL != directA || len(media.Variants) != 1 ||
		media.Variants[0].URL != directB || !media.StandardHTTP {
		t.Fatalf("Duoduo direct playback variants failed: media=%+v err=%v", media, err)
	}
}
