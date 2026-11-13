package core

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func heitaoTestDocument(t *testing.T, source string) *html.Node {
	t.Helper()
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return document
}

func TestParseHeitaoCardsKeepsPosterAndEpisodeBadge(t *testing.T) {
	document := heitaoTestDocument(t, `<!doctype html><html><body>
<a class="thumb" href="/index.php/vod/detail/id/123.html" title="測試短劇">
  <img data-src="//img.example.test/poster.webp" alt="測試短劇">
  <span class="label">6集</span>
</a>
<a class="thumb" href="/index.php/vod/detail/id/124.html" title="完整版短劇">
  <img src="/poster/full.jpg">
  <span class="label">完整版</span>
</a>
<a class="thumb" href="https://other.example.test/index.php/vod/detail/id/125.html" title="外站不得納入">
  <img src="/bad.jpg">
</a>
</body></html>`)
	items := parseHeitaoCards(document, "https://heitaodj.com/index.php/vod/show.html")
	if len(items) != 2 {
		t.Fatalf("expected two same-site cards, got %d", len(items))
	}
	if items[0].ID != "heitao:123" || items[0].Cover != "https://img.example.test/poster.webp" || items[0].Badge != "6集" || items[0].EpisodeCount != 6 {
		t.Fatalf("first Heitao card metadata mismatch: %+v", items[0])
	}
	if items[1].ID != "heitao:124" || items[1].Badge != "完整版" {
		t.Fatalf("second Heitao card badge mismatch: %+v", items[1])
	}
}

func TestParseHeitaoCardsRecognizesLazyArtworkAttributes(t *testing.T) {
	for _, attribute := range []string{
		"data-lazy-src", "data-original-src", "data-url", "data-echo",
	} {
		t.Run(attribute, func(t *testing.T) {
			source := "<!doctype html><html><body>" +
				"<a class=\"thumb\" href=\"/index.php/vod/detail/id/321.html\" title=\"延遲海報\">" +
				"<img " + attribute + "=\"/poster/lazy.webp\" alt=\"延遲海報\">" +
				"</a></body></html>"
			document := heitaoTestDocument(t, source)
			items := parseHeitaoCards(document, "https://heitaodj.com/index.php/vod/show.html")
			if len(items) != 1 || items[0].Cover != "https://heitaodj.com/poster/lazy.webp" {
				t.Fatalf("lazy attribute %q was not parsed: %+v", attribute, items)
			}
		})
	}
}

func TestHeitaoCategoriesAndChapterParsing(t *testing.T) {
	document := heitaoTestDocument(t, `<!doctype html><html><body>
<a href="/index.php/vod/type/id/1.html">甜寵</a>
<a href="/index.php/vod/type/id/2.html">古裝</a>
<a href="/index.php/vod/type/id/not-number.html">無效分類</a>
<a href="/index.php/vod/play/id/123/sid/1/nid/1.html">第1集</a>
<a href="/index.php/vod/play/id/123/sid/1/nid/2.html">第2集</a>
<a href="/index.php/vod/play/id/123/sid/2/nid/1.html">另一條線路第1集</a>
<a href="/index.php/vod/play/id/999/sid/1/nid/1.html">其他劇集</a>
</body></html>`)
	site := "https://heitaodj.com"
	categories := []string{}
	for _, anchor := range providerHTMLNodes(document, func(node *html.Node) bool { return node.Data == "a" }) {
		action, values, valid := dsdRouteParameters(site+"/", providerHTMLAttr(anchor, "href"))
		id, name := values["id"], strings.TrimSpace(providerHTMLText(anchor))
		if valid && action == "type" && validHeitaoCategory(id) && id != "" && name != "" {
			categories = append(categories, id+":"+name)
		}
	}
	if strings.Join(categories, ",") != "1:甜寵,2:古裝" {
		t.Fatalf("unexpected Heitao category extraction: %v", categories)
	}
	chapters := heitaoChapters(document, site, "123")
	if len(chapters) != 2 {
		t.Fatalf("expected two episodes from selected line, got %d: %+v", len(chapters), chapters)
	}
	if chapters[0].ID != "heitao:123:1:1" || chapters[1].ID != "heitao:123:1:2" {
		t.Fatalf("unexpected Heitao episode IDs: %+v", chapters)
	}
}

func TestHeitaoPlayerConfigAndEncryptedURLs(t *testing.T) {
	entry, err := heitaoReadPlayerConfig(`var player_aaaa = {"url":"https://media.example.test/a.m3u8","encrypt":0};`)
	if err != nil || entry.URL != "https://media.example.test/a.m3u8" {
		t.Fatalf("plain player config parse failed: %+v %v", entry, err)
	}
	encodedPath := "%2Fmedia%2Fepisode%2F1.m3u8"
	decoded, err := heitaoDecryptMediaURL(encodedPath, 1)
	if err != nil || decoded != "/media/episode/1.m3u8" {
		t.Fatalf("path-unescape mode failed: %q %v", decoded, err)
	}
	base64Path := base64.StdEncoding.EncodeToString([]byte(encodedPath))
	decoded, err = heitaoDecryptMediaURL(base64Path, 2)
	if err != nil || decoded != "/media/episode/1.m3u8" {
		t.Fatalf("base64 mode failed: %q %v", decoded, err)
	}
	if _, err := heitaoReadPlayerConfig("player_aaaa = {};"); err == nil {
		t.Fatal("empty player URL should be rejected")
	}
	if _, err := heitaoDecryptMediaURL("invalid", 9); err == nil {
		t.Fatal("unknown encryption type should be rejected")
	}
}

func TestHeitaoRouteAndIdentifierValidation(t *testing.T) {
	if !validHeitaoCategory("") || !validHeitaoCategory("12") || validHeitaoCategory("abc") || validHeitaoCategory("1234567") {
		t.Fatal("Heitao category validation changed")
	}
	sid, nid, ok := heitaoSeparateEpisode("3:12")
	if !ok || sid != 3 || nid != 12 {
		t.Fatalf("episode key parser returned %d:%d, %t", sid, nid, ok)
	}
	if _, _, ok := heitaoSeparateEpisode("0:12"); ok {
		t.Fatal("zero source line should be rejected")
	}
	sid, nid, ok = heitaoPlayerID("https://heitaodj.com/index.php/vod/player/id/123/nid/12/sid/3.html")
	if !ok || sid != 3 || nid != 12 {
		t.Fatalf("player route parser returned %d:%d, %t", sid, nid, ok)
	}
}


func TestHeitaoSynopsisFallsBackFromJSONLDToMetaAndDetailContent(t *testing.T) {
	document := heitaoTestDocument(t, `<!doctype html><html><head>
<meta property="og:description" content="&lt;p&gt;官方簡介&amp;amp;補充&lt;/p&gt;">
<script type="application/ld+json">
{"@context":"https://schema.org","@graph":[
  {"@type":"WebSite","name":"黑桃"},
  {"@type":"TVSeries","name":"測試劇","image":"https://img.example.test/poster.jpg"}
]}
</script>
</head><body><div class="module-info-content"><p>不應覆蓋 meta description</p></div></body></html>`)
	metadata := heitaoSeriesMetadata(document)
	if metadata.Name != "測試劇" {
		t.Fatalf("TVSeries was not found inside @graph: %+v", metadata)
	}
	if got, want := heitaoSynopsis(document, metadata), "官方簡介&補充"; got != want {
		t.Fatalf("synopsis fallback = %q, want %q", got, want)
	}
}

func TestHeitaoSynopsisFallsBackToDetailContentAndIgnoresEmptyMarkup(t *testing.T) {
	document := heitaoTestDocument(t, `<!doctype html><html><head></head><body>
<div class="module-info-content"><p>第一段</p><p>第二段 &amp; 更多</p><p> </p></div>
</body></html>`)
	if got, want := heitaoSynopsis(document, heitaoSeriesMeta{}), "第一段 第二段 & 更多"; got != want {
		t.Fatalf("detail synopsis = %q, want %q", got, want)
	}

	empty := heitaoTestDocument(t, `<!doctype html><html><body><div class="module-info-content"><p> </p></div></body></html>`)
	if got := heitaoSynopsis(empty, heitaoSeriesMeta{}); got != "" {
		t.Fatalf("empty detail markup returned visible synopsis %q", got)
	}
}


func TestHeitaoPageRequestsCarryAgeConfirmationCookie(t *testing.T) {
	ctx := heitaoPageContext(context.Background())
	value, ok := ctx.Value(providerTextCookieKey{}).(string)
	if !ok || value != heitaoAgeCookie {
		t.Fatalf("Heitao page cookie = %q, want %q", value, heitaoAgeCookie)
	}
}
