package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

func TestRestoreHongguoCatalogKeepsWebsiteHistoryCursorBeyondPage500(t *testing.T) {
	downloader := &Downloader{}
	state := &hongguoCatalogState{
		Version: 1,
		DeviceID: "1001",
		InstallID: "10019",
		Feeds: map[string]hongguoCatalogCursor{
			"web:real-drama": {
				Offset: 501, Initialized: true, UpdatedAt: time.Now(),
			},
		},
	}
	downloader.restoreHongguoCatalog(state)
	got := downloader.hongguoCatalogSnapshot()
	cursor, ok := got.Feeds["web:real-drama"]
	if !ok || !cursor.Initialized || cursor.Offset != 501 {
		t.Fatalf("website history cursor at page 501 was not restored: %#v", got.Feeds["web:real-drama"])
	}
}


func TestHongguoWebsiteHistoryContinuesBeyondPage500(t *testing.T) {
	var calls int
	bodyData := map[string]any{"loaderData": map[string]any{"category_page": map[string]any{
		"isSuccess": true,
		"pagination": map[string]any{"totalPages": 501},
		"recommendList": func() []any {
			rows := make([]any, 24)
			for i := range rows {
				rows[i] = map[string]any{"series_id": fmt.Sprintf("900%03d", i), "series_title": fmt.Sprintf("第 501 頁劇集 %d", i)}
			}
			return rows
		}(),
	}}}}
	encoded, err := json.Marshal(bodyData)
	if err != nil {
		t.Fatal(err)
	}
	body := "window._ROUTER_DATA = " + string(encoded) + ";"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Host != "hongguoduanju.com" || request.URL.Path != "/category/real-drama" || request.URL.Query().Get("page") != "501" {
			t.Fatalf("unexpected website history request: %s", request.URL.String())
		}
		return sourceFixtureResponse(request, http.StatusOK, body), nil
	})
	client := d.hongguoClient()
	client.mu.Lock()
	client.state.Feeds = make(map[string]hongguoCatalogCursor)
	for _, route := range hongguoWebCatalogRoutes {
		client.state.Feeds["web:"+route.path] = hongguoCatalogCursor{Initialized: true, Exhausted: route.path != "real-drama", Offset: 500, PageSignature: "previous-page", UpdatedAt: time.Now()}
	}
	client.mu.Unlock()

	items, err := d.fetchHongguoWebsiteCatalogPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(items) != 24 {
		t.Fatalf("page 501 was not loaded on demand: requests=%d items=%d", calls, len(items))
	}
	cursor := d.hongguoCatalogSnapshot().Feeds["web:real-drama"]
	if cursor.Offset != 501 || cursor.Exhausted {
		t.Fatalf("website history stopped at page 501: %+v", cursor)
	}
}

func TestHongguoSourceUpdateDoesNotScanWebsiteHistory(t *testing.T) {
	state := &hongguoCatalogState{Version: 1, Feeds: map[string]hongguoCatalogCursor{}}
	for _, genre := range hongguoAppGenres {
		state.Feeds[genre.key] = hongguoCatalogCursor{Initialized: true, Exhausted: true}
	}
	for _, route := range hongguoWebCatalogRoutes {
		state.Feeds["web:"+route.path] = hongguoCatalogCursor{}
	}

	updateCtx := context.WithValue(context.Background(), hongguoSourceUpdateKey{}, true)
	if shouldFetchHongguoWebsiteHistory(updateCtx, "", state) {
		t.Fatal("Red Fruit source update should not start website-history scanning")
	}
	if hongguoCatalogHasMoreForContext(updateCtx, "", state) {
		t.Fatal("Red Fruit source update should stop when App API feeds are exhausted")
	}

	browseCtx := context.Background()
	if !shouldFetchHongguoWebsiteHistory(browseCtx, "", state) {
		t.Fatal("normal catalog browsing should retain website-history continuation")
	}
	if !hongguoCatalogHasMoreForContext(browseCtx, "", state) {
		t.Fatal("normal catalog browsing should report website history as more content")
	}
}
