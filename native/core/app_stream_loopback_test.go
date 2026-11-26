package core

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNativePlaybackReopensUnreachableLoopback(t *testing.T) {
	ctx := context.Background()
	engine := &nativeEngine{downloader: &Downloader{}}
	stream, err := engine.streamServer()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.server.Close() })
	choice := nativePlaybackChoices(providerMedia{
		URL: "https://media.example/index.m3u8",
		Playlist: "#EXTM3U\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST",
	}, 0)
	before, err := engine.nativeOpenPlayback(ctx, choice)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.nativeReleasePlayback(before.Session) })
	if err := stream.server.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := engine.nativeOpenPlayback(ctx, choice)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.nativeReleasePlayback(after.Session) })
	oldURL, _ := url.Parse(before.URL)
	newURL, _ := url.Parse(after.URL)
	if oldURL.Host == newURL.Host {
		t.Fatalf("reused unreachable listener %s", oldURL.Host)
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get(after.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "http://"+newURL.Host+"/") {
		t.Fatalf("recovered playlist: status=%d err=%v body=%s", response.StatusCode, err, body)
	}
	// Existing LAN sessions retain their original origin for rewriting even
	// when the loopback listener has been replaced.
	engine.mu.Lock()
	oldChoice := engine.playbacks[before.Session]
	engine.mu.Unlock()
	stream.mu.Lock()
	oldSession := stream.sessions[oldChoice.streamSession]
	stream.mu.Unlock()
	if oldSession == nil || oldSession.origin != "http://"+oldURL.Host {
		t.Fatal("old session origin was lost")
	}
	request, _ := http.NewRequest(http.MethodGet, "http://192.0.2.1:12345/test", nil)
	if got := oldSession.lanPlaylist(request, before.URL); !strings.HasPrefix(got, "http://192.0.2.1:12345/") {
		t.Fatalf("LAN playlist lost its original origin: %s", got)
	}
}
