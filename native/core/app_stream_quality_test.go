package core

import (
	"strings"
	"testing"
)

func TestNativeFilterHLSMasterVariantsKeeps720AndHigher(t *testing.T) {
	master := strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-STREAM-INF:BANDWIDTH=400000,RESOLUTION=640x360",
		"360p/video.m3u8",
		"#EXT-X-STREAM-INF:BANDWIDTH=700000,RESOLUTION=854x480",
		"480p/video.m3u8",
		"#EXT-X-STREAM-INF:BANDWIDTH=1600000,RESOLUTION=1280x720",
		"720p/video.m3u8",
		"#EXT-X-STREAM-INF:BANDWIDTH=3500000,RESOLUTION=1920x1080",
		"1080p/video.m3u8",
	}, "\n")

	got, removed, err := nativeFilterHLSMasterVariants(master)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed variants=%d, want 2", removed)
	}
	for _, forbidden := range []string{"360p/video.m3u8", "480p/video.m3u8", "RESOLUTION=640x360", "RESOLUTION=854x480"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("low-resolution variant remained: %q\n%s", forbidden, got)
		}
	}
	for _, required := range []string{"720p/video.m3u8", "1080p/video.m3u8", "RESOLUTION=1280x720", "RESOLUTION=1920x1080"} {
		if !strings.Contains(got, required) {
			t.Fatalf("720p-or-higher variant was removed: %q\n%s", required, got)
		}
	}
}

func TestNativeFilterHLSMasterVariantsUsesURIWhenResolutionIsMissing(t *testing.T) {
	master := "#EXTM3U\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=400000\n/360p/video.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=1600000\n/720p/video.m3u8\n"
	got, removed, err := nativeFilterHLSMasterVariants(master)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 || strings.Contains(got, "/360p/") || !strings.Contains(got, "/720p/") {
		t.Fatalf("unexpected filtered master playlist: removed=%d body=%s", removed, got)
	}
}

func TestNativeFilterHLSMasterVariantsPreservesUnknownAndMediaPlaylists(t *testing.T) {
	master := "#EXTM3U\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=900000\n/unlabelled/video.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=1600000,RESOLUTION=1280x720\n/720p/video.m3u8\n"
	got, removed, err := nativeFilterHLSMasterVariants(master)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 || got != master {
		t.Fatalf("unknown variant should be preserved: removed=%d body=%s", removed, got)
	}

	media := "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\nsegment.ts\n#EXT-X-ENDLIST\n"
	got, removed, err = nativeFilterHLSMasterVariants(media)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 || got != media {
		t.Fatalf("media playlist should remain untouched: removed=%d body=%s", removed, got)
	}
}

func TestNativeFilterHLSMasterVariantsRejectsOnlySub720Master(t *testing.T) {
	master := "#EXTM3U\n" +
		"#EXT-X-STREAM-INF:RESOLUTION=640x360\n/360p/video.m3u8\n" +
		"#EXT-X-STREAM-INF:RESOLUTION=854x480\n/480p/video.m3u8\n"
	if _, removed, err := nativeFilterHLSMasterVariants(master); err == nil || removed != 2 {
		t.Fatalf("expected low-only playlist rejection, removed=%d err=%v", removed, err)
	}
}
