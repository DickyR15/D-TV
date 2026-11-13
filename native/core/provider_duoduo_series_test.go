package core

import (
	"context"
	"net/http"
	"testing"
)

func TestDuoduoSeriesTitleRequiresExplicitSeasonMarker(t *testing.T) {
	tests := []struct {
		title string
		base string
		number int
		unit string
		ok bool
	}{
		{"測試系列第一季", "測試系列", 1, "季", true},
		{"測試系列 第十二季", "測試系列", 12, "季", true},
		{"測試系列第二部", "測試系列", 2, "部", true},
		{"測試系列 Season 3", "測試系列", 3, "季", true},
		{"測試系列 S04", "測試系列", 4, "季", true},
		{"測試系列外傳", "", 0, "", false},
		{"另一部同名作品", "", 0, "", false},
	}
	for _, test := range tests {
		base, number, unit, ok := duoduoSeriesTitle(test.title)
		if base != test.base || number != test.number || unit != test.unit || ok != test.ok {
			t.Fatalf(
				"duoduoSeriesTitle(%q) = (%q, %d, %q, %t), want (%q, %d, %q, %t)",
				test.title, base, number, unit, ok,
				test.base, test.number, test.unit, test.ok,
			)
		}
	}
}

func TestFetchDuoduoSeriesSeasonsMatchesExactBaseTitle(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api.php/web/search/index" {
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
		}
		if request.URL.Query().Get("wd") != "測試系列" {
			t.Fatalf("series search keyword = %q", request.URL.Query().Get("wd"))
		}
		return sourceFixtureResponse(request, http.StatusOK, `{
			"code":200,
			"data":[
				{"vod_id":100,"vod_name":"測試系列第一季","vod_pic":"https://img.example.test/1.jpg","vod_remarks":"12集"},
				{"vod_id":101,"vod_name":"測試系列第二季","vod_pic":"https://img.example.test/2.jpg","vod_remarks":"24集"},
				{"vod_id":102,"vod_name":"測試系列外傳第三季","vod_remarks":"8集"},
				{"vod_id":103,"vod_name":"測試系列番外篇","vod_remarks":"6集"},
				{"vod_id":104,"vod_name":"測試系列 第二季 完整版","vod_remarks":"10集"}
			]
		}`), nil
	})

	current := nativeDrama{
		ID: "duoduo:101", Source: sourceDuoduo, SourceID: "101",
		Title: "測試系列第二季", Episodes: 24,
	}
	seasons, err := d.fetchDuoduoSeriesSeasons(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 2 {
		t.Fatalf("expected exactly two related seasons, got %+v", seasons)
	}
	if seasons[0].Drama.ID != "duoduo:100" || seasons[0].Number != 1 ||
		seasons[0].Label != "第1季" || !seasons[0].Playable {
		t.Fatalf("first season metadata mismatch: %+v", seasons[0])
	}
	if seasons[1].Drama.ID != "duoduo:101" || seasons[1].Number != 2 ||
		seasons[1].Label != "第2季" || seasons[1].Drama.SeriesGroupID == "" {
		t.Fatalf("second season metadata mismatch: %+v", seasons[1])
	}
	if seasons[0].Drama.SeriesGroupID != seasons[1].Drama.SeriesGroupID {
		t.Fatalf("related seasons do not share a stable group ID: %+v", seasons)
	}
}

func TestFetchDuoduoSeriesSeasonsCanUseUnsuffixedFirstSeason(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		return sourceFixtureResponse(request, http.StatusOK, `{
			"code":200,
			"data":[
				{"vod_id":200,"vod_name":"無季別後綴的系列名","vod_remarks":"18集"},
				{"vod_id":201,"vod_name":"無季別後綴的系列名第二季","vod_remarks":"20集"}
			]
		}`), nil
	})
	current := nativeDrama{
		ID: "duoduo:201", Source: sourceDuoduo, SourceID: "201",
		Title: "無季別後綴的系列名第二季", Episodes: 20,
	}
	seasons, err := d.fetchDuoduoSeriesSeasons(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 2 || seasons[0].Drama.ID != "duoduo:200" ||
		seasons[0].Number != 1 || seasons[1].Number != 2 {
		t.Fatalf("unsuffixed first season was not linked safely: %+v", seasons)
	}
}

func TestNativeNormalizeKeepsDuoduoSeasonMetadata(t *testing.T) {
	item := nativeNormalize(Drama{
		ID: "duoduo:301", Source: sourceDuoduo, SourceID: "301",
		Title: "測試系列第三季",
		SeriesGroupID: "duoduo-series:測試系列:季",
		SeriesSeasonNumber: 3,
		SeriesSeasonUnit: "季",
	})
	if item.ID != "duoduo:301" || item.SeriesGroupID == "" ||
		item.SeriesSeasonNumber != 3 || item.SeriesSeasonUnit != "季" {
		t.Fatalf("Duoduo series metadata was not serialized: %+v", item)
	}
}

func TestFetchDuoduoSeriesSeasonsOpensFromUnsuffixedFirstSeason(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		return sourceFixtureResponse(request, http.StatusOK, `{
			"code":200,
			"data":[
				{"vod_id":210,"vod_name":"原名系列","vod_remarks":"18集"},
				{"vod_id":211,"vod_name":"原名系列第二季","vod_remarks":"20集"}
			]
		}`), nil
	})
	current := nativeDrama{
		ID: "duoduo:210", Source: sourceDuoduo, SourceID: "210",
		Title: "原名系列", Episodes: 18,
	}
	seasons, err := d.fetchDuoduoSeriesSeasons(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 2 || seasons[0].Drama.ID != "duoduo:210" ||
		seasons[0].Number != 1 || seasons[1].Drama.ID != "duoduo:211" ||
		seasons[1].Number != 2 {
		t.Fatalf("unsuffixed first title did not expose its confirmed sibling: %+v", seasons)
	}
}
