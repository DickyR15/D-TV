package core

import (
	"encoding/base64"
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
