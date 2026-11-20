package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestHongguoRegistrationWasRateLimitedOnlyForHTTP444(t *testing.T) {
	for _, tc := range []struct {
		message string
		want bool
	}{
		{message: "取得紅果系列季別失敗：紅果 App 介面未提供有效資料（裝置註冊失敗：紅果設備註冊 HTTP 444）", want: true},
		{message: "紅果設備註冊 HTTP 403", want: false},
		{message: "紅果設備註冊 HTTP 429", want: false},
		{message: "context deadline exceeded", want: false},
		{message: "", want: false},
	} {
		var err error
		if tc.message != "" {
			err = errors.New(tc.message)
		}
		if got := hongguoRegistrationWasRateLimited(err); got != tc.want {
			t.Errorf("hongguoRegistrationWasRateLimited(%q) = %v, want %v", tc.message, got, tc.want)
		}
	}
}

func TestHongguoCurrentSeasonFallbackKeepsKnownEpisodeCountAndDoesNotInventSiblings(t *testing.T) {
	drama := nativeDrama{
		ID: "hongguo:7690163276214176792",
		Source: sourceHongguo,
		Title: "少夫人來自東北3",
		Episodes: 96,
		SeriesSeasonNumber: 3,
		SeriesSeasonUnit: "季",
	}
	items := hongguoCurrentSeasonFallback(drama)
	if len(items) != 1 {
		t.Fatalf("fallback should preserve only the currently known season: %+v", items)
	}
	item := items[0]
	if item.Drama.ID != drama.ID || item.Drama.Episodes != 96 ||
		item.Number != 3 || item.Label != "第3季" ||
		!item.Playable || item.Upcoming {
		t.Fatalf("current season fallback lost known playable metadata: %+v", item)
	}
}

func TestHongguoAppSeasonQueryUsesOfficialSelector(t *testing.T) {
	const seriesID = "7690163276214176792"
	query := hongguoAppSeasonQuery(seriesID)
	expected := map[string]string{
		"scene": "10",
		"from": "single_col_same_ip_series",
		"book_id": seriesID,
		"filter_ids": seriesID,
		"user_is_login": "0",
		"dragon_device_type": "phone",
		"pv_player": "74332",
		"compliance_status": "0",
		"need_personal_recommend": "1",
		"player_so_load": "1",
		"is_android_pad_screen": "0",
	}
	for key, value := range expected {
		if query.Get(key) != value {
			t.Fatalf("query %q = %q, want %q", key, query.Get(key), value)
		}
	}
	for _, key := range []string{"session_uuid", "cdid", "openudid"} {
		if query.Get(key) == "" {
			t.Fatalf("required plan request field %q is missing: %v", key, query)
		}
	}
	if _, err := url.ParseQuery(query.Encode()); err != nil {
		t.Fatal(err)
	}
}

func TestHongguoAppSeasonsUsesVersionedPlanEndpointAndRequiredHeaders(t *testing.T) {
	const seriesID = "7690163276214176792"
	var calls int
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet || request.URL.Path != hongguoAppSeasonPlanPath {
			t.Fatalf("season request = %s %s, want GET %s", request.Method, request.URL.Path, hongguoAppSeasonPlanPath)
		}
		query := request.URL.Query()
		for key, want := range map[string]string{
			"from": "single_col_same_ip_series", "scene": "10",
			"book_id": seriesID, "filter_ids": seriesID, "user_is_login": "0",
		} {
			if query.Get(key) != want { t.Errorf("query %s=%q, want %q", key, query.Get(key), want) }
		}
		for _, key := range []string{"session_uuid", "cdid", "openudid"} {
			if query.Get(key) == "" { t.Errorf("missing query %s: %v", key, query) }
		}
		if request.Header.Get("x-ss-dp") != "8662" || request.Header.Get("lc") != "101" {
			t.Errorf("missing reading app headers: %v", request.Header)
		}
		if request.Header.Get("x-reading-request") == "" {
			t.Error("missing x-reading-request header")
		}
		body := `{"code":0,"data":[{"show_type":547,"video_data":[{"series_id":"12345678","vid":"12345679","title":"測試系列第一季","episode_cnt":12,"secondary_info_list":[{"content":"第1季"}]}]}]}`
		return sourceFixtureResponse(request, http.StatusOK, body), nil
	})
	d.cfg.Retries = 1
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "9001", "9002", true
	items, err := d.hongguoAppSeasons(context.Background(), seriesID)
	if err != nil { t.Fatal(err) }
	if calls != 1 || len(items) != 1 || items[0].Drama.ID != "hongguo:12345678" || items[0].Number != 1 {
		t.Fatalf("season result mismatch: calls=%d items=%+v", calls, items)
	}
}
func TestParseHongguoAppSeasonsKeepsOfficialIDsAndUpcomingRows(t *testing.T) {
	raw := `{
		"code": 0,
		"data": [{
			"show_type": 547,
			"video_data": [
				{
					"series_id": "7621200213801176126",
					"vid": "7621200213801176127",
					"title": "萬妖圖錄傳第一季",
					"series_cover": "https://img.example.test/season-1.jpg",
					"episode_cnt": 81,
					"secondary_info_list": [{"content":"第1季"}],
					"sub_title_list": [{"content":"已完結"},{"content":"81集"}]
				},
				{
					"series_id": "7628202881140657177",
					"vid": "7628204794481495065",
					"title": "萬妖圖錄傳第二季",
					"episode_cnt": 53,
					"secondary_info_list": [{"content":"第2季"}],
					"sub_title_list": [{"content":"更新中"}]
				},
				{
					"series_id": "7691518344146455576",
					"vid": "7691520694017854526",
					"title": "萬妖圖錄傳第十四季",
					"episode_cnt": 1,
					"subscribe_item": {"item_id":"7691518344146455577"},
					"secondary_info_list": [{"content":"第14季"}],
					"sub_title_list": [{"content":"即將上線"}]
				},
				{
					"series_id": "7777777777777777777",
					"vid": "12345",
					"title": "不可播放季",
					"episode_cnt": 20,
					"disable_play": true,
					"secondary_info_list": [{"content":"第15季"}]
				}
			]
		}]
	}`
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	items, err := parseHongguoAppSeasons(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("expected four official rows, got %d", len(items))
	}
	if items[0].Drama.SourceID != "7621200213801176126" ||
		items[0].Drama.ID != "hongguo:7621200213801176126" ||
		items[0].Drama.SeriesGroupID != "7621200213801176126" ||
		items[1].Drama.SeriesGroupID != items[0].Drama.SeriesGroupID ||
		items[2].Drama.SeriesGroupID != items[0].Drama.SeriesGroupID ||
		items[3].Drama.SeriesGroupID != items[0].Drama.SeriesGroupID ||
		items[0].Number != 1 || items[0].Label != "第1季" ||
		!items[0].Playable || items[0].Upcoming {
		t.Fatalf("first season identity or metadata lost: %+v", items[0])
	}
	if items[1].Drama.SourceID != "7628202881140657177" ||
		items[1].Number != 2 || items[1].Drama.Episodes != 53 ||
		!items[1].Playable {
		t.Fatalf("second season metadata lost: %+v", items[1])
	}
	if !items[2].Upcoming || items[2].Playable ||
		items[2].Drama.Episodes != 0 || items[2].Number != 14 {
		t.Fatalf("reserved season must be shown but not played: %+v", items[2])
	}
	if items[3].Playable || items[3].Upcoming || items[3].Number != 15 {
		t.Fatalf("disabled season should remain visible but unplayable: %+v", items[3])
	}
}

func TestParseHongguoAppSeasonsIgnoresNonSeasonPlanCells(t *testing.T) {
	raw := `{"code":0,"data":[
		{"show_type":401,"video_data":[{"series_id":"11111111","vid":"11111112","title":"推薦影片第一季","episode_cnt":10}]},
		{"show_type":547,"video_data":[{"series_id":"22222222","vid":"22222223","title":"正式系列第一季","episode_cnt":12,"secondary_info_list":[{"content":"第1季"}]}]}
	]}`
	var response map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil { t.Fatal(err) }
	items, err := parseHongguoAppSeasons(response)
	if err != nil { t.Fatal(err) }
	if len(items) != 1 || items[0].Drama.ID != "hongguo:22222222" {
		t.Fatalf("non-season plan cells were not excluded: %+v", items)
	}
}
func TestParseHongguoAppSeasonsDoesNotMergeBySameTitle(t *testing.T) {
	raw := `{"code":0,"data":[{"video_data":[
		{"series_id":"1111111111111111111","vid":"1","title":"同名短劇","episode_cnt":10},
		{"series_id":"2222222222222222222","vid":"2","title":"同名短劇","episode_cnt":10}
	]}]}`
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	items, err := parseHongguoAppSeasons(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Drama.ID == items[1].Drama.ID {
		t.Fatalf("different official series IDs were merged: %+v", items)
	}
}

func TestParseHongguoAppSeasonsRejectsUnconfirmedOrMalformedResponses(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"code":1,"data":[]}`,
		`{"code":0,"data":{}}`,
		`{"code":0,"data":[{"video_data":{}}]}`,
	} {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if _, err := parseHongguoAppSeasons(response); err == nil {
			t.Fatalf("malformed response was accepted: %s", raw)
		}
	}
}

func TestParseHongguoAppSeasonsAcceptsConfirmedEmptyList(t *testing.T) {
	items, err := parseHongguoAppSeasons(map[string]any{
		"code": json.Number("0"),
		"data": []any{},
	})
	if err != nil || len(items) != 0 {
		t.Fatalf("confirmed empty list was not distinguished from an API failure: %v, %v", items, err)
	}
}


func TestHongguoAppNextSeasonQueryUsesSeparateVerifiedSource(t *testing.T) {
	query := hongguoAppNextSeasonQuery("7690163276214176792")
	if query.Get("from") != "series_short_play_info" {
		t.Fatalf("next-season query from = %q, want series_short_play_info", query.Get("from"))
	}
	if query.Get("book_id") != "7690163276214176792" ||
		query.Get("filter_ids") != "7690163276214176792" {
		t.Fatalf("next-season query lost the exact current series ID: %v", query)
	}
}

func TestHongguoNeedsSeasonFallbackOnlyFor101116(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{message: "紅果 App 接口暫不可用（101116）", want: true},
		{message: "紅果 App 介面未提供有效資料", want: true},
		{message: "紅果 App 接口未返回有效資料", want: true},
		{message: "紅果設備註冊 HTTP 444", want: false},
		{message: "紅果 App 接口 HTTP 403", want: false},
		{message: "紅果 App 接口 HTTP 429", want: false},
		{message: "紅果 App 接口暫不可用（100001）", want: false},
		{message: "context deadline exceeded", want: false},
		{message: "", want: false},
	} {
		var err error
		if tc.message != "" {
			err = errors.New(tc.message)
		}
		if got := hongguoNeedsSeasonFallback(err); got != tc.want {
			t.Errorf("hongguoNeedsSeasonFallback(%q) = %v, want %v", tc.message, got, tc.want)
		}
	}
}
