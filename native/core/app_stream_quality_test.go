package core

import (
	"strings"
	"testing"
)

func TestNativeRewriteKeeps720PAndHigherHLSVariants(t *testing.T) {
	stream := &nativeStreamServer{address: "http://127.0.0.1:12345"}
	session := &nativeStreamSession{origin: stream.address, assets: map[string]nativeStreamAsset{}}
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

func TestNativeHLSVariantQualityIgnoresOpaqueSignedTokenValues(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want int
	}{
		{
			name: "opaque token contains quality digits",
			uri:  "https://cdn.example/master.m3u8?token=abc-quality480-def",
			want: 0,
		},
		{
			name: "opaque token contains resolution text",
			uri:  "https://cdn.example/master.m3u8?token=resolution%3D480%26height%3D360",
			want: 0,
		},
		{
			name: "explicit quality query parameter",
			uri:  "https://cdn.example/master.m3u8?quality=480&token=opaque",
			want: 480,
		},
		{
			name: "explicit portrait resolution query parameter",
			uri:  "https://cdn.example/master.m3u8?resolution=720x1280&token=opaque",
			want: 720,
		},
		{
			name: "explicit video height query parameter",
			uri:  "https://cdn.example/master.m3u8?video_height=360&token=opaque",
			want: 360,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeHLSVariantQuality("", test.uri); got != test.want {
				t.Fatalf("nativeHLSVariantQuality(%q)=%d, want %d", test.uri, got, test.want)
			}
		})
	}
}

func TestNativeHLSVariantQualityConflictingQueryMetadata(t *testing.T) {
	for _, tc := range []struct {
		uri string
		want int
	}{
		{"https://cdn.example/master.m3u8?height=1280&resolution=720x1280&quality=1080", 720},
		{"https://cdn.example/master.m3u8?quality=480&definition=1080p", 0},
		{"https://cdn.example/master.m3u8?quality=480&quality=1080", 0},
		{"https://cdn.example/master.m3u8?QUALITY=720&quality=720", 720},
	} {
		for i := 0; i < 30; i++ {
			if got := nativeHLSVariantQuality("", tc.uri); got != tc.want {
				t.Fatalf("quality for %s = %d, want %d", tc.uri, got, tc.want)
			}
		}
	}
}
