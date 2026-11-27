package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
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
		{errHongguoSDKPlaybackCooling, "playback_cooldown"},
		{errHongguoSDKRegistrationCooling, "registration_cooldown"},
		{openhongguo.ErrAuthenticationRequired, "authentication_required"},
		{openhongguo.ErrChallengeRequired, "verification_required"},
		{openhongguo.ErrSignatureRejected, "signature_rejected"},
		{openhongguo.ErrRateLimited, "rate_limited"},
		{&openhongguo.APIError{HTTPStatus: 403}, "access_rejected"},
		{fmt.Errorf("wrapped: %w", &openhongguo.APIError{HTTPStatus: 403}), "access_rejected"},
		{&openhongguo.APIError{HTTPStatus: 401}, "authentication_required"},
		{&openhongguo.APIError{HTTPStatus: 403, Code: "auth_required"}, "authentication_required"},
		{&openhongguo.APIError{HTTPStatus: 403, Code: "signature_rejected"}, "signature_rejected"},
		{&openhongguo.APIError{HTTPStatus: 403, Code: "rate_limited"}, "rate_limited"},
		{context.Canceled, "interrupted"},
		{errors.New("network failure"), "request_failed"},
		{openhongguo.ErrTransport, "transport_failed"},
	} {
		got, explanation := hongguoPlaybackAccessStatus(tc.err)
		if got != tc.want || explanation == "" {
			t.Fatalf("status = %q (%q), want %q", got, explanation, tc.want)
		}
	}
}

func TestHongguoSDKTransportFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		err error
		want string
	}{
		{context.Canceled, "cancelled"},
		{context.DeadlineExceeded, "timeout"},
		{&net.DNSError{Err: "no such host", Name: "example.invalid"}, "dns"},
		{x509.UnknownAuthorityError{}, "tls_certificate"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection failed")}, "connection"},
		{io.ErrUnexpectedEOF, "connection_closed"},
		{errors.New("other"), "transport"},
	} {
		if got := hongguoSDKTransportFailureKind(fmt.Errorf("wrapped: %w", tc.err)); got != tc.want {
			t.Errorf("classification = %q, want %q", got, tc.want)
		}
	}
}

func TestHongguoSDKTransportReportsFailureWithoutURL(t *testing.T) {
	var host, stage, kind string
	client := &http.Client{Transport: hongguoSDKTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		return nil, &net.DNSError{Err: "no such host", Name: r.URL.Hostname()}
	})}
	transport := hongguoSDKTransport{
		client: client,
		onTransportFailure: func(h, s, k string, elapsed time.Duration) {
			host, stage, kind = h, s, k
		},
	}
	_, err := transport.Do(context.Background(), &openhongguo.Request{
		URL: "https://example.invalid/test?private=synthetic",
	})
	if err == nil || host != "example.invalid" || stage != "request" || kind != "dns" {
		t.Fatalf("unexpected diagnostic host=%q stage=%q kind=%q err=%v", host, stage, kind, err)
	}
}

func TestHongguoSDKEpisodesPreserveIdentityOrderAndAvailability(t *testing.T) {
	const series = "7710822671759461438"
	const first = "7710822671759461439"
	const second = "7710822671759461440"
	rows := []model.Episode{
		{ID: second, SeriesID: series, Number: 2, Playable: false},
		{ID: first, SeriesID: series, Number: 1, Playable: true, Vertical: true},
	}
	chapters, err := hongguoSDKEpisodesToChapters(series, rows)
	if err != nil || len(chapters) != 2 {
		t.Fatalf("unexpected chapter list: %+v, %v", chapters, err)
	}
	if chapters[0].ID != providerChapterID(sourceHongguo, series, first) ||
		chapters[0].VideoURL != "hongguo-cenc://" + first ||
		chapters[0].VIP || chapters[0].Vertical == nil || !*chapters[0].Vertical {
		t.Fatalf("playable episode mapping: %+v", chapters[0])
	}
	if chapters[1].EpisodeString(0) != "2" || !chapters[1].VIP || chapters[1].VideoURL != "" {
		t.Fatalf("unavailable episode mapping: %+v", chapters[1])
	}
	for _, invalid := range [][]model.Episode{
		{{ID: first, SeriesID: "9999999999999999999", Number: 1, Playable: true}},
		{{ID: first, SeriesID: series, Number: 1}, {ID: second, SeriesID: series, Number: 1}},
		{{ID: first, SeriesID: series, Number: 1}, {ID: first, SeriesID: series, Number: 2}},
		{{ID: "0", SeriesID: series, Number: 1}},
	} {
		if _, err := hongguoSDKEpisodesToChapters(series, invalid); err == nil {
			t.Fatalf("invalid SDK list accepted: %+v", invalid)
		}
	}
}

func TestHongguoUnavailableSDKEpisodeDoesNotResolve(t *testing.T) {
	var downloader Downloader
	_, err := downloader.resolveHongguoMedia(context.Background(), Task{
		DramaID: "hongguo:7710822671759461438",
		Chapter: Chapter{
			ID: "hongguo:7710822671759461438:7710822671759461439",
			Source: sourceHongguo,
			VIP: true,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "不可播放") {
		t.Fatalf("unavailable episode was not rejected: %v", err)
	}
}

func TestHongguoCooldownSkipsAllPlaybackNetworkRequests(t *testing.T) {
	requests := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		requests++
		return sourceFixtureResponse(request, http.StatusNotFound, "unexpected request"), nil
	})
	d.hongguoSDKSession.recordPlaybackResult(openhongguo.ErrAnonymousLimited)
	task := Task{
		DramaID: "hongguo:7710822671759461438",
		Chapter: Chapter{VideoURL: "hongguo-cenc://7710822671759461439"},
	}
	for i := 0; i < 2; i++ {
		_, err := d.resolveHongguoMedia(context.Background(), task)
		if !errors.Is(err, errHongguoSDKPlaybackCooling) ||
			!strings.Contains(err.Error(), "秒後重試") {
			t.Fatalf("expected cooldown and remaining time: %v", err)
		}
	}
	if requests != 0 {
		t.Fatalf("cooldown sent %d requests; want zero", requests)
	}
	d.hongguoSDKSession.recordPlaybackResult(nil)
	d.hongguoSDKSession.recordRegistrationStatus(http.StatusForbidden)
	if _, err := d.resolveHongguoMedia(context.Background(), task); !errors.Is(err, errHongguoSDKRegistrationCooling) {
		t.Fatalf("expected registration cooldown: %v", err)
	}
	if requests != 0 {
		t.Fatalf("registration cooldown sent %d requests; want zero", requests)
	}
}

func TestHongguoPlaybackOriginDistinguishesPreloadAndForeground(t *testing.T) {
	for _, action := range []string{"resolve", "preload", "prepareHandoff"} {
		ctx := context.WithValue(context.Background(), hongguoPlaybackOriginKey{}, action)
		if got := hongguoPlaybackRequestOrigin(ctx); got != action {
			t.Fatalf("origin = %q, want %q", got, action)
		}
	}
	ctx := context.WithValue(context.Background(), sourceTraceKey{}, &sourceResponseTrace{})
	if got := hongguoPlaybackRequestOrigin(ctx); got != "source_health_check" {
		t.Fatalf("health origin = %q", got)
	}
}
