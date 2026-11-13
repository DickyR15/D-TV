package core

import (
	"errors"
	"testing"
	"time"
)

func TestNativeCoverFailureCooldownClassifiesHTTPStatus(t *testing.T) {
	tests := []struct{ status int; want time.Duration }{
		{404, 30 * time.Minute}, {410, 30 * time.Minute},
		{403, 5 * time.Minute}, {429, 2 * time.Minute}, {503, 90 * time.Second},
	}
	for _, test := range tests {
		got := nativeCoverFailureCooldown(&nativeCoverHTTPStatusError{StatusCode: test.status})
		if got != test.want {
			t.Errorf("HTTP %d cooldown = %s, want %s", test.status, got, test.want)
		}
	}
	if got := nativeCoverFailureCooldown(errors.New("timeout")); got != 90*time.Second {
		t.Fatalf("network failure cooldown = %s, want 90s", got)
	}
}

func TestNativeCoverFailureCooldownIsScopedByRefererAndCanBeCleared(t *testing.T) {
	cache := &nativeCoverCache{failures: make(map[string]time.Time)}
	address := "https://images.example.test/poster.jpg"
	referer := "https://source.example.test/play/1"
	if cache.coverOnCooldown(address, referer) { t.Fatal("new cover URL unexpectedly cooled down") }
	cache.noteCoverFailure(address, referer, &nativeCoverHTTPStatusError{StatusCode: 404})
	if !cache.coverOnCooldown(address, referer) { t.Fatal("failed cover URL was not cooled down") }
	if cache.coverOnCooldown(address, "https://other.example.test/") { t.Fatal("different Referer should have its own retry opportunity") }
	cache.clearCoverFailure(address, referer)
	if cache.coverOnCooldown(address, referer) { t.Fatal("successful retry did not clear cooldown") }
}

func TestNativeCoverFailureCooldownPersistsAcrossCacheRestart(t *testing.T) {
	directory := t.TempDir()
	address := "https://images.example.test/missing-poster.jpg"
	referer := "https://source.example.test/play/137937"
	first := newNativeCoverCache(directory, nil)
	first.noteCoverFailure(address, referer, &nativeCoverHTTPStatusError{StatusCode: 404})

	second := newNativeCoverCache(directory, nil)
	if !second.coverOnCooldown(address, referer) {
		t.Fatal("poster failure cooldown was lost after cache restart")
	}
	if second.coverOnCooldown(address, "https://other.example.test/") {
		t.Fatal("persisted cooldown was not scoped to the original Referer")
	}
	second.clearCoverFailure(address, referer)

	third := newNativeCoverCache(directory, nil)
	if third.coverOnCooldown(address, referer) {
		t.Fatal("successful poster retry did not persist cooldown removal")
	}
}

func TestNativeCoverFailureCooldownExpires(t *testing.T) {
	cache := &nativeCoverCache{failures: make(map[string]time.Time)}
	address, referer := "https://images.example.test/poster.jpg", "https://source.example.test/"
	key := nativeCoverFailureKey(address, referer)
	cache.failures[key] = time.Now().Add(-time.Second)
	if cache.coverOnCooldown(address, referer) { t.Fatal("expired failure remained on cooldown") }
	if _, ok := cache.failures[key]; ok { t.Fatal("expired failure was not removed") }
}


func TestPermanentMissingCoverOnlyTreatsNotFoundAsPermanent(t *testing.T) {
	for _, status := range []int{404, 410} {
		if !isPermanentMissingCover(&nativeCoverHTTPStatusError{StatusCode: status}) {
			t.Errorf("HTTP %d should be treated as permanently missing", status)
		}
	}
	for _, status := range []int{401, 403, 429, 500, 503} {
		if isPermanentMissingCover(&nativeCoverHTTPStatusError{StatusCode: status}) {
			t.Errorf("HTTP %d must not invalidate a cached poster", status)
		}
	}
	if isPermanentMissingCover(errors.New("timeout")) {
		t.Fatal("network timeout must not invalidate a cached poster")
	}
}
