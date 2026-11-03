package core

import "testing"

func TestGuipianParseCards(t *testing.T) {
	body := `<ul class="row video-list video-film-list"><a class="video-link" href="/nv/123.html" title="測試鬼片"><div data-original="/upload/poster.jpg"></div><div class="video-duration">HD</div></a></ul>`
	got := parseGuipianCards(body)
	if len(got) != 1 {
		t.Fatalf("got %d cards, want 1", len(got))
	}
	if got[0].Source != sourceGuipian || got[0].SourceID != "123" || got[0].Title != "測試鬼片" {
		t.Fatalf("unexpected drama: %#v", got[0])
	}
	if got[0].Cover != guipianSiteBaseURL+"/upload/poster.jpg" {
		t.Fatalf("unexpected cover: %#v", got[0].Cover)
	}
}

func TestGuipianPlayURLValidation(t *testing.T) {
	if got := guipianPlayURL("/play/123-1-1.html"); got != guipianSiteBaseURL+"/play/123-1-1.html" {
		t.Fatalf("unexpected normalized URL: %q", got)
	}
	if got := guipianPlayURL("https://example.com/play/123-1-1.html"); got != "" {
		t.Fatalf("foreign host accepted: %q", got)
	}
}

func TestGuipianPlaylistEpisodes(t *testing.T) {
	body := `<li class="swiper-slide ewave-tab" data-target="#ewave-playlist-1">线路一</li><div id="ewave-playlist-1"><ul><li><a title='第1集' href='/play/123-1-1.html'>第1集</a></li><li><a title='第2集' href='/play/123-1-2.html'>第2集</a></li></ul></div><section>`
	name, chapters, err := guipianPickPlaylist(body)
	if err != nil {
		t.Fatal(err)
	}
	if name != "线路一" || len(chapters) != 2 {
		t.Fatalf("unexpected playlist: name=%q chapters=%d", name, len(chapters))
	}
	if chapters[0].PageURL != guipianSiteBaseURL+"/play/123-1-1.html" {
		t.Fatalf("unexpected episode URL: %#v", chapters[0])
	}
}

func TestGuipianCategoryValidation(t *testing.T) {
	if !validGuipianCategory("") || !validGuipianCategory("3") || !validGuipianCategory("11") {
		t.Fatal("valid category rejected")
	}
	if validGuipianCategory("999") || validGuipianCategory("../3") {
		t.Fatal("invalid category accepted")
	}
}
