package core

import "testing"

func TestAdultProviderRegistrationAndCategories(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"missav.ws", sourceMissAV},
		{"www.missav.ai", sourceMissAV},
		{"supjav.com", sourceSupJav},
	}
	for _, test := range tests {
		if got := canonicalProviderSource(test.input); got != test.want {
			t.Fatalf("canonicalProviderSource(%q) = %q, want %q", test.input, got, test.want)
		}
		if !isHuangguoProviderSource(test.want) {
			t.Fatalf("provider %q is not registered", test.want)
		}
		if got := providerSourceForURL("https://" + test.input + "/"); got != test.want {
			t.Fatalf("providerSourceForURL(%q) = %q, want %q", test.input, got, test.want)
		}
		if adultProviderBaseURL(test.want) == "" {
			t.Fatalf("provider %q has no base URL", test.want)
		}
		if !validNativeCategory(test.want, "") {
			t.Fatalf("provider %q rejected the all category", test.want)
		}
		if !validAdultProviderCategory(test.want, adultProviderCategories(test.want)[0].ID) {
			t.Fatalf("provider %q rejected its first category", test.want)
		}
	}
	if validAdultProviderCategory(sourceMissAV, "../bad") ||
		validAdultProviderCategory(sourceSupJav, "https://attacker.example") {
		t.Fatal("adult provider accepted an invalid category")
	}
}

func TestAdultProviderDramaIDsAndDetailURLs(t *testing.T) {
	missav := providerDramaID(sourceMissAV, "sone-543")
	source, sourceID, ok := splitProviderDramaID(missav)
	if !ok || source != sourceMissAV || sourceID != "sone-543" {
		t.Fatalf("split MISS AV ID = %q/%q/%v", source, sourceID, ok)
	}
	if got := adultDetailURL(sourceMissAV, "sone-543"); got != "https://missav.ws/en/sone-543" {
		t.Fatalf("MISS AV detail URL = %q", got)
	}
	if got := adultDetailURL(sourceSupJav, "12345"); got != "https://supjav.com/zh/12345.html" {
		t.Fatalf("SupJav detail URL = %q", got)
	}
	if adultDetailURL(sourceMissAV, "today-hot") != "" ||
		adultDetailURL(sourceSupJav, "not-a-number") != "" {
		t.Fatal("adult detail URL builder accepted a category or malformed ID")
	}
}

func TestAdultCardParsingAndLazyCoverFallback(t *testing.T) {
	missav := "<div class=\"thumbnail\"><a href=\"/en/sone-543\" title=\"SAMPLE-543\"></a><img data-src=\"/covers/sample.webp\"></div>"
	items := adultParseCards(missav, missAVBaseURL, sourceMissAV)
	if len(items) != 1 {
		t.Fatalf("MISS AV cards = %d, want 1", len(items))
	}
	if items[0].ID != providerDramaID(sourceMissAV, "sone-543") ||
		items[0].Title != "SAMPLE-543" ||
		items[0].Cover != "https://missav.ws/covers/sample.webp" {
		t.Fatalf("unexpected MISS AV card: %#v", items[0])
	}

	supjav := "<div class=\"post\"><a href=\"/zh/12345.html\" title=\"作品測試\"><img data-original=\"/images/cover.jpg\"></a></div>"
	items = adultParseCards(supjav, supJavBaseURL, sourceSupJav)
	if len(items) != 1 {
		t.Fatalf("SupJav cards = %d, want 1", len(items))
	}
	if items[0].ID != providerDramaID(sourceSupJav, "12345") ||
		items[0].Cover != "https://supjav.com/images/cover.jpg" {
		t.Fatalf("unexpected SupJav card: %#v", items[0])
	}
}

func TestAdultPlaybackURLsResolveRelativeToTheActualPlayerPage(t *testing.T) {
	got := adultFindM3U8(
		"<script>var player={source:'/media/master.m3u8?token=abc'};</script>",
		"https://missav.ws/en/simw-014",
	)
	if got != "https://missav.ws/media/master.m3u8?token=abc" {
		t.Fatalf("relative player URL = %q", got)
	}
	if got := adultFindM3U8("<script>source='https://cdn.example/master.m3u8'</script>"); got != "https://cdn.example/master.m3u8" {
		t.Fatalf("absolute player URL without a page base = %q", got)
	}
}

func TestAdultSupJavFSTUsesDeclaredHLSOrderAndResolvesRelativeURL(t *testing.T) {
	body := `<script>
var links={"hls4":"/stream/high/master.m3u8","hls3":"https://cdn.example/low/master.m3u8"};
jwplayer("vplayer").setup({sources:[{file:links.hls4 || links.hls3,type:"hls"}]});
</script>`
	got := adultSupJavFSTHLS(body, "https://fc2stream.example/e/video")
	if got != "https://fc2stream.example/stream/high/master.m3u8" {
		t.Fatalf("FST primary HLS = %q", got)
	}
}

func TestAdultSupJavFSTAcceptsExtensionlessDeclaredHLS(t *testing.T) {
	body := `<script>
var links={"hls4":"","hls3":"https://cdn.example/manifest.txt","hls2":"https://cdn.example/low/master.m3u8"};
jwplayer("vplayer").setup({sources:[{type:"hls",file:links.hls4 || links.hls3 || links.hls2}]});
</script>`
	got := adultSupJavFSTHLS(body, "https://fc2stream.example/e/video")
	if got != "https://cdn.example/manifest.txt" {
		t.Fatalf("FST extensionless HLS = %q", got)
	}
}

func TestAdultSupJavFSTRejectsExtensionlessNonHLS(t *testing.T) {
	body := `<script>
var links={"hls4":"https://cdn.example/manifest","hls3":"https://cdn.example/low/master.m3u8"};
jwplayer("vplayer").setup({sources:[{file:links.hls4 || links.hls3,type:"mp4"}]});
</script>`
	got := adultSupJavFSTHLS(body, "https://fc2stream.example/e/video")
	if got != "" {
		t.Fatalf("FST non-HLS extensionless source = %q, want empty", got)
	}
}

func TestAdultProviderRejectsUntrustedURLsAndParsesPlaybackHints(t *testing.T) {
	if adultSafeURL(missAVBaseURL, "javascript:alert(1)", sourceMissAV) != "" {
		t.Fatal("MISS AV accepted a script URL")
	}
	if adultSafeURL(supJavBaseURL, "https://user:pass@supjav.com/12345.html", sourceSupJav) != "" {
		t.Fatal("SupJav accepted URL credentials")
	}
	if got := adultFindM3U8("<script>source=\"https://cdn.example.test/master.m3u8?token=abc\"</script>"); got != "https://cdn.example.test/master.m3u8?token=abc" {
		t.Fatalf("M3U8 extraction = %q", got)
	}
	servers := adultSupJavDataLinks("<a class=\"btn-server\" data-link=\"abc123\">TV</a><a class=\"btn-server\" data-link=\"def456\">FST</a>")
	if servers["TV"] != "abc123" || servers["FST"] != "def456" {
		t.Fatalf("SupJav servers = %#v", servers)
	}
}
