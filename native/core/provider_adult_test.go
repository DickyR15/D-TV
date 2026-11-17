package core

import "testing"

func adultCategoryURLMust(source, category string) string {
	address, err := adultCategoryURL(source, category)
	if err != nil {
		return ""
	}
	return address
}

func TestAdultProviderRegistrationAndCategories(t *testing.T) {
	for _, input := range []string{"missav.ws", "www.missav.ai"} {
		if got := canonicalProviderSource(input); got != sourceMissAV {
			t.Fatalf("canonicalProviderSource(%q) = %q", input, got)
		}
		if !isHuangguoProviderSource(sourceMissAV) {
			t.Fatal("MissAV is not registered")
		}
		if got := providerSourceForURL("https://" + input + "/"); got != sourceMissAV {
			t.Fatalf("providerSourceForURL(%q) = %q", input, got)
		}
	}
	if got := providerSourceForURL("https://missav.live/"); got != sourceMissAV {
		t.Fatalf("current MissAV domain = %q", got)
	}
	if isHuangguoProviderSource(canonicalProviderSource("supjav")) ||
		providerSourceForURL("https://supjav.com/") != "" {
		t.Fatal("retired SupJav must not be registered as an active provider")
	}
	if !validNativeCategory(sourceMissAV, "") || !validAdultProviderCategory(sourceMissAV, "") ||
		validAdultProviderCategory(sourceMissAV, "../bad") {
		t.Fatal("MissAV category validation changed")
	}
	if len(adultProviderCategories(sourceMissAV)) == 0 {
		t.Fatal("MissAV categories are missing")
	}
}

func TestAdultMissAVDynamicGenreParser(t *testing.T) {
	body := `<nav>
		<a href="/cn/genres/chinese-subtitle">中文字幕類</a>
		<a href="/dm298/cn/genres/uncensored-leak">無碼流出</a>
		<a href="/cn/genres/chinese-subtitle">中文字幕</a>
		<a href="/cn/videos/not-a-genre">不應收錄</a>
		<a href="https://evil.example/cn/genres/fake">外站分類</a>
	</nav>`
	got := adultParseMissAVCategories(body, missAVBaseURL)
	if len(got) != 2 {
		t.Fatalf("MissAV genres = %#v, want two trusted categories", got)
	}
	byID := map[string]string{}
	for _, category := range got {
		byID[category.ID] = category.Name
	}
	if byID["chinese-subtitle"] != "中文字幕" || byID["uncensored-leak"] != "無碼流出" {
		t.Fatalf("MissAV dynamic genres = %#v", byID)
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
	if adultDetailURL(sourceMissAV, "today-hot") != "" {
		t.Fatal("adult detail URL builder accepted a category")
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
}

func TestAdultPackerBase62AlphabetBound(t *testing.T) {
	if got := adultEncodeBase(61, 62); got != "Z" {
		t.Fatalf("base-62 digit 61 = %q, want Z", got)
	}
	packedWithUnsupportedRadix := "eval(function(p,a,c,k,e,d){return p;}('0',64,1,'x'.split('|'))"
	if got := adultUnpackPacker(packedWithUnsupportedRadix); got != "" {
		t.Fatalf("base-64 Packer payload should be rejected, got %q", got)
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

func TestAdultMissAVPrefersCurrentSurritPlaylist(t *testing.T) {
	body := `<script>const playlist = "https://surrit.com/01234567-89ab-cdef-0123-456789abcdef/playlist.m3u8?token=fixture";</script>`
	got := adultFindMissAVMedia(body, "https://missav.live/cn/sample-123")
	want := "https://surrit.com/01234567-89ab-cdef-0123-456789abcdef/playlist.m3u8?token=fixture"
	if got != want {
		t.Fatalf("MissAV Surrit media URL = %q, want %q", got, want)
	}
}

func TestAdultProviderRejectsUntrustedURLsAndParsesPlaybackHints(t *testing.T) {
	if adultSafeURL(missAVBaseURL, "javascript:alert(1)", sourceMissAV) != "" {
		t.Fatal("MISS AV accepted a script URL")
	}
	if got := adultFindM3U8("<script>source=\"https://cdn.example.test/master.m3u8?token=abc\"</script>"); got != "https://cdn.example.test/master.m3u8?token=abc" {
		t.Fatalf("M3U8 extraction = %q", got)
	}
}
