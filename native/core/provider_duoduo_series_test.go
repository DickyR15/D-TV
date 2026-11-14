package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
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
		seasons[0].Label != "第1季" || !seasons[0].Playable ||
		seasons[0].Drama.Cover != "https://img.example.test/1.jpg" {
		t.Fatalf("first season metadata mismatch: %+v", seasons[0])
	}
	if seasons[1].Drama.ID != "duoduo:101" || seasons[1].Number != 2 ||
		seasons[1].Label != "第2季" || seasons[1].Drama.SeriesGroupID == "" ||
		seasons[1].Drama.Cover != "https://img.example.test/2.jpg" {
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


func TestDuoduoDramaEpisodeCountReadsCatalogAliases(t *testing.T) {
	tests := []struct {
		name  string
		drama Drama
		want  int
	}{
		{name: "episodes", drama: Drama{Episodes: 12}, want: 12},
		{name: "episode count", drama: Drama{EpisodeCount: 24}, want: 24},
		{name: "total episode", drama: Drama{TotalEpisode: 36}, want: 36},
		{name: "remark", drama: Drama{Remark: "更新至42集"}, want: 42},
		{name: "missing count", drama: Drama{}, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := duoduoDramaEpisodeCount(test.drama); got != test.want {
				t.Fatalf("duoduoDramaEpisodeCount() = %d, want %d", got, test.want)
			}
		})
	}
}


func TestFetchDuoduoSeriesSeasonsUsesSearchPlaybackResolverForPlayableFlag(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/search/index":
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[
					{"vod_id":900,"vod_name":"緊急呼救第一季","vod_pic":"https://img.example.test/s1.jpg","vod_remarks":"已完結"},
					{"vod_id":901,"vod_name":"緊急呼救第二季","vod_pic":"https://img.example.test/s2.jpg","vod_remarks":"18集"}
				]
			}`), nil
		case "/api.php/web/vod/get_detail":
			if request.URL.Query().Get("vod_id") != "900" {
				t.Fatalf("unexpected detail ID: %s", request.URL.Query().Get("vod_id"))
			}
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[{
					"vod_id":900,
					"vod_name":"緊急呼救第一季",
					"vod_pic":"https://img.example.test/s1.jpg",
					"vod_remarks":"已完結",
					"vod_play_from":"",
					"vod_play_url":""
				}],
				"vodplayer":[]
			}`), nil
		case "/api.php/web/internal/search_aggregate":
			if request.URL.Query().Get("vod_id") != "900" {
				t.Fatalf("unexpected aggregate ID: %s", request.URL.Query().Get("vod_id"))
			}
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[{
					"site_key":"series-direct",
					"site_name":"系列直連",
					"decode_status":0,
					"vod_play_url":"第1集$https://media.example.test/s1e1.m3u8#第2集$https://media.example.test/s1e2.m3u8"
				}]
			}`), nil
		default:
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
			return nil, nil
		}
	})
	current := nativeDrama{
		ID: "duoduo:901", Source: sourceDuoduo, SourceID: "901",
		Title: "緊急呼救第二季", Episodes: 18,
	}
	seasons, err := d.fetchDuoduoSeriesSeasons(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 2 {
		t.Fatalf("expected two seasons, got %+v", seasons)
	}
	first := seasons[0]
	if first.Drama.ID != "duoduo:900" || !first.Playable || first.Upcoming ||
		duoduoDramaEpisodeCount(duoduoDramaFromNative(first.Drama)) < 2 {
		t.Fatalf("season with aggregate playback URLs was incorrectly unavailable: %+v", first)
	}
}

func TestFetchDuoduoChicagoFireSeasonsHydrateWithIndependentDetailBudget(t *testing.T) {
	var mu sync.Mutex
	var catalogDeadline time.Time
	hydrated := map[string]bool{}
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/search/index":
			if deadline, ok := request.Context().Deadline(); ok {
				mu.Lock()
				catalogDeadline = deadline
				mu.Unlock()
			}
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[
					{"vod_id":901,"vod_name":"芝加哥烈焰第一季","vod_remarks":"已完結"},
					{"vod_id":902,"vod_name":"芝加哥烈焰第二季","vod_remarks":"已完結"}
				]
			}`), nil
		case "/api.php/web/vod/get_detail":
			id := request.URL.Query().Get("vod_id")
			if deadline, ok := request.Context().Deadline(); ok {
				mu.Lock()
				if !deadline.After(catalogDeadline.Add(time.Second)) {
					t.Errorf("season %s reused the short catalogue deadline", id)
				}
				hydrated[id] = true
				mu.Unlock()
			}
			season := "第一季"
			poster := "https://img.example.test/chicago-fire-s1.jpg"
			if id == "902" {
				season = "第二季"
				poster = "https://img.example.test/chicago-fire-s2.jpg"
			} else if id != "901" {
				t.Fatalf("unexpected Chicago Fire detail ID: %s", id)
			}
			body := fmt.Sprintf(`{
				"code":200,
				"data":[{
					"vod_id":%s,
					"vod_name":"芝加哥烈焰%s",
					"vod_pic_detail":%q,
					"vod_total":2,
					"vod_remarks":"已完結"
				}]
			}`, id, season, poster)
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		case "/api.php/web/internal/search_aggregate":
			id := request.URL.Query().Get("vod_id")
			if id != "901" && id != "902" {
				t.Fatalf("unexpected Chicago Fire aggregate ID: %s", id)
			}
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[{
					"site_key":"direct",
					"site_name":"官方直連",
					"decode_status":0,
					"vod_play_url":"第1集$https://media.example.test/chicago-e1.m3u8#第2集$https://media.example.test/chicago-e2.m3u8"
				}]
			}`), nil
		default:
			t.Fatalf("unexpected Chicago Fire endpoint: %s", request.URL.String())
			return nil, nil
		}
	})
	current := nativeDrama{ID:"duoduo:902", Source:sourceDuoduo, SourceID:"902", Title:"芝加哥烈焰第二季"}
	seasons, err := d.fetchDuoduoSeriesSeasons(context.Background(), current)
	if err != nil { t.Fatal(err) }
	if len(seasons) != 2 { t.Fatalf("expected two seasons, got %+v",seasons) }
	mu.Lock()
	defer mu.Unlock()
	for _, season := range seasons {
		id := strings.TrimPrefix(season.Drama.ID,"duoduo:")
		if !hydrated[id] { t.Errorf("season %s was not hydrated",id) }
		if !season.Playable || season.Upcoming || season.Drama.Episodes != 2 {
			t.Errorf("season %s unavailable: %+v",id,season)
		}
		want := "https://img.example.test/chicago-fire-s1.jpg"
		if id=="902" { want="https://img.example.test/chicago-fire-s2.jpg" }
		if season.Drama.Cover != want { t.Errorf("season %s poster %q, want %q",id,season.Drama.Cover,want) }
	}
}
