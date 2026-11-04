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

func TestNativeMergeGuipianCoverRecoveryMetadataPreservesYearAndType(t *testing.T) {
	drama := nativeDrama{
		ID: "guipian:123", Source: sourceGuipian, SourceID: "123",
		Title: "夺金战", Category: "影视", Tags: []string{"劇集"},
	}
	fresh := Drama{
		ID: "guipian:123", Source: sourceGuipian, SourceID: "123",
		Title: "奪金戰", CategoryName: "影視", OnlineDate: "2018",
		Cover: "https://images.example.test/duojinzhan.jpg",
	}
	addresses := nativeMergeCoverRecoveryMetadata(&drama, fresh)
	if drama.OnlineDate != "2018" {
		t.Fatalf("detail release year was not propagated: %+v", drama)
	}
	if len(drama.Tags) != 1 || drama.Tags[0] != "劇集" {
		t.Fatalf("catalogue work type was not preserved: %#v", drama.Tags)
	}
	if len(addresses) != 1 || addresses[0] != "https://images.example.test/duojinzhan.jpg" {
		t.Fatalf("detail poster address = %#v", addresses)
	}
	if got := bangumiPosterSubjectTypes(drama); len(got) != 1 || got[0] != 6 {
		t.Fatalf("external poster matching lost the work type: %#v", got)
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
	)
	if err != nil {
		t.Fatal(err)
	}
	if seenReferer != "https://movie.douban.com/" {
		t.Fatalf("external Douban poster used wrong Referer: %q", seenReferer)
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
		ID: "duoduo:1192", DoubanID: "37394760",
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

func TestValidNativeCoverURLRejectsZXKIImageAPI(t *testing.T) {
	address, err := url.Parse("https://api.zxki.cn/api/imgfdl?url=https%3A%2F%2Fexample.com%2Fposter.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if validNativeCoverURL(address) {
		t.Fatal("paid API gateway must not be treated as a public image CDN")
	}
}
