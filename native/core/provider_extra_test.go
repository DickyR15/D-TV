package core

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
)

func TestExtraAdultImportedModuleCards(t *testing.T) {
	tests := []struct {
		name, source, base, html, wantID, wantTitle string
	}{
		{
			name: "SupJav", source: sourceSupJav, base: supjavBaseURL,
			html: `<div class="box-item"><a href="/video/sample-123"><img src="https://img.supjav.blog/sample.jpg" alt="SupJav 海報"><div class="detail">SupJav 測試作品</div></a></div>`,
			wantID: "video/sample-123", wantTitle: "SupJav 測試作品",
		},
		{
			name: "PPP", source: sourcePPP, base: pppBaseURL,
			html: `<div class="item card-video"><a href="https://ppp.porn/v/abc123/"><img data-src="https://cdnlab.live/abc.jpg" alt="PPP 測試作品"><figcaption class="card-video__duration">6:57</figcaption></a></div>`,
			wantID: "abc123", wantTitle: "PPP 測試作品",
		},
		{
			name: "Madou", source: sourceMadou, base: madouBaseURL,
			html: `<article class="excerpt"><a class="thumbnail" href="https://madou.club/sample-123.html"><img data-src="https://madou.club/poster.jpg"></a><h2>麻豆測試作品</h2></article>`,
			wantID: "sample-123.html", wantTitle: "麻豆測試作品",
		},
		{
			name: "91Porna", source: source91Porna, base: ninetyOneBaseURL,
			html: `<div class="video-item"><a href="/comic/index/avdetail?video_key=key_123"><img data-src="https://91porna.com/poster.jpg" alt="91 測試作品"></a><span>12:34</span></div>`,
			wantID: "key_123", wantTitle: "91 測試作品",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			segments := extraAdultSegments(test.html, test.source)
			if len(segments) != 1 { t.Fatalf("expected one card segment, got %d", len(segments)) }
			item, ok := extraAdultParseCard(test.source, segments[0], test.base)
			if !ok { t.Fatal("card parser rejected valid module fixture") }
			if item.SourceID != test.wantID { t.Fatalf("source ID = %q, want %q", item.SourceID, test.wantID) }
			if item.Title != test.wantTitle { t.Fatalf("title = %q, want %q", item.Title, test.wantTitle) }
			if item.Cover == "" { t.Fatal("poster URL was not parsed") }
		})
	}
}

func TestExtraAdultSupJavBlogIsPrimaryHost(t *testing.T) {
	if got := extraAdultBaseURL(sourceSupJav); got != "https://supjav.blog" { t.Fatalf("SupJav base URL = %q", got) }
	if got := providerSourceForURL("https://supjav.blog/video/sample-123"); got != sourceSupJav { t.Fatalf("SupJav host mapped to %q", got) }
	if got := extraAdultDetailURL(sourceSupJav, "video/sample-123"); got != "https://supjav.blog/video/sample-123" { t.Fatalf("SupJav detail URL = %q", got) }
}

func TestExtraAdultPlaybackMarkupPatterns(t *testing.T) {
	body := `<iframe src="https://upload18.com/embed/123"></iframe><script>setVideoHLS("https://cdn.supjav.blog/video/index.m3u8?token=x")</script>`
	if got := extraAdultFrame("https://supjav.blog/video/example", body); got != "https://upload18.com/embed/123" { t.Fatalf("iframe URL = %q", got) }
	if got := extraAdultHLS(body, "https://supjav.blog/video/example"); !strings.Contains(got, "/index.m3u8?token=x") { t.Fatalf("HLS URL = %q", got) }
}

func TestExtraAdultCategoryAndSearchPaths(t *testing.T) {
	got, err := extraAdultCategoryPath(sourceSupJav, 1, "uncensored")
	if err != nil || got != "/uncensored?page=1" { t.Fatalf("SupJav category path = %q, err=%v", got, err) }
	got, paged, err := extraAdultSearchPath(sourcePPP, 2, "taiwan")
	if err != nil || !paged || got != "/search/?q=taiwan&page=2" { t.Fatalf("PPP search path = %q, paged=%v, err=%v", got, paged, err) }
}

func TestExtraAdult91PornaUsesCurrentPublicRoutes(t *testing.T) {
	if !extraAdultValidCategory(source91Porna, "play") ||
		!extraAdultValidCategory(source91Porna, "month_hot") {
		t.Fatal("91Porna current playing or previous-month category is not registered")
	}
	got, err := extraAdultCategoryPath(source91Porna, 1, "")
	if err != nil || got != "/comic/index/video?category=play" {
		t.Fatalf("91Porna default category path = %q, err=%v", got, err)
	}
	got, err = extraAdultCategoryPath(source91Porna, 2, "")
	if err != nil || got != "/comic/index/video?category=play&page=2" {
		t.Fatalf("91Porna second-page category path = %q, err=%v", got, err)
	}
	if got := extraAdultDetailURL(source91Porna, "key_123"); got != "https://91porna.com/comic/index/detail?video_key=key_123" {
		t.Fatalf("91Porna detail URL = %q", got)
	}
	html := `<div class="video-item"><a href="/comic/index/detail?video_key=key_123"><img data-src="/poster.jpg" alt="91 current detail route"></a><span>12:34</span></div>`
	segments := extraAdultSegments(html, source91Porna)
	if len(segments) != 1 {
		t.Fatalf("91Porna current card segments = %d, want 1", len(segments))
	}
	item, ok := extraAdultParseCard(source91Porna, segments[0], ninetyOneBaseURL)
	if !ok || item.SourceID != "key_123" || item.Cover != "https://91porna.com/poster.jpg" {
		t.Fatalf("91Porna current card parse = %#v, ok=%v", item, ok)
	}
}

func TestExtraAdultMadouUsesCurrentCategorySlugs(t *testing.T) {
	if !extraAdultValidCategory(sourceMadou, "大象传媒") ||
		!extraAdultValidCategory(sourceMadou, "猫爪影像") ||
		!extraAdultValidCategory(sourceMadou, "hongkongdoll") ||
		!extraAdultValidCategory(sourceMadou, "psychoporntw") ||
		!extraAdultValidCategory(sourceMadou, "sa国际传媒") ||
		!extraAdultValidCategory(sourceMadou, "kiss糖果屋") {
		t.Fatal("current Madou categories are missing")
	}

	tests := []struct {
		page     int
		category string
		want     string
	}{
		{page: 1, category: "0", want: "/"},
		{page: 2, category: "0", want: "/page/2/"},
		{page: 1, category: "大象传媒", want: "/category/%E5%A4%A7%E8%B1%A1%E4%BC%A0%E5%AA%92"},
		{page: 2, category: "大象传媒", want: "/category/%E5%A4%A7%E8%B1%A1%E4%BC%A0%E5%AA%92/page/2"},
		{page: 1, category: "hongkongdoll", want: "/category/hongkongdoll"},
	}
	for _, test := range tests {
		got, err := extraAdultCategoryPath(sourceMadou, test.page, test.category)
		if err != nil || got != test.want {
			t.Fatalf("Madou category path (%d, %q) = %q, err=%v; want %q",
				test.page, test.category, got, err, test.want)
		}
	}
}

func TestExtraAdultMadouAcceptsUnicodeDetailSlugsSafely(t *testing.T) {
	const slug = "大象传媒-中文作品"
	raw := "/%E5%A4%A7%E8%B1%A1%E4%BC%A0%E5%AA%92-%E4%B8%AD%E6%96%87%E4%BD%9C%E5%93%81.html"
	id := extraAdultNormalizeID(sourceMadou, raw)
	if id != slug+".html" || !extraAdultValidID(sourceMadou, id) {
		t.Fatalf("Madou normalized detail ID = %q", id)
	}
	if got := extraAdultDetailURL(sourceMadou, id); got != madouBaseURL+"/"+url.PathEscape(id) {
		t.Fatalf("Madou Unicode detail URL = %q", got)
	}
	if got := extraAdultNormalizeID(sourceMadou, "https://evil.example/"+raw); got != "" {
		t.Fatalf("Madou accepted an external detail host: %q", got)
	}
}

func TestExtraAdultMadouParsesUnicodeDetailCard(t *testing.T) {
	html := `<article class="excerpt">
		<a href="/%E5%A4%A7%E8%B1%A1%E4%BC%A0%E5%AA%92-%E4%B8%AD%E6%96%87%E4%BD%9C%E5%93%81.html" class="thumbnail">
			<img data-src="/wp-content/uploads/poster.webp" alt="大象传媒中文作品">
		</a>
		<h2>大象传媒中文作品</h2>
	</article>`
	segments := extraAdultSegments(html, sourceMadou)
	if len(segments) != 1 {
		t.Fatalf("Madou article segments = %d, want 1", len(segments))
	}
	item, ok := extraAdultParseCard(sourceMadou, segments[0], madouBaseURL)
	if !ok || item.SourceID != "大象传媒-中文作品.html" ||
		item.Title != "大象传媒中文作品" ||
		item.Cover != madouBaseURL+"/wp-content/uploads/poster.webp" {
		t.Fatalf("Madou Unicode card = %#v, ok=%v", item, ok)
	}
}

func TestExtraAdult91PornaDetectsMobileQROnlyDetailPages(t *testing.T) {
	body := `<section>
		<div>手機看</div>
		<p>使用 手機瀏覽器 掃碼觀看</p>
	</section>`
	if !extraAdultMobileScanRequired(body) {
		t.Fatal("91Porna mobile QR-only page was not detected")
	}
	if extraAdultMobileScanRequired(`<p>使用手機瀏覽器瀏覽分類</p>`) {
		t.Fatal("ordinary mobile-browser text was incorrectly classified as QR-only")
	}
}

func TestAdultPackerUnpacksDAndRDecoderForms(t *testing.T) {
	for _, decoder := range []string{"d", "r"} {
		t.Run(decoder, func(t *testing.T) {
			script := "eval(function(p,a,c,k,e," + decoder + "){return p;}('https://media.example.test/playlist.m3u8',36,0,''.split('|'),0,{}))"
			if got := adultUnpackPacker(script); got != "https://media.example.test/playlist.m3u8" {
				t.Fatalf("Packer %s decoder output = %q", decoder, got)
			}
		})
	}
}

func TestExtraAdultPPPExtractsVarStreamAndKeepsSignedQuery(t *testing.T) {
	body := `<script>var stream = 'https://cdn.ppp.porn/stream/opaque/index.m3u8?token=abc%2F123&expires=456';</script>`
	got := extraAdultPPPStreamURL(body, pppBaseURL+"/v/example/")
	want := "https://cdn.ppp.porn/stream/opaque/index.m3u8?token=abc%2F123&expires=456"
	if got != want {
		t.Fatalf("PPP stream URL = %q, want %q", got, want)
	}
}

func TestExtraAdultMadouAcceptsUnquotedIframeSource(t *testing.T) {
	body := `<iframe id="video" src=https://dash.madou.club/share/68ff2021d2f708714780a8c1></iframe>`
	got := extraAdultFrame(madouBaseURL+"/sample.html", body)
	want := "https://dash.madou.club/share/68ff2021d2f708714780a8c1"
	if got != want {
		t.Fatalf("Madou unquoted iframe URL = %q, want %q", got, want)
	}
}

func TestExtraAdult91PornaExtractsCurrentEmbedPlaybackParameters(t *testing.T) {
	body := `<script>
		var player = "embed_play.js?img=poster%2Fabc123";
		var src = encodeURIComponent("https://91porna.com/comic/index/embed?id=169397");
	</script>`
	img, encodedURL := extraAdult91PornaPlayParams(body)
	if img != "poster%2Fabc123" {
		t.Fatalf("91Porna embed image parameter = %q", img)
	}
	if encodedURL != "https://91porna.com/comic/index/embed?id=169397" {
		t.Fatalf("91Porna encoded URL parameter = %q", encodedURL)
	}
}

func TestExtraAdult91PornaExtractsHLSAndMP4FromEmbedPlayerScript(t *testing.T) {
	hls := `document.write('<video src="https://media.example.test/playlist.m3u8?auth=one%2Ftwo"></video>');`
	if got := extraAdult91PornaMediaURL(hls, ninetyOneBaseURL+"/comic/index/embed?id=169397"); got != "https://media.example.test/playlist.m3u8?auth=one%2Ftwo" {
		t.Fatalf("91Porna HLS URL = %q", got)
	}
	mp4 := `document.write('<video src="https://media.example.test/movie.mp4?token=abc"></video>');`
	if got := extraAdult91PornaMediaURL(mp4, ninetyOneBaseURL+"/comic/index/embed?id=169397"); got != "https://media.example.test/movie.mp4?token=abc" {
		t.Fatalf("91Porna MP4 URL = %q", got)
	}
}

func TestExtraAdult91PornaDecryptsEncodedMediaURL(t *testing.T) {
	const media = "https://media.example.test/playlist.m3u8?auth=encrypted"
	plain := []byte(media)
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	for i := 0; i < padding; i++ {
		plain = append(plain, byte(padding))
	}
	block, err := aes.NewCipher([]byte("f5d965df75336270"))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, []byte("97b60394abc2fbe1")).CryptBlocks(ciphertext, plain)

	for name, encoded := range map[string]string{
		"base64": base64.StdEncoding.EncodeToString(ciphertext),
		"hex":    hex.EncodeToString(ciphertext),
	} {
		t.Run(name, func(t *testing.T) {
			body := "<script>var payload = '" + encoded + "';</script>"
			got := extraAdult91PornaMediaURL(body, ninetyOneBaseURL+"/comic/index/embed?id=169397")
			if got != media {
				t.Fatalf("decrypted 91Porna media URL = %q, want %q", got, media)
			}
		})
	}
}

func TestExtraAdult91PornaUsesOfficialDisplayName(t *testing.T) {
	if got := extraAdultSourceName(source91Porna); got != "91porna" {
		t.Fatalf("91Porna display name = %q", got)
	}
}
