package core

import "testing"

func TestHongguoMediaShortEdgeQuality(t *testing.T) {
	tests := []struct {
		name       string
		width      int
		height     int
		definition int
		want       int
	}{
		{name: "portrait prefers short edge over long-edge definition", width: 720, height: 1280, definition: 1280, want: 720},
		{name: "landscape uses short edge and ignores conflicting definition", width: 1920, height: 1080, definition: 1920, want: 1080},
		{name: "square uses the common edge", width: 1080, height: 1080, definition: 1080, want: 1080},
		{name: "definition is fallback when dimensions are absent", definition: 720, want: 720},
		{name: "width is fallback when only width is present", width: 720, want: 720},
		{name: "height is fallback when only height is present", height: 720, want: 720},
		{name: "missing dimensions and definition return zero", want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := hongguoMediaShortEdgeQuality(test.width, test.height, test.definition)
			if got != test.want {
				t.Fatalf("hongguoMediaShortEdgeQuality(%d, %d, %d) = %d, want %d",
					test.width, test.height, test.definition, got, test.want)
			}
		})
	}
}

func TestHongguoCodecAliasesAndUnsupportedVVC(t *testing.T) {
	for _, tc := range []struct {
		codec string
		rank int
		vvc bool
	}{
		{" AVC1.640028 ", 3, false},
		{"avc3.640028", 3, false},
		{"h.264", 3, false},
		{"hvc1.1.6.L120.B0", 2, false},
		{"hev1.1.6.L120", 2, false},
		{"H.265", 2, false},
		{"bytevc1", 2, false},
		{"bytevc2", 0, true},
		{"h.266", 0, true},
		{"vvc1.1.L120", 0, true},
		{"vvi1", 0, true},
		{"unknown", 0, false},
		{"", 1, false},
	} {
		if rank := hongguoSDKCodecRank(tc.codec); rank != tc.rank {
			t.Errorf("codec %q rank=%d want %d", tc.codec, rank, tc.rank)
		}
		if got := hongguoCodecIsH266(tc.codec); got != tc.vvc {
			t.Errorf("codec %q VVC=%t want %t", tc.codec, got, tc.vvc)
		}
	}
}

func TestHongguoProfileQualifiedCodecMediaSelection(t *testing.T) {
	model := map[string]any{"video_list": []any{
		map[string]any{"main_url": "https://media.test/vvc.mp4", "video_meta": map[string]any{"codec_type": "vvc1.1.L120", "definition": "2160p"}},
		map[string]any{"main_url": "https://media.test/hevc.mp4", "video_meta": map[string]any{"codec_type": "hvc1.1.6.L120", "definition": "1080p"}},
		map[string]any{"main_url": "https://media.test/avc.mp4", "video_meta": map[string]any{"codec_type": "avc1.640028", "definition": "1080p"}},
	}}
	got, err := selectHongguoAppMedia(model)
	if err != nil || got.URL != "https://media.test/avc.mp4" || len(got.Variants) != 2 {
		t.Fatalf("compatible media selection=%+v err=%v", got, err)
	}
}

func TestHongguoAppResponseAcceptsMapVideoListAndModelString(t *testing.T) {
	video := map[string]any{"main_url": "https://media.test/full.mp4", "video_meta": map[string]any{"codec_type": "h264", "definition": "1080p"}}
	for _, data := range []map[string]any{
		{"video_list": map[string]any{"video_1": video}},
		{"video_model": map[string]any{"video_list": []any{video}}},
		{"video_model": `{"video_list":{"video_1":{"main_url":"https://media.test/full.mp4","video_meta":{"codec_type":"h264","definition":"1080p"}}}}`},
		{"video_model": "invalid", "video_list": map[string]any{"video_1": video}},
	} {
		got, err := selectHongguoAppResponseMedia(data)
		if err != nil || got.URL != "https://media.test/full.mp4" || got.Quality != 1080 {
			t.Fatalf("compatible response selection=%+v err=%v", got, err)
		}
	}
}

func TestHongguoRejectsNestedUnsupportedGear(t *testing.T) {
	_, err := selectHongguoAppMedia(map[string]any{"video_list": []any{
		map[string]any{"main_url":"https://media.test/unsupported.mp4", "video_meta": map[string]any{"gear_des_key":"1080p_bytevc2","codec_type":"h264","definition":"1080p"}},
	}})
	if err != errHongguoH266Only {
		t.Fatalf("nested unsupported gear should signal VVC: %v", err)
	}
}
