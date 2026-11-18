package core

import (
	"net/url"
	"strings"
	"testing"

	htmlnet "golang.org/x/net/html"
)

func TestIkanbotUsesCurrentOfficialHostAndCategoryRoutes(t *testing.T) {
	if ikanbotBaseURL != "https://v.ikanbot.com" {
		t.Fatalf("IkanBot base URL = %q", ikanbotBaseURL)
	}
	got, err := ikanbotCategoryPath(1, "movie-热门")
	if err != nil || got != "/hot/index-movie-热门.html" {
		t.Fatalf("IkanBot page-one route = %q, err=%v", got, err)
	}
	got, err = ikanbotCategoryPath(2, "movie-热门")
	if err != nil || got != "/hot/index-movie-热门-p-2.html" {
		t.Fatalf("IkanBot page-two route = %q, err=%v", got, err)
	}
}

func TestIkanbotParsesCurrentCardLinksWithoutLegacyItemClass(t *testing.T) {
	body := `<div class="module-list">
		<div class="module-item">
			<a href="/play/1023899" title="黑球">
				<img data-original="//img.example.test/black-ball.webp" alt="黑球海報">
				<span>黑球</span>
			</a>
		</div>
		<div class="module-item">
			<a href="https://v.ikanbot.com/play/1023900">
				<img data-src="/covers/second.webp" alt="第二部作品">
				<p>第二部作品</p>
			</a>
		</div>
		<div class="module-item">
			<a href="https://evil.example/play/999">
				<img src="/not-trusted.webp" alt="外站作品">
			</a>
		</div>
	</div>`
	doc, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	items := ikanbotParseCards(doc, "https://v.ikanbot.com/hot/index-movie-热门.html", "movie-热门")
	if len(items) != 2 {
		t.Fatalf("IkanBot parsed %d cards, want 2: %#v", len(items), items)
	}
	if items[0].SourceID != "1023899" || items[0].Title != "黑球" ||
		items[0].Cover != "https://img.example.test/black-ball.webp" {
		t.Fatalf("first IkanBot card = %#v", items[0])
	}
	if items[1].SourceID != "1023900" || items[1].Title != "第二部作品" ||
		items[1].Cover != "https://v.ikanbot.com/covers/second.webp" {
		t.Fatalf("second IkanBot card = %#v", items[1])
	}
}

func TestIkanbotImageSupportsLazyImageAttributes(t *testing.T) {
	body := `<img data-lazy-src="/covers/lazy.webp" src="data:image/gif;base64,placeholder">`
	doc, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var image *htmlnet.Node
	for _, node := range providerHTMLNodes(doc, func(n *htmlnet.Node) bool {
		return n.Type == htmlnet.ElementNode && n.Data == "img"
	}) {
		image = node
		break
	}
	if got := ikanbotImage(image, ikanbotBaseURL+"/play/1023899"); got != ikanbotBaseURL+"/covers/lazy.webp" {
		t.Fatalf("IkanBot lazy cover = %q", got)
	}
}

func TestIkanbotPlaybackAPIUsesResolvedPageHost(t *testing.T) {
	for _, host := range []string{"v.ikanbot.com", "v.aikanbot.com", "www.ikanbot.com"} {
		got, err := ikanbotPlaybackAPIURL(
			"https://"+host+"/play/1023900",
			"1023900",
			"2",
			"722ae90e3a9be1a774fa099083c3cd15",
		)
		if err != nil {
			t.Fatalf("IkanBot API URL for %s: %v", host, err)
		}
		parsed, err := url.Parse(got)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		if parsed.Host != host || parsed.Path != "/api/getResN" ||
			query.Get("videoId") != "1023900" ||
			query.Get("mtype") != "2" ||
			query.Get("token") != "722ae90e3a9be1a774fa099083c3cd15" {
			t.Fatalf("unexpected IkanBot API URL: %s", got)
		}
	}
}

func TestIkanbotPlaybackAPIRejectsUntrustedPageHosts(t *testing.T) {
	for _, page := range []string{
		"http://v.ikanbot.com/play/853970",
		"https://evil.example/play/853970",
		"https://img1.ikanbot.com/play/853970",
	} {
		if _, err := ikanbotPlaybackAPIURL(page, "853970", "2", "signed-token"); err == nil {
			t.Fatalf("IkanBot API builder accepted untrusted page %q", page)
		}
	}
}

func TestIkanbotPlaybackUsesSourceVideoIDButSignsCurrentID(t *testing.T) {
	currentID := "853970"
	sourceID := "1023900"
	token := "mre0530ce88964487488y67d38c0a1uj7fd15cb8"
	signed := ikanbotSignToken(currentID, token)
	if signed == "" {
		t.Fatal("IkanBot current_id token signature was empty")
	}
	address, err := ikanbotPlaybackAPIURL(
		"https://v.ikanbot.com/play/"+sourceID,
		sourceID,
		"2",
		signed,
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("videoId") != sourceID {
		t.Fatalf("API videoId = %q, want source ID %q", parsed.Query().Get("videoId"), sourceID)
	}
	if parsed.Query().Get("token") != signed {
		t.Fatal("API did not use the signature derived from current_id")
	}
}
