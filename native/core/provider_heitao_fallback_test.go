package core

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestHeitaoExactCatalogIDRejectsFuzzyAndAmbiguousMatches(t *testing.T) {
	items := []Drama{
		{ID: "heitao:200", Source: sourceHeitao, SourceID: "200", Title: "歡喜禪院"},
		{ID: "heitao:201", Source: sourceHeitao, SourceID: "201", Title: "歡喜禪院番外"},
	}
	if got, ok := heitaoExactCatalogID(items, "歡喜禪院"); !ok || got != "200" {
		t.Fatalf("exact title match = %q, %t", got, ok)
	}
	if got, ok := heitaoExactCatalogID(items, "歡喜禪"); ok || got != "" {
		t.Fatalf("partial title unexpectedly matched: %q, %t", got, ok)
	}
	items = append(items, Drama{
		ID: "heitao:202", Source: sourceHeitao, SourceID: "202", Title: "歡喜禪院",
	})
	if got, ok := heitaoExactCatalogID(items, "歡喜禪院"); ok || got != "" {
		t.Fatalf("ambiguous exact title unexpectedly selected ID %q", got)
	}
}

func TestHeitaoPlayerRoutePartsUsesActualProviderID(t *testing.T) {
	id, sid, nid, ok := heitaoPlayerRouteParts(
		"https://heitaodj.com/index.php/vod/player/id/200/nid/12/sid/3.html",
		"https://heitaodj.com",
	)
	if !ok || id != "200" || sid != 3 || nid != 12 {
		t.Fatalf("player route parsed as id=%q sid=%d nid=%d ok=%t", id, sid, nid, ok)
	}
	if _, _, _, ok := heitaoPlayerRouteParts(
		"https://evil.example.test/index.php/vod/player/id/200/nid/12/sid/3.html",
		"https://heitaodj.com",
	); ok {
		t.Fatal("external host must not replace the provider ID")
	}
}

func TestHeitao404RecoversOnlyUniqueExactTitleAndKeepsStableIdentity(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Cookie") != heitaoAgeCookie {
			t.Fatalf("Heitao age confirmation cookie missing for %s", request.URL.String())
		}
		switch {
		case strings.HasSuffix(request.URL.Path, "/vod/detail/id/100.html"):
			return sourceFixtureResponse(request, http.StatusNotFound, "not found"), nil
		case strings.Contains(request.URL.Path, "/vod/search/"):
			return sourceFixtureResponse(request, http.StatusOK, `<!doctype html><html><body>
<a class="thumb" href="/index.php/vod/detail/id/200.html" title="歡喜禪院">
  <img src="https://img.example.test/200.jpg" alt="歡喜禪院">
</a>
<a class="thumb" href="/index.php/vod/detail/id/201.html" title="歡喜禪院番外">
  <img src="https://img.example.test/201.jpg" alt="歡喜禪院番外">
</a>
</body></html>`), nil
		case strings.HasSuffix(request.URL.Path, "/vod/detail/id/200.html"):
			return sourceFixtureResponse(request, http.StatusOK, `<!doctype html><html><head>
<script type="application/ld+json">{"@type":"TVSeries","name":"歡喜禪院","description":"測試簡介"}</script>
</head><body>
<a href="/index.php/vod/play/id/200/sid/1/nid/1.html">第1集</a>
</body></html>`), nil
		default:
			t.Fatalf("unexpected Heitao fixture request: %s", request.URL.String())
			return nil, nil
		}
	})

	raw, chapters, err := d.fetchHeitaoDetailWithFallback(
		context.Background(), "100", "100", "歡喜禪院",
	)
	if err != nil {
		t.Fatal(err)
	}
	if raw.ID != "heitao:100" || raw.SourceID != "200" || raw.Title != "歡喜禪院" {
		t.Fatalf("fallback did not preserve stable ID and update provider ID: %+v", raw)
	}
	if len(chapters) != 1 {
		t.Fatalf("expected one recovered episode, got %+v", chapters)
	}
	if chapters[0].ID != "heitao:100:1:1" ||
		!strings.Contains(chapters[0].PageURL, "/vod/player/id/200/") {
		t.Fatalf("recovered chapter did not retain stable key + new route: %+v", chapters[0])
	}
}


func TestHeitaoCoverRecoveryUsesExactStaleIDFallback(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Cookie") != heitaoAgeCookie {
			t.Fatalf("Heitao age confirmation cookie missing for %s", request.URL.String())
		}
		switch {
		case strings.HasSuffix(request.URL.Path, "/vod/detail/id/100.html"):
			return sourceFixtureResponse(request, http.StatusNotFound, "not found"), nil
		case strings.Contains(request.URL.Path, "/vod/search/"):
			return sourceFixtureResponse(request, http.StatusOK, `<!doctype html><html><body>
<a class="thumb" href="/index.php/vod/detail/id/200.html" title="歡喜禪院">
  <img src="https://img.example.test/200.jpg" alt="歡喜禪院">
</a>
<a class="thumb" href="/index.php/vod/detail/id/201.html" title="歡喜禪院番外">
  <img src="https://img.example.test/201.jpg" alt="歡喜禪院番外">
</a>
</body></html>`), nil
		case strings.HasSuffix(request.URL.Path, "/vod/detail/id/200.html"):
			return sourceFixtureResponse(request, http.StatusOK, `<!doctype html><html><head>
<script type="application/ld+json">{"@type":"TVSeries","name":"歡喜禪院","description":"測試簡介","image":"https://img.example.test/200.jpg"}</script>
</head><body>
<a href="/index.php/vod/play/id/200/sid/1/nid/1.html">第1集</a>
</body></html>`), nil
		default:
			t.Fatalf("unexpected Heitao poster recovery request: %s", request.URL.String())
			return nil, nil
		}
	})
	drama := nativeDrama{
		ID: "heitao:100", Source: sourceHeitao, SourceID: "100",
		Title: "歡喜禪院", Cover: "https://img.example.test/old.jpg",
	}
	addresses, _, err := d.nativeCoverRecoveryMetadata(context.Background(), &drama)
	if err != nil {
		t.Fatal(err)
	}
	if len(addresses) == 0 || addresses[0] != "https://img.example.test/200.jpg" {
		t.Fatalf("Heitao cover recovery = %#v, want official replacement poster", addresses)
	}
}
