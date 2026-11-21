package core

import (
	"strconv"
	"strings"
	"testing"

	htmlnet "golang.org/x/net/html"
)

func dsdTestAaChar(octal string) string {
	parts := make([]string, 0, len(octal))
	for _, digit := range octal {
		switch digit {
		case '0':
			parts = append(parts, "(c^_^o)")
		case '1':
			parts = append(parts, "(ﾟΘﾟ)")
		case '3':
			parts = append(parts, "(o^_^o)")
		case '4':
			parts = append(parts, "(ﾟｰﾟ)")
		default:
			parts = append(parts, string(digit))
		}
	}
	return "(ﾟДﾟ)[ﾟεﾟ]+" + strings.Join(parts, "+")
}

func TestDSDAaencodeDecoderExtractsSignedVPath(t *testing.T) {
	payload := `var vPath = "/video/fixture/1000k/index.m3u8?sign=abc%2Fdef";`
	encoded := "(ﾟДﾟ) ['_'] ( (ﾟДﾟ) ['_']"
	for _, char := range payload {
		encoded += dsdTestAaChar(strconv.FormatInt(int64(char), 8))
	}
	encoded += "+ (ﾟДﾟ)[ﾟoﾟ]"
	page := `<html><script type="text/javascript">` + encoded + `</script></html>`
	path, ok := dsdSignedPathFromVplayer(page)
	if !ok || path != "/video/fixture/1000k/index.m3u8?sign=abc%2Fdef" {
		t.Fatalf("signed vPath not decoded: %q %v", path, ok)
	}
}

func TestDSDVplayerMediaPathDropsOrigin(t *testing.T) {
	got := dsdVplayerMediaPath("https://www.dsd.com.se/video/a/1000k/index.m3u8?token=a%2Fb")
	if got != "/video/a/1000k/index.m3u8?token=a%2Fb" {
		t.Fatalf("wrong media path: %s", got)
	}
	if dsdVplayerMediaPath("/video/a/index.m3u8") != "/video/a/index.m3u8" {
		t.Fatal("relative media path changed")
	}
}

func TestDSDDetailPosterCandidatesKeepPlayerAndCardFallbacks(t *testing.T) {
	document, err := htmlnet.Parse(strings.NewReader(`<div class="video-box">
		<img src="/loading.gif" data-original="/poster/original.webp"
			data-src="/poster/lazy.jpg" data-srcset="/poster/small.jpg 320w, /poster/large.jpg 1200w">
		<div class="img-bg" style="background-image:url('/poster/background.jpg')"></div>
	</div>`))
	if err != nil {
		t.Fatal(err)
	}
	box := providerHTMLFirstClass(document, "video-box")
	player := map[string]any{
		"poster": []any{"/player/poster.webp", "/player/poster-backup.jpg"},
	}
	got := dsdDetailCoverCandidates(player, box, "https://dsd.com.se/play/123")
	for _, want := range []string{
		"https://dsd.com.se/player/poster.webp",
		"https://dsd.com.se/player/poster-backup.jpg",
		"https://dsd.com.se/poster/original.webp",
		"https://dsd.com.se/poster/lazy.jpg",
		"https://dsd.com.se/poster/small.jpg",
		"https://dsd.com.se/poster/large.jpg",
		"https://dsd.com.se/poster/background.jpg",
	} {
		if !containsString(got, want) {
			t.Fatalf("DSD poster candidate %q missing from %#v", want, got)
		}
	}
}
