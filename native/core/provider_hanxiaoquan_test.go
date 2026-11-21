package core

import (
	"testing"
	"strings"
)

func TestHanxiaoquanImageCandidatesHandleLazyAndConfiguredHosts(t *testing.T) {
	fragment := `<div class="video-cover">
		<img src="/loading.gif" data-original="/posters/original.webp"
			data-src="/posters/lazy.jpg"
			data-srcset="/posters/small.jpg 320w, /posters/large.jpg 1200w">
		<img style="background-image:url('/posters/background.jpg')" src="data:image/gif;base64,AAAA">
	</div>`
	got := hanxiaoquanImageCandidates(fragment, "https://mirror.example")
	want := []string{
		"https://mirror.example/posters/original.webp",
		"https://mirror.example/posters/lazy.jpg",
		"https://mirror.example/loading.gif",
		"https://mirror.example/posters/small.jpg",
		"https://mirror.example/posters/large.jpg",
		"https://mirror.example/posters/background.jpg",
	}
	if len(got) != len(want) {
		t.Fatalf("Hanxiaoquan candidate list = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("Hanxiaoquan candidate %d = %q, want %q (%#v)", index, got[index], want[index], got)
		}
	}
}

func TestHanxiaoquanCardAndDetailKeepPosterBackups(t *testing.T) {
	catalog := `<div class="module-item">
		<a href="/hanxiaoquan/123.html" title="測試韓劇" class="module-item-pic">
			<img src="/loading.gif" data-original="/poster/catalog-original.webp" data-src="/poster/catalog-lazy.jpg">
		</a><a href="/hanxiaoquan/123.html" class="module-item-title">測試韓劇</a>
	</div>`
	items := parseHanxiaoquanCards(catalog, "https://mirror.example")
	if len(items) != 1 {
		t.Fatalf("Hanxiaoquan cards = %d, want 1", len(items))
	}
	if items[0].Cover != "https://mirror.example/poster/catalog-original.webp" ||
		len(items[0].CoverCandidates) < 2 ||
		items[0].CoverCandidates[0] != "https://mirror.example/poster/catalog-lazy.jpg" {
		t.Fatalf("Hanxiaoquan catalog poster backups = %#v", items[0])
	}

	detail := `<meta content="/meta/poster.webp" property="og:image">
		<div class="video-cover"><img src="/loading.gif" data-original="/detail/original.jpg" data-src="/detail/lazy.webp"></div>`
	covers := hanxiaoquanDetailCoverCandidates(detail, "https://mirror.example")
	if len(covers) < 4 ||
		covers[0] != "https://mirror.example/meta/poster.webp" ||
		covers[1] != "https://mirror.example/detail/original.jpg" ||
		covers[2] != "https://mirror.example/detail/lazy.webp" {
		t.Fatalf("Hanxiaoquan detail poster candidates = %#v", covers)
	}
	if strings.Contains(strings.Join(covers, "|"), "https://www.jennyhow.com") {
		t.Fatalf("configured host was ignored for relative poster URLs: %#v", covers)
	}
}
