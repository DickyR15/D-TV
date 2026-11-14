package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestNativeDetailKeepsDuoduoMetadataAndLoadsSeriesWithoutDirectPlayback(t *testing.T) {
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api.php/web/vod/get_detail":
			id := request.URL.Query().Get("vod_id")
			title := map[string]string{
				"962": "芝加哥醫院第一季",
				"963": "芝加哥醫院第二季",
			}[id]
			if title == "" {
				t.Fatalf("unexpected detail ID: %s", id)
			}
			count := "18"
			if id == "963" {
				count = "23"
			}
			body := fmt.Sprintf(
				`{"code":200,"data":[{"vod_id":%s,"vod_name":%q,"vod_pic_detail":"https://img.example.test/%s.jpg","vod_total":%s,"vod_remarks":"已完結","vod_play_from":"","vod_play_url":""}],"vodplayer":[]}`,
					id, title, id, count,
				)
			return sourceFixtureResponse(request, http.StatusOK, body), nil
		case "/api.php/web/internal/search_aggregate":
			return sourceFixtureResponse(request, http.StatusOK, `{"code":200,"data":[]}`), nil
		case "/api.php/web/search/index":
			if request.URL.Query().Get("wd") == "芝加哥醫院" {
				return sourceFixtureResponse(request, http.StatusOK, `{
					"code":200,
					"data":[
						{"vod_id":962,"vod_name":"芝加哥醫院第一季","vod_pic":"https://img.example.test/962.jpg","vod_total":18,"vod_remarks":"已完結"},
						{"vod_id":963,"vod_name":"芝加哥醫院第二季","vod_pic":"https://img.example.test/963.jpg","vod_total":23,"vod_remarks":"已完結"}
					]
				}`), nil
			}
			return sourceFixtureResponse(request, http.StatusOK, `{"code":200,"data":[]}`), nil
		default:
			t.Fatalf("unexpected Duoduo endpoint: %s", request.URL.String())
			return nil, nil
		}
	})

	input := nativeDrama{
		ID: "duoduo:963", Source: sourceDuoduo, SourceID: "963",
		Title: "芝加哥醫院第二季", Episodes: 23,
	}
	value, err := engine.nativeDetail(context.Background(), input)
	if err != nil {
		t.Fatalf("metadata-only detail should remain available: %v", err)
	}
	payload, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("unexpected native detail payload: %#v", value)
	}
	drama, ok := payload["drama"].(nativeDrama)
	if !ok || drama.Title != input.Title || drama.Episodes != 23 {
		t.Fatalf("season metadata was lost: %#v", payload["drama"])
	}
	chapters, ok := payload["chapters"].([]Chapter)
	if !ok || len(chapters) != 0 {
		t.Fatalf("expected an empty chapter list with metadata retained: %#v", payload["chapters"])
	}
	warning, _ := payload["warning"].(string)
	if !strings.Contains(warning, "沒有可直接播放") {
		t.Fatalf("expected a clear no-direct-playback warning, got %q", warning)
	}

	seasonPayload, err := engine.nativeSeasons(context.Background(), drama)
	if err != nil {
		t.Fatalf("series seasons should still load: %v", err)
	}
	seasonMap := seasonPayload.(map[string]any)
	seasons, ok := seasonMap["items"].([]nativeDramaSeason)
	if !ok || len(seasons) != 2 {
		t.Fatalf("expected both official seasons even without a direct route on the open season: %#v", seasonMap["items"])
	}
	if seasons[0].Drama.SeriesGroupID == "" ||
		seasons[0].Drama.SeriesGroupID != seasons[1].Drama.SeriesGroupID {
		t.Fatalf("seasons did not retain a shared series identity: %+v", seasons)
	}
}
