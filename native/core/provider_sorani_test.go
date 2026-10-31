package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSoraniAcceptsCurrentOpaqueVideoAndEpisodeIDs(t *testing.T) {
	const videoID = "6aca7a6a39897e0688f09952"
	const episodeID = "b447e8aef29d4a10b8c3c9d4"

	if !soraniValidID("12345") || !soraniValidID(videoID) || !soraniValidID(episodeID) {
		t.Fatal("Sorani rejected a supported numeric or opaque source ID")
	}
	for _, invalid := range []string{"", "../episode", "a/b", "a:b", "a b", strings.Repeat("x", soraniMaxID+1)} {
		if soraniValidID(invalid) {
			t.Fatalf("Sorani accepted an unsafe ID %q", invalid)
		}
	}
	drama, err := soraniDramaFromMap(map[string]any{
		"id": videoID, "title": "青空測試番劇", "episodeCount": 2,
	})
	if err != nil || drama.SourceID != videoID || drama.ID != providerDramaID(sourceSorani, videoID) {
		t.Fatalf("current Sorani video ID was not parsed: %+v, %v", drama, err)
	}
}

func TestSoraniPlaybackResolvesOpaqueEpisodeIDAndPreservesReferer(t *testing.T) {
	const videoID = "6aca7a6a39897e0688f09952"
	const episodeID = "b447e8aef29d4a10b8c3c9d4"
	var mediaReferer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/video/episode/"+episodeID+"/play":
			if r.Method != http.MethodGet || r.URL.Query().Get("lineCode") != soraniLineCodeDefault {
				t.Errorf("unexpected Sorani play request: %s %s", r.Method, r.URL.String())
			}
			if r.Header.Get("Referer") != serverURL(r) {
				t.Errorf("API Referer = %q, want %q", r.Header.Get("Referer"), serverURL(r))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"code":200,"data":{"playUrl":%q}}`, "http://"+r.Host+"/media/index.m3u8?signature=fixture")
		case r.URL.Path == "/media/index.m3u8":
			mediaReferer = r.Header.Get("Referer")
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nsegment.ts?token=fixture\n#EXT-X-ENDLIST\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	d := &Downloader{
		cfg: Config{SoraniAPIURL: server.URL + "/api", SoraniURL: server.URL},
		client: server.Client(),
	}
	task := Task{
		DramaID: providerDramaID(sourceSorani, videoID),
		Chapter: Chapter{
			ID: providerChapterID(sourceSorani, videoID, episodeID),
			Source: sourceSorani,
			CurrentEpisode: json.RawMessage("2"),
		},
	}
	media, err := d.resolveSoraniMedia(context.Background(), task)
	if err != nil {
		t.Fatalf("Sorani media resolution failed for opaque episode ID: %v", err)
	}
	if media.Playlist == "" || !strings.HasPrefix(media.Playlist, "#EXTM3U") {
		t.Fatalf("Sorani playlist was not read: %+v", media)
	}
	if media.URL != server.URL+"/media/index.m3u8?signature=fixture" {
		t.Fatalf("Sorani final media URL = %q", media.URL)
	}
	if mediaReferer != soraniSiteBaseURL+"/" {
		t.Fatalf("media Referer = %q, want %q", mediaReferer, soraniSiteBaseURL+"/")
	}
}

func serverURL(request *http.Request) string {
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + request.Host + "/"
}

func TestProviderSourceRecognizesSoraniMediaCDN(t *testing.T) {
	for _, address := range []string{
		"https://sorani.net/",
		"https://www.sorani.net/",
		"https://api.sorani.cc/sorani-cms/api/video",
		"https://www.sorani-vids.xyz/videos/sample/index.m3u8",
	} {
		if got := providerSourceForURL(address); got != sourceSorani {
			t.Fatalf("Sorani host not recognized: %s -> %s", address, got)
		}
	}
}

