package core

import (
	"encoding/json"
	"testing"
)

func TestHongguoQuickAppMediaFromPageDecodesEscapedPublicAddress(t *testing.T) {
	body := "<html><script>window.player={\"video_player_info\":{\"main_url\":\"https:\\/\\/media.test\\/episode.mp4?token=a\\u002Fb&amp;sig=1\"}};</script></html>"
	media, err := hongguoQuickAppMediaFromPage(body)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://media.test/episode.mp4?token=a/b&sig=1"
	if media.URL != want {
		t.Fatalf("decoded media URL = %q, want %q", media.URL, want)
	}
	if media.Referer != "https://novelquickapp.com/" {
		t.Fatalf("unexpected media referer: %q", media.Referer)
	}
}

func TestHongguoQuickAppMediaFromPageRejectsMissingOrUnsafeAddress(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "missing player data", body: "<html>no player data</html>"},
		{name: "missing main URL", body: "<script>video_player_info={\"other_url\":\"https://media.test/episode.mp4\"}</script>"},
		{name: "non HTTP scheme", body: "<script>video_player_info={\"main_url\":\"javascript:alert(1)\"}</script>"},
		{name: "credentials in URL", body: "<script>video_player_info={\"main_url\":\"https://user:pass@media.test/episode.mp4\"}</script>"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if media, err := hongguoQuickAppMediaFromPage(testCase.body); err == nil || media.URL != "" {
				t.Fatalf("unsafe or missing media was accepted: %+v, %v", media, err)
			}
		})
	}
}

func TestHongguoSkippedAttemptDiagnostic(t *testing.T) {
	if got := hongguoAttemptDiagnostic(nil); got != "未執行" {
		t.Fatalf("skipped attempt diagnostic = %q", got)
	}
}

func TestHongguoExactChapterIdentity(t *testing.T) {
	const series = "7710822671759461438"
	const video = "7710822671759461439"
	for _, tc := range []struct{ value any; want string }{
		{video, video},
		{json.Number(video), video},
		{map[string]any{"vid": json.Number(video)}, video},
		{map[string]any{"id": video}, ""},
		{float64(7710822671759461439), ""},
		{json.Number("7.7108e18"), ""},
		{"0", ""},
		{"map[vid:123]", ""},
	} {
		if got := hongguoWebChapterID(tc.value); got != tc.want {
			t.Errorf("video ID = %q, want %q for %T", got, tc.want, tc.value)
		}
	}
	c := Chapter{ID: providerChapterID(sourceHongguo, series, video), VideoURL: "hongguo-cenc://" + video}
	if !hongguoChaptersValid(series, []Chapter{c}) || hongguoChaptersValid(series, []Chapter{c,c}) {
		t.Fatal("chapter validation did not reject duplicate IDs")
	}
	c.VideoURL = "hongguo-cenc://invalid"
	if got := hongguoChapterVideoID(series, c); got != video {
		t.Fatalf("canonical stored identity = %q, want %q", got, video)
	}
	if got := hongguoChapterVideoID("1234", c); got != "" {
		t.Fatalf("unrelated chapter ID was accepted: %q", got)
	}
	c.ID = ""
	if hongguoChaptersValid(series, []Chapter{c}) {
		t.Fatal("invalid chapter accepted")
	}
}
