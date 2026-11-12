package core

import (
	"context"
	"encoding/base64"
	"errors"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestHongguoMultiModelFromResponse(t *testing.T) {
	modelJSON := `{"video_list":[{"main_url":"https://media.test/1080.mp4?token=a%2Fb","video_meta":{"codec_type":"h264","definition":"1080p"}},{"main_url":"https://media.test/720.mp4","video_meta":{"codec_type":"h264","definition":"720p"}}]}`
	cases := []struct {
		name  string
		value any
	}{
		{name: "JSON string", value: modelJSON},
		{name: "base64 JSON string", value: base64.StdEncoding.EncodeToString([]byte(modelJSON))},
		{name: "decoded object", value: map[string]any{"video_list": []any{map[string]any{"main_url": "https://media.test/1080.mp4"}}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			model, err := hongguoMultiModelFromResponse(map[string]any{
				"data": map[string]any{"123456": map[string]any{"video_model": testCase.value}},
			}, "123456")
			if err != nil {
				t.Fatal(err)
			}
			if !hongguoHasVideoList(model["video_list"]) {
				t.Fatalf("model did not preserve its video_list: %+v", model)
			}
		})
	}
}

func TestHongguoFallbackAPIShapes(t *testing.T) {
	want := "https://api.test/fallback?token=a%2Fb"
	cases := []any{
		want,
		map[string]any{"fallback_api": want},
		[]any{map[string]any{"fallback_api": want}},
		`{"fallback_api":"https://api.test/fallback?token=a%2Fb"}`,
	}
	for _, input := range cases {
		if got := hongguoFallbackAPI(input); got != want {
			t.Fatalf("fallback URL changed: got %q, want %q", got, want)
		}
	}
}

func TestHongguoFallbackResponseSelectsPlayableMedia(t *testing.T) {
	response := map[string]any{
		"video_info": map[string]any{"data": map[string]any{
			"video_list": map[string]any{
				"video_1": map[string]any{"main_url": "https://media.test/main.mp4?sig=abc", "video_meta": map[string]any{"codec_type": "h264", "definition": "1080p"}},
				"video_2": map[string]any{"main_url": "https://backup.test/backup.mp4?sig=xyz", "backup_url": "https://backup.test/extra.mp4", "video_meta": map[string]any{"codec_type": "h264", "definition": "1080p"}},
			},
		}},
	}
	media, err := hongguoMediaFromFallbackResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	choices := nativePlaybackChoices(media, 0)
	var got []string
	for _, item := range choices.media {
		got = append(got, item.URL)
	}
	want := []string{"https://media.test/main.mp4?sig=abc", "https://backup.test/backup.mp4?sig=xyz", "https://backup.test/extra.mp4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback candidate order changed: got %#v, want %#v", got, want)
	}
}

func TestResolveHongguoMultiModelMediaUsesDocumentedV1Endpoint(t *testing.T) {
	const videoID = "123456"
	var calls []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls = append(calls, request.URL.Path)
		if request.URL.Path != "/novel/player/multi_video_model/v1/" {
			t.Errorf("unexpected playback endpoint: %s", request.URL.Path)
			return sourceFixtureResponse(request, http.StatusNotFound, "{}"), nil
		}
		if request.Method != http.MethodPost {
			t.Errorf("playback method = %s, want POST", request.Method)
		}
		var payload map[string]any
		decoder := json.NewDecoder(request.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			t.Fatalf("decode playback request: %v", err)
		}
		mixed := nestedMap(payload, "mixed_video_id_map")
		ids := anyList(mixed["1"])
		if len(ids) != 1 || ids[0] != videoID {
			t.Errorf("mixed_video_id_map = %#v, want key 1 with video ID %s", mixed, videoID)
		}
		if mapString(nestedMap(payload, "biz_param"), "video_platform") != "1024" {
			t.Errorf("unexpected video platform in payload: %#v", payload["biz_param"])
		}
		model := `{"video_list":[{"main_url":"https://media.test/1080.mp4","video_meta":{"codec_type":"h264","definition":"1080p"}}]}`
		body := `{"code":0,"data":{"123456":{"video_model":` + mustJSONTestString(t, model) + `}}}`
		return sourceFixtureResponse(request, http.StatusOK, body), nil
	})
	d.cfg.Retries = 1

	media, err := d.resolveHongguoMultiModelMedia(context.Background(), videoID)
	if err != nil {
		t.Fatal(err)
	}
	if media.URL != "https://media.test/1080.mp4" {
		t.Fatalf("media URL = %q, want documented video_model URL", media.URL)
	}
	if !reflect.DeepEqual(calls, []string{"/novel/player/multi_video_model/v1/"}) {
		t.Fatalf("unexpected fallback requests after successful v1 response: %#v", calls)
	}
}

func TestResolveHongguoMultiModelMediaKeepsPreloadFallback(t *testing.T) {
	const videoID = "123456"
	var calls []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls = append(calls, request.URL.Path)
		if request.URL.Path == "/novel/player/multi_video_model/v1/" {
			return sourceFixtureResponse(request, http.StatusOK, `{"code":1001}`), nil
		}
		if request.URL.Path != "/novel/player/multi_video_model/preload/v1" {
			t.Errorf("unexpected playback endpoint: %s", request.URL.Path)
			return sourceFixtureResponse(request, http.StatusNotFound, "{}"), nil
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode preload request: %v", err)
		}
		if mapString(payload, "dr_scene") != "preload" {
			t.Errorf("preload marker missing: %#v", payload)
		}
		model := `{"video_list":[{"main_url":"https://media.test/preload.mp4","video_meta":{"codec_type":"h264","definition":"720p"}}]}`
		body := `{"code":0,"data":{"123456":{"video_model":` + mustJSONTestString(t, model) + `}}}`
		return sourceFixtureResponse(request, http.StatusOK, body), nil
	})
	d.cfg.Retries = 1

	media, err := d.resolveHongguoMultiModelMedia(context.Background(), videoID)
	if err != nil {
		t.Fatal(err)
	}
	if media.URL != "https://media.test/preload.mp4" {
		t.Fatalf("media URL = %q, want preload fallback URL", media.URL)
	}
	if !reflect.DeepEqual(calls, []string{
		"/novel/player/multi_video_model/v1/",
		"/novel/player/multi_video_model/preload/v1",
	}) {
		t.Fatalf("unexpected endpoint fallback order: %#v", calls)
	}
}

func mustJSONTestString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}


func TestSelectHongguoAppMediaReportsH266OnlyForHelperFallback(t *testing.T) {
	model := map[string]any{"video_list": []any{
		map[string]any{"main_url":"https://media.test/h266.mp4","video_meta":map[string]any{"codec_type":"bytevc2","definition":"1080p"}},
	}}
	_, err := selectHongguoAppMedia(model)
	if !errors.Is(err, errHongguoH266Only) {
		t.Fatalf("H.266-only response error = %v, want errHongguoH266Only", err)
	}
}


func TestResolveHongguoHelperMediaUsesH265OnlyForHelperDevice(t *testing.T) {
	const videoID = "123456"
	var calls []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		deviceID := request.URL.Query().Get("device_id")
		calls = append(calls, request.URL.Path+"@"+deviceID)
		if request.URL.Path == "/service/2/device_register/" {
			return sourceFixtureResponse(request, http.StatusOK, `{"device_id_str":"9001","install_id_str":"90019"}`), nil
		}
		if request.URL.Path == "/novel/player/video_model/v1/" {
			if deviceID == "9001" {
				model := `{"video_list":[{"main_url":"https://media.test/helper-h265.mp4","video_meta":{"codec_type":"h264","definition":"1080p"}}]}`
				return sourceFixtureResponse(request, http.StatusOK, `{"code":0,"data":{"video_model":`+mustJSONTestString(t,model)+`}}`),nil
			}
			return sourceFixtureResponse(request,http.StatusForbidden,`{}`),nil
		}
		return sourceFixtureResponse(request,http.StatusNotFound,`{}`),nil
	})
	d.cfg.Retries = 1
	client := d.hongguoClient()
	client.state.DeviceID,client.state.InstallID,client.state.Registered = "8001","80019",true
	media,err := d.resolveHongguoHelperMedia(context.Background(),"123","123456")
	if err != nil { t.Fatal(err) }
	if media.URL!="https://media.test/helper-h265.mp4" { t.Fatalf("helper URL = %q",media.URL) }
	if !client.state.HelperRegistered || client.state.HelperDeviceID!="9001" { t.Fatalf("helper device not registered: %#v",client.state) }
	if len(calls)<2 || calls[0] != "/service/2/device_register/@8001" && calls[0] != "/service/2/device_register/@" {
		// A helper registration request must be present; the request itself intentionally omits device_id.
		found:=false
		for _,call:=range calls { if call=="/service/2/device_register/@" { found=true } }
		if !found { t.Fatalf("helper device registration missing: %#v",calls) }
	}
}

func TestHongguoMediaSelectionDistinguishesH266FromOtherUnavailableMedia(t *testing.T) {
	_,err:=selectHongguoAppMedia(map[string]any{"video_list":[]any{
		map[string]any{"main_url":"https://media.test/a.mp4","video_meta":map[string]any{"codec_type":"bytevc2"}},
	}})
	if !errors.Is(err,errHongguoH266Only){t.Fatalf("H266 result = %v",err)}
	_,err=selectHongguoAppMedia(map[string]any{"video_list":[]any{
		map[string]any{"video_meta":map[string]any{"codec_type":"h264"}},
	}})
	if errors.Is(err,errHongguoH266Only){t.Fatalf("missing-address result must not look like H266: %v",err)}
}
