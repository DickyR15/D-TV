package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeServePrefixUsesSessionStandardHTTPAndHeaders(t *testing.T) {
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
	var upstreamHeaders http.Header
	engine.downloader.client = &http.Client{Transport: nativeStreamRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		standardCalls.Add(1)
		upstreamHeaders = request.Header.Clone()
		header := make(http.Header)
		header.Set("Content-Range", "bytes 4-7/8")
		header.Set("ETag", `"stable"`)
		header.Set("Content-Type", "video/mp2t")
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader("efgh")),
			Request:    request,
		}, nil
	})}
	engine.downloader.streamClient = &http.Client{Transport: nativeStreamRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		streamCalls.Add(1)
		return nil, errors.New("preload continuation unexpectedly used native stream transport")
	})}

	referer := "https://media.example.test/play/123-1-1.html"
	session := &nativeStreamSession{
		assets:        map[string]nativeStreamAsset{},
		referer:       referer,
		standardHTTP:  true,
	}
	asset := nativeStreamAsset{
		address:     "https://cdn.example.test/segment.ts",
		contentType: "video/mp2t",
		data:        []byte("abcd"),
		total:       8,
		etag:        `"stable"`,
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/token/segment.ts", nil)
	writer := httptest.NewRecorder()
	if !stream.nativeServePrefix(context.Background(), writer, request, session, asset) {
		t.Fatal("preloaded prefix continuation was not served")
	}
	if writer.Code != http.StatusOK || writer.Body.String() != "abcdefgh" {
		t.Fatalf("prefix response = status %d body %q, want 200/abcdefgh", writer.Code, writer.Body.String())
	}
	if standardCalls.Load() != 1 || streamCalls.Load() != 0 {
		t.Fatalf("transport calls: standard=%d native-stream=%d, want 1/0", standardCalls.Load(), streamCalls.Load())
	}
	if upstreamHeaders.Get("User-Agent") != userAgent {
		t.Fatalf("continuation User-Agent = %q, want %q", upstreamHeaders.Get("User-Agent"), userAgent)
	}
	if upstreamHeaders.Get("Referer") != referer {
		t.Fatalf("continuation Referer = %q, want %q", upstreamHeaders.Get("Referer"), referer)
	}
	if upstreamHeaders.Get("Range") != "bytes=4-7" || upstreamHeaders.Get("If-Range") != `"stable"` {
		t.Fatalf("continuation range headers = Range %q If-Range %q", upstreamHeaders.Get("Range"), upstreamHeaders.Get("If-Range"))
	}
}
