package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type nativeStreamRoundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTrip nativeStreamRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func streamDiagnosticEvents(t *testing.T, stream *nativeStreamServer) ([]diagnosticEvent, string) {
	t.Helper()
	data, err := os.ReadFile(stream.downloader.diagnostics.path)
	if err != nil {
		t.Fatal(err)
	}
	var events []diagnosticEvent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event diagnosticEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events, string(data)
}

func TestNativeStreamDiagnosticsRewriteAndReadFailures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if r.URL.Path == "/truncated.m3u8" {
			w.Header().Set("Content-Length", "1000")
			io.WriteString(w, "#EXTM3U\n")
			return
		}
		io.WriteString(w, "not-a-playlist")
	}))
	defer upstream.Close()
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()
	for _, media := range []providerMedia{
		{URL: upstream.URL + "/truncated.m3u8"},
		{URL: upstream.URL + "/invalid.m3u8"},
		{URL: upstream.URL + "/cached.m3u8", Playlist: "#EXTM3U\nftp://invalid.test/segment.ts\n"},
	} {
		address, token := stream.nativeOpen(media)
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		stream.nativeRelease(token)
		if response.StatusCode != http.StatusBadGateway {
			t.Fatalf("failure returned status %d", response.StatusCode)
		}
	}
	_, text := streamDiagnosticEvents(t, stream)
	for _, marker := range []string{"stream.upstream_read_error", "error=unexpected_eof", "stream.playlist_invalid", "stream.playlist_rewrite", "error=io_error"} {
		if !strings.Contains(text, marker) {
			t.Errorf("missing diagnostic %s", marker)
		}
	}
	for _, sample := range []struct {
		err  error
		want string
	}{
		{&net.DNSError{Err: "secret DNS error", Name: "private.test"}, "dns"},
		{&net.OpError{Op: "dial", Err: io.ErrUnexpectedEOF}, "connect"},
		{&net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}, "io_error"},
		{context.Canceled, "canceled"},
		{context.DeadlineExceeded, "deadline"},
	} {
		if got := nativeStreamError(sample.err); got != sample.want {
			t.Errorf("error category=%s want %s", got, sample.want)
		}
	}
}

func TestNativeStreamIOErrorsTriggerHostCooldownAndRedactURL(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	address := "https://private.example.test/media.mp4?token=hidden-query"
	engine.downloader.rememberNativeStreamHostFailure(address, "io_error")
	if !engine.downloader.nativeStreamHostCoolingDown(address) {
		t.Fatal("generic media I/O errors should temporarily cool down the failed host")
	}
	wrapped := &url.Error{Op: "Get", URL: address, Err: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}}
	detail := nativeStreamErrorDetail(wrapped)
	if strings.Contains(detail, "hidden-query") || strings.Contains(detail, "private.example.test/media") {
		t.Fatalf("error details leaked a signed playback URL: %s", detail)
	}
	if !strings.Contains(detail, "connection reset by peer") || !strings.Contains(detail, "op=read") {
		t.Fatalf("missing underlying network failure details: %s", detail)
	}
}

func TestNativeStreamDoesNotClearHostCooldownAtHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		writer.Header().Set("Content-Range", "bytes 0-31/100")
		writer.Header().Set("Content-Length", "32")
		writer.Header().Set("Accept-Ranges", "bytes")
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(writer, strings.Repeat("x", 32))
	}))
	defer upstream.Close()

	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()

	address := upstream.URL + "/video.mp4"
	engine.downloader.rememberNativeStreamHostFailure(address, "timeout")
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := stream.nativeRequest(request, false)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if !engine.downloader.nativeStreamHostCoolingDown(address) {
		t.Fatal("HTTP 206 headers alone must not clear host cooldown before the body is consumed")
	}
}

func TestNativeStreamDiagnosticsHLSAssetsAndPrivacy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200000\nchild.m3u8?secret=hidden-query\n")
		case "/child.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			io.WriteString(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"secret.key\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:3,\nsegment.ts\n#EXT-X-ENDLIST\n")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
			io.WriteString(w, "binary-media")
		}
	}))
	defer upstream.Close()
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()
	address, token := stream.nativeOpen(providerMedia{URL: upstream.URL + "/master.m3u8?token=hidden-query", HLSKey: []byte("hidden-key-bytes")})
	read := func(address string) string {
		t.Helper()
		request, _ := http.NewRequest(http.MethodGet, address, nil)
		request.Header.Set("Authorization", "Bearer hidden-auth")
		request.Header.Set("Cookie", "hidden-cookie")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s error=%v", response.StatusCode, body, err)
		}
		return string(body)
	}
	playlistURL := func(body string) string {
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "http:") {
				return line
			}
		}
		t.Fatal("missing rewritten media URL")
		return ""
	}
	child := read(playlistURL(read(address)))
	for index, match := range nativePlaylistURI.FindAllStringSubmatch(child, -1) {
		want := "binary-media"
		if index == 0 {
			want = "hidden-key-bytes"
		}
		if body := read(match[1]); body != want {
			t.Fatalf("key/map was treated as a playlist: %s", body)
		}
	}
	if body := read(playlistURL(child)); body != "binary-media" {
		t.Fatal("segment was treated as a playlist")
	}
	session := stream.sessions[token]
	keyURL := stream.nativeAsset(token, session, nativeStreamAsset{kind: "key", address: upstream.URL + "/remote.key", contentType: "application/octet-stream"})
	if body := read(keyURL); body != "binary-media" {
		t.Fatal("upstream key was treated as a playlist")
	}
	stream.nativeRelease(token)
	events, text := streamDiagnosticEvents(t, stream)
	for _, secret := range []string{token, "hidden-query", "hidden-key-bytes", "hidden-auth", "hidden-cookie"} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostic exposed %s", secret)
		}
	}
	for _, marker := range []string{"stream.local_request", "stream.upstream_headers", "stream.local_response", "kind=master_playlist", "kind=media_playlist", "asset=child_playlist", "asset=key", "asset=map", "asset=segment", "reason=release", "elapsed_ms=", "phase=connect_done", "network=tcp", "remote="} {
		if !strings.Contains(text, marker) {
			t.Errorf("missing diagnostic %s", marker)
		}
	}
	// The HTTP client can observe the final response body before the
	// server handler's deferred local_response diagnostic has finished
	// appending its log line. Wait briefly for that synchronous write instead
	// of making this test depend on runner scheduling (notably Windows).
	count := 0
	deadline := time.Now().Add(2 * time.Second)
	for {
		count = 0
		for _, event := range events {
			if event.Event == "stream.local_response" && event.HTTPStatus == 200 {
				count++
			}
		}
		if count >= 6 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
		events, text = streamDiagnosticEvents(t, stream)
	}
	if count != 6 {
		var summary []string
		for _, event := range events {
			if event.Event == "stream.local_response" {
				summary = append(summary, fmt.Sprintf("asset=%s status=%d method=%s", event.Message, event.HTTPStatus, event.Client))
			}
		}
		t.Fatalf("logged %d completed responses, want 6; local response events: %s", count, strings.Join(summary, " | "))
	}
}

func TestNativeStreamDiagnosticsCancellationAndHTTPFailure(t *testing.T) {
	entered := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.m3u8" {
			close(entered)
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer upstream.Close()
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()
	for _, name := range []string{"forbidden.m3u8", "slow.m3u8"} {
		address, token := stream.nativeOpen(providerMedia{URL: upstream.URL + "/" + name})
		done := make(chan int, 1)
		go func() {
			response, err := http.Get(address)
			if err != nil {
				done <- 0
				return
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			done <- response.StatusCode
		}()
		if strings.HasPrefix(name, "slow") {
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("upstream never entered")
			}
			stream.nativeRelease(token)
		}
		select {
		case status := <-done:
			if status != http.StatusForbidden && status != http.StatusBadGateway {
				t.Fatalf("unexpected status %d", status)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("request did not finish after cancellation")
		}
		stream.nativeRelease(token)
	}
	_, text := streamDiagnosticEvents(t, stream)
	for _, marker := range []string{"\"httpStatus\":403", "\"httpStatus\":502", "error=canceled", "reason=release"} {
		if !strings.Contains(text, marker) {
			t.Errorf("missing diagnostic %s", marker)
		}
	}
	if nativeStreamError(context.DeadlineExceeded) != "deadline" {
		t.Fatal("deadline classification lost")
	}
}

func TestNativeStreamMP4ResponseDoesNotWaitForLargePeek(t *testing.T) {
	releaseUpstream := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		writer.Header().Set("Content-Length", "1024")
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "0123456789abcdef")
		writer.(http.Flusher).Flush()
		select {
		case <-releaseUpstream:
		case <-request.Context().Done():
			return
		}
		_, _ = io.WriteString(writer, strings.Repeat("x", 1024-16))
	}))
	defer upstream.Close()
	defer close(releaseUpstream)

	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// Closing the local response cancels the in-flight upstream read. Wait
		// for the handler to finish writing its final diagnostics before TempDir
		// cleanup removes the logs directory.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := stream.server.Shutdown(ctx); err != nil {
			_ = stream.server.Close()
		}
	}()
	address, token := stream.nativeOpen(providerMedia{URL: upstream.URL + "/video.mp4"})
	defer stream.nativeRelease(token)

	type result struct {
		response *http.Response
		err      error
	}
	done := make(chan result, 1)
	go func() {
		response, err := http.Get(address)
		done <- result{response: response, err: err}
	}()
	select {
	case opened := <-done:
		if opened.err != nil {
			t.Fatal(opened.err)
		}
		opened.response.Body.Close()
		if opened.response.StatusCode != http.StatusOK {
			t.Fatalf("local MP4 response status=%d, want 200", opened.response.StatusCode)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("MP4 response waited for a large body peek")
	}
}

type nativeStreamIdleTimeoutReader struct{}

func (nativeStreamIdleTimeoutReader) Read([]byte) (int, error) {
	return 0, errNativeStreamIdleTimeout
}

func TestNativeStreamResumesMP4AfterIdleTimeout(t *testing.T) {
	payload := []byte(strings.Repeat("0123456789abcdef", 4096))
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		start, end, total, ok := nativeParseContentRange(
			fmt.Sprintf("bytes %s/%d", strings.TrimPrefix(request.Header.Get("Range"), "bytes="), len(payload)),
		)
		if request.Header.Get("Range") != fmt.Sprintf("bytes=0-%d", len(payload)-1) {
			http.Error(writer, "expected Range resume after idle timeout", http.StatusBadRequest)
			return
		}
		if !ok || start != 0 || end != int64(len(payload)-1) || total != int64(len(payload)) {
			http.Error(writer, "invalid resume range", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "video/mp4")
		writer.Header().Set("Accept-Ranges", "bytes")
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)))
		writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(payload)
	}))
	defer upstream.Close()

	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, upstream.URL+"/video.mp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{
		StatusCode:    http.StatusPartialContent,
		Header:        http.Header{},
		ContentLength: int64(len(payload)),
		Body:          io.NopCloser(strings.NewReader("")),
		Request:       request,
	}
	response.Header.Set("Content-Type", "video/mp4")
	response.Header.Set("Accept-Ranges", "bytes")
	response.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)))
	response.Header.Set("Content-Length", strconv.Itoa(len(payload)))

	stream.downloader.rememberNativeStreamHostFailure(request.URL.String(), "timeout")
	var destination bytes.Buffer
	written, err := stream.nativeCopyMediaWithResume(
		context.Background(),
		&destination,
		request,
		request.URL.String(),
		response,
		nativeStreamIdleTimeoutReader{},
		nativeStreamDiagnostic{request: 1, session: "idle-resume", asset: "media", method: http.MethodGet, started: time.Now()},
		false,
	)
	if err != nil {
		t.Fatalf("idle-timeout resume failed after %d bytes: %v", written, err)
	}
	if !bytes.Equal(destination.Bytes(), payload) {
		t.Fatalf("resumed stream has %d bytes, want %d", destination.Len(), len(payload))
	}
	if stream.downloader.nativeStreamHostCoolingDown(request.URL.String()) {
		t.Fatal("a fully transferred media body should clear the host cooldown")
	}
	_, diagnosticText := streamDiagnosticEvents(t, stream)
	for _, marker := range []string{"stream.resume_attempt", "stream.resume_response"} {
		if !strings.Contains(diagnosticText, marker) {
			t.Errorf("missing diagnostic event %q", marker)
		}
	}
}

func TestNativeStreamResumePreservesStandardHTTPTransportAndHeaders(t *testing.T) {
	payload := []byte("abcdef")
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()

	var standardCalls, streamCalls atomic.Int32
	engine.downloader.client = &http.Client{Transport: nativeStreamRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		standardCalls.Add(1)
		if got := request.Header.Get("User-Agent"); got != userAgent {
			return nil, fmt.Errorf("resume User-Agent = %q, want the configured User-Agent", got)
		}
		if got := request.Header.Get("Referer"); got != "https://cdn.example.test/" {
			return nil, fmt.Errorf("resume Referer = %q, want the configured Referer", got)
		}
		if got := request.Header.Get("Range"); got != "bytes=0-5" {
			return nil, fmt.Errorf("resume Range = %q, want bytes=0-5", got)
		}
		header := make(http.Header)
		header.Set("Content-Type", "video/mp4")
		header.Set("Accept-Ranges", "bytes")
		header.Set("Content-Range", "bytes 0-5/6")
		header.Set("Content-Length", "6")
		return &http.Response{
			StatusCode:    http.StatusPartialContent,
			Header:        header,
			Body:          io.NopCloser(bytes.NewReader(payload)),
			ContentLength: int64(len(payload)),
			Request:       request,
		}, nil
	})}
	engine.downloader.streamClient = &http.Client{Transport: nativeStreamRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		streamCalls.Add(1)
		return nil, errors.New("standard HTTP resume unexpectedly used native stream transport")
	})}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://cdn.example.test/video.mp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Referer", "https://cdn.example.test/")
	header := make(http.Header)
	header.Set("Content-Type", "video/mp4")
	header.Set("Accept-Ranges", "bytes")
	header.Set("Content-Range", "bytes 0-5/6")
	header.Set("Content-Length", "6")
	response := &http.Response{
		StatusCode:    http.StatusPartialContent,
		Header:        header,
		ContentLength: int64(len(payload)),
		Body:          io.NopCloser(strings.NewReader("")),
		Request:       request,
	}

	var destination bytes.Buffer
	written, err := stream.nativeCopyMediaWithResume(
		context.Background(),
		&destination,
		request,
		request.URL.String(),
		response,
		nativeStreamIdleTimeoutReader{},
		nativeStreamDiagnostic{request: 88, session: "standard-http-resume", asset: "media", method: http.MethodGet, started: time.Now()},
		true,
	)
	if err != nil {
		t.Fatalf("standard HTTP resume failed after %d bytes: %v", written, err)
	}
	if !bytes.Equal(destination.Bytes(), payload) {
		t.Fatalf("resumed body = %q, want %q", destination.Bytes(), payload)
	}
	if standardCalls.Load() != 1 || streamCalls.Load() != 0 {
		t.Fatalf("resume transport calls: standard=%d native-stream=%d, want 1/0", standardCalls.Load(), streamCalls.Load())
	}
}

func TestNativeStreamResumesPrematureMP4Body(t *testing.T) {
	payload := []byte(strings.Repeat("0123456789abcdef", 4096))
	cut := len(payload) / 2
	var firstRequest atomic.Bool

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		rangeHeader := request.Header.Get("Range")
		if rangeHeader == "" && firstRequest.CompareAndSwap(false, true) {
			connection, buffered, err := writer.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack truncated response: %v", err)
				return
			}
			defer connection.Close()
			_, _ = fmt.Fprintf(buffered, "HTTP/1.1 200 OK\r\nContent-Type: video/mp4\r\nContent-Length: %d\r\nAccept-Ranges: bytes\r\nConnection: close\r\n\r\n", len(payload))
			_, _ = buffered.Write(payload[:cut])
			if err := buffered.Flush(); err != nil {
				t.Errorf("flush truncated response: %v", err)
			}
			return
		}

		if !strings.HasPrefix(rangeHeader, "bytes=") {
			http.Error(writer, "expected Range retry", http.StatusBadRequest)
			return
		}
		bounds := strings.SplitN(strings.TrimPrefix(rangeHeader, "bytes="), "-", 2)
		if len(bounds) != 2 {
			http.Error(writer, "invalid Range", http.StatusBadRequest)
			return
		}
		start, errStart := strconv.Atoi(bounds[0])
		end, errEnd := strconv.Atoi(bounds[1])
		if errStart != nil || errEnd != nil || start < 0 || end >= len(payload) || start > end {
			http.Error(writer, "invalid Range offsets", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "video/mp4")
		writer.Header().Set("Accept-Ranges", "bytes")
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		writer.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(payload[start : end+1])
	}))
	defer upstream.Close()

	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()
	address, token := stream.nativeOpen(providerMedia{URL: upstream.URL + "/video.mp4"})
	defer stream.nativeRelease(token)

	response, err := http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read repaired local media: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("local response status=%d, want 200", response.StatusCode)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("local response has %d bytes, want %d; body was not resumed correctly", len(body), len(payload))
	}
	_, diagnosticText := streamDiagnosticEvents(t, stream)
	for _, marker := range []string{"stream.resume_attempt", "stream.resume_response"} {
		if !strings.Contains(diagnosticText, marker) {
			t.Errorf("missing diagnostic event %q", marker)
		}
	}
}

func TestNativeStreamTransportReusesConnectionsAndBoundsTimeouts(t *testing.T) {
	base := &http.Transport{
		TLSClientConfig:       &tls.Config{NextProtos: []string{"h2", "http/1.1"}},
		MaxIdleConnsPerHost: 8,
		TLSHandshakeTimeout: 20 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	stream := newNativeStreamTransport(base)
	if stream.DisableKeepAlives {
		t.Fatal("native media transport should reuse idle upstream connections")
	}
	if stream.DialContext == nil {
		t.Fatal("native media transport should bound TCP connection attempts")
	}
	if stream.ForceAttemptHTTP2 {
		t.Fatal("native media transport must not attempt HTTP/2")
	}
	if stream.TLSNextProto == nil || len(stream.TLSNextProto) != 0 {
		t.Fatal("native media transport should explicitly disable automatic HTTP/2 setup")
	}
	if stream.TLSClientConfig == nil || len(stream.TLSClientConfig.NextProtos) != 1 || stream.TLSClientConfig.NextProtos[0] != "http/1.1" {
		t.Fatalf("native media transport must advertise HTTP/1.1 only via TLS ALPN: %+v", stream.TLSClientConfig)
	}
	if stream.TLSHandshakeTimeout != nativeStreamTLSHandshakeTimeout {
		t.Fatalf("TLS handshake timeout=%s, want %s", stream.TLSHandshakeTimeout, nativeStreamTLSHandshakeTimeout)
	}
	if stream.ResponseHeaderTimeout != nativeStreamResponseHeaderTimeout {
		t.Fatalf("response header timeout=%s, want %s", stream.ResponseHeaderTimeout, nativeStreamResponseHeaderTimeout)
	}
	if stream.MaxIdleConns != nativeStreamMaxIdleConnections || stream.MaxIdleConnsPerHost != nativeStreamMaxIdlePerHost || stream.IdleConnTimeout != nativeStreamIdleConnTimeout {
		t.Fatalf("unexpected idle connection policy: global=%d per-host=%d timeout=%s", stream.MaxIdleConns, stream.MaxIdleConnsPerHost, stream.IdleConnTimeout)
	}
	if base.DisableKeepAlives || base.DialContext != nil || base.TLSHandshakeTimeout != 20*time.Second || base.ResponseHeaderTimeout != 20*time.Second || base.MaxIdleConnsPerHost != 8 {
		t.Fatal("creating the native media transport mutated the shared transport")
	}
	if len(base.TLSClientConfig.NextProtos) != 2 || base.TLSClientConfig.NextProtos[0] != "h2" || base.TLSClientConfig.NextProtos[1] != "http/1.1" {
		t.Fatal("forcing HTTP/1.1 must not mutate the shared TLS config")
	}
}

func TestNativeStreamTransportUsesHTTP11ALPN(t *testing.T) {
	var negotiated atomic.Value
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.TLS != nil {
			negotiated.Store(request.TLS.NegotiatedProtocol)
		}
		_, _ = io.WriteString(writer, "ok")
	}))
	// Exercise a real TLS ALPN handshake. Avoid EnableHTTP2 here because
	// httptest's HTTP/2 setup replaces the server TLS NextProtos and can leave
	// the selected protocol unobservable for this handler-based assertion.
	server.TLS = &tls.Config{NextProtos: []string{"h2", "http/1.1"}}
	server.StartTLS()
	defer server.Close()

	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} // test server uses a self-signed certificate
	transport := newNativeStreamTransport(base)
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport}).Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.Proto != "HTTP/1.1" {
		t.Fatalf("stream transport negotiated %s, want HTTP/1.1", response.Proto)
	}
	if got, ok := negotiated.Load().(string); !ok || got != "http/1.1" {
		t.Fatalf("server negotiated ALPN %q, want http/1.1", got)
	}
}

func TestNativeStreamTransportReusesAnIdleConnection(t *testing.T) {
	var opened atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, "ok")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			opened.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	transport := newNativeStreamTransport(http.DefaultTransport.(*http.Transport).Clone())
	client := &http.Client{Transport: transport}
	defer client.CloseIdleConnections()
	for i := 0; i < 2; i++ {
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, response.Body); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("request %d returned status %d", i+1, response.StatusCode)
		}
	}
	if got := opened.Load(); got != 1 {
		t.Fatalf("opened %d TCP connections for two sequential reads, want one reused connection", got)
	}
}


func TestNativeStreamHostCooldownEscalatesAfterRepeatedFailureEpisodes(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	downloader := engine.downloader
	address := "https://cdn.example.test/video.mp4"
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.ToLower(parsed.Hostname())

	downloader.rememberNativeStreamHostFailure(address, "tls")
	readState := func() nativeStreamHostFailureState {
		downloader.streamHostMu.Lock()
		defer downloader.streamHostMu.Unlock()
		return downloader.streamHostFailures[host]
	}
	first := readState()
	if first.consecutive != 1 {
		t.Fatalf("first failure streak=%d, want 1", first.consecutive)
	}
	if remaining := time.Until(first.until); remaining < nativeStreamHostFailureCooldown-time.Second ||
		remaining > nativeStreamHostFailureCooldown+time.Second {
		t.Fatalf("first cooldown remaining=%s, want about %s", remaining, nativeStreamHostFailureCooldown)
	}

	// Parallel errors while the same penalty is active are not separate retry
	// episodes and must not immediately drive the host to maximum backoff.
	downloader.rememberNativeStreamHostFailure(address, "timeout")
	if got := readState().consecutive; got != 1 {
		t.Fatalf("failure count increased during active cooldown: %d", got)
	}

	// Simulate the caller retrying after the cooldown elapsed, without sleeping
	// for the real 90 seconds.
	downloader.streamHostMu.Lock()
	state := downloader.streamHostFailures[host]
	state.until = time.Now().Add(-time.Second)
	downloader.streamHostFailures[host] = state
	downloader.streamHostMu.Unlock()
	downloader.rememberNativeStreamHostFailure(address, "tls")
	second := readState()
	if second.consecutive != 2 {
		t.Fatalf("second failure streak=%d, want 2", second.consecutive)
	}
	wantCooldown := nativeStreamHostCooldownForFailures(2)
	if remaining := time.Until(second.until); remaining < wantCooldown-time.Second ||
		remaining > wantCooldown+time.Second {
		t.Fatalf("second cooldown remaining=%s, want about %s", remaining, wantCooldown)
	}

	downloader.clearNativeStreamHostFailure(address)
	if downloader.nativeStreamHostCoolingDown(address) {
		t.Fatal("a successful stream should clear both cooldown and failure streak")
	}
	downloader.rememberNativeStreamHostFailure(address, "tls")
	if got := readState().consecutive; got != 1 {
		t.Fatalf("failure streak after successful recovery=%d, want 1", got)
	}
}

func TestNativeStreamErrorClassifiesTLSHandshakeTimeout(t *testing.T) {
	wrapped := &url.Error{
		Op:  "Get",
		URL: "https://private.example.test/video.mp4?token=secret",
		Err: errors.New("net/http: TLS handshake timeout"),
	}
	if got := nativeStreamError(wrapped); got != "tls" {
		t.Fatalf("TLS handshake timeout classified as %q, want tls", got)
	}
}

func TestNativeStreamLogsIncompleteTransferByteRange(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://cdn.example.test/video.mp4?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader("")),
		ContentLength: -1,
		Request:       request,
	}
	response.Header.Set("Content-Type", "video/mp4")
	var destination bytes.Buffer
	written, err := stream.nativeCopyMediaWithResume(
		context.Background(),
		&destination,
		request,
		request.URL.String(),
		response,
		nativeStreamIdleTimeoutReader{},
		nativeStreamDiagnostic{request: 77, session: "transfer-test", asset: "media", method: http.MethodGet, started: time.Now()},
		false,
	)
	if !errors.Is(err, errNativeStreamIdleTimeout) || written != 0 {
		t.Fatalf("written=%d error=%v, want zero bytes and idle timeout", written, err)
	}
	_, diagnosticText := streamDiagnosticEvents(t, stream)
	for _, marker := range []string{
		"stream.transfer_incomplete",
		"stage=resume_unavailable_or_exhausted",
		"bytes_forwarded=0",
		"expected_bytes=0",
		"resumable=false",
		"range_start=0",
		"range_end=0",
		"total_bytes=0",
	} {
		if !strings.Contains(diagnosticText, marker) {
			t.Errorf("missing incomplete-transfer diagnostic %q", marker)
		}
	}
	if strings.Contains(diagnosticText, "token=secret") {
		t.Fatal("transfer diagnostics leaked a signed media URL")
	}
}

func TestDuoduoDirectHLSPlaybackUsesStandardHTTPAndRewritesSegments(t *testing.T) {
	const referer = "https://duoduozhuiju.com/"
	var upstreamRequests atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamRequests.Add(1)
		if got := request.Header.Get("Referer"); got != referer {
			http.Error(writer, "unexpected Referer: "+got, http.StatusForbidden)
			return
		}
		switch request.URL.Path {
		case "/movie/index.m3u8":
			writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(writer, "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:4.0,\nsegment.ts\n#EXT-X-ENDLIST\n")
		case "/movie/segment.ts":
			writer.Header().Set("Content-Type", "video/mp2t")
			_, _ = writer.Write([]byte("duoduo-segment-bytes"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer origin.Close()

	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Duoduo direct media opts into the standard HTTP client. Make the
	// specialized streaming client fail so this test proves that the proxy
	// honors that transport choice for both playlists and child segments.
	engine.downloader.client = origin.Client()
	engine.downloader.streamClient = &http.Client{
		Transport: nativeStreamRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return nil, errors.New("Duoduo unexpectedly used the specialized stream client")
		}),
	}
	media, err := engine.downloader.resolveDuoduoMedia(context.Background(), Task{
		DramaID: "duoduo:78296",
		Chapter: Chapter{
			ID: providerChapterID(sourceDuoduo, "78296", "1-1"),
			Source: sourceDuoduo,
			Title: "第1集",
			VideoURL: origin.URL + "/movie/index.m3u8",
			Referer: referer,
		},
	})
	if err != nil {
		t.Fatalf("resolve Duoduo direct media: %v", err)
	}
	if !media.StandardHTTP || media.URL != origin.URL+"/movie/index.m3u8" {
		t.Fatalf("Duoduo direct media did not retain its standard HTTP URL: %+v", media)
	}

	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()
	localURL, token := stream.nativeOpen(media)
	defer stream.nativeRelease(token)

	playlistResponse, err := http.Get(localURL)
	if err != nil {
		t.Fatalf("open proxied HLS playlist: %v", err)
	}
	playlist, readErr := io.ReadAll(playlistResponse.Body)
	_ = playlistResponse.Body.Close()
	if readErr != nil {
		t.Fatalf("read proxied HLS playlist: %v", readErr)
	}
	if playlistResponse.StatusCode != http.StatusOK ||
		!strings.Contains(playlistResponse.Header.Get("Content-Type"), "mpegurl") {
		t.Fatalf("proxied playlist response = HTTP %d, type=%q, body=%q",
			playlistResponse.StatusCode, playlistResponse.Header.Get("Content-Type"), playlist)
	}
	var segmentURL string
	for _, line := range strings.Split(string(playlist), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			segmentURL = line
			break
		}
	}
	if segmentURL == "" || !strings.HasPrefix(segmentURL, stream.address+"/") {
		t.Fatalf("HLS segment was not rewritten to the local proxy: %q", playlist)
	}

	segmentResponse, err := http.Get(segmentURL)
	if err != nil {
		t.Fatalf("open proxied HLS segment: %v", err)
	}
	segment, readErr := io.ReadAll(segmentResponse.Body)
	_ = segmentResponse.Body.Close()
	if readErr != nil {
		t.Fatalf("read proxied HLS segment: %v", readErr)
	}
	if segmentResponse.StatusCode != http.StatusOK || string(segment) != "duoduo-segment-bytes" {
		t.Fatalf("proxied segment response = HTTP %d, body=%q", segmentResponse.StatusCode, segment)
	}
	if upstreamRequests.Load() != 2 {
		t.Fatalf("upstream requests = %d, want playlist and one segment", upstreamRequests.Load())
	}
}

