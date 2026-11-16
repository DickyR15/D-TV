package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestResolveHongguoV5MediaUsesPlayerV1Endpoint(t *testing.T) {
	const videoID = "123456"
	var requestCount int
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		requestCount++
		if request.Method != http.MethodPost {
			t.Errorf("method=%s, want POST", request.Method)
		}
		if request.URL.Host != "api5-normal-sinfonlinea.fqnovel.com" {
			t.Errorf("host=%s, want dedicated playback host", request.URL.Host)
		}
		if request.URL.Path != hongguoPlaybackPath {
			t.Errorf("path=%s, want %s", request.URL.Path, hongguoPlaybackPath)
		}
		for _, name := range []string{"X-Khronos", "X-Helios", "X-Medusa", "X-SS-STUB"} {
			if request.Header.Get(name) == "" {
				t.Errorf("missing V5 header %s", name)
			}
		}
		if request.Header.Get("X-Gorgon") != "" || request.Header.Get("X-Argus") != "" {
			t.Error("legacy signatures must not be sent to the V5 playback endpoint")
		}
		if request.Header.Get("User-Agent") != hongguoPlaybackUserAgent {
			t.Errorf("playback user-agent mismatch: %q", request.Header.Get("User-Agent"))
		}
		var payload map[string]any
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode playback payload: %v", err)
		}
		if mapString(payload, "dr_scene") != "player" {
			t.Errorf("dr_scene=%v, want player", payload["dr_scene"])
		}
		mixed := nestedMap(payload, "mixed_video_id_map")
		ids := anyList(mixed["1004"])
		if len(ids) != 1 || ids[0] != videoID {
			t.Errorf("mixed_video_id_map[1004]=%#v, want only %s", ids, videoID)
		}
		biz := nestedMap(payload, "biz_param")
		if mapString(biz, "video_platform") != "1024" {
			t.Errorf("video_platform=%v, want 1024", biz["video_platform"])
		}
		model := `{"video_duration":80.5,"video_list":[{"main_url":"https://media.test/v5-main.mp4","backup_url":"https://media.test/v5-backup.mp4","video_meta":{"codec_type":"h264","definition":"1080p"}}]}`
		response := `{"code":0,"data":{"1004":{"123456":{"video_model":` + mustJSONTestString(t, model) + `}}}}`
		return sourceFixtureResponse(request, http.StatusOK, response), nil
	})
	d.cfg.Retries = 1
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "9001", "9002", true

	media, err := d.resolveHongguoV5Media(context.Background(), videoID)
	if err != nil {
		t.Fatal(err)
	}
	if media.URL != "https://media.test/v5-main.mp4" {
		t.Fatalf("media URL=%q, want V5 main URL", media.URL)
	}
	if len(media.Variants) < 2 {
		t.Fatalf("expected main and backup variants, got %d", len(media.Variants))
	}
	if requestCount != 1 {
		t.Fatalf("request count=%d, want one V5 request", requestCount)
	}
}

func TestResolveHongguoV5MediaRejectsWrongEpisode(t *testing.T) {
	const videoID = "123456"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		model := `{"video_list":[{"main_url":"https://media.test/wrong-episode.mp4","video_meta":{"codec_type":"h264"}}]}`
		response := `{"code":0,"data":{"1004":{"654321":{"video_model":` + mustJSONTestString(t, model) + `}}}}`
		return sourceFixtureResponse(request, http.StatusOK, response), nil
	})
	d.cfg.Retries = 1
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "9001", "9002", true

	_, err := d.resolveHongguoV5Media(context.Background(), videoID)
	if err == nil {
		t.Fatal("expected an error when API returns a different episode ID")
	}
}
