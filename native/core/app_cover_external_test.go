package core

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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