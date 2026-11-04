package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type posterLookupRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip posterLookupRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func posterLookupJSONResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func TestDoubanPosterAddressUsesMobileDetailAndOfficialSubjectID(t *testing.T) {
	const subjectID = "36524559"
	const expectedImage = "https://img9.doubanio.com/view/photo/s_ratio_poster/public/p2920000000.jpg"
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet ||
			request.URL.Host != "m.douban.com" ||
			request.URL.Path != "/rexxar/api/v2/movie/"+subjectID {
			t.Fatalf("unexpected Douban mobile request: %s %s", request.Method, request.URL.String())
		}
		if request.Header.Get("User-Agent") != doubanMobileUserAgent {
			t.Fatalf("mobile User-Agent missing: %q", request.Header.Get("User-Agent"))
		}
		if request.Header.Get("Referer") != "https://m.douban.com/movie/subject/"+subjectID+"/" {
			t.Fatalf("unexpected Douban Referer: %q", request.Header.Get("Referer"))
		}
		return posterLookupJSONResponse(request, `{
			"id":"36524559",
			"pic":{"normal":"https://img9.doubanio.com/view/photo/s_ratio_poster/public/p2920000000.jpg"},
			"cover_url":"https://img9.doubanio.com/view/photo/s_ratio_poster/public/p2919999999.jpg"
		}`), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	address, err := downloader.doubanPosterAddress(context.Background(), nativeDrama{
		ID: "duoduo:1625", DoubanID: subjectID, Title: "鬼灭之刃 剧场版 无限城篇 第一章 猗窝座再来",
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != expectedImage {
		t.Fatalf("Douban poster = %q, want %q", address, expectedImage)
	}
}

func TestDoubanPosterAddressRejectsSubjectIDMismatch(t *testing.T) {
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		return posterLookupJSONResponse(request, `{
			"id":"12345678",
			"pic":{"normal":"https://img9.doubanio.com/view/photo/s_ratio_poster/public/p2920000000.jpg"}
		}`), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	if _, err := downloader.doubanPosterAddress(context.Background(), nativeDrama{DoubanID: "36524559"}); err == nil {
		t.Fatal("mismatched Douban subject ID was accepted")
	}
}

func TestDoubanPosterAddressRejectsNonDoubanImageHosts(t *testing.T) {
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		return posterLookupJSONResponse(request, `{
			"id":"36524559",
			"pic":{"normal":"https://attacker.example.test/view/photo/p2920000000.jpg"}
		}`), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	if _, err := downloader.doubanPosterAddress(context.Background(), nativeDrama{DoubanID: "36524559"}); err == nil {
		t.Fatal("non-Douban poster host was accepted")
	}
}

func TestExternalPosterAddressPrefersOfficialDoubanID(t *testing.T) {
	var bangumiCalls int
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "m.douban.com":
			return posterLookupJSONResponse(request, `{
				"id":"36809864",
				"pic":{"normal":"https://img9.doubanio.com/view/photo/s_ratio_poster/public/nanjing.jpg"}
			}`), nil
		case "api.bgm.tv":
			bangumiCalls++
			t.Fatal("Bangumi search should not override an exact official Douban subject ID")
		}
		return nil, errors.New("unexpected host")
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	address, err := downloader.externalPosterAddress(context.Background(), nativeDrama{
		ID: "duoduo:1206", DoubanID: "36809864", Title: "南京照相馆", Category: "電影",
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != "https://img9.doubanio.com/view/photo/s_ratio_poster/public/nanjing.jpg" || bangumiCalls != 0 {
		t.Fatalf("external poster = %q; Bangumi calls=%d", address, bangumiCalls)
	}
}

func TestBangumiPosterAddressUsesExactTitleAndCategoryType(t *testing.T) {
	const expectedImage = "https://lain.bgm.tv/pic/cover/anime.jpg"
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Host != "api.bgm.tv" || request.URL.Path != "/v0/search/subjects" {
			t.Fatalf("unexpected Bangumi request: %s %s", request.Method, request.URL.String())
		}
		if request.URL.Query().Get("limit") != "20" {
			t.Fatalf("unexpected search limit: %s", request.URL.Query().Get("limit"))
		}
		var payload bangumiPosterSearchRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Keyword != "凡人修仙传" || payload.Sort != "match" ||
			len(payload.Filter.Type) != 1 || payload.Filter.Type[0] != 2 || payload.Filter.NSFW {
			t.Fatalf("unexpected title/type safety filter: %+v", payload)
		}
		return posterLookupJSONResponse(request, `{
			"data":[
				{"id":101,"type":6,"name_cn":"凡人修仙传","date":"2025-01-01","images":{"large":"https://lain.bgm.tv/pic/cover/live.jpg"}},
				{"id":102,"type":2,"name_cn":"凡人修仙传","date":"2024-07-20","images":{"large":"//lain.bgm.tv/pic/cover/anime.jpg"}}
			]
		}`), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	address, err := downloader.bangumiPosterAddress(context.Background(), nativeDrama{
		Title: "凡人修仙传", Category: "動漫", OnlineDate: "2024-07-20",
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != expectedImage {
		t.Fatalf("Bangumi poster = %q, want %q", address, expectedImage)
	}
}

func TestBangumiPosterAddressSupportsVarietyAndSeasonSpecificSeries(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		category  string
		year      string
		candidate string
		date      string
		image     string
	}{
		{
			name: "variety show",
			title: "花兒與少年·同心季", category: "綜藝", year: "2025",
			candidate: "花儿与少年·同心季", date: "2025-08-16",
			image: "https://lain.bgm.tv/pic/cover/variety.jpg",
		},
		{
			name: "season suffix maps to parent series and simplifies",
			title: "這是我的西遊 第二季", category: "綜藝", year: "2026",
			candidate: "这是我的西游", date: "2026-05-29",
			image: "https://lain.bgm.tv/pic/cover/adventure-s2.jpg",
		},
		{
			name: "first season title maps to parent series",
			title: "怪奇物語 第一季", category: "劇集", year: "2016",
			candidate: "怪奇物語", date: "2016-07-15",
			image: "https://lain.bgm.tv/pic/cover/stranger-things-s1.jpg",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
				var payload bangumiPosterSearchRequest
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload.Filter.Type) != 1 || payload.Filter.Type[0] != 6 {
					t.Fatalf("real-world subject type 6 was not requested: %+v", payload.Filter.Type)
				}
				body, err := json.Marshal(map[string]any{
					"data": []any{map[string]any{
						"id": 501, "type": 6, "name_cn": tc.candidate, "date": tc.date,
						"images": map[string]any{"large": tc.image},
					}},
				})
				if err != nil {
					t.Fatal(err)
				}
				return posterLookupJSONResponse(request, string(body)), nil
			})}
			downloader := &Downloader{client: client, cfg: defaultConfig()}
			address, err := downloader.bangumiPosterAddress(context.Background(), nativeDrama{
				Title: tc.title, Category: tc.category, OnlineDate: tc.year,
			})
			if err != nil {
				t.Fatal(err)
			}
			if address != tc.image {
				t.Fatalf("Bangumi poster = %q, want %q", address, tc.image)
			}
		})
	}
}

func TestBangumiPosterAddressRejectsAmbiguousAndNonExactTitles(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "ambiguous exact titles",
			body: `{"data":[
				{"id":201,"type":2,"name_cn":"凡人修仙传","date":"2024-01-01","images":{"large":"https://lain.bgm.tv/201.jpg"}},
				{"id":202,"type":2,"name_cn":"凡人修仙传","date":"2025-01-01","images":{"large":"https://lain.bgm.tv/202.jpg"}}
			]}`,
			want: "多個同名候選",
		},
		{
			name: "season suffix is not an exact match",
			body: `{"data":[
				{"id":203,"type":2,"name_cn":"凡人修仙传 年番","date":"2024-01-01","images":{"large":"https://lain.bgm.tv/203.jpg"}}
			]}`,
			want: "完全相符",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
				return posterLookupJSONResponse(request, test.body), nil
			})}
			downloader := &Downloader{client: client, cfg: defaultConfig()}
			_, err := downloader.bangumiPosterAddress(context.Background(), nativeDrama{
				Title: "凡人修仙传", Category: "動漫",
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected safe rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestBangumiPosterAddressRejectsMismatchedReleaseYear(t *testing.T) {
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		return posterLookupJSONResponse(request, `{"data":[
			{"id":301,"type":6,"name_cn":"測試電影","date":"2024-01-01","images":{"large":"https://lain.bgm.tv/301.jpg"}}
		]}`), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	_, err := downloader.bangumiPosterAddress(context.Background(), nativeDrama{
		Title: "測試電影", Category: "電影", OnlineDate: "2025-08-01",
	})
	if err == nil || !strings.Contains(err.Error(), "年份") {
		t.Fatalf("expected release-year mismatch to be rejected, got %v", err)
	}
}

func TestNativeVerifiedOfficialPosterUsesExactTitleTypeAndYear(t *testing.T) {
	flowerURL := "https://k.sinaimg.cn/n/sinakd20250816s/450/w800h450/20250816/ec1c-4c5fc3ef466113eb1d27a75cc8c37fcf.jpg/w700d1q75cms.jpg?by=cms_fixed_width"
	xiyouURL := "https://live.ykimg.com/image/545880B90349426B93A8696CA9CC578B?x-oss-process=image%2Fresize%2Cm_fixed%2Cw_1278%2Ch_720%2Climit_0"
	cases := []struct {
		name string
		drama nativeDrama
		want string
	}{
		{
			name: "traditional title maps to official simplified-origin MangoTV artwork",
			drama: nativeDrama{Title: "花兒與少年．同心季", Category: "綜藝", OnlineDate: "2025"},
			want: flowerURL,
		},
		{
			name: "simplified duplicate source title matches the same work",
			drama: nativeDrama{Title: "花儿与少年同心季", Category: "综艺", OnlineDate: "2025"},
			want: flowerURL,
		},
		{
			name: "exact second-season Youku title overrides stale upstream year",
			drama: nativeDrama{Title: "这是我的西游 第二季", Category: "综艺", OnlineDate: "2025"},
			want: xiyouURL,
		},
		{
			name: "traditional second-season title",
			drama: nativeDrama{Title: "這是我的西遊 第二季", Category: "綜藝", OnlineDate: "2026"},
			want: xiyouURL,
		},
		{
			name: "Duoduo 87221 Single's Inferno Season 5 uses Netflix key art",
			drama: nativeDrama{ID: "duoduo:87221", Title: "单身即地狱 第五季", Category: "剧集", OnlineDate: "2026"},
			want: "https://resource02.ulifestyle.com.hk/ulcms/content/article/image/w1280/2026/01/20260106180130_adf32c82e8e9ede7a5f96d795e9f224666da43bd.jpg",
		},
		{
			name: "Duoduo 59902 明星大侦探 Season 2 uses MangoTV programme artwork",
			drama: nativeDrama{ID: "duoduo:59902", Title: "明星大侦探 第二季", Category: "综艺", OnlineDate: "2017"},
			want: "https://4img.hitv.com/preview/internettv/sp_images/ott/2018/zongyi/309556/20180328100639870-new.jpg",
		},
		{
			name: "Duoduo 78011 Obsessed uses distributor poster",
			drama: nativeDrama{ID: "duoduo:78011", Title: "人间中毒", Category: "電影", OnlineDate: "2014"},
			want: "https://img.its-new.co.kr/uploaded/poster/264/poster_368.jpg",
		},
		{
			name: "Duoduo 1192 Shotgun Marriage uses Fuji Creative official artwork",
			drama: nativeDrama{ID: "duoduo:1192", Title: "奉子成婚", Category: "劇集", OnlineDate: "2001"},
			want: "https://fujicreative.co.jp/forBuyers/wp-content/uploads/2022/06/2_0227.jpg",
		},
		{
			name: "Duoduo 66452 逆愛2後會無期 uses exact-record archive artwork as fallback",
			drama: nativeDrama{ID: "duoduo:66452", Title: "逆爱2：后会无期", Category: "爱情", OnlineDate: "2014"},
			want: "https://www.jendow.com.tw/img/5/105/nBnauM3X1gTO1AzM4MzMzIzM3QTM5YzNyITOzQTNwAzMwIzLzMzL3YzLt92YucmbvRWdo5Cd0FmL0E2LvoDc0RHa.jpg",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			address, ok := nativeVerifiedOfficialPosterAddress(tc.drama)
			if !ok || address != tc.want {
				t.Fatalf("verified poster = %q, ok=%t; want %q", address, ok, tc.want)
			}
			// Exact, trusted artwork should be available before a third-party search.
			got, err := (&Downloader{}).externalPosterAddress(context.Background(), tc.drama)
			if err != nil || got != tc.want {
				t.Fatalf("external poster = %q, err=%v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestNativePreferredOfficialPosterIsRestrictedToExactUserReportedRecords(t *testing.T) {
	tests := []struct {
		name  string
		drama nativeDrama
		want  string
	}{
		{
			name: "Unicorn Chapter Two",
			drama: nativeDrama{ID: "retired-source:77681", Title: "独角兽学院：秘密揭开第二章"},
			want: "https://www.whats-on-netflix.com/wp-content/uploads/2026/09/Unicorn-Academy-Secrets-Revealed-n-S2-E1-00-10-43-15-1536x864.jpg",
		},
		{
			name: "Single's Inferno Season 5",
			drama: nativeDrama{ID: "duoduo:87221", Title: "單身即地獄 第五季"},
			want: "https://resource02.ulifestyle.com.hk/ulcms/content/article/image/w1280/2026/01/20260106180130_adf32c82e8e9ede7a5f96d795e9f224666da43bd.jpg",
		},
		{
			name: "明星大侦探 Season 2",
			drama: nativeDrama{ID: "duoduo:59902", Title: "明星大偵探 第二季"},
			want: "https://4img.hitv.com/preview/internettv/sp_images/ott/2018/zongyi/309556/20180328100639870-new.jpg",
		},
		{
			name: "Obsessed",
			drama: nativeDrama{ID: "duoduo:78011", Title: "人間中毒"},
			want: "https://img.its-new.co.kr/uploaded/poster/264/poster_368.jpg",
		},
		{
			name: "Shotgun Marriage 2001",
			drama: nativeDrama{ID: "duoduo:1192", Title: "奉子成婚"},
			want: "https://fujicreative.co.jp/forBuyers/wp-content/uploads/2022/06/2_0227.jpg",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			address, ok := nativePreferredOfficialPosterAddress(tc.drama)
			if !ok || address != tc.want {
				t.Fatalf("preferred official poster = %q, ok=%t; want %q", address, ok, tc.want)
			}
		})
	}
	for _, drama := range []nativeDrama{
		{ID: "duoduo:1193", Title: "奉子成婚"},
		{ID: "duoduo:1192", Title: "奉子不成婚"},
	} {
		if address, ok := nativePreferredOfficialPosterAddress(drama); ok {
			t.Errorf("wrong record received preferred artwork %q: %+v", address, drama)
		}
	}
}

func TestNativeVerifiedOfficialPosterRejectsWrongTitleTypeAndYear(t *testing.T) {
	cases := []nativeDrama{
		{Title: "花兒與少年．同心季", Category: "綜藝", OnlineDate: "2024"},
		{Title: "花兒與少年．同心季 特別篇", Category: "綜藝", OnlineDate: "2025"},
		{Title: "花兒與少年．同心季", Category: "電影", OnlineDate: "2025"},
		{Title: "這是我的西遊", Category: "綜藝", OnlineDate: "2026"},
		{Title: "這是我的西遊 第二季", Category: "電影", OnlineDate: "2026"},
		{ID: "duoduo:77333", Title: "歹路無可退", Category: "電影", OnlineDate: "2026"},
		{ID: "retired-source:77682", Title: "獨角獸學院：秘密揭開第二章", Category: "動漫", OnlineDate: "2026"},
		{ID: "duoduo:77681", Title: "獨角獸學院：秘密揭開第二章", Category: "動漫", OnlineDate: "2026"},
		{ID: "retired-source:77681", Title: "獨角獸學院：秘密揭開第一章", Category: "動漫", OnlineDate: "2026"},
		{ID: "duoduo:1193", Title: "奉子成婚", Category: "劇集", OnlineDate: "2001"},
		{ID: "duoduo:87222", Title: "單身即地獄 第五季", Category: "劇集", OnlineDate: "2026"},
		{ID: "duoduo:87221", Title: "單身即地獄 第四季", Category: "劇集", OnlineDate: "2025"},
		{ID: "duoduo:59903", Title: "明星大偵探 第二季", Category: "綜藝", OnlineDate: "2017"},
		{ID: "duoduo:59902", Title: "明星大偵探 第一季", Category: "綜藝", OnlineDate: "2016"},
		{ID: "duoduo:78012", Title: "人間中毒", Category: "電影", OnlineDate: "2014"},
		{ID: "duoduo:78011", Title: "其他作品", Category: "電影", OnlineDate: "2014"},
		{ID: "duoduo:1192", Title: "奉子不成婚", Category: "電影", OnlineDate: "2016"},
		{ID: "duoduo:66453", Title: "逆愛2後會無期", Category: "愛情", OnlineDate: "2014"},
		{ID: "duoduo:66452", Title: "其他作品", Category: "愛情", OnlineDate: "2014"},
	}
	for _, drama := range cases {
		if address, ok := nativeVerifiedOfficialPosterAddress(drama); ok {
			t.Errorf("unverified title/type/year received poster %q for %+v", address, drama)
		}
	}
}

func TestNativeNormalizeRetainsAlternativePosterFields(t *testing.T) {
	drama := nativeNormalize(Drama{
		ID: "duoduo:901",
		Cover: "https://images.example.test/original.jpg",
		CoverURL: "https://images.example.test/cover-url.jpg",
		ImageURL: map[string]any{
			"large": "https://images.example.test/image-large.jpg",
			"small": "https://images.example.test/image-small.jpg",
		},
		Poster: []any{"https://images.example.test/poster.jpg"},
	})
	if drama.Cover != "https://images.example.test/original.jpg" {
		t.Fatalf("original poster priority changed: %q", drama.Cover)
	}
	want := []string{
		"https://images.example.test/cover-url.jpg",
		"https://images.example.test/image-large.jpg",
		"https://images.example.test/image-small.jpg",
		"https://images.example.test/poster.jpg",
	}
	if len(drama.CoverCandidates) != len(want) {
		t.Fatalf("poster candidates = %#v, want %#v", drama.CoverCandidates, want)
	}
	for i := range want {
		if drama.CoverCandidates[i] != want[i] {
			t.Fatalf("poster candidate %d = %q, want %q", i, drama.CoverCandidates[i], want[i])
		}
	}
}

func TestNativeCoverLoadTriesCatalogueVariantBeforeMetadataRepair(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	var requests []string
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.Host)
		if request.URL.Host == "variant.example" {
			return &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(bytes.NewReader(image)), ContentLength: int64(len(image)), Request: request,
			}, nil
		}
		// Duoduo poster loading also probes its image proxy. Return an ordinary
		// failed response for unrelated hosts; do not call t.Fatalf from the
		// background cache.fetch goroutine, which would leave call.done open.
		return &http.Response{
			StatusCode: http.StatusNotFound, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("not found")), Request: request,
		}, nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig(), limiter: newRequestLimiter(2, 0)}
	cache := newNativeCoverCache(t.TempDir(), downloader)
	path, err := cache.load(context.Background(), nativeDrama{
		ID: "duoduo:901", Source: sourceDuoduo, Title: "測試作品",
		Cover: "https://original.example/poster.jpg",
		CoverCandidates: []string{"https://variant.example/poster.jpg"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(body, image) {
		t.Fatalf("catalogue variant was not saved as the cover: %v", err)
	}
	originalIndex, variantIndex := -1, -1
	for i, host := range requests {
		if host == "original.example" && originalIndex < 0 {
			originalIndex = i
		}
		if host == "variant.example" && variantIndex < 0 {
			variantIndex = i
		}
	}
	if originalIndex < 0 || variantIndex <= originalIndex {
		t.Fatalf("original poster must be attempted before its catalogue variant: %#v", requests)
	}
}

func TestMergeNativeDramaKeepsOriginalAndFallbackCoversSeparate(t *testing.T) {
	previous := nativeDrama{
		ID: "duoduo:554", Cover: "https://original.example/old.jpg",
		CoverCandidates: []string{"https://original.example/old-alt.jpg"},
		FallbackCover: "https://lain.bgm.tv/fallback.jpg",
	}
	fresh := nativeDrama{
		ID: "duoduo:554", Cover: "https://original.example/new.jpg",
		CoverCandidates: []string{"https://original.example/new-alt.jpg"},
	}
	merged := mergeNativeDrama(previous, fresh)
	if merged.Cover != fresh.Cover {
		t.Fatalf("original source cover was not refreshed: got %q", merged.Cover)
	}
	if merged.FallbackCover != previous.FallbackCover {
		t.Fatalf("verified fallback cover was not retained separately: got %q", merged.FallbackCover)
	}
	if len(merged.CoverCandidates) != 3 ||
		merged.CoverCandidates[0] != "https://original.example/new-alt.jpg" ||
		merged.CoverCandidates[1] != "https://original.example/old.jpg" ||
		merged.CoverCandidates[2] != "https://original.example/old-alt.jpg" {
		t.Fatalf("catalogue poster variants were not preserved in priority order: %#v", merged.CoverCandidates)
	}
}

func TestNativeCoverLoadPrefersHealthyOriginalBeforeFallback(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	var originalCalls, fallbackCalls int
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "original.example":
			originalCalls++
		case "fallback.example":
			fallbackCalls++
		default:
			// The provider-native metadata lookup is expected before an
			// already-saved external fallback; simulate an unavailable detail API.
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("unavailable")), Request: request,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(bytes.NewReader(image)), ContentLength: int64(len(image)), Request: request,
		}, nil
	})}
	directory := t.TempDir()
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	cache := newNativeCoverCache(directory, downloader)
	if err := os.MkdirAll(cache.directory, 0700); err != nil {
		t.Fatal(err)
	}
	path, err := cache.load(context.Background(), nativeDrama{
		ID: "hongguo:123", Source: sourceHongguo, Title: "測試作品",
		Cover: "https://original.example/poster.jpg",
		FallbackCover: "https://fallback.example/poster.jpg",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || originalCalls == 0 {
		t.Fatalf("original poster was not requested: path=%q originalCalls=%d", path, originalCalls)
	}
	if fallbackCalls != 0 {
		t.Fatalf("fallback was requested before original succeeded: calls=%d", fallbackCalls)
	}
}

func TestNativeCoverLoadPrefersProviderPosterOverExternalPoster(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	var requests []string
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.Host)
		switch request.URL.Host {
		case "original.example":
			return &http.Response{
				StatusCode: http.StatusNotFound, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("not found")), Request: request,
			}, nil
		case "provider.example":
			return &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(bytes.NewReader(image)), ContentLength: int64(len(image)), Request: request,
			}, nil
		case "external.example":
			return &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(bytes.NewReader(image)), ContentLength: int64(len(image)), Request: request,
			}, nil
		default:
			t.Fatalf("unexpected poster request: %s", request.URL)
			return nil, nil
		}
	})}
	directory := t.TempDir()
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	cache := newNativeCoverCache(directory, downloader)
	if err := os.MkdirAll(cache.directory, 0700); err != nil {
		t.Fatal(err)
	}
	path, err := cache.load(context.Background(), nativeDrama{
		ID: "hongguo:125", Source: sourceHongguo, Title: "測試作品",
		Cover: "https://original.example/poster.jpg",
		ProviderFallbackCover: "https://provider.example/poster.jpg",
		FallbackCover: "https://external.example/poster.jpg",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || len(requests) < 2 || requests[0] != "original.example" || requests[1] != "provider.example" {
		t.Fatalf("provider poster must be tried before external poster: path=%q requests=%v", path, requests)
	}
	for _, host := range requests {
		if host == "external.example" {
			t.Fatalf("external poster was requested before provider poster succeeded: %v", requests)
		}
	}
}

func TestNativeCoverLoadUsesFallbackOnlyAfterOriginalFails(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	var requests []string
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.Host)
		if request.URL.Host == "original.example" {
			return &http.Response{
				StatusCode: http.StatusNotFound, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("not found")), Request: request,
			}, nil
		}
		if request.URL.Host == "fallback.example" {
			return &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(bytes.NewReader(image)), ContentLength: int64(len(image)), Request: request,
			}, nil
		}
		t.Fatalf("unexpected provider metadata request in fallback fixture: %s", request.URL)
		return nil, nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	cache := newNativeCoverCache(t.TempDir(), downloader)
	if err := os.MkdirAll(cache.directory, 0700); err != nil {
		t.Fatal(err)
	}
	// Seed the completed repair cache with an explicit provider miss. This
	// isolates the test to persisted fallback ordering and avoids invoking
	// a real provider API or device registration.
	done := make(chan struct{})
	close(done)
	cache.repairs["hongguo:124"] = &nativeCoverRepair{
		providerAddress: "",
		address:         "https://fallback.example/poster.jpg",
		retryAt:         time.Now().Add(time.Minute),
		done:            done,
	}
	path, err := cache.load(context.Background(), nativeDrama{
		ID: "hongguo:124", Source: sourceHongguo, Title: "測試作品",
		Cover: "https://original.example/poster.jpg",
		FallbackCover: "https://fallback.example/poster.jpg",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || len(requests) < 2 || requests[0] != "original.example" || requests[len(requests)-1] != "fallback.example" {
		t.Fatalf("fallback order was incorrect: path=%q requests=%v", path, requests)
	}
}
func TestNativeMergeCoverRepairMetadataRestoresDoubanID(t *testing.T) {
	incoming := nativeDrama{
		ID: "duoduo:54635", Source: sourceDuoduo, SourceID: "54635",
		Title: "玩物", Cover: "https://dbzy5.com/original.jpg",
	}
	cached := nativeDrama{
		ID: "duoduo:54635", Source: sourceDuoduo, SourceID: "54635",
		DoubanID: "20502443", Title: "玩物",
		Category: "电影", OnlineDate: "2025",
		Tags: []string{"剧情", "悬疑"},
	}
	merged := nativeMergeCoverRepairMetadata(incoming, cached)
	if merged.DoubanID != "20502443" {
		t.Fatalf("cached Douban ID was lost during poster recovery: %q", merged.DoubanID)
	}
	if merged.Category != "电影" || merged.OnlineDate != "2025" ||
		len(merged.Tags) != 2 || merged.Tags[0] != "剧情" {
		t.Fatalf("cached matching metadata was not restored: %+v", merged)
	}
	if merged.Cover != incoming.Cover {
		t.Fatalf("cached repair metadata overwrote the original poster: %q", merged.Cover)
	}
}

func TestNativeMergeCoverRepairMetadataPrefersFreshIdentifiers(t *testing.T) {
	incoming := nativeDrama{
		ID: "duoduo:54635", Source: sourceDuoduo, SourceID: "54635",
		DoubanID: "99999999", Title: "最新片名", Category: "電影",
		OnlineDate: "2026",
	}
	cached := nativeDrama{
		ID: "duoduo:54635", DoubanID: "20502443",
		Title: "舊片名", Category: "剧集", OnlineDate: "2025",
	}
	merged := nativeMergeCoverRepairMetadata(incoming, cached)
	if merged.DoubanID != "99999999" || merged.Title != "最新片名" ||
		merged.Category != "電影" || merged.OnlineDate != "2026" {
		t.Fatalf("fresh poster metadata should keep priority: %+v", merged)
	}
}

func TestDuoduoExternalDoubanPosterKeepsDoubanReferer(t *testing.T) {
	imageBytes := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}
	var seenReferer string
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "img9.doubanio.com" {
			t.Fatalf("unexpected external poster host: %s", request.URL.Host)
		}
		seenReferer = request.Header.Get("Referer")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"image/jpeg"}},
			Body: io.NopCloser(bytes.NewReader(imageBytes)),
			ContentLength: int64(len(imageBytes)),
			Request: request,
		}, nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	cache := newNativeCoverCache(t.TempDir(), downloader)
	if err := os.MkdirAll(cache.directory, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := cache.loadAlternativeAddress(
		context.Background(),
		nativeDrama{ID: "duoduo:54635", Source: sourceDuoduo, SourceID: "54635"},
		"https://img9.doubanio.com/view/photo/s_ratio_poster/public/p2170193684.jpg",
		"https://movie.douban.com/",
		"external",
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if seenReferer != "https://movie.douban.com/" {
		t.Fatalf("external Douban poster used wrong Referer: %q", seenReferer)
	}
}


func TestDuoduoSatanTheDarkPosterLoadsThroughNativeCache(t *testing.T) {
	const poster = "https://img9.doubanio.com/view/photo/s_ratio_poster/public/p2932687546.jpg"
	imageBytes := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}
	var seenReferer string
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != poster {
			t.Fatalf("unexpected poster request: %s", request.URL)
		}
		seenReferer = request.Header.Get("Referer")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"image/jpeg"}},
			Body: io.NopCloser(bytes.NewReader(imageBytes)),
			ContentLength: int64(len(imageBytes)),
			Request: request,
		}, nil
	})}
	downloader := &Downloader{
		client: client,
		cfg: defaultConfig(),
		providerHosts: map[string]string{sourceDuoduo: "https://323433ssdfd.top"},
	}
	cache := newNativeCoverCache(t.TempDir(), downloader)
	if err := os.MkdirAll(cache.directory, 0700); err != nil {
		t.Fatal(err)
	}
	path, err := cache.load(context.Background(), nativeDrama{
		ID: "duoduo:137937", Source: sourceDuoduo, SourceID: "137937",
		Title: "撒旦：黑暗", Cover: poster,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if seenReferer != "https://323433ssdfd.top/play/137937" {
		t.Fatalf("poster Referer = %q, want the work's official play page", seenReferer)
	}
	cached, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) == 0 || !nativeIsCoverImage(cached) {
		t.Fatalf("native cover cache did not save a valid image: %d bytes", len(cached))
	}
}

func TestExternalPosterAddressUsesBangumiWhenDoubanIDIsMissing(t *testing.T) {
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "api.bgm.tv" {
			t.Fatalf("expected Bangumi fallback, got %s", request.URL.Host)
		}
		return posterLookupJSONResponse(request, `{
			"data":[{
				"id":12345,
				"type":6,
				"name":"Example Movie",
				"name_cn":"Example Movie",
				"date":"2025-01-01",
				"images":{"large":"https://lain.bgm.tv/pic/cover/l/12345.jpg"}
			}]
		}`), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	address, err := downloader.externalPosterAddress(context.Background(), nativeDrama{
		ID: "duoduo:999", Title: "Example Movie", Category: "電影",
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != "https://lain.bgm.tv/pic/cover/l/12345.jpg" {
		t.Fatalf("unexpected Bangumi fallback poster: %q", address)
	}
}

func TestExternalPosterAddressDoesNotReplaceFailedExactDoubanIDWithTitleSearch(t *testing.T) {
	var bangumiCalls int
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "m.douban.com" {
			return posterLookupJSONResponse(request, `{"id":"12345678","pic":{"normal":"https://img9.doubanio.com/view/photo/s_ratio_poster/public/p1.jpg"}}`), nil
		}
		if request.URL.Host == "api.bgm.tv" {
			bangumiCalls++
		}
		return posterLookupJSONResponse(request, `{"data":[]}`), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	if _, err := downloader.externalPosterAddress(context.Background(), nativeDrama{
		ID: "duoduo:999", DoubanID: "87654321", Title: "Example Movie", Category: "電影",
	}); err == nil {
		t.Fatal("mismatched Douban ID unexpectedly succeeded")
	}
	if bangumiCalls != 0 {
		t.Fatalf("title-only fallback ran despite an exact but failed Douban ID: %d calls", bangumiCalls)
	}
}


func TestExternalPosterAddressTreatsZeroDoubanIDAsMissing(t *testing.T) {
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "api.bgm.tv" {
			t.Fatalf("placeholder Douban ID should use Bangumi fallback, got %s", request.URL.Host)
		}
		return posterLookupJSONResponse(request, `{
			"data":[{
				"id":12345,
				"type":6,
				"name":"Example Movie",
				"name_cn":"Example Movie",
				"date":"2025-01-01",
				"images":{"large":"https://lain.bgm.tv/pic/cover/l/12345.jpg"}
			}]
		}`), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	address, err := downloader.externalPosterAddress(context.Background(), nativeDrama{
		ID: "duoduo:999", DoubanID: "0", Title: "Example Movie", Category: "電影",
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != "https://lain.bgm.tv/pic/cover/l/12345.jpg" {
		t.Fatalf("unexpected Bangumi fallback poster: %q", address)
	}
}

func TestValidDoubanSubjectIDRejectsPlaceholderValues(t *testing.T) {
	for _, value := range []string{"", "0", "000000", "abc12345", "12345"} {
		if validDoubanSubjectID(value) {
			t.Errorf("placeholder or malformed Douban ID %q was accepted", value)
		}
	}
	if !validDoubanSubjectID("20502443") {
		t.Fatal("valid Douban subject ID was rejected")
	}
}


func TestStartNativeCoverExternalPosterLookupRunsInParallel(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "m.douban.com" {
			t.Fatalf("unexpected external poster host: %s", request.URL.Host)
		}
		close(started)
		<-release
		return posterLookupJSONResponse(request, `{
			"id":"20502443",
			"pic":{"normal":"https://img9.doubanio.com/view/photo/s_ratio_poster/public/p2170193684.jpg"}
		}`), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	lookup := startNativeCoverExternalPosterLookup(ctx, downloader, nativeDrama{
		ID: "duoduo:54635", Source: sourceDuoduo, DoubanID: "20502443",
		Title: "玩物",
	})
	if lookup == nil {
		t.Fatal("valid exact Douban ID should start a parallel lookup")
	}
	select {
	case <-started:
		// The request reached the transport without waiting for provider metadata.
	case <-ctx.Done():
		t.Fatal("external poster lookup did not start promptly")
	}
	close(release)

	select {
	case result := <-lookup:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.address != "https://img9.doubanio.com/view/photo/s_ratio_poster/public/p2170193684.jpg" {
			t.Fatalf("unexpected poster URL: %q", result.address)
		}
	case <-ctx.Done():
		t.Fatal("parallel external poster lookup did not complete")
	}
}

func TestStartNativeCoverExternalPosterLookupSkipsPlaceholderDoubanID(t *testing.T) {
	lookup := startNativeCoverExternalPosterLookup(
		context.Background(),
		&Downloader{client: &http.Client{}},
		nativeDrama{ID: "duoduo:157219", DoubanID: "0"},
	)
	if lookup != nil {
		t.Fatal("placeholder Douban ID should not start an external lookup")
	}
}



func TestExternalPosterAddressFallsBackToBangumiWhenDoubanIsDenied(t *testing.T) {
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "m.douban.com":
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("unauthorized")),
				Request: request,
			}, nil
		case "api.bgm.tv":
			var payload bangumiPosterSearchRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Keyword != "奉子成婚" || len(payload.Filter.Type) != 1 || payload.Filter.Type[0] != 6 {
				t.Fatalf("unexpected guarded Bangumi fallback query: %+v", payload)
			}
			return posterLookupJSONResponse(request, `{
				"data":[
					{"id":37394760,"type":6,"name_cn":"奉子成婚","date":"2025-01-01",
					 "images":{"large":"https://lain.bgm.tv/pic/cover/fengzi.jpg"}}
				]
			}`), nil
		default:
			return nil, errors.New("unexpected poster metadata host: " + request.URL.Host)
		}
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	address, err := downloader.externalPosterAddress(context.Background(), nativeDrama{
		ID: "duoduo:1193", DoubanID: "37394760",
		Title: "奉子成婚", Category: "短劇",
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != "https://lain.bgm.tv/pic/cover/fengzi.jpg" {
		t.Fatalf("Bangumi fallback poster = %q", address)
	}
}

func TestNativeCoverExternalDownloadFailureFallsBackToBangumi(t *testing.T) {
	image := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0, 1}
	var bangumiCalls int
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "img9.doubanio.com":
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("image unauthorized")),
				Request: request,
			}, nil
		case "api.bgm.tv":
			bangumiCalls++
			return posterLookupJSONResponse(request, `{
				"data":[
					{"id":37394760,"type":6,"name_cn":"奉子成婚","date":"2025-01-01",
					 "images":{"large":"https://lain.bgm.tv/pic/cover/fengzi.jpg"}}
				]
			}`), nil
		case "lain.bgm.tv":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{"image/jpeg"}},
				Body: io.NopCloser(bytes.NewReader(image)),
				ContentLength: int64(len(image)),
				Request: request,
			}, nil
		default:
			return nil, errors.New("unexpected poster host: " + request.URL.Host)
		}
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	cache := newNativeCoverCache(t.TempDir(), downloader)
	drama := nativeDrama{
		ID: "duoduo:1192", Source: sourceDuoduo, DoubanID: "37394760",
		Title: "奉子成婚", Category: "短劇",
	}
	path, address, err := cache.loadExternalPoster(
		context.Background(),
		drama,
		"https://img9.doubanio.com/view/photo/s_ratio_poster/public/p1234567890.jpg",
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if address != "https://lain.bgm.tv/pic/cover/fengzi.jpg" {
		t.Fatalf("fallback address = %q", address)
	}
	if bangumiCalls != 1 {
		t.Fatalf("Bangumi fallback calls = %d, want 1", bangumiCalls)
	}
	body, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(body, image) {
		t.Fatalf("Bangumi fallback image was not cached: %v", err)
	}
}

func TestNativeCoverExternalDownloadFailureRetriesVerifiedOfficialProgrammePoster(t *testing.T) {
	image := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0, 1}
	cases := []struct {
		name      string
		drama     nativeDrama
		wantURL   string
		wantHost  string
		wantRef   string
	}{
		{
			name: "flower programme retries Sina official artwork",
			drama: nativeDrama{
				ID: "duoduo:153909", Source: sourceDuoduo,
				Title: "花兒與少年．同心季", Category: "綜藝", OnlineDate: "2025",
			},
			wantURL: "https://k.sinaimg.cn/n/sinakd20250816s/450/w800h450/20250816/ec1c-4c5fc3ef466113eb1d27a75cc8c37fcf.jpg/w700d1q75cms.jpg?by=cms_fixed_width",
			wantHost: "k.sinaimg.cn", wantRef: "https://news.sina.com.cn/",
		},
		{
			name: "Xiyou second season retries Youku official artwork",
			drama: nativeDrama{
				ID: "duoduo:77041", Source: sourceDuoduo,
				Title: "這是我的西遊 第二季", Category: "綜藝", OnlineDate: "2025",
			},
			wantURL: "https://live.ykimg.com/image/545880B90349426B93A8696CA9CC578B?x-oss-process=image%2Fresize%2Cm_fixed%2Cw_1278%2Ch_720%2Climit_0",
			wantHost: "live.ykimg.com", wantRef: "https://323433ssdfd.top/",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var officialCalls int
			client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
				switch request.URL.Host {
				case "old-fallback.example.test":
					return &http.Response{
						StatusCode: http.StatusNotFound, Header: make(http.Header),
						Body: io.NopCloser(strings.NewReader("missing")), Request: request,
					}, nil
				case tc.wantHost:
					officialCalls++
					if request.Header.Get("Referer") != tc.wantRef {
						t.Errorf("official image Referer = %q, want %q", request.Header.Get("Referer"), tc.wantRef)
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header: http.Header{"Content-Type": []string{"image/jpeg"}},
						Body: io.NopCloser(bytes.NewReader(image)),
						ContentLength: int64(len(image)), Request: request,
					}, nil
				default:
					return nil, errors.New("unexpected poster request: " + request.URL.String())
				}
			})}
			downloader := &Downloader{
				client: client, cfg: defaultConfig(),
				providerHosts: map[string]string{sourceDuoduo: "https://323433ssdfd.top"},
			}
			cache := newNativeCoverCache(t.TempDir(), downloader)
			path, address, err := cache.loadExternalPoster(
				context.Background(), tc.drama,
				"https://old-fallback.example.test/poster.jpg",
				false,
			)
			if err != nil {
				t.Fatal(err)
			}
			if address != tc.wantURL || officialCalls != 1 {
				t.Fatalf("official fallback address=%q calls=%d, want %q and one download", address, officialCalls, tc.wantURL)
			}
			cached, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(cached, image) {
				t.Fatalf("official programme poster was not cached: %v", readErr)
			}
		})
	}
}

func TestValidNativeCoverURLRejectsZXKIImageAPI(t *testing.T) {
	address, err := url.Parse("https://api.zxki.cn/api/imgfdl?url=https%3A%2F%2Fexample.com%2Fposter.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if validNativeCoverURL(address) {
		t.Fatal("paid API gateway must not be treated as a public image CDN")
	}
}

func TestDoubanSuggestedPosterAddressUsesExactTitleTypeAndYear(t *testing.T) {
	const expected = "https://img9.doubanio.com/view/photo/s_ratio_poster/public/p1234567890.jpg"
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Host != "movie.douban.com" || request.URL.Path != "/j/subject_suggest" || request.URL.Query().Get("q") == "" {
			t.Fatalf("unexpected Douban suggestion request: %s", request.URL.String())
		}
		body := `[{"id":"12345678","title":"怪奇物語","type":"tv","year":"2016","img":"` + expected + `"},{"id":"12345679","title":"怪奇物語","type":"movie","year":"2016","img":"https://img9.doubanio.com/view/photo/s_ratio_poster/public/wrong.jpg"},{"id":"12345680","title":"怪奇物語","type":"tv","year":"2019","img":"https://img9.doubanio.com/view/photo/s_ratio_poster/public/wrong-year.jpg"}]`
		return posterLookupJSONResponse(request, body), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	address, err := downloader.doubanSuggestedPosterAddress(context.Background(), nativeDrama{Title: "怪奇物語（2016）", Category: "劇集", OnlineDate: "2016"})
	if err != nil { t.Fatal(err) }
	if address != expected { t.Fatalf("Douban suggested poster = %q, want %q", address, expected) }
}

func TestDoubanSuggestedPosterAddressRejectsAmbiguousExactMatches(t *testing.T) {
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		return posterLookupJSONResponse(request, `[{"id":"12345678","title":"同名作品","type":"movie","year":"2020","img":"https://img9.doubanio.com/view/photo/s_ratio_poster/public/a.jpg"},{"id":"12345679","title":"同名作品","type":"movie","year":"2020","img":"https://img9.doubanio.com/view/photo/s_ratio_poster/public/b.jpg"}]`), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	_, err := downloader.doubanSuggestedPosterAddress(context.Background(), nativeDrama{Title: "同名作品", Category: "電影", OnlineDate: "2020"})
	if err == nil || !strings.Contains(err.Error(), "多個同名候選") { t.Fatalf("ambiguous candidates should be rejected, got %v", err) }
}

func TestAniListPosterAddressUsesExactAnimeTitleAndYear(t *testing.T) {
	const expected = "https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/bx12345-test.jpg"
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Host != "graphql.anilist.co" { t.Fatalf("unexpected AniList request: %s", request.URL.String()) }
		var payload struct { Query string `json:"query"`; Variables map[string]any `json:"variables"` }
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil { t.Fatal(err) }
		if !strings.Contains(payload.Query, "type: ANIME") || payload.Variables["search"] == nil { t.Fatalf("AniList query is missing anime filter: %+v", payload) }
		body := `{"data":{"Page":{"media":[{"id":12345,"title":{"native":"葬送のフリーレン","romaji":"Sousou no Frieren","english":"Frieren: Beyond Journey's End"},"synonyms":["葬送的芙莉蓮","芙莉蓮"],"startDate":{"year":2023},"coverImage":{"extraLarge":"` + expected + `"}}]}}}`
		return posterLookupJSONResponse(request, body), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	address, err := downloader.aniListPosterAddress(context.Background(), nativeDrama{Title: "葬送的芙莉蓮", Category: "動漫", OnlineDate: "2023"})
	if err != nil { t.Fatal(err) }
	if address != expected { t.Fatalf("AniList poster = %q, want %q", address, expected) }
}

func TestTVMazePosterAddressUsesExactTVTitleAndYear(t *testing.T) {
	const expected = "https://static.tvmaze.com/uploads/images/original_untouched/1/3456.jpg"
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Host != "api.tvmaze.com" || request.URL.Path != "/search/shows" || request.URL.Query().Get("q") == "" { t.Fatalf("unexpected TVmaze request: %s", request.URL.String()) }
		body := `[{"score":1,"show":{"id":139,"name":"怪奇物語","premiered":"2016-07-15","image":{"original":"` + expected + `"}}},{"score":0.9,"show":{"id":140,"name":"怪奇物語","premiered":"2019-01-01","image":{"original":"https://static.tvmaze.com/uploads/images/original_untouched/2/wrong-year.jpg"}}}]`
		return posterLookupJSONResponse(request, body), nil
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	address, err := downloader.tvMazePosterAddress(context.Background(), nativeDrama{Title: "怪奇物語", Category: "劇集", OnlineDate: "2016"})
	if err != nil { t.Fatal(err) }
	if address != expected { t.Fatalf("TVmaze poster = %q, want %q", address, expected) }
}

func TestExternalPosterAddressUsesDoubanSuggestionAfterBangumiMiss(t *testing.T) {
	const expected = "https://img9.doubanio.com/view/photo/s_ratio_poster/public/suggestion.jpg"
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "api.bgm.tv": return posterLookupJSONResponse(request, `{"data":[]}`), nil
		case "movie.douban.com": return posterLookupJSONResponse(request, `[{"id":"12345678","title":"補圖測試電影","type":"movie","year":"2025","img":"` + expected + `"}]`), nil
		default: t.Fatalf("unexpected poster metadata host: %s", request.URL.Host); return nil, errors.New("unexpected host")
		}
	})}
	downloader := &Downloader{client: client, cfg: defaultConfig()}
	address, err := downloader.externalPosterAddress(context.Background(), nativeDrama{ID: "duoduo:777", Source: sourceDuoduo, Title: "補圖測試電影", Category: "電影", OnlineDate: "2025"})
	if err != nil { t.Fatal(err) }
	if address != expected { t.Fatalf("external fallback poster = %q, want %q", address, expected) }
}

func TestLoadAlternativeAddressForcedRetryReDownloadsFreshCache(t *testing.T) {
	firstImage := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 1}
	secondImage := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 2}
	var calls int
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		image := firstImage
		if calls > 1 {
			image = secondImage
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"image/jpeg"}},
			Body: io.NopCloser(bytes.NewReader(image)),
			ContentLength: int64(len(image)),
			Request: request,
		}, nil
	})}
	cache := newNativeCoverCache(t.TempDir(), &Downloader{client: client, cfg: defaultConfig()})
	drama := nativeDrama{ID: "duoduo:retry", Source: sourceDuoduo, SourceID: "retry"}
	address := "https://img9.doubanio.com/view/photo/s_ratio_poster/public/retry.jpg"
	referer := "https://movie.douban.com/"

	firstPath, err := cache.loadAlternativeAddress(
		context.Background(), drama, address, referer, "external", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstCached, err := os.ReadFile(firstPath)
	if err != nil || !bytes.Equal(firstCached, firstImage) {
		t.Fatalf("initial external poster cache mismatch: %v", err)
	}

	secondPath, err := cache.loadAlternativeAddress(
		context.Background(), drama, address, referer, "external", true,
	)
	if err != nil {
		t.Fatal(err)
	}
	secondCached, err := os.ReadFile(secondPath)
	if err != nil || !bytes.Equal(secondCached, secondImage) {
		t.Fatalf("forced retry reused stale external poster cache: %v", err)
	}
	if calls != 2 {
		t.Fatalf("forced retry made %d downloads, want 2", calls)
	}
}
