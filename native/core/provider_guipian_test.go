package core

import "testing"

func TestGuipianProviderRegistration(t *testing.T) {
	if got := canonicalProviderSource("guipianwu.com"); got != sourceGuipian {
		t.Fatalf("canonical provider = %q, want %q", got, sourceGuipian)
	}
	if got := providerSourceForURL("https://guipianwu.com/play/123-1-1.html"); got != sourceGuipian {
		t.Fatalf("provider URL source = %q, want %q", got, sourceGuipian)
	}
	if !isHuangguoProviderSource(sourceGuipian) {
		t.Fatal("Guipian was not registered as a native provider")
	}
	if !validGuipianCategory("3") || validGuipianCategory("../3") {
		t.Fatal("Guipian category validation regression")
	}
	source, sourceID, ok := splitProviderDramaID(providerDramaID(sourceGuipian, "123"))
	if !ok || source != sourceGuipian || sourceID != "123" {
		t.Fatalf("split Guipian drama ID = %q/%q/%v", source, sourceID, ok)
	}
}

func TestGuipianPlaybackURLValidation(t *testing.T) {
	valid := "https://guipianwu.com/play/123-1-1.html"
	if got := guipianPlayURL(valid); got != valid {
		t.Fatalf("valid Guipian URL = %q, want %q", got, valid)
	}
	for _, value := range []string{
		"https://example.com/play/123-1-1.html",
		"https://guipianwu.com/detail/123.html",
		"javascript:alert(1)",
	} {
		if got := guipianPlayURL(value); got != "" {
			t.Fatalf("guipianPlayURL(%q) = %q, want empty", value, got)
		}
	}
}

func TestGuipianTransportSelectionPreservesWindows(t *testing.T) {
	if guipianStandardHTTPForOS("windows") {
		t.Fatal("Windows must keep its existing native stream transport")
	}
	if !guipianStandardHTTPForOS("android") {
		t.Fatal("Android must use the standard HTTP media transport")
	}
	if guipianStandardHTTPForOS("darwin") || guipianStandardHTTPForOS("ios") {
		t.Fatal("non-Android platforms must keep their existing media transport")
	}
}

func TestGuipianResolveMediaURLUsesFinalPageURL(t *testing.T) {
	tests := []struct {
		name    string
		pageURL string
		raw     string
		want    string
	}{
		{
			name:    "relative media path after page redirect",
			pageURL: "https://player.example.test/watch/123.html",
			raw:     "../media/master.m3u8?token=a%2Fb",
			want:    "https://player.example.test/media/master.m3u8?token=a%2Fb",
		},
		{
			name:    "protocol relative CDN URL",
			pageURL: "https://player.example.test/watch/123.html",
			raw:     "//cdn.example.test/media/master.m3u8",
			want:    "https://cdn.example.test/media/master.m3u8",
		},
		{
			name:    "absolute media URL",
			pageURL: "https://player.example.test/watch/123.html",
			raw:     "https://cdn.example.test/media/master.m3u8?token=abc",
			want:    "https://cdn.example.test/media/master.m3u8?token=abc",
		},
		{
			name:    "reject credentials in URL",
			pageURL: "https://player.example.test/watch/123.html",
			raw:     "https://user:pass@cdn.example.test/media/master.m3u8",
			want:    "",
		},
		{
			name:    "reject non HTTP scheme",
			pageURL: "https://player.example.test/watch/123.html",
			raw:     "javascript:alert(1)",
			want:    "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := guipianResolveMediaURL(test.pageURL, test.raw); got != test.want {
				t.Fatalf("resolved URL = %q, want %q", got, test.want)
			}
		})
	}
}
