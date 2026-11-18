package core

import (
	"strings"
	"testing"

	htmlnet "golang.org/x/net/html"
)

func TestIkanbotUsesCurrentOfficialHostAndCategoryRoutes(t *testing.T) {
	if ikanbotBaseURL != "https://www.ikanbot.com" {
		t.Fatalf("IkanBot base URL = %q", ikanbotBaseURL)
	}
	got, err := ikanbotCategoryPath(1, "movie-熱門")
	if err != nil || got != "/hot/index-movie-熱門.html" {
		t.Fatalf("IkanBot page-one route = %q, err=%v", got, err)
	}
	got, err = ikanbotCategoryPath(2, "movie-熱門")
	if err != nil || got != "/hot/index-movie-熱門-p-2.html" {
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
			<a href="https://www.ikanbot.com/play/1023900">
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
	items := ikanbotParseCards(doc, "https://www.ikanbot.com/hot/index-movie-熱門.html", "movie-熱門")
	if len(items) != 2 {
		t.Fatalf("IkanBot parsed %d cards, want 2: %#v", len(items), items)
	}
	if items[0].SourceID != "1023899" || items[0].Title != "黑球" ||
		items[0].Cover != "https://img.example.test/black-ball.webp" {
		t.Fatalf("first IkanBot card = %#v", items[0])
	}
	if items[1].SourceID != "1023900" || items[1].Title != "第二部作品" ||
		items[1].Cover != "https://www.ikanbot.com/covers/second.webp" {
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
