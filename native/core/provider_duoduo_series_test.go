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

func TestDuoduoSeriesAliasesAndExternalIDsAreSafe(t *testing.T) {
	if duoduoVerifiedSeriesAliasKey("芝加哥急救") != "chicago-med" ||
		duoduoVerifiedSeriesAliasKey("芝加哥醫院") != "chicago-med" ||
		duoduoVerifiedSeriesAliasKey("芝加哥医院") != "chicago-med" ||
		duoduoVerifiedSeriesAliasKey("Chicago Med") != "chicago-med" {
		t.Fatal("verified Chicago Med aliases did not resolve to one identity")
	}
	if duoduoVerifiedSeriesAliasKey("芝加哥烈焰") != "" || duoduoVerifiedSeriesAliasKey("芝加哥警署") != "" {
		t.Fatal("other Chicago franchises must remain separate")
	}
	if !duoduoSameSeriesIdentity("示例劇", "Example Show", "tt12345678", "", "tt12345678", "") {
		t.Fatal("matching IMDb IDs should join alternate localized titles")
	}
	if duoduoSameSeriesIdentity("示例劇", "Example Show", "tt12345678", "", "tt87654321", "") {
		t.Fatal("different IMDb IDs must remain separate")
	}
	if duoduoCanonicalSeriesGroupID("芝加哥急救", "季", "", "") != duoduoCanonicalSeriesGroupID("芝加哥醫院", "季", "", "") {
		t.Fatal("verified aliases must generate one stable group ID")
	}
	if duoduoCanonicalSeriesGroupID("芝加哥烈焰", "季", "", "") == duoduoCanonicalSeriesGroupID("芝加哥醫院", "季", "", "") {
		t.Fatal("other Chicago shows were incorrectly grouped")
	}
}

func TestDuoduoIMDbIDExtractionAndSeasonIdentity(t *testing.T) {
	drama := duoduoDramaFromRow(map[string]any{
		"vod_id": "1234", "vod_name": "示例作品第二季",
		"vod_content": "<p>原名 Example Show. IMDb: tt12345678</p>",
		"vod_douban_id": "987654",
	})
	if drama.IMDbID != "tt12345678" || drama.DoubanID != "987654" {
		t.Fatalf("external IDs were not retained: %+v", drama)
	}
	if drama.SeriesSeasonNumber != 2 || drama.SeriesSeasonUnit != "季" ||
		drama.SeriesGroupID != "duoduo-series:imdb:tt12345678:季" {
		t.Fatalf("external identity was not applied to season row: %+v", drama)
	}
}

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
		switch request.URL.Path {
		case "/api.php/web/search/index":
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
		case "/api.php/web/vod/get_detail":
			if request.URL.Query().Get("vod_id") != "100" {
				t.Fatalf("only the sibling season should be hydrated, unexpected detail ID: %s", request.URL.Query().Get("vod_id"))
			}
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[{"vod_id":100,"vod_name":"測試系列第一季","vod_remarks":"12集"}],
				"vodplayer":[]
			}`), nil
		case "/api.php/web/internal/search_aggregate":
			if request.URL.Query().Get("vod_id") != "100" {
				t.Fatalf("unexpected aggregate ID: %s", request.URL.Query().Get("vod_id"))
			}
			return sourceFixtureResponse(request, http.StatusOK, `{"code":200,"data":[]}`), nil
		default:
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
			return nil, nil
		}
	})

	current := nativeDrama{
		ID: "duoduo:101", Source: sourceDuoduo, SourceID: "101",
		Title: "測試系列第二季", Episodes: 24, Cover: "https://img.example.test/2.jpg",
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

func TestFetchDuoduoSeriesSeasonsMergesVerifiedLocalizedAliases(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/search/index":
			switch request.URL.Query().Get("wd") {
			case "芝加哥醫院":
				return sourceFixtureResponse(request, http.StatusOK, `{"code":200,"data":[
					{"vod_id":912,"vod_name":"芝加哥醫院第十二季","vod_pic":"https://img.example.test/med12.jpg","vod_remarks":"21集"},
					{"vod_id":913,"vod_name":"芝加哥醫院第12季","vod_remarks":"21集"}
				]}`), nil
			case "芝加哥急救":
				return sourceFixtureResponse(request, http.StatusOK, `{"code":200,"data":[
					{"vod_id":901,"vod_name":"芝加哥急救第一季","vod_pic":"https://img.example.test/med1.jpg","vod_remarks":"18集"},
					{"vod_id":904,"vod_name":"芝加哥醫院第1季"},
					{"vod_id":902,"vod_name":"芝加哥急救第二季","vod_pic":"https://img.example.test/med2.jpg","vod_remarks":"23集"},
					{"vod_id":990,"vod_name":"芝加哥烈焰第一季","vod_pic":"https://img.example.test/fire1.jpg","vod_remarks":"24集"}
				]}`), nil
			default:
				return sourceFixtureResponse(request, http.StatusOK, `{"code":200,"data":[]}`), nil
			}
		case "/api.php/web/vod/get_detail":
			id := request.URL.Query().Get("vod_id")
			title := map[string]string{"901":"芝加哥急救第一季","902":"芝加哥急救第二季"}[id]
			if title == "" { t.Fatalf("unexpected sibling detail ID: %s", id) }
			return sourceFixtureResponse(request, http.StatusOK, fmt.Sprintf(`{"code":200,"data":[{"vod_id":%s,"vod_name":%q,"vod_remarks":"已完結"}],"vodplayer":[]}`, id, title)), nil
		case "/api.php/web/internal/search_aggregate":
			return sourceFixtureResponse(request, http.StatusOK, `{"code":200,"data":[]}`), nil
		default:
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
			return nil, nil
		}
	})
	current := nativeDrama{ID:"duoduo:912", Source:sourceDuoduo, SourceID:"912",
		Title:"芝加哥醫院第十二季", Episodes:21, Cover:"https://img.example.test/med12.jpg"}
	seasons, err := d.fetchDuoduoSeriesSeasons(context.Background(), current)
	if err != nil { t.Fatal(err) }
	if len(seasons) != 3 { t.Fatalf("expected seasons 1, 2 and 12, got %+v", seasons) }
	want := []struct{id string; number int}{{"duoduo:901",1},{"duoduo:902",2},{"duoduo:912",12}}
	for i, expected := range want {
		if seasons[i].Drama.ID != expected.id || seasons[i].Number != expected.number {
			t.Fatalf("season %d mismatch: got %+v want %+v", i, seasons[i], expected)
		}
		if seasons[i].Drama.SeriesGroupID != "duoduo-series:alias:chicago-med:季" {
			t.Fatalf("season %d group ID mismatch: %+v", i, seasons[i])
		}
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
			if id != "901" {
				t.Fatalf("current opened season 902 must reuse its already-resolved metadata; unexpected sibling detail ID: %s", id)
			}
			if deadline, ok := request.Context().Deadline(); ok {
				mu.Lock()
				if !deadline.After(catalogDeadline.Add(time.Second)) {
					t.Errorf("season %s reused the short catalogue deadline", id)
				}
				hydrated[id] = true
				mu.Unlock()
			}
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[{
					"vod_id":901,
					"vod_name":"芝加哥烈焰第一季",
					"vod_pic_detail":"https://img.example.test/chicago-fire-s1.jpg",
					"vod_total":2,
					"vod_remarks":"已完結"
				}],
				"vodplayer":[]
			}`), nil
		case "/api.php/web/internal/search_aggregate":
			id := request.URL.Query().Get("vod_id")
			if id != "901" {
				t.Fatalf("current opened season 902 must not be redundantly re-resolved; unexpected aggregate ID: %s", id)
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
	current := nativeDrama{
		ID: "duoduo:902", Source: sourceDuoduo, SourceID: "902",
		Title: "芝加哥烈焰第二季",
		Cover: "https://img.example.test/chicago-fire-s2.jpg",
		Episodes: 2, EpisodesVerified: true,
	}
	seasons, err := d.fetchDuoduoSeriesSeasons(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 2 {
		t.Fatalf("expected two seasons, got %+v", seasons)
	}
	mu.Lock()
	defer mu.Unlock()
	if !hydrated["901"] || hydrated["902"] {
		t.Errorf("only the unopened sibling season should be hydrated: %+v", hydrated)
	}
	for _, season := range seasons {
		id := strings.TrimPrefix(season.Drama.ID, "duoduo:")
		if !season.Playable || season.Upcoming || season.Drama.Episodes != 2 {
			t.Errorf("season %s unavailable: %+v", id, season)
		}
		want := "https://img.example.test/chicago-fire-s1.jpg"
		if id == "902" {
			want = "https://img.example.test/chicago-fire-s2.jpg"
		}
		if season.Drama.Cover != want {
			t.Errorf("season %s poster %q, want %q", id, season.Drama.Cover, want)
		}
	}
}

func TestDuoduoMergeSameSeasonMetadataPreservesPreviousPosterCandidates(t *testing.T) {
	catalog := Drama{
		ID: "duoduo:950", Source: sourceDuoduo, SourceID: "950",
		Title: "測試系列第一季",
		Cover: "https://img.example.test/catalog-main.jpg",
		CoverURL: "https://img.example.test/catalog-original.jpg",
		CoverCandidates: []string{"https://img.example.test/catalog-alt.jpg"},
		Episodes: 24, EpisodeCount: 24, TotalEpisode: 24,
	}
	detail := Drama{
		ID: "duoduo:950", Source: sourceDuoduo, SourceID: "950",
		Title: "測試系列第一季",
		Cover: "https://img.example.test/detail-main.jpg",
		CoverURL: "https://img.example.test/detail-original.jpg",
		CoverCandidates: []string{"https://img.example.test/detail-alt.jpg"},
		Episodes: 18, EpisodeCount: 18, TotalEpisode: 18,
	}
	merged := duoduoMergeSameSeasonMetadata(catalog, detail)
	if merged.Cover != "https://img.example.test/detail-main.jpg" {
		t.Fatalf("refreshed detail poster = %q", merged.Cover)
	}
	if merged.Episodes != 18 || merged.EpisodeCount != 18 || merged.TotalEpisode != 18 {
		t.Fatalf("official season detail count did not replace stale catalogue count: %+v", merged)
	}
	want := []string{
		"https://img.example.test/detail-original.jpg",
		"https://img.example.test/detail-alt.jpg",
		"https://img.example.test/catalog-main.jpg",
		"https://img.example.test/catalog-original.jpg",
		"https://img.example.test/catalog-alt.jpg",
	}
	if strings.Join(merged.CoverCandidates, "|") != strings.Join(want, "|") {
		t.Fatalf("merged poster candidates = %#v, want %#v", merged.CoverCandidates, want)
	}
}

func TestFetchDuoduoSeriesSeasonsAllowsMissingEpisodeCountUnlessExplicitlyUpcoming(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/search/index":
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[
					{"vod_id":951,"vod_name":"測試系列第一季","vod_remarks":"即將上線"},
					{"vod_id":952,"vod_name":"測試系列第二季","vod_remarks":"已完結"}
				]
			}`), nil
		case "/api.php/web/vod/get_detail":
			id := request.URL.Query().Get("vod_id")
			season := "第二季"
			if id == "951" {
				season = "第一季"
			} else if id != "952" {
				t.Fatalf("unexpected season detail ID: %s", id)
			}
			body := fmt.Sprintf(`{
				"code":200,
				"data":[{"vod_id":%s,"vod_name":"測試系列%s","vod_remarks":"%s"}],
				"vodplayer":[]
			}`, id, season, map[string]string{"951":"即將上線","952":"已完結"}[id])
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		case "/api.php/web/internal/search_aggregate":
			return sourceFixtureResponse(request, http.StatusOK, `{"code":200,"data":[]}`), nil
		default:
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
			return nil, nil
		}
	})
	current := nativeDrama{
		ID: "duoduo:952", Source: sourceDuoduo, SourceID: "952",
		Title: "測試系列第二季",
	}
	seasons, err := d.fetchDuoduoSeriesSeasons(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 2 {
		t.Fatalf("expected two seasons, got %+v", seasons)
	}
	if seasons[0].Number != 1 || !seasons[0].Upcoming || seasons[0].Playable {
		t.Fatalf("official upcoming season should remain disabled: %+v", seasons[0])
	}
	if seasons[1].Number != 2 || seasons[1].Upcoming || !seasons[1].Playable ||
		seasons[1].Drama.Episodes != 0 {
		t.Fatalf("missing episode count was incorrectly treated as unavailable: %+v", seasons[1])
	}
}



func TestFetchDuoduoSeriesSeasonsKeepsOfficialMetadataWhenAggregateHasNoDirectLines(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/search/index":
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[
					{"vod_id":960,"vod_name":"芝加哥醫院第一季","vod_remarks":"已完結"},
					{"vod_id":961,"vod_name":"芝加哥醫院第二季","vod_pic":"https://img.example.test/med-s2.jpg","vod_total":10,"vod_remarks":"已完結"}
				]
			}`), nil
		case "/api.php/web/vod/get_detail":
			if request.URL.Query().Get("vod_id") != "960" {
				t.Fatalf("unexpected detail ID: %s", request.URL.Query().Get("vod_id"))
			}
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[{
					"vod_id":960,
					"vod_name":"芝加哥醫院第一季",
					"vod_pic_detail":"https://img.example.test/med-s1-detail.jpg",
					"vod_total":18,
					"vod_remarks":"全18集",
					"vod_play_from":"",
					"vod_play_url":""
				}],
				"vodplayer":[]
			}`), nil
		case "/api.php/web/internal/search_aggregate":
			if request.URL.Query().Get("vod_id") != "960" {
				t.Fatalf("unexpected aggregate ID: %s", request.URL.Query().Get("vod_id"))
			}
			return sourceFixtureResponse(request, http.StatusOK, `{"code":200,"data":[]}`), nil
		default:
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
			return nil, nil
		}
	})
	current := nativeDrama{
		ID: "duoduo:961", Source: sourceDuoduo, SourceID: "961",
		Title: "芝加哥醫院第二季", Episodes: 10,
	}
	seasons, err := d.fetchDuoduoSeriesSeasons(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons) != 2 {
		t.Fatalf("expected two seasons, got %+v", seasons)
	}
	first := seasons[0]
	if first.Drama.ID != "duoduo:960" ||
		first.Drama.Cover != "https://img.example.test/med-s1-detail.jpg" ||
		first.Drama.Episodes != 18 || !first.Playable || first.Upcoming {
		t.Fatalf("official detail poster/count were lost when aggregate had no direct line: %+v", first)
	}
}


func TestFetchDuoduoSeriesHydratesCompleteCatalogueSeasonFromOfficialDetailAndAggregate(t *testing.T) {
	var hydrated []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/search/index":
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[
					{"vod_id":970,"vod_name":"全域系列第一季","vod_pic":"https://img.example.test/catalog-s1.jpg","vod_total":24,"vod_remarks":"全24集"},
					{"vod_id":971,"vod_name":"全域系列第二季","vod_pic":"https://img.example.test/catalog-s2.jpg","vod_total":10,"vod_remarks":"全10集"}
				]
			}`), nil
		case "/api.php/web/vod/get_detail":
			id := request.URL.Query().Get("vod_id")
			if id != "970" {
				t.Fatalf("current opened season must reuse its resolved detail, unexpected detail ID: %s", id)
			}
			hydrated = append(hydrated, id)
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[{
					"vod_id":970,
					"vod_name":"全域系列第一季",
					"vod_pic_detail":"https://img.example.test/official-s1-detail.jpg",
					"vod_remarks":"已完結",
					"vod_play_from":"",
					"vod_play_url":""
				}],
				"vodplayer":[]
			}`), nil
		case "/api.php/web/internal/search_aggregate":
			if request.URL.Query().Get("vod_id") != "970" {
				t.Fatalf("unexpected aggregate ID: %s", request.URL.Query().Get("vod_id"))
			}
			return sourceFixtureResponse(request, http.StatusOK, `{
				"code":200,
				"data":[{
					"site_key":"public-season-one",
					"site_name":"第一季公開線路",
					"decode_status":0,
					"vod_play_url":"第1集$https://media.example.test/s1e1.m3u8#第2集$https://media.example.test/s1e2.m3u8#第3集$https://media.example.test/s1e3.m3u8"
				}]
			}`), nil
		default:
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
			return nil, nil
		}
	})
	current := nativeDrama{
		ID:"duoduo:971", Source:sourceDuoduo, SourceID:"971",
		Title:"全域系列第二季", Cover:"https://img.example.test/current-s2-detail.jpg",
		Episodes:10, EpisodesVerified:true,
	}
	seasons, err := d.fetchDuoduoSeriesSeasons(context.Background(), current)
	if err != nil { t.Fatal(err) }
	if len(seasons) != 2 { t.Fatalf("expected two seasons, got %+v", seasons) }
	if len(hydrated) != 1 || hydrated[0] != "970" {
		t.Fatalf("complete sibling catalogue row was not resolved by its own official ID: hydrated=%v", hydrated)
	}
	first := seasons[0]
	if first.Drama.ID != "duoduo:970" ||
		first.Drama.Cover != "https://img.example.test/official-s1-detail.jpg" ||
		first.Drama.Episodes != 3 || !first.Playable || first.Upcoming {
		t.Fatalf("official sibling detail + aggregate did not replace stale catalogue metadata: %+v", first)
	}
}


func TestDuoduoSeriesIdentityUsesDoubanWhenIMDbIsMissing(t *testing.T) {
	got := duoduoCanonicalSeriesGroupID("Example Series", "季", "", "douban:1234567")
	want := "duoduo-series:douban:1234567:季"
	if got != want {
		t.Fatalf("canonical group ID = %q, want %q", got, want)
	}
	if !duoduoSameSeriesIdentity("Example Series", "Example Series", "", "1234567", "", "douban:1234567") {
		t.Fatal("matching Douban identity should preserve the same series")
	}
}

func TestDuoduoSeriesIdentityRejectsConflictingOfficialIDs(t *testing.T) {
	if duoduoSameSeriesIdentity(
		"Chicago Med", "芝加哥醫院",
		"tt1234567", "111111", "tt7654321", "222222",
	) {
		t.Fatal("conflicting official IDs must never merge as one series")
	}
}
