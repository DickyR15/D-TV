package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
		{name: "verification required", err: openhongguo.ErrChallengeRequired, want: true},
		{name: "signature rejected", err: openhongguo.ErrSignatureRejected, want: true},
		{name: "anonymous playback response empty", err: openhongguo.ErrAnonymousLimited, want: true},
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


type hongguoSDKTestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip hongguoSDKTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestHongguoSDKPlaybackRefreshesRejectedAnonymousDeviceTwice(t *testing.T) {
	var registrations int
	const registerHost = "log5-applog-lq.fqnovel.com"
	const apiHost = "api5-normal-sinfonlinea.fqnovel.com"
	playbackDeviceIDs := map[string]bool{}
	transport := hongguoSDKTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		var body string
		switch {
		case request.URL.Host == registerHost && request.URL.Path == hongguoDeviceRegisterPath:
			registrations++
			body = fmt.Sprintf(
				`{"new_user":1,"device_id_str":"%d","install_id_str":"%d"}`,
				7100000000000000+registrations, 7200000000000000+registrations,
			)
		case request.URL.Host == apiHost && strings.HasPrefix(request.URL.Path, "/novel/player/"):
			deviceID := request.URL.Query().Get("device_id")
			if deviceID == "" {
				t.Fatalf("playback request missing device_id: %s", request.URL.Path)
			}
			playbackDeviceIDs[deviceID] = true
			body = ""
		default:
			t.Fatalf("unexpected Redfruit SDK request: %s %s", request.Method, request.URL.Host+request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"Set-Cookie": []string{"ttreq=unit-test-ticket; Path=/"},
			},
			Body: io.NopCloser(strings.NewReader(body)),
			Request: request,
		}, nil
	})
	downloader := &Downloader{
		cfg: Config{dataDir: t.TempDir()},
		client: &http.Client{Transport: transport},
	}
	client, err := downloader.hongguoSDKClient()
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ResolvePlayback(context.Background(), "7690163276214176792", openhongguo.PlaybackOptions{})
	if !errors.Is(err, openhongguo.ErrAnonymousLimited) {
		t.Fatalf("ResolvePlayback error = %v, want anonymous empty response", err)
	}
	if registrations != 3 {
		t.Fatalf("device registrations = %d, want initial registration plus two refreshes", registrations)
	}
	if len(playbackDeviceIDs) != 3 {
		t.Fatalf("unique playback device IDs = %d, want three refreshed identities", len(playbackDeviceIDs))
	}
}

func TestHongguoSDKPlaybackSoftRejectCooldownPreventsRepeatedRegistration(t *testing.T) {
	session := &hongguoSDKClientSession{}
	session.recordPlaybackResult(openhongguo.ErrAnonymousLimited)
	err := session.playbackCooldownError()
	if !errors.Is(err, errHongguoSDKPlaybackCooling) {
		t.Fatalf("empty playback response did not enter cooldown: %v", err)
	}
	if !hongguoSDKPlaybackRegistrationRejected(err) {
		t.Fatal("playback cooldown must suppress legacy App registration fallbacks")
	}
	session.recordPlaybackResult(nil)
	if err := session.playbackCooldownError(); err != nil {
		t.Fatalf("successful playback did not clear cooldown: %v", err)
	}
}

func TestHongguoSDKPlaybackGateHonorsCancellationAndCooldown(t *testing.T) {
	session := &hongguoSDKClientSession{}
	release, err := session.acquirePlayback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if unexpectedRelease, err := session.acquirePlayback(ctx); !errors.Is(err, context.Canceled) {
		if unexpectedRelease != nil {
			unexpectedRelease()
		}
		release()
		t.Fatalf("cancelled wait error = %v", err)
	}
	session.recordPlaybackResult(openhongguo.ErrAnonymousLimited)
	session.recordPlaybackResult(context.Canceled)
	release()
	nextRelease, err := session.acquirePlayback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer nextRelease()
	if err := session.playbackCooldownError(); !errors.Is(err, errHongguoSDKPlaybackCooling) {
		t.Fatalf("queued playback must retain preceding rejection cooldown: %v", err)
	}
}

func TestHongguoSDKTransportPreservesRawCompressedResponse(t *testing.T) {
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	if _, err := writer.Write([]byte("synthetic response")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		headers map[string]string
		wantEncoding string
	}{
		{name: "no explicit encoding", wantEncoding: "identity"},
		{name: "explicit gzip", headers: map[string]string{"Accept-Encoding": "gzip"}, wantEncoding: "gzip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Accept-Encoding"); got != tc.wantEncoding {
					t.Errorf("Accept-Encoding = %q, want %q", got, tc.wantEncoding)
				}
				w.Header().Set("Content-Encoding", "gzip")
				_, _ = w.Write(encoded.Bytes())
			}))
			defer server.Close()
			transport := hongguoSDKTransport{client: server.Client()}
			response, err := transport.Do(context.Background(), &openhongguo.Request{
				Method: http.MethodGet, URL: server.URL, Headers: tc.headers,
			})
			if err != nil {
				t.Fatal(err)
			}
			if response.Headers["Content-Encoding"] != "gzip" || !bytes.Equal(response.Body, encoded.Bytes()) {
				t.Fatal("SDK transport must preserve the encoded response and its header")
			}
		})
	}
}

func TestHongguoPlaybackAccessStatus(t *testing.T) {
	for _, tc := range []struct {
		err error
		want string
	}{
		{nil, "available"},
		{fmt.Errorf("wrapped: %w", openhongguo.ErrAnonymousLimited), "anonymous_empty"},
		{errHongguoSDKPlaybackCooling, "anonymous_empty"},
		{openhongguo.ErrAuthenticationRequired, "authentication_required"},
		{openhongguo.ErrChallengeRequired, "verification_required"},
		{openhongguo.ErrSignatureRejected, "signature_rejected"},
		{openhongguo.ErrRateLimited, "rate_limited"},
		{&openhongguo.APIError{HTTPStatus: 403}, "access_rejected"},
		{context.Canceled, "interrupted"},
		{errors.New("network failure"), "request_failed"},
	} {
		got, explanation := hongguoPlaybackAccessStatus(tc.err)
		if got != tc.want || explanation == "" {
			t.Fatalf("status = %q (%q), want %q", got, explanation, tc.want)
		}
	}
}
