package core

import "testing"

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
