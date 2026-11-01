package core

import "testing"

func TestIndependentMaccmsSourceRegistry(t *testing.T) {
	expected := map[string]string{
		sourceBaofeng: "bfzyapi.com",
		sourceJisu:    "jszyapi.com",
		source360:     "360zy.com",
		sourceHuya:    "huyazy.net",
	}
	if len(maccmsSources) != len(expected) {
		t.Fatalf("registered %d independent MacCMS sources, want %d", len(maccmsSources), len(expected))
	}
	for source, host := range expected {
		config, ok := maccmsSourceByID(source)
		if !ok {
			t.Fatalf("source %q is not registered", source)
		}
		if config.host != host {
			t.Fatalf("source %q host = %q, want %q", source, config.host, host)
		}
		if canonicalProviderSource(host) != source {
			t.Fatalf("host %q did not resolve to source %q", host, source)
		}
		if !isHuangguoProviderSource(source) {
			t.Fatalf("source %q is not registered with native provider routing", source)
		}
	}
}

func TestIndependentMaccmsIDsDoNotCollide(t *testing.T) {
	const upstreamID = "12345"
	seen := map[string]bool{}
	for _, source := range []string{sourceBaofeng, sourceJisu, source360, sourceHuya} {
		id := providerDramaID(source, upstreamID)
		if seen[id] {
			t.Fatalf("duplicate provider ID %q", id)
		}
		seen[id] = true
		gotSource, gotID, ok := splitProviderDramaID(id)
		if !ok || gotSource != source || gotID != upstreamID {
			t.Fatalf("splitProviderDramaID(%q) = %q/%q/%v", id, gotSource, gotID, ok)
		}
	}
}

func TestIndependentMaccmsCategories(t *testing.T) {
	for _, valid := range []string{"", "1", "42", "1000000"} {
		if !validMaccmsCategory(valid) {
			t.Errorf("valid category %q rejected", valid)
		}
	}
	for _, invalid := range []string{"0", "-1", "1/2", "abc", "1000001"} {
		if validMaccmsCategory(invalid) {
			t.Errorf("invalid category %q accepted", invalid)
		}
	}
}

func TestIndependentMaccmsDramaUsesSourceScopedID(t *testing.T) {
	config, ok := maccmsSourceByID(sourceHuya)
	if !ok {
		t.Fatal("Huya source is missing")
	}
	drama := maccmsDramaFromRecord(config, map[string]any{
		"vod_id": "789",
		"vod_name": "測試影片",
		"vod_pic": "https://huyazy.net/poster.jpg",
	}, "電影")
	if drama.ID != "huya:789" || drama.Source != sourceHuya || drama.SourceID != "789" {
		t.Fatalf("unexpected Huya identity: %+v", drama)
	}
	if drama.ChannelName != "虎牙" {
		t.Fatalf("channel name = %q, want 虎牙", drama.ChannelName)
	}
}
