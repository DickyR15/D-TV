package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"
)

func TestProviderCompatibilityManifestURLsPreferStableBranchesAndKeepCandidateFallbackLast(t *testing.T) {
	want := []string{
		"https://raw.githubusercontent.com/DickyR15/D-TV/release/v1.0.8-candidate/config/provider-compatibility.json",
		"https://raw.githubusercontent.com/DickyR15/D-TV/main/config/provider-compatibility.json",
		"https://raw.githubusercontent.com/DickyR15/D-TV/fix/v1.0.8-mobile-playback-posters/config/provider-compatibility.json",
	}
	if len(providerCompatibilityManifestURLs) != len(want) {
		t.Fatalf("manifest URL count = %d, want %d", len(providerCompatibilityManifestURLs), len(want))
	}
	for index, address := range want {
		if providerCompatibilityManifestURLs[index] != address {
			t.Errorf("manifest URL[%d] = %q, want %q", index, providerCompatibilityManifestURLs[index], address)
		}
	}
}

func TestProviderCompatibilityManifestSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"schemaVersion":1,"manifestVersion":7}`)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, body))
	if err := verifyProviderCompatibilitySignatureWithKey(body, signature, publicKey); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := verifyProviderCompatibilitySignatureWithKey(append(body, ' '), signature, publicKey); err == nil {
		t.Fatal("modified manifest body passed signature verification")
	}
	if err := verifyProviderCompatibilitySignatureWithKey(body, signature, publicKey[:len(publicKey)-1]); err == nil {
		t.Fatal("invalid public key length passed verification")
	}
}

func TestProviderCompatibilityManifestAcceptsRedFruitHostsAndBuiltInProfile(t *testing.T) {
	now := time.Now().UTC()
	manifest := providerCompatibilityManifest{
		SchemaVersion: 1,
		ManifestVersion: 1,
		GeneratedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(30 * 24 * time.Hour),
		Providers: map[string]providerCompatibilityEntry{
			sourceHongguo: {
				APIBaseURL: "https://api5-normal-sinfonlineb.fqnovel.com",
				PlaybackBaseURL: "https://api5-normal-sinfonlinea.fqnovel.com",
				AllowedHosts: []string{
					"api5-normal-sinfonlineb.fqnovel.com",
					"api5-normal-sinfonlinea.fqnovel.com",
					"log.snssdk.com",
				},
				Endpoints: map[string]string{
					"deviceRegister": "https://log.snssdk.com/service/2/device_register/",
				},
				Paths: map[string]string{
					"videoDetail": "/novel/player/video_detail/v1/",
					"videoModel": "/novel/player/video_model/v1/",
					"playback": "/novel/player/multi_video_model/player/v1",
				},
				ParserProfile: "hongguo_v5_first",
			},
		},
	}
	if err := validateProviderCompatibilityManifest(manifest, now, false); err != nil {
		t.Fatalf("valid Red Fruit manifest rejected: %v", err)
	}
}

func TestProviderCompatibilityManifestRejectsUnsignedOrUntrustedConfiguration(t *testing.T) {
	now := time.Now().UTC()
	base := providerCompatibilityManifest{
		SchemaVersion: 1,
		ManifestVersion: 1,
		GeneratedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(24 * time.Hour),
		Providers: map[string]providerCompatibilityEntry{
			sourceHongguo: {
				APIBaseURL: "https://evil.example",
				AllowedHosts: []string{"api5-normal-sinfonlineb.fqnovel.com"},
			},
		},
	}
	if err := validateProviderCompatibilityManifest(base, now, false); err == nil {
		t.Fatal("manifest containing an untrusted host was accepted")
	}
	base.Providers[sourceHongguo] = providerCompatibilityEntry{
		AllowedHosts: []string{"api5-normal-sinfonlineb.fqnovel.com"},
		Paths: map[string]string{"videoModel": "//evil.example/collect"},
	}
	if err := validateProviderCompatibilityManifest(base, now, false); err == nil {
		t.Fatal("manifest containing an external API path was accepted")
	}
	base.Providers[sourceHongguo] = providerCompatibilityEntry{
		AllowedHosts: []string{"api5-normal-sinfonlineb.fqnovel.com"},
		ParserProfile: "https://evil.example/parser.js",
	}
	if err := validateProviderCompatibilityManifest(base, now, false); err == nil {
		t.Fatal("manifest containing a remote executable parser was accepted")
	}
}

func TestProviderCompatibilityManifestRejectsVersionRollbackAndExpiry(t *testing.T) {
	now := time.Now().UTC()
	manifest := providerCompatibilityManifest{
		SchemaVersion: 1,
		ManifestVersion: 1,
		GeneratedAt: now.Add(-17 * 24 * time.Hour),
		ExpiresAt: now.Add(-16 * 24 * time.Hour),
		Providers: map[string]providerCompatibilityEntry{},
	}
	if err := validateProviderCompatibilityManifest(manifest, now, false); err == nil {
		t.Fatal("expired remote manifest was accepted")
	}
	if err := validateProviderCompatibilityManifest(manifest, now, true); err == nil {
		t.Fatal("expired cache outside the grace period was accepted")
	}
}

func TestProviderCompatibilityPathUsesValidatedTemplate(t *testing.T) {
	d := &Downloader{}
	now := time.Now().UTC()
	d.compatibilityManifest = &providerCompatibilityManifest{
		SchemaVersion: 1,
		ManifestVersion: 1,
		GeneratedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(24 * time.Hour),
		Providers: map[string]providerCompatibilityEntry{
			sourceHongguo: {
				Paths: map[string]string{
					"videoModel": "/novel/player/custom_model/v2/",
				},
			},
		},
	}
	got := d.providerCompatibilityPath(sourceHongguo, "videoModel", nil, "/novel/player/video_model/v1/")
	if got != "/novel/player/custom_model/v2/" {
		t.Fatalf("compatibility path=%q, want remote path", got)
	}
}
