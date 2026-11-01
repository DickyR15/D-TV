package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderMediaCredentialsPreserveRefererAndUserAgentAcrossRedirect(t *testing.T) {
	const referer = "https://guipianwu.com/play/123-1-1.html"
	const agent = "GuipianMediaTest/1.0"
	firstHeaders := make(chan http.Header, 1)
	finalHeaders := make(chan http.Header, 1)

	final := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		finalHeaders <- request.Header.Clone()
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(writer, "#EXTM3U\n")
	}))
	defer final.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		firstHeaders <- request.Header.Clone()
		http.Redirect(writer, request, final.URL+"/master.m3u8", http.StatusFound)
	}))
	defer redirect.Close()

	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	downloader := &Downloader{client: client}
	credentials := &providerMediaCredentials{referer: referer, userAgent: agent}
	ctx := providerMediaContext(context.Background(), credentials)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, redirect.URL+"/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Referer", referer)
	request.Header.Set("User-Agent", agent)
	response, err := downloader.doMediaRequestWithClient(request, client)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL.String() != final.URL+"/master.m3u8" {
		t.Fatalf("final response URL = %v, want %s/master.m3u8", response.Request, final.URL)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || !strings.HasPrefix(string(body), "#EXTM3U") {
		t.Fatalf("final playlist body = %q, error = %v", body, err)
	}

	for name, headers := range map[string]http.Header{
		"initial request": <-firstHeaders,
		"redirect target": <-finalHeaders,
	} {
		if headers.Get("Referer") != referer || headers.Get("User-Agent") != agent {
			t.Errorf("%s lost playback headers: Referer=%q User-Agent=%q", name, headers.Get("Referer"), headers.Get("User-Agent"))
		}
		if headers.Get("Origin") != "https://guipianwu.com" {
			t.Errorf("%s Origin = %q, want https://guipianwu.com", name, headers.Get("Origin"))
		}
	}
}
