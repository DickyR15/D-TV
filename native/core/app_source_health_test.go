package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSourceHealthSampleCandidatesSkipsVIPAndChecksSeveralFreeWorks(t *testing.T) {
	vip := true
	nonVIP := false
	items := []nativeDrama{
		{ID:"91porna:stale-1",Title:"第一部失效作品",VIP:&nonVIP},
		{ID:"91porna:vip-1",Title:"VIP 作品",VIP:&vip},
		{ID:"91porna:stale-2",Title:"第二部失效作品",VIP:&nonVIP},
		{ID:"91porna:free-3",Title:"第三部免費作品",VIP:&nonVIP},
		{ID:"91porna:free-4",Title:"第四部免費作品",VIP:&nonVIP},
		{ID:"91porna:free-5",Title:"第五部免費作品",VIP:&nonVIP},
		{ID:"91porna:free-6",Title:"第六部免費作品",VIP:&nonVIP},
	}
	got := sourceHealthSampleCandidates(nativeDrama{}, items)
	if len(got) != 5 {
		t.Fatalf("health sample candidate count = %d, want 5: %#v", len(got), got)
	}
	for _, item := range got {
		if item.VIP != nil && *item.VIP {
			t.Fatalf("VIP item should not be selected while free candidates exist: %#v", item)
		}
	}
	if got[0].ID != "91porna:stale-1" || got[1].ID != "91porna:stale-2" {
		t.Fatalf("catalogue order was not preserved among free samples: %#v", got)
	}
}

func TestSourceHealthSampleCandidatesKeepsExplicitSelection(t *testing.T) {
	selected := nativeDrama{ID:"91porna:selected",Title:"明確選取的作品"}
	items := []nativeDrama{{ID:"91porna:other",Title:"其他作品"}}
	got := sourceHealthSampleCandidates(selected, items)
	if len(got) != 1 || got[0].ID != selected.ID {
		t.Fatalf("explicit diagnostic selection was silently replaced: %#v", got)
	}
}

func TestSourceProbePlayableRouteFallsBackWhenPreferredCDNIsDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bad.m3u8":
			http.Error(w, "TLS policy denied this edge", http.StatusForbidden)
		case "/good.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\nsegment.ts\n#EXT-X-ENDLIST\n"))
		case "/segment.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("transport-stream-segment"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := server.Client()
	d := &Downloader{client: client, streamClient: client}
	media := providerMedia{
		URL: server.URL + "/bad.m3u8",
		Referer: server.URL + "/",
		StandardHTTP: true,
		Variants: []providerMedia{{
			URL: server.URL + "/good.m3u8",
			Referer: server.URL + "/",
			StandardHTTP: true,
		}},
	}
	selected, keyURL, segmentURL, segmentBytes, err := d.sourceProbePlayableRoute(context.Background(), media)
	if err != nil {
		t.Fatalf("source health should select the working same-episode fallback: %v", err)
	}
	if !strings.HasSuffix(selected.URL, "/good.m3u8") ||
		keyURL != "" ||
		!strings.HasSuffix(segmentURL, "/segment.ts") ||
		len(segmentBytes) == 0 {
		t.Fatalf("unexpected selected route: url=%q key=%q segment=%q", selected.URL, keyURL, segmentURL)
	}
}

func TestSourceProbePlayableRouteReportsFailureWhenAllRoutesFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()
	client := server.Client()
	d := &Downloader{client: client, streamClient: client}
	media := providerMedia{
		URL: server.URL + "/one.m3u8",
		Referer: server.URL + "/",
		StandardHTTP: true,
		Variants: []providerMedia{{URL: server.URL + "/two.m3u8", Referer: server.URL + "/", StandardHTTP: true}},
	}
	if _, _, _, _, err := d.sourceProbePlayableRoute(context.Background(), media); err == nil {
		t.Fatal("all-denied routes should fail the playback health check")
	}
}
