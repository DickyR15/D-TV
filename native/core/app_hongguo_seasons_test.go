package core

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestHongguoAppSeasonQueryUsesOfficialSelector(t *testing.T) {
	query := hongguoAppSeasonQuery("7690163276214176792")
	expected := map[string]string{
		"scene": "10",
		"from": "series_short_play_info",
		"offset": "0",
		"limit": "0",
		"total_chapter_num": "0",
		"current_chapter_num": "0",
		"post_id": "0",
		"is_horizontal_screen": "false",
		"bookstore_tab": "0",
		"bookstore_tab_type": "0",
		"book_id": "7690163276214176792",
		"filter_ids": "7690163276214176792",
	}
	if len(query) != len(expected) {
		t.Fatalf("unexpected query fields: %v", query)
	}
	for key, value := range expected {
		if query.Get(key) != value {
			t.Fatalf("query %q = %q, want %q", key, query.Get(key), value)
		}
	}
	if _, err := url.ParseQuery(query.Encode()); err != nil {
		t.Fatal(err)
	}
}

func TestParseHongguoAppSeasonsKeepsOfficialIDsAndUpcomingRows(t *testing.T) {
	raw := `{
		"code": 0,
		"data": [{
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
