package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

const escapedBaiduGimgPoster = "https://gimg0.baidu.com/gimg/app=2001&amp;n=0&amp;g=0n&amp;fmt=webp&amp;src=img.baipiaozhe.com/assets/images/07/07ee1e07dfe068858170dfc5ef4df80af77732a31bf6a1567f588624d02562e7.webp"

const decodedBaiduGimgPoster = "https://gimg0.baidu.com/gimg/app=2001&n=0&g=0n&fmt=webp&src=img.baipiaozhe.com/assets/images/07/07ee1e07dfe068858170dfc5ef4df80af77732a31bf6a1567f588624d02562e7.webp"

const unwrappedBaiduPoster = "https://img.baipiaozhe.com/assets/images/07/07ee1e07dfe068858170dfc5ef4df80af77732a31bf6a1567f588624d02562e7.webp"

func TestNativeCoverValueStringsDecodesEscapedBaiduGimgPoster(t *testing.T) {
	values := nativeCoverValueStrings(escapedBaiduGimgPoster)
	if len(values) != 1 {
		t.Fatalf("decoded poster values = %#v, want one value", values)
	}
	if values[0] != decodedBaiduGimgPoster {
		t.Fatalf("decoded poster URL = %q, want %q", values[0], decodedBaiduGimgPoster)
	}
	if got := duoduoBaiduPosterSourceURL(values[0]); got != unwrappedBaiduPoster {
		t.Fatalf("unwrapped Baidu poster = %q, want %q", got, unwrappedBaiduPoster)
	}
}

func TestDuoduoPosterAttemptsIncludeUnwrappedBaiduSource(t *testing.T) {
	downloader := &Downloader{
		cfg: defaultConfig(),
		providerHosts: map[string]string{sourceDuoduo: "https://dduotv01.top"},
	}
	attempts := duoduoPosterAttemptsForDrama(
		decodedBaiduGimgPoster,
		downloader,
		nativeDrama{ID: "duoduo:157219", Source: sourceDuoduo, SourceID: "157219"},
	)
	for _, attempt := range attempts {
		if attempt.address == unwrappedBaiduPoster {
			return
		}
	}
	var addresses []string
	for _, attempt := range attempts {
		addresses = append(addresses, attempt.address)
	}
	if !strings.Contains(strings.Join(addresses, "\n"), "gimg0.baidu.com") {
		t.Fatalf("unexpected poster attempts: %#v", attempts)
	}
	t.Fatalf("Baidu source URL was not tried; attempts: %#v", attempts)
}

func TestFailedPrimaryPosterDoesNotRepeatDuoduoDetailLookup(t *testing.T) {
	detailRequests := 0
	client := &http.Client{Transport: posterLookupRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/api.php/web/vod/get_detail" {
			detailRequests++
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("not found")),
			Request:    request,
		}, nil
	})}
	downloader := &Downloader{
		client: client,
		cfg: defaultConfig(),
		providerHosts: map[string]string{sourceDuoduo: "https://dduotv01.top"},
	}
	cache := newNativeCoverCache(t.TempDir(), downloader)
	_, err := cache.loadAddress(context.Background(), nativeDrama{
		ID: "duoduo:157219", Source: sourceDuoduo, SourceID: "157219",
		Title: "余红旧事", Cover: "https://broken.example/poster.jpg",
	}, false)
	if err == nil {
		t.Fatal("expected unavailable primary and proxy poster to fail")
	}
	if detailRequests != 0 {
		t.Fatalf("primary image failure made %d duplicate detail requests; repair() owns the single metadata lookup", detailRequests)
	}
}


func TestAVIFImageDetectionIncludesCompatibleBrands(t *testing.T) {
	majorBrand := []byte{
		0, 0, 0, 24, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f',
		0, 0, 0, 0, 'm', 'i', 'f', '1', 'a', 'v', 'i', 'f',
	}
	if !isAVIFImage(majorBrand) || !nativeIsCoverImage(majorBrand) {
		t.Fatal("AVIF major brand was not recognized as a cover image")
	}

	compatibleBrand := []byte{
		0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'i', 'f', '1',
		0, 0, 0, 0, 'm', 'i', 'f', '1', 'a', 'v', 'i', 'f',
	}
	if !isAVIFImage(compatibleBrand) || !nativeIsCoverImage(compatibleBrand) {
		t.Fatal("AVIF compatible brand was not recognized as a cover image")
	}
	if isAVIFImage([]byte{0, 0, 0, 16, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c', 0, 0, 0, 0}) {
		t.Fatal("HEIC was incorrectly classified as AVIF")
	}
}
