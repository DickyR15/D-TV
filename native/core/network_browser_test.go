package core

import (
	"net/http"
	"testing"
)

func TestBrowserTransportRoutesProtectedProviders(t *testing.T) {
	transport := &huangguoBrowserTransport{host: "configured-huangguo.example"}
	tests := []struct {
		name       string
		method     string
		address    string
		wantSource string
	}{
		{name: "configured Huangguo host", method: http.MethodGet, address: "https://configured-huangguo.example/", wantSource: sourceHuangguoVideo},
		{name: "Huangguo canonical host", method: http.MethodGet, address: "https://huangguo.video/", wantSource: sourceHuangguoVideo},
		{name: "MissAV primary domain", method: http.MethodGet, address: "https://missav.ws/", wantSource: sourceMissAV},
		{name: "MissAV www alias", method: http.MethodHead, address: "https://www.missav.ws/", wantSource: sourceMissAV},
		{name: "MissAV alternate domain", method: http.MethodGet, address: "https://missav.ai/", wantSource: sourceMissAV},
		{name: "MissAV live domain", method: http.MethodGet, address: "https://missav.live/", wantSource: sourceMissAV},
		{name: "MissAV numbered domain", method: http.MethodGet, address: "https://missav123.com/", wantSource: sourceMissAV},
		{name: "Huangguo AI API", method: http.MethodPost, address: "https://huangguoai.com/api/playback/request", wantSource: sourceHuangguoAI},
		{name: "unknown host", method: http.MethodGet, address: "https://example.invalid/", wantSource: ""},
		{name: "unrelated scheme", method: http.MethodGet, address: "file:///tmp/index.html", wantSource: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, test.address, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := transport.source(request); got != test.wantSource {
				t.Fatalf("source(%s %s) = %q, want %q", test.method, test.address, got, test.wantSource)
			}
			if got := transport.matches(request); got != (test.wantSource != "") {
				t.Fatalf("matches(%s %s) = %v, want %v", test.method, test.address, got, test.wantSource != "")
			}
		})
	}
}

