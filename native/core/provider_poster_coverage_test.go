package core

import "testing"

func TestProviderCoverAddressesRecognizesSourceSpecificPosterAliases(t *testing.T) {
	row := map[string]any{
		"series_cover":  "/hongguo/series.jpg",
		"coverUrl":      "/huangju/cover.webp",
		"img_y":         "/huangdou/img-y.jpg",
		"img_x":         "/huangdou/img-x.jpg",
		"poster_url":    "/poster/backup.jpg",
		"thumbnail_url": "/thumbnail/backup.jpg",
	}
	got := providerCoverAddresses(row, "https://images.example/")
	for _, want := range []string{
		"https://images.example/hongguo/series.jpg",
		"https://images.example/huangju/cover.webp",
		"https://images.example/huangdou/img-y.jpg",
		"https://images.example/huangdou/img-x.jpg",
		"https://images.example/poster/backup.jpg",
		"https://images.example/thumbnail/backup.jpg",
	} {
		if !containsString(got, want) {
			t.Fatalf("poster alias %q missing from %#v", want, got)
		}
	}
}

func TestAPIProviderDramaRowsRetainAlternatePosters(t *testing.T) {
	hongguo := hongguoDramaFromAny(map[string]any{
		"video_data": map[string]any{
			"series_id_str": "12345",
			"series_title":  "紅果測試作品",
			"series_cover":  "/hongguo/main.jpg",
			"cover":         "/hongguo/backup.jpg",
		},
	}, "短劇")
	if hongguo.Cover != "https://api.hongguoduanju.com/hongguo/main.jpg" ||
		!containsString(hongguo.CoverCandidates, "https://api.hongguoduanju.com/hongguo/backup.jpg") {
		t.Fatalf("Hongguo posters = %#v", hongguo)
	}

	huangdou := huangdouDramaFromMap(map[string]any{
		"id": "rp_88", "name": "黃豆測試作品",
		"img_y": "/huangdou/main.jpg", "img_x": "/huangdou/backup.jpg",
	})
	if huangdou.Cover != "https://huangdou.com/huangdou/main.jpg" ||
		!containsString(huangdou.CoverCandidates, "https://huangdou.com/huangdou/backup.jpg") {
		t.Fatalf("Huangdou posters = %#v", huangdou)
	}

	huangju, err := huangjuDramaFromMap(map[string]any{
		"id":  "8", "slug": "test-drama", "title": "劇果測試作品",
		"coverUrl": "/huangju/main.jpg", "poster_url": "/huangju/backup.jpg",
	}, "https://huangju.net", "test-drama-8")
	if err != nil || huangju.Cover != "https://huangju.net/huangju/main.jpg" ||
		!containsString(huangju.CoverCandidates, "https://huangju.net/huangju/backup.jpg") {
		t.Fatalf("Huangju posters = %#v, err=%v", huangju, err)
	}

	yeguo, err := yeguoDramaFromMap(map[string]any{
		"video_id": "99", "title": "野果測試作品",
		"cover": "/yeguo/main.jpg", "poster_url": "/yeguo/backup.jpg",
	}, "https://www.ygdj7.com")
	if err != nil || yeguo.Cover != "https://www.ygdj7.com/yeguo/main.jpg" ||
		!containsString(yeguo.CoverCandidates, "https://www.ygdj7.com/yeguo/backup.jpg") {
		t.Fatalf("Yeguo posters = %#v, err=%v", yeguo, err)
	}
}
