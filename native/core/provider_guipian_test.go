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
