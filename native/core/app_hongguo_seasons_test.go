package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	openhongguo "github.com/beilunyang/openhongguo/sdk"
	openhongguomodel "github.com/beilunyang/openhongguo/sdk/model"
)

func TestHongguoRegistrationWasRateLimitedOnlyForHTTP444(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{message: "取得紅果系列季別失敗：紅果設備註冊 HTTP 444", want: true},
		{message: "openhongguo: HTTP 444", want: true},
		{message: "紅果設備註冊 HTTP 403", want: false},
		{message: "紅果設備註冊 HTTP 429", want: false},
		{message: "context deadline exceeded", want: false},
		{message: "", want: false},
	} {
		var err error
		if tc.message != "" {
			err = errors.New(tc.message)
		}
		if got := hongguoRegistrationWasRateLimited(err); got != tc.want {
			t.Errorf("hongguoRegistrationWasRateLimited(%q) = %v, want %v", tc.message, got, tc.want)
		}
	}
}

func TestHongguoShouldUseCurrentSeasonFallbackForUnavailableAnonymousSeasonReads(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{message: "openhongguo: HTTP 444", want: true},
		{message: "openhongguo: HTTP 404", want: true},
		{message: "openhongguo: HTTP 401", want: true},
		{message: "openhongguo: HTTP 403", want: true},
		{message: "openhongguo: authentication required", want: true},
		{message: "openhongguo: album is not available anonymously", want: true},
		{message: "openhongguo: anonymous response was empty", want: true},
		{message: "openhongguo: device rejected", want: true},
		{message: "openhongguo: content unavailable (101116)", want: true},
		{message: "openhongguo: rate limited", want: false},
		{message: "context deadline exceeded", want: false},
		{message: "", want: false},
	} {
		var err error
		if tc.message != "" {
			err = errors.New(tc.message)
		}
		if got := hongguoShouldUseCurrentSeasonFallback(err); got != tc.want {
			t.Errorf("hongguoShouldUseCurrentSeasonFallback(%q) = %v, want %v", tc.message, got, tc.want)
		}
	}
}

func TestHongguoCurrentSeasonFallbackKeepsKnownEpisodeCountAndDoesNotInventSiblings(t *testing.T) {
	drama := nativeDrama{
		ID:                 "hongguo:7690163276214176792",
		Source:             sourceHongguo,
		SourceID:           "7690163276214176792",
		Title:              "少夫人來自東北3",
		Episodes:           96,
		SeriesSeasonNumber: 3,
		SeriesSeasonUnit:   "季",
	}
	items := hongguoCurrentSeasonFallback(drama)
	if len(items) != 1 {
		t.Fatalf("fallback should preserve only the currently known season: %+v", items)
	}
	item := items[0]
	if item.Drama.ID != drama.ID || item.Drama.Episodes != 96 ||
		item.Number != 3 || item.Label != "第3季" ||
		!item.Playable || item.Upcoming {
		t.Fatalf("current season fallback lost known playable metadata: %+v", item)
	}
}

func TestHongguoSDKAlbumToNativeSeasonsPreservesOfficialIDsAndProgressMetadata(t *testing.T) {
	album := &openhongguomodel.Album{Seasons: []openhongguomodel.Season{
		{SeriesID: "7628202881140657177", Title: "萬妖圖錄傳第二季", Index: 2, CoverURL: "https://img.example.test/season-2.jpg", EpisodeCount: 53, Status: openhongguomodel.SeriesUpdating},
		{SeriesID: "7621200213801176126", Title: "萬妖圖錄傳第一季", Index: 1, CoverURL: "https://img.example.test/season-1.jpg", EpisodeCount: 81, Status: openhongguomodel.SeriesEnd},
		{SeriesID: "7691518344146455576", Title: "萬妖圖錄傳第十四季", Index: 14, EpisodeCount: 0, Status: openhongguomodel.SeriesUpdating},
		{SeriesID: "7621200213801176126", Title: "萬妖圖錄傳第一季 duplicate", Index: 1, EpisodeCount: 81},
	}}
	items, err := hongguoSDKAlbumToNativeSeasons(album)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected three unique official seasons, got %+v", items)
	}
	if items[0].Drama.SourceID != "7621200213801176126" || items[0].Number != 1 ||
		items[0].Drama.Episodes != 81 || !items[0].Playable {
		t.Fatalf("season one metadata mismatch: %+v", items[0])
	}
	if items[1].Drama.SourceID != "7628202881140657177" || items[1].Number != 2 ||
		items[1].Drama.Episodes != 53 || !items[1].Playable {
		t.Fatalf("season two metadata mismatch: %+v", items[1])
	}
	if items[2].Number != 14 || items[2].Playable || !items[2].Upcoming {
		t.Fatalf("upcoming season must remain visible but unplayable: %+v", items[2])
	}
	for _, item := range items {
		if item.Drama.SeriesGroupID != "7621200213801176126" {
			t.Fatalf("season favorite group is not canonical: %+v", item)
		}
	}
}

func TestHongguoSDKProfileStoreClonesAndResetsProfiles(t *testing.T) {
	store := &hongguoSDKMemoryProfileStore{}
	if _, err := store.Load(context.Background()); !errors.Is(err, openhongguo.ErrProfileNotFound) {
		t.Fatalf("empty profile store error = %v", err)
	}
	device := openhongguomodel.DefaultDevice
	profile := &openhongguo.DeviceProfile{
		CDID: "cdid", OpenUDID: "openudid", DeviceID: "12345", InstallID: "67890", Device: &device,
	}
	if err := store.Save(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	profile.Device.Model = "mutated"
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Device == nil || loaded.Device.Model != openhongguomodel.DefaultDevice.Model ||
		loaded.DeviceID != "12345" {
		t.Fatalf("profile was not safely cloned: %+v", loaded)
	}
	if err := store.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, openhongguo.ErrProfileNotFound) {
		t.Fatalf("reset profile store error = %v", err)
	}
}

func TestHongguoSDKTransportForwardsResponseHeadersAndCookies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Header") != "season-sdk" {
			t.Errorf("request header was not forwarded: %v", r.Header)
		}
		w.Header().Set("X-Test-Response", "ok")
		http.SetCookie(w, &http.Cookie{Name: "ttreq", Value: "guest-ticket"})
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	transport := hongguoSDKTransport{client: server.Client()}
	response, err := transport.Do(context.Background(), &openhongguo.Request{
		Method: http.MethodGet, URL: server.URL, Headers: map[string]string{"X-Test-Header": "season-sdk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(response.Body) != `{"ok":true}` ||
		response.Headers["X-Test-Response"] != "ok" || response.Cookies["ttreq"] != "guest-ticket" {
		t.Fatalf("unexpected SDK transport response: %+v", response)
	}
}
