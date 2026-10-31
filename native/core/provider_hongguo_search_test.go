package core

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestHongguoSearchTextNormalizesTraditionalAndSimplifiedTitles(t *testing.T) {
	pairs := [][2]string{
		{"万妖图录传第二季", "萬妖圖錄傳第二季"},
		{"发型", "髮型"},
		{"面条", "麵條"},
		{"后宫", "後宮"},
	}
	for _, pair := range pairs {
		if left, right := hongguoSearchText(pair[0]), hongguoSearchText(pair[1]); left != right {
			t.Errorf("search normalization differs for %q and %q: %q != %q", pair[0], pair[1], left, right)
		}
	}
}

func TestHongguoSearchSeasonGroupsMatchTraditionalAndSimplifiedTitles(t *testing.T) {
	dramas := []Drama{
		{ID: "hongguo:101", Title: "萬妖圖錄傳第一季"},
		{ID: "hongguo:103", Title: "万妖图录传第三季"},
	}
	groups := hongguoSearchSeriesGroups("万妖图录传", dramas)
	if len(groups) != 1 {
		t.Fatalf("expected one incomplete multi-season group, got %d", len(groups))
	}
	if next := groups[0].nextQuery(map[string]bool{"万妖图录传": true}); next == "" {
		t.Fatal("traditional/simplified season group did not produce a missing-season query")
	}
}

func TestHongguoSearchPageContinuesUsingServerCursor(t *testing.T) {
	var queries []url.Values
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != hongguoSearchTabPath {
			t.Fatalf("unexpected request path: %s", request.URL.Path)
		}
		queries = append(queries, request.URL.Query())
		if len(queries) == 1 {
			body := "{\"search_tabs\":[{\"tab_type\":\"29\",\"search_id\":\"search-1\",\"passback\":\"cursor-1\",\"next_offset\":\"10\",\"has_more\":true,\"data\":[{\"video_data\":[{\"series_id\":\"101\",\"name\":\"示例第一季\"}]}]}]}"
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		}
		body := "{\"search_tabs\":[{\"tab_type\":\"29\",\"search_id\":\"search-1\",\"passback\":\"cursor-2\",\"next_offset\":\"20\",\"has_more\":false,\"data\":[{\"video_data\":[{\"series_id\":\"101\",\"name\":\"示例第一季\"},{\"series_id\":\"102\",\"name\":\"示例第二季\"}]}]}]}"
		return sourceFixtureResponse(request, http.StatusOK, body), nil
	})
	d.cfg.Retries = 1
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "1001", "1002", true
	first, err := d.fetchHongguoSearchPage(context.Background(), "示例", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || len(first.Dramas) != 1 {
		t.Fatalf("unexpected first page: %+v", first)
	}
	second, err := d.fetchHongguoSearchPage(context.Background(), "示例", 2)
	if err != nil {
		t.Fatal(err)
	}
	if second.HasMore || len(second.Dramas) != 1 {
		t.Fatalf("unexpected second page: %+v", second)
	}
	if second.Dramas[0].ID != "hongguo:102" {
		t.Fatalf("second page did not filter an ID repeated from page one: %+v", second.Dramas)
	}
	if len(queries) != 2 {
		t.Fatalf("expected two requests, got %d", len(queries))
	}
	next := queries[1]
	if next.Get("offset") != "10" || next.Get("passback") != "cursor-1" || next.Get("search_id") != "search-1" {
		t.Fatalf("second page did not use the server cursor: %#v", next)
	}
}

func TestHongguoSearchPageRejectsExpiredCursor(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Fatalf("expired cursor should not issue a request: %s", request.URL)
		return nil, nil
	})
	client := d.hongguoClient()
	client.searchCursors["示例"] = hongguoSearchCursor{
		Page: 1, HasMore: true, ExpiresAt: time.Now().Add(-time.Second),
	}
	if _, err := d.fetchHongguoSearchPage(context.Background(), "示例", 2); err == nil {
		t.Fatal("expired search cursor was accepted")
	}
}
