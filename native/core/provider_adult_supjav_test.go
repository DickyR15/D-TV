package core

import (
	"testing"
)

func TestAdultSupJavGatewayCandidatesIncludeOfficialMirrorsAndParameterForms(t *testing.T) {
	got := adultSupJavGatewayCandidates("abc123")
	if len(got) != 6 {
		t.Fatalf("gateway candidates = %d, want six: %#v", len(got), got)
	}
	want := []string{
		"https://lk1.supremejav.com/supjav.php?c=321cba",
		"https://lk2.supremejav.com/supjav.php?c=321cba",
		"https://supremejav.com/supjav.php?c=321cba",
		"https://lk1.supremejav.com/supjav.php?l=abc123",
		"https://lk2.supremejav.com/supjav.php?l=abc123",
		"https://supremejav.com/supjav.php?l=abc123",
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("gateway[%d] = %q, want %q", index, got[index], want[index])
		}
	}
	if adultSupJavGatewayCandidates("   ") != nil {
		t.Fatal("empty token should not produce gateway URLs")
	}
}

func TestAdultSupJavServerLinksNormalizeProviderAliases(t *testing.T) {
	body := `<div class="servers">
		<a class="btn-server" data-link="tv-token"><span>TV</span></a>
		<a class="btn-server" data-link="fast-token">FC2Stream</a>
		<a class="btn-server" data-link="stream-token">Streamtape</a>
		<a data-link="voe-token">VOE</a>
	</div>`
	got := adultSupJavServerLinks(body)
	for name, want := range map[string]string{
		"TV": "tv-token", "FST": "fast-token", "ST": "stream-token", "VOE": "voe-token",
	} {
		if got[name] != want {
			t.Fatalf("server %s = %q, want %q; all=%#v", name, got[name], want, got)
		}
	}
	if adultSupJavDataLinks(body)["FST"] != "fast-token" {
		t.Fatal("legacy server-link helper should share the normalized parser")
	}
}

func TestAdultSupJavTVParsesURLPlayAndRelativeMedia(t *testing.T) {
	body := `<script>player.urlPlay = "/live/master.m3u8?token=abc";</script>`
	got := adultSupJavTVHLS(body, "https://lk1.supremejav.com/embed/1")
	want := "https://lk1.supremejav.com/live/master.m3u8?token=abc"
	if got != want {
		t.Fatalf("TV media URL = %q, want %q", got, want)
	}
}

func TestAdultSupJavStreamtapeBuildsProgressiveMediaURL(t *testing.T) {
	body := `<script>
document.getElementById("robotlink").innerHTML = "streamtape.com/get_video?id=" + ("0123456789").substring(3).substring(2);
</script>`
	got := adultSupJavStreamtapeDirectURL(body, "https://streamtape.com/e/example")
	if got != "https://streamtape.com/get_video?id=56789" {
		t.Fatalf("Streamtape direct URL = %q", got)
	}
	if adultSupJavStreamtapeDirectURL(`<div id="robotlink">decoy</div>`, "https://streamtape.com/e/example") != "" {
		t.Fatal("Streamtape parser accepted a static decoy")
	}
}

func TestAdultSupJavVASRequestUsesEmbedOriginAndValidFilecode(t *testing.T) {
	endpoint, filecode, ok := adultSupJavVASRequest(
		"https://vas.example.test/embed/AbC123?token=abc&filecode=ignored",
		`<script>fetch("/api/stream")</script>`,
	)
	if !ok || endpoint != "https://vas.example.test/api/stream?token=abc" || filecode != "AbC123" {
		t.Fatalf("VAS request = %q %q %v", endpoint, filecode, ok)
	}
	if _, _, ok := adultSupJavVASRequest(
		"https://vas.example.test/embed/../../bad",
		`/api/stream`,
	); ok {
		t.Fatal("VAS accepted an unsafe filecode")
	}
	if _, _, ok := adultSupJavVASRequest(
		"https://vas.example.test/embed/AbC123",
		`no api route`,
	); ok {
		t.Fatal("VAS accepted a page without an API hint")
	}
}

func TestAdultSupJavVOERedirectOnlyAllowsVOEEmbedPath(t *testing.T) {
	body := `<script>window.location.href = "https://voe.sx/e/AbC123";</script>`
	if got := adultSupJavVOERedirect(body, "https://lk1.supremejav.com/supjav.php"); got != "https://voe.sx/e/AbC123" {
		t.Fatalf("VOE redirect = %q", got)
	}
	bad := `<script>window.location.href = "https://evil.example/e/AbC123";</script>`
	if got := adultSupJavVOERedirect(bad, "https://lk1.supremejav.com/supjav.php"); got != "" {
		t.Fatalf("VOE accepted unexpected host/path: %q", got)
	}
}

func TestAdultSupJavRoutesKeepMultipleProviderFallbacks(t *testing.T) {
	one := providerMedia{URL: "https://cdn.example.test/one.m3u8", Referer: supJavBaseURL + "/"}
	two := providerMedia{URL: "https://cdn.example.test/two.m3u8", Referer: "https://fst.example.test/"}
	choice := nativePlaybackChoices(providerMedia{URL: one.URL, Referer: one.Referer, Variants: []providerMedia{two}}, 0)
	if len(choice.media) != 2 || choice.media[0].URL != one.URL || choice.media[1].URL != two.URL {
		t.Fatalf("native playback choices lost SupJav fallbacks: %#v", choice.media)
	}
}
