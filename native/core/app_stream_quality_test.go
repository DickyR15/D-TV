package core

import (
	"strings"
	"testing"
)

func TestNativeRewriteKeeps720PAndHigherHLSVariants(t *testing.T) {
	stream := &nativeStreamServer{address: "http://127.0.0.1:12345"}
	session := &nativeStreamSession{assets: map[string]nativeStreamAsset{}}
	playlist := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=350000,RESOLUTION=640x360
360p/video.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=850000,RESOLUTION=854x480
480p/video.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2200000,RESOLUTION=1280x720
720p/video.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=4200000,RESOLUTION=1920x1080
1080p/video.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1200000
adaptive/video.m3u8
#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=100000,RESOLUTION=640x360,URI="iframe-360.m3u8"
#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=300000,RESOLUTION=1280x720,URI="iframe-720.m3u8"
`
	got, err := stream.nativeRewrite("session", session, playlist, "https://cdn.example/master.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	for _, rejected := range []string{"RESOLUTION=640x360", "RESOLUTION=854x480"} {
		if strings.Contains(got, rejected) {
			t.Fatalf("below-720p variant %q remained in rewritten playlist: %s", rejected, got)
		}
	}
	for _, kept := range []string{"RESOLUTION=1280x720", "RESOLUTION=1920x1080"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("720p-or-higher variant %q was removed: %s", kept, got)
		}
	}
	if count := strings.Count(got, "#EXT-X-STREAM-INF:"); count != 3 {
		t.Fatalf("kept %d stream variants, want 720p, 1080p and unknown-quality: %s", count, got)
	}
	if count := strings.Count(got, "#EXT-X-I-FRAME-STREAM-INF:"); count != 1 {
		t.Fatalf("kept %d I-frame variants, want only the 720p variant: %s", count, got)
	}
	if quality := nativeHLSVariantQuality("RESOLUTION=720x1280", "portrait.m3u8"); quality != 720 {
		t.Fatalf("portrait 720x1280 visible quality = %d, want 720", quality)
	}
}

func TestNativeHLSVariantQualityUsesURIWhenResolutionTagIsMissing(t *testing.T) {
	for _, sample := range []struct {
		uri  string
		want int
	}{
		{"/stream/360p/video.m3u8", 360},
		{"/stream/480p/video.m3u8", 480},
		{"/stream/720p/video.m3u8", 720},
		{"/stream/1080p/video.m3u8", 1080},
		{"/stream/1280x720/video.m3u8", 720},
		{"/stream/adaptive/video.m3u8", 0},
	} {
		if got := nativeHLSVariantQuality("", sample.uri); got != sample.want {
			t.Errorf("nativeHLSVariantQuality(%q) = %d, want %d", sample.uri, got, sample.want)
		}
	}
}
