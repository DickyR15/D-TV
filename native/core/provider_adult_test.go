package core

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func adultNodeImagesFromHTMLForTest(body, base string) string {
	document, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		return ""
	}
	covers := adultNodeImages(document, base, sourceSupJav)
	if len(covers) == 0 {
		return ""
	}
	return covers[0]
}

func adultCategoryURLMust(source, category string) string {
	address, err := adultCategoryURL(source, category)
	if err != nil {
		return ""
	}
	return address
}

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
	if got := adultDetailURL(sourceMissAV, "sone-543"); got != "https://missav.live/cn/sone-543" {
		t.Fatalf("MISS AV detail URL = %q", got)
	}
	if got := adultCategoryURLMust(sourceMissAV, "vr"); got != "https://missav.live/cn/genres/vr" {
		t.Fatalf("MISS AV genre URL = %q", got)
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
	missav := "<div class=\"thumbnail group\"><a href=\"/cn/sone-543\"><img data-src=\"/covers/sample.webp\" alt=\"SAMPLE-543\"></a><div class=\"my-2\">SAMPLE-543</div></div>"
	items := adultParseCards(missav, missAVBaseURL, sourceMissAV)
	if len(items) != 1 {
		t.Fatalf("MISS AV cards = %d, want 1", len(items))
	}
	if items[0].ID != providerDramaID(sourceMissAV, "sone-543") ||
		items[0].Title != "SAMPLE-543" ||
		items[0].Cover != "https://missav.live/covers/sample.webp" {
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

	// SupJav themes do not always expose the ".post" wrapper or a plain
	// image src. Discover a card through its detail link, then parse lazy
	// srcset and CSS background poster candidates.
	supjavFallback := `<div class="listing-card">
		<a href="/zh/67890.html"><span>備援海報作品</span></a>
		<div class="poster">
			<img src="data:image/gif;base64,R0lGODlhAQABA" data-srcset="/covers/67890-small.jpg 320w, /covers/67890-large.jpg 800w">
			<div class="fallback" data-background-image="/covers/67890-bg.webp"></div>
		</div>
	</div>`
	items = adultParseCards(supjavFallback, supJavBaseURL, sourceSupJav)
	if len(items) != 1 {
		t.Fatalf("SupJav fallback cards = %d, want 1", len(items))
	}
	if items[0].ID != providerDramaID(sourceSupJav, "67890") ||
		items[0].Cover != "https://supjav.com/covers/67890-small.jpg" {
		t.Fatalf("SupJav lazy srcset poster was not parsed: %#v", items[0])
	}
	wantFallback := "https://supjav.com/covers/67890-large.jpg"
	if len(items[0].CoverCandidates) == 0 || items[0].CoverCandidates[0] != wantFallback {
		t.Fatalf("SupJav poster candidates = %#v, want %q first fallback", items[0].CoverCandidates, wantFallback)
	}
	if got := adultNodeImagesFromHTMLForTest(`<div style="background-image:url('/covers/bg.webp')"></div>`, supJavBaseURL); got != "https://supjav.com/covers/bg.webp" {
		t.Fatalf("SupJav CSS background poster = %q", got)
	}
}

func TestAdultPlaybackURLsResolveRelativeToTheActualPlayerPage(t *testing.T) {
	got := adultFindM3U8(
		"<script>var player={source:'/media/master.m3u8?token=abc'};</script>",
		"https://missav.live/cn/simw-014",
	)
	if got != "https://missav.live/media/master.m3u8?token=abc" {
		t.Fatalf("relative player URL = %q", got)
	}
	if got := adultFindM3U8("<script>source='https://cdn.example/master.m3u8'</script>"); got != "https://cdn.example/master.m3u8" {
		t.Fatalf("absolute player URL without a page base = %q", got)
	}
}

func adultEncodeVOETestPayload(t *testing.T, mediaURL string) string {
	t.Helper()
	config, err := json.Marshal(map[string]string{"source": mediaURL})
	if err != nil {
		t.Fatal(err)
	}
	reversedBase64 := base64.StdEncoding.EncodeToString(config)
	data := []byte(reversedBase64)
	for left, right := 0, len(data)-1; left < right; left, right = left+1, right-1 {
		data[left], data[right] = data[right], data[left]
	}
	for index := range data {
		data[index] += 3
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	encoded = adultRot13(encoded)
	var chunks []string
	for len(encoded) > 4 {
		chunks = append(chunks, encoded[:4])
		encoded = encoded[4:]
	}
	if encoded != "" {
		chunks = append(chunks, encoded)
	}
	return strings.Join(chunks, "@$")
}

func TestAdultSupJavVOEDecodesStaticJSONHLSPayload(t *testing.T) {
	payload := adultEncodeVOETestPayload(t, "https://cdn.example/master.m3u8?token=abc")
	raw, err := json.Marshal([]string{payload})
	if err != nil {
		t.Fatal(err)
	}
	body := "<script type=\"application/json\">" + string(raw) + "</script>"
	got := adultSupJavVOEHLS(body, "https://voe.sx/e/video")
	if got != "https://cdn.example/master.m3u8?token=abc" {
		t.Fatalf("VOE decoded URL = %q", got)
	}
}

func TestAdultSupJavVOERejectsInvalidOrNonHLSPayload(t *testing.T) {
	invalid := `<script type="application/json">["not-a-valid-payload"]</script>`
	if got := adultSupJavVOEHLS(invalid, "https://voe.sx/e/video"); got != "" {
		t.Fatalf("invalid VOE payload = %q, want empty", got)
	}
	payload := adultEncodeVOETestPayload(t, "https://cdn.example/video.mp4")
	raw, err := json.Marshal([]string{payload})
	if err != nil {
		t.Fatal(err)
	}
	body := "<script type=\"application/json\">" + string(raw) + "</script>"
	if got := adultSupJavVOEHLS(body, "https://voe.sx/e/video"); got != "" {
		t.Fatalf("non-HLS VOE payload = %q, want empty", got)
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

func TestAdultSupJavFSTSkipsExtensionlessNonHLSAndUsesValidM3U8Fallback(t *testing.T) {
	body := `<script>
var links={"hls4":"https://cdn.example/manifest","hls3":"https://cdn.example/low/master.m3u8"};
jwplayer("vplayer").setup({sources:[{file:links.hls4 || links.hls3,type:"mp4"}]});
</script>`
	got := adultSupJavFSTHLS(body, "https://fc2stream.example/e/video")
	if got != "https://cdn.example/low/master.m3u8" {
		t.Fatalf("FST fallback HLS = %q, want the valid .m3u8 alternative", got)
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
