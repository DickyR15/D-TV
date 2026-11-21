package core

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	openhongguo "github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"
)

func TestHongguoSDKPlaybackMapsStreamsAndBackupURLs(t *testing.T) {
	playback := &model.Playback{
		DurationSeconds: 95,
		Streams: []model.Stream{
			{
				Definition: "720p", Codec: "h264", Width: 720, Height: 1280,
				URL: "https://media.test/720.m3u8?token=720",
				BackupURL: "https://backup.test/720.m3u8?token=720",
				HLS: true,
			},
			{
				Definition: "1080p", Codec: "h264", Width: 1080, Height: 1920,
				URL: "https://media.test/1080.m3u8?token=1080",
				BackupURL: "https://backup.test/1080.m3u8?token=1080",
				HLS: true,
			},
		},
	}
	got, err := hongguoSDKPlaybackToNativeMedia(playback)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://media.test/1080.m3u8?token=1080" {
		t.Fatalf("selected stream = %q, want the highest-quality stream", got.URL)
	}
	if got.Quality != 1080 || got.Duration != 95*time.Second {
		t.Fatalf("selected quality/duration = %d/%s", got.Quality, got.Duration)
	}
	var urls []string
	for _, variant := range got.Variants {
		urls = append(urls, variant.URL)
	}
	want := []string{
		"https://media.test/1080.m3u8?token=1080",
		"https://backup.test/1080.m3u8?token=1080",
		"https://media.test/720.m3u8?token=720",
		"https://backup.test/720.m3u8?token=720",
	}
	if !reflect.DeepEqual(urls, want) {
		t.Fatalf("stream and backup order = %#v, want %#v", urls, want)
	}
	if got.Referer == "" {
		t.Fatal("SDK stream mapping must preserve a playback referer")
	}
}

func TestHongguoSDKPlaybackRejectsInvalidAndUnsupportedStreams(t *testing.T) {
	for _, tc := range []struct {
		name string
		playback *model.Playback
		wantH266 bool
	}{
		{name: "nil"},
		{name: "empty", playback: &model.Playback{}},
		{name: "invalid address", playback: &model.Playback{Streams: []model.Stream{{Definition: "720p", Codec: "h264", URL: "javascript:alert(1)"}}}},
		{name: "H266 only", playback: &model.Playback{Streams: []model.Stream{{Definition: "1080p", Codec: "bytevc2", URL: "https://media.test/h266.mp4"}}}, wantH266: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := hongguoSDKPlaybackToNativeMedia(tc.playback)
			if err == nil {
				t.Fatal("invalid playback model unexpectedly accepted")
			}
			if errors.Is(err, errHongguoH266Only) != tc.wantH266 {
				t.Fatalf("H.266 classification = %v, want %v (err=%v)",
					errors.Is(err, errHongguoH266Only), tc.wantH266, err)
			}
		})
	}
}

func TestHongguoSDKRegistrationCooldownBlocksRepeatedGuestRegistration(t *testing.T) {
	session := &hongguoSDKClientSession{}
	if err := session.registrationCooldownError(); err != nil {
		t.Fatalf("new SDK session unexpectedly starts in cooldown: %v", err)
	}
	session.recordRegistrationStatus(444)
	err := session.registrationCooldownError()
	if !errors.Is(err, errHongguoSDKRegistrationCooling) {
		t.Fatalf("HTTP 444 did not enter registration cooldown: %v", err)
	}
	if !hongguoSDKPlaybackRegistrationRejected(err) {
		t.Fatal("cooldown error must suppress legacy App registration fallbacks")
	}
	session.recordRegistrationStatus(http.StatusOK)
	if err := session.registrationCooldownError(); err != nil {
		t.Fatalf("successful registration did not clear cooldown: %v", err)
	}
}

func TestHongguoSDKRegistrationCooldownClassifiesEdgeStatuses(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusTooManyRequests, 444,
	} {
		t.Run(fmt.Sprintf("HTTP-%d", status), func(t *testing.T) {
			session := &hongguoSDKClientSession{}
			session.recordRegistrationStatus(status)
			err := session.registrationCooldownError()
			if !errors.Is(err, errHongguoSDKRegistrationCooling) {
				t.Fatalf("HTTP %d did not enter SDK registration cooldown: %v", status, err)
			}
		})
	}
}

func TestHongguoSDKPlaybackRegistrationRejectedClassifiesEdgeAndPolicyResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		err error
		want bool
	}{
		{name: "edge 444", err: &openhongguo.APIError{HTTPStatus: 444}, want: true},
		{name: "forbidden", err: &openhongguo.APIError{HTTPStatus: 403}, want: true},
		{name: "rate limited", err: &openhongguo.APIError{HTTPStatus: 429}, want: true},
		{name: "device rejected", err: openhongguo.ErrDeviceRejected, want: true},
		{name: "authentication", err: openhongguo.ErrAuthenticationRequired, want: true},
		{name: "server error", err: &openhongguo.APIError{HTTPStatus: 503}, want: false},
		{name: "timeout", err: errors.New("context deadline exceeded"), want: false},
		{name: "nil", err: nil, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hongguoSDKPlaybackRegistrationRejected(tc.err); got != tc.want {
				t.Fatalf("registration rejection = %v, want %v", got, tc.want)
			}
		})
	}
}
