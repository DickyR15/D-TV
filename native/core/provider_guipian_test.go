package core

import "testing"

func TestGuipianProviderRegistration(t *testing.T) {
	if got := canonicalProviderSource("guipianwu.com"); got != sourceGuipian {
		t.Fatalf("canonical provider = %q, want %q", got, sourceGuipian)
	}
	if got := providerSourceForURL("https://guipianwu.com/play/123-1-1.html"); got != sourceGuipian {
		t.Fatalf("provider URL source = %q, want %q", got, sourceGuipian)
	}
	if !isHuangguoProviderSource(sourceGuipian) {
		t.Fatal("Guipian was not registered as a native provider")
	}
	if !validGuipianCategory("3") || validGuipianCategory("../3") {
		t.Fatal("Guipian category validation regression")
	}
	source, sourceID, ok := splitProviderDramaID(providerDramaID(sourceGuipian, "123"))
	if !ok || source != sourceGuipian || sourceID != "123" {
		t.Fatalf("split Guipian drama ID = %q/%q/%v", source, sourceID, ok)
	}
}

func TestGuipianPlaybackURLValidation(t *testing.T) {
	valid := "https://guipianwu.com/play/123-1-1.html"
	if got := guipianPlayURL(valid); got != valid {
		t.Fatalf("valid Guipian URL = %q, want %q", got, valid)
	}
	for _, value := range []string{
		"https://example.com/play/123-1-1.html",
		"https://guipianwu.com/detail/123.html",
		"javascript:alert(1)",
	} {
		if got := guipianPlayURL(value); got != "" {
			t.Fatalf("guipianPlayURL(%q) = %q, want empty", value, got)
		}
	}
}

func TestGuipianTransportSelectionPreservesWindows(t *testing.T) {
	if guipianStandardHTTPForOS("windows") {
		t.Fatal("Windows must keep its existing native stream transport")
	}
	if !guipianStandardHTTPForOS("android") {
		t.Fatal("Android must use the standard HTTP media transport")
	}
	if guipianStandardHTTPForOS("darwin") || guipianStandardHTTPForOS("ios") {
		t.Fatal("non-Android platforms must keep their existing media transport")
	}
}

func TestGuipianResolveMediaURLUsesFinalPageURL(t *testing.T) {
	tests := []struct {
		name    string
		pageURL string
		raw     string
		want    string
	}{
		{
			name:    "relative media path after page redirect",
			pageURL: "https://player.example.test/watch/123.html",
			raw:     "../media/master.m3u8?token=a%2Fb",
			want:    "https://player.example.test/media/master.m3u8?token=a%2Fb",
		},
		{
			name:    "protocol relative CDN URL",
			pageURL: "https://player.example.test/watch/123.html",
			raw:     "//cdn.example.test/media/master.m3u8",
			want:    "https://cdn.example.test/media/master.m3u8",
		},
		{
			name:    "absolute media URL",
			pageURL: "https://player.example.test/watch/123.html",
			raw:     "https://cdn.example.test/media/master.m3u8?token=abc",
			want:    "https://cdn.example.test/media/master.m3u8?token=abc",
		},
		{
			name:    "reject credentials in URL",
			pageURL: "https://player.example.test/watch/123.html",
			raw:     "https://user:pass@cdn.example.test/media/master.m3u8",
			want:    "",
		},
		{
			name:    "reject non HTTP scheme",
			pageURL: "https://player.example.test/watch/123.html",
			raw:     "javascript:alert(1)",
			want:    "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := guipianResolveMediaURL(test.pageURL, test.raw); got != test.want {
				t.Fatalf("resolved URL = %q, want %q", got, test.want)
			}
		})
	}
}

func TestGuipianCoverCandidatesPreferLazyImagesAndKeepFallbacks(t *testing.T) {
	got := guipianImageCandidates(`<img src="/placeholder.png" data-src="/lazy.webp" data-original="//img.guipianwu.com/original.jpg" data-background="/background.jpg">`)
	want := []string{
		"https://img.guipianwu.com/original.jpg",
		"https://guipianwu.com/background.jpg",
		"https://guipianwu.com/lazy.webp",
		"https://guipianwu.com/placeholder.png",
	}
	if len(got) != len(want) { t.Fatalf("cover candidates = %#v, want %#v", got, want) }
	for index := range want {
		if got[index] != want[index] { t.Fatalf("cover candidate %d = %q, want %q", index, got[index], want[index]) }
	}
}

func TestGuipianPickPlaylistRetainsOtherLinePagesForEachEpisode(t *testing.T) {
	body := `
<li class="swiper-slide ewave-tab" data-target="#ewave-playlist-1">線路一</li>
<li class="swiper-slide ewave-tab" data-target="#ewave-playlist-2">線路二</li>
<div id="ewave-playlist-1"><div class="playlist-slide"><li><a title='第1集' href='/play/123-1-1.html'>1</a></li><li><a title='第2集' href='/play/123-1-2.html'>2</a></li></div></div>
<div id="ewave-playlist-2"><div class="playlist-slide"><li><a title='第1集' href='/play/123-2-1.html'>1</a></li></div></div>
`
	_, chapters, err := guipianPickPlaylist(body)
	if err != nil {
		t.Fatalf("guipianPickPlaylist failed: %v", err)
	}
	if len(chapters) != 2 {
		t.Fatalf("display episodes = %d, want 2", len(chapters))
	}
	if len(chapters[0].PlaybackVariants) != 1 ||
		chapters[0].PlaybackVariants[0] != "https://guipianwu.com/play/123-2-1.html" {
		t.Fatalf("episode 1 alternative line was not retained: %#v", chapters[0])
	}
	if len(chapters[1].PlaybackVariants) != 0 {
		t.Fatalf("episode 2 should not receive another episode's route: %#v", chapters[1])
	}
}



func TestGuipianImageCandidatesSupportsSrcsetAndBackgroundStyles(t *testing.T) {
	got := guipianImageCandidates(`<div class="poster" style="background-image:url('/poster/background.webp')"><img data-srcset="/poster/small.jpg 320w, /poster/large.jpg 1200w" src="/placeholder.gif"></div>`)
	want := []string{
		"https://guipianwu.com/placeholder.gif",
		"https://guipianwu.com/poster/small.jpg",
		"https://guipianwu.com/poster/large.jpg",
		"https://guipianwu.com/poster/background.webp",
	}
	if len(got) != len(want) {
		t.Fatalf("srcset/background candidates = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("candidate %d = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestGuipianCategoryIsPreservedForPosterMatching(t *testing.T) {
	cases := map[string]string{
		"3": "電影", "14": "劇集", "23": "動漫", "26": "動漫",
	}
	for category, want := range cases {
		if got := guipianCategoryName(category); got != want {
			t.Errorf("category %s = %q, want %q", category, got, want)
		}
	}
	items := parseGuipianCards(`<ul class="row video-list video-film-list"><a class="video-link" href="/nv/123.html" title="動漫測試"><img data-src="/poster.jpg"></a></ul>`, "23")
	if len(items) != 1 || items[0].CategoryName != "動漫" {
		t.Fatalf("anime catalogue category was lost: %#v", items)
	}
}
