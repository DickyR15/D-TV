package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestResolveHongguoV5MediaUsesPlayerV1Endpoint(t *testing.T) {
	const videoID = "123456"
	var requestCount int
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		requestCount++
		if request.Method != http.MethodPost {
			t.Errorf("method=%s, want POST", request.Method)
		}
		if request.URL.Host != "api5-normal-sinfonlineb.fqnovel.com" {
			t.Errorf("host=%s, want THL-compatible primary playback host", request.URL.Host)
		}
		if request.URL.Path != "/novel/player/multi_video_model/v1/" {
			t.Errorf("path=%s, want official multi_video_model endpoint", request.URL.Path)
		}
		for _, name := range []string{
			"X-Khronos", "X-Gorgon", "X-Ladon", "X-Argus",
			"X-Helios", "X-Medusa", "X-SS-STUB", "X-SS-Req-Ticket",
			"X-XS-From-Web", "X-TT-Request-Tag", "Passport-SDK-Version",
			"X-Vc-Bdturing-Sdk-Version",
		} {
			if request.Header.Get(name) == "" {
				t.Errorf("missing official compatibility header %s", name)
			}
		}
		if request.Header.Get("Accept") != "application/json; charset=utf-8,application/x-protobuf" {
			t.Errorf("playback Accept header=%q", request.Header.Get("Accept"))
		}
		if request.Header.Get("X-SS-Req-Ticket") != request.URL.Query().Get("_rticket") {
			t.Errorf("request ticket header=%q query=%q", request.Header.Get("X-SS-Req-Ticket"), request.URL.Query().Get("_rticket"))
		}
		if request.Header.Get("User-Agent") != hongguoPlaybackUserAgent {
			t.Errorf("playback user-agent mismatch: %q", request.Header.Get("User-Agent"))
		}
		if request.URL.Query().Get("version_name") != "7.4.3.32" ||
			request.URL.Query().Get("channel") != "update_64" ||
			request.URL.Query().Get("device_type") != "25053RT47C" {
			t.Errorf("playback device profile did not match the current official route: %v", request.URL.Query())
		}
		var payload map[string]any
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode playback payload: %v", err)
		}
		if _, found := payload["dr_scene"]; found {
			t.Errorf("unexpected legacy dr_scene field in official playback payload")
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


func TestResolveHongguoV5MediaFallsBackAcrossOfficialPlaybackRoutes(t *testing.T) {
	const videoID = "123456"
	var hosts []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		hosts = append(hosts, request.URL.Host)
		if request.URL.Host == "api5-normal-sinfonlineb.fqnovel.com" {
			return sourceFixtureResponse(request, http.StatusServiceUnavailable, `{"code":503}`), nil
		}
		if request.URL.Host != "reading.snssdk.com" {
			t.Errorf("unexpected fallback host: %s", request.URL.Host)
		}
		model := `{"video_list":[{"main_url":"https://media.test/reading-main.mp4","backup_url_list":["https://media.test/reading-backup.mp4"],"video_meta":{"codec_type":"h264","definition":"1080p"}}]}`
		response := `{"code":0,"data":{"123456":{"video_model":` + mustJSONTestString(t, model) + `}}}`
		return sourceFixtureResponse(request, http.StatusOK, response), nil
	})
	d.cfg.Retries = 1
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "9001", "9002", true

	media, err := d.resolveHongguoV5Media(context.Background(), videoID)
	if err != nil {
		t.Fatal(err)
	}
	if media.URL != "https://media.test/reading-main.mp4" {
		t.Fatalf("fallback URL=%q", media.URL)
	}
	if len(hosts) != 2 || hosts[0] != "api5-normal-sinfonlineb.fqnovel.com" || hosts[1] != "reading.snssdk.com" {
		t.Fatalf("route order=%v", hosts)
	}
	if len(media.Variants) < 2 {
		t.Fatalf("expected main and backup list variants, got %#v", media.Variants)
	}
}

func TestResolveHongguoV5MediaRemembersSuccessfulRoute(t *testing.T) {
	const videoID = "123456"
	var hosts []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		hosts = append(hosts, request.URL.Host)
		model := `{"video_list":[{"main_url":"https://media.test/preferred.mp4","video_meta":{"codec_type":"h264","definition":"720p"}}]}`
		response := `{"code":0,"data":{"123456":{"video_model":` + mustJSONTestString(t, model) + `}}}`
		return sourceFixtureResponse(request, http.StatusOK, response), nil
	})
	d.cfg.Retries = 1
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "9001", "9002", true

	if _, err := d.resolveHongguoV5Media(context.Background(), videoID); err != nil {
		t.Fatal(err)
	}
	d.hongguoPlaybackMu.Lock()
	d.hongguoPlaybackModels = nil
	d.hongguoPlaybackMu.Unlock()
	if _, err := d.resolveHongguoV5Media(context.Background(), videoID); err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0] != "api5-normal-sinfonlineb.fqnovel.com" || hosts[1] != hosts[0] {
		t.Fatalf("preferred route was not reused: %v", hosts)
	}
}

func TestResolveHongguoV5MediaSingleModelRoute(t *testing.T) {
	const videoID = "123456"
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "reading.snssdk.com" ||
			request.URL.Path != "/novel/player/video_model/v1/" {
			return sourceFixtureResponse(request, http.StatusNotFound, `{}`), nil
		}
		var payload map[string]any
		body, _ := io.ReadAll(request.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if mapString(payload, "video_id") != videoID {
			t.Fatalf("single route payload video_id=%v", payload["video_id"])
		}
		model := `{"video_list":[{"main_url":"https://media.test/single-main.mp4","video_meta":{"codec_type":"h264"}}]}`
		response := `{"code":0,"data":{"video_model":` + mustJSONTestString(t, model) + `}}`
		return sourceFixtureResponse(request, http.StatusOK, response), nil
	})
	d.cfg.Retries = 1
	d.hongguoPlaybackMu.Lock()
	d.hongguoPlaybackPreferredRoute = ""
	d.hongguoPlaybackPreferredUntil = time.Time{}
	d.hongguoPlaybackMu.Unlock()
	d.compatibilityManifest = &providerCompatibilityManifest{
		SchemaVersion: 1,
		ManifestVersion: 1,
		GeneratedAt: time.Now().Add(-time.Minute),
		ExpiresAt: time.Now().Add(time.Hour),
		Providers: map[string]providerCompatibilityEntry{
			sourceHongguo: {PlaybackRouteOrder: []string{
				"playbackReadingSingle",
			}},
		},
	}
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "9001", "9002", true

	media, err := d.resolveHongguoV5Media(context.Background(), videoID)
	if err != nil {
		t.Fatal(err)
	}
	if media.URL != "https://media.test/single-main.mp4" {
		t.Fatalf("single route media=%q", media.URL)
	}
}
