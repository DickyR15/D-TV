package core

import "testing"

func TestHuangguoVideoPageRefreshKeepsExistingHLSWhenMarkupHasNoDataHLS(t *testing.T) {
	current := "https://cdn.example/episode/master.m3u8?token=valid"
	got := huangguoVideoPageMediaURL(current, `<html><video controls></video></html>`, "https://huangguo.video/video/123")
	if got != current {
		t.Fatalf("page without data-hls erased the existing episode URL: %q", got)
	}
}

func TestHuangguoVideoPageRefreshReplacesHLSWhenItFindsValidURL(t *testing.T) {
	current := "https://cdn.example/episode/old.m3u8"
	body := `<video data-hls="/media/new.m3u8?token=refreshed"></video>`
	got := huangguoVideoPageMediaURL(current, body, "https://huangguo.video/video/123")
	want := "https://huangguo.video/media/new.m3u8?token=refreshed"
	if got != want {
		t.Fatalf("refreshed Huangguo video URL = %q, want %q", got, want)
	}
}

func TestHuangguoVideoPageRefreshIgnoresInvalidDataHLS(t *testing.T) {
	current := "https://cdn.example/episode/master.m3u8?token=valid"
	body := `<video data-hls="javascript:alert(1)"></video>`
	got := huangguoVideoPageMediaURL(current, body, "https://huangguo.video/video/123")
	if got != current {
		t.Fatalf("invalid refreshed data-hls erased the existing URL: %q", got)
	}
}
