package core

import (
	"context"
	"testing"
	"time"
)

func TestHongguoCatalogContinuesWithWebsiteAfterAppFeedsExhaust(t *testing.T) {
	state := &hongguoCatalogState{Version: 1, Feeds: map[string]hongguoCatalogCursor{}}
	for _, genre := range hongguoAppGenres {
		state.Feeds[genre.key] = hongguoCatalogCursor{Initialized: true, Exhausted: true}
	}
	if hongguoAppCatalogHasMore(state) {
		t.Fatal("App API feeds should be exhausted")
	}
	if !hongguoCatalogHasMore(state) {
		t.Fatal("website catalog feeds should keep the overall catalog open")
	}

	for _, route := range hongguoWebCatalogRoutes {
		state.Feeds["web:"+route.path] = hongguoCatalogCursor{Initialized: true, Exhausted: true}
	}
	if hongguoCatalogHasMore(state) {
		t.Fatal("catalog should finish after all App API and website feeds are exhausted")
	}
}

func TestRestoreHongguoCatalogKeepsWebsiteHistoryCursor(t *testing.T) {
	downloader := &Downloader{}
	state := &hongguoCatalogState{
		Version: 1,
		DeviceID: "1001",
		InstallID: "10019",
		Feeds: map[string]hongguoCatalogCursor{
			"web:real-drama": {
				Offset: 17, Initialized: true, UpdatedAt: time.Now(),
			},
		},
	}
	downloader.restoreHongguoCatalog(state)
	got := downloader.hongguoCatalogSnapshot()
	cursor, ok := got.Feeds["web:real-drama"]
	if !ok || !cursor.Initialized || cursor.Offset != 17 {
		t.Fatalf("website history cursor was not restored: %#v", got.Feeds["web:real-drama"])
	}
}

func TestHongguoWebsiteHistoryRequiresExplicitBackfillContext(t *testing.T) {
	state := &hongguoCatalogState{Version: 1, Feeds: map[string]hongguoCatalogCursor{}}
	for _, genre := range hongguoAppGenres {
		state.Feeds[genre.key] = hongguoCatalogCursor{Initialized: true, Exhausted: true}
	}
	plain := context.Background()
	if shouldFetchHongguoWebsiteHistory(plain, "", state) {
		t.Fatal("normal catalog reads and health checks must not start a website history scan")
	}
	enabled := context.WithValue(plain, libraryWebsiteBackfillKey{}, true)
	if shouldFetchHongguoWebsiteHistory(enabled, "short_play", state) {
		t.Fatal("category-specific reads must not start the full website history scan")
	}
	state.Feeds[hongguoAppGenres[0].key] = hongguoCatalogCursor{Initialized: true, Exhausted: false}
	if shouldFetchHongguoWebsiteHistory(enabled, "", state) {
		t.Fatal("website history must wait until App API feeds are exhausted")
	}
	state.Feeds[hongguoAppGenres[0].key] = hongguoCatalogCursor{Initialized: true, Exhausted: true}
	if !shouldFetchHongguoWebsiteHistory(enabled, "", state) {
		t.Fatal("explicitly enabled backfill should run after App API exhaustion")
	}
}
