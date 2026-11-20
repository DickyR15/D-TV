package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHongguoCurrentPlayerRegistrationUsesXiaomi73932Profile(t *testing.T) {
	const cdid = "123e4567-e89b-12d3-a456-426614174000"
	const openudid = "0123456789abcdef"
	query := hongguoCurrentPlayerRegisterQuery(cdid, openudid)
	values, err := url.ParseQuery(query)
	if err != nil { t.Fatal(err) }
	expected := map[string]string{
		"aid": "8662", "app_name": "novelread", "version_code": "73932", "version_name": "7.3.9.32",
		"channel": "xiaomi_8662_64", "device_type": "23127PN0CC", "device_brand": "Xiaomi",
		"device_platform": "android", "os": "android", "os_api": "34", "os_version": "14",
		"resolution": "1200*2670", "dpi": "460", "cdid": cdid, "openudid": openudid,
		"manifest_version_code": "73932", "update_version_code": "73932",
	}
	for key, want := range expected {
		if got := values.Get(key); got != want { t.Errorf("registration query %s=%q, want %q", key, got, want) }
	}
	for _, key := range []string{"device_id", "iid"} {
		if values.Get(key) != "" { t.Errorf("player registration unexpectedly includes %s", key) }
	}

	body, err := hongguoCurrentPlayerRegistrationBody(cdid, openudid)
	if err != nil { t.Fatal(err) }
	var envelope struct {
		MagicTag string `json:"magic_tag"`
		Header map[string]any `json:"header"`
		GenTime int64 `json:"_gen_time"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil { t.Fatal(err) }
	if envelope.MagicTag != "ss_app_log" || envelope.GenTime <= 0 {
		t.Fatalf("registration envelope mismatch: %#v", envelope)
	}
	for key, want := range map[string]string{
		"app_version": "7.3.9.32", "channel": "xiaomi_8662_64", "device_model": "23127PN0CC",
		"device_brand": "Xiaomi", "device_manufacturer": "Xiaomi", "os_version": "14",
		"release_build": "UKQ1.230804.001", "resolution": "2670x1200", "tz_name": "Asia/Shanghai",
	} {
		if got, _ := envelope.Header[key].(string); got != want {
			t.Errorf("registration body %s=%q, want %q", key, got, want)
		}
	}
}

func TestHongguoCurrentPlayerQueryUsesRegisteredIdentityAndExactProfile(t *testing.T) {
	identity := hongguoCurrentPlayerIdentity{
		DeviceID: "1234567890123456789", InstallID: "1234567890123456790",
		CDID: "123e4567-e89b-12d3-a456-426614174000", OpenUDID: "0123456789abcdef",
	}
	pairs := hongguoCurrentPlayerPairs(identity)
	values := make(map[string]string, len(pairs))
	for _, pair := range pairs { values[pair.key] = pair.value }
	for key, want := range map[string]string{
		"device_id": identity.DeviceID, "iid": identity.InstallID, "cdid": identity.CDID,
		"openudid": identity.OpenUDID, "version_code": "73932", "version_name": "7.3.9.32",
		"channel": "xiaomi_8662_64", "device_type": "23127PN0CC", "device_brand": "Xiaomi",
		"os_api": "34", "os_version": "14", "resolution": "1200*2670", "dpi": "460",
	} {
		if values[key] != want { t.Errorf("%s=%q, want %q", key, values[key], want) }
	}
	if values["version_name"] == "7.4.3.32" || values["device_type"] == "PGT-AN10" {
		t.Fatal("Current Player identity accidentally inherited the older V5 profile")
	}
}

func TestResolveHongguoCurrentPlayerUsesCurrentPlayerV1AndSixHeaders(t *testing.T) {
	const videoID = "1234567890123456789"
	identity := hongguoCurrentPlayerIdentity{
		DeviceID: "1234567890123456789", InstallID: "1234567890123456790",
		CDID: "123e4567-e89b-12d3-a456-426614174000", OpenUDID: "0123456789abcdef",
	}
	var playbackRequests int
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.Method {
		case http.MethodHead:
			response := sourceFixtureResponse(request, http.StatusOK, "")
			response.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
			response.Header.Set("Age", "0")
			return response, nil
		case http.MethodPost:
			playbackRequests++
			if request.URL.Host != hongguoCurrentPlayerHost || request.URL.Path != hongguoCurrentPlayerPath {
				t.Fatalf("Current Player endpoint=%s%s", request.URL.Host, request.URL.Path)
			}
			if request.Header.Get("User-Agent") != hongguoCurrentPlayerUA {
				t.Errorf("Current Player UA mismatch: %q", request.Header.Get("User-Agent"))
			}
			query := request.URL.Query()
			expected := map[string]string{
				"device_id": identity.DeviceID, "iid": identity.InstallID, "cdid": identity.CDID,
				"openudid": identity.OpenUDID, "version_code": "73932", "version_name": "7.3.9.32",
				"channel": "xiaomi_8662_64", "device_type": "23127PN0CC", "device_brand": "Xiaomi",
				"os_api": "34", "os_version": "14",
			}
			for key, want := range expected {
				if got := query.Get(key); got != want { t.Errorf("query %s=%q, want %q", key, got, want) }
			}
			if query.Get("ts") == "" || query.Get("_rticket") == "" {
				t.Error("Current Player query missing signed ticket binding")
			}
			for _, name := range []string{"X-Khronos", "X-Argus", "X-Ladon", "X-Gorgon", "X-Helios", "X-Medusa", "X-SS-Stub", "X-SS-Req-Ticket"} {
				if request.Header.Get(name) == "" { t.Errorf("missing Current Player header %s", name) }
			}
			if request.Header.Get("X-SS-Req-Ticket") != query.Get("_rticket") {
				t.Error("X-SS-Req-Ticket does not match the signed _rticket")
			}
			body, err := io.ReadAll(request.Body)
			if err != nil { t.Fatal(err) }
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil { t.Fatal(err) }
			if mapString(payload, "video_id") != videoID {
				t.Errorf("video_id=%q, want %q", mapString(payload, "video_id"), videoID)
			}
			if !strings.Contains(string(body), "video_platform") {
				t.Error("Current Player body omitted video_platform")
			}
			model := `{"video_duration":80.5,"video_list":[{"main_url":"https://media.test/current-player.mp4","backup_url":"https://media.test/current-player-backup.mp4","video_meta":{"codec_type":"h264","definition":"1080p"}}]}`
			response := `{"code":0,"data":{"video_model":` + mustJSONTestString(t, model) + `}}`
			return sourceFixtureResponse(request, http.StatusOK, response), nil
		default:
			t.Fatalf("unexpected Current Player request method %s", request.Method)
			return nil, nil
		}
	})
	d.cfg.Retries = 1
	media, err := d.resolveHongguoCurrentPlayerWithIdentity(context.Background(), videoID, 3, identity)
	if err != nil { t.Fatal(err) }
	if media.URL != "https://media.test/current-player.mp4" {
		t.Fatalf("media URL=%q, want Current Player media", media.URL)
	}
	if playbackRequests != 1 { t.Fatalf("playback requests=%d, want 1", playbackRequests) }
}
