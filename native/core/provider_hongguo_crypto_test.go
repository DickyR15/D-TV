package core

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func fixtureHongguoSpadeA(key []byte) string {
	target := append([]byte{0}, []byte(hex.EncodeToString(key))...)
	encoded := make([]byte, len(target))
	previousEven, previousOdd := byte(246), byte(85)
	for index, desired := range target {
		previous := previousEven
		if index&1 == 0 {
			previousEven = 0
		} else {
			previous = previousOdd
			previousOdd = 0
		}
		value := byte(int(desired) + 21 + bits.OnesCount(uint(index)))
		current := previous ^ value
		encoded[index] = current
		if index&1 == 0 {
			previousEven = current
		} else {
			previousOdd = current
		}
	}
	raw := make([]byte, len(encoded)+1)
	copy(raw[1:], encoded)
	raw[0] = encoded[0] ^ encoded[1]
	return base64.StdEncoding.EncodeToString(raw)
}

func fixtureHongguoEncryptedURL(t *testing.T, address string, seed []byte) string {
	t.Helper()
	first := sha512.Sum512(seed)
	combined := append(append([]byte(nil), first[:]...), hongguoEncryptedURLKeyMaterial...)
	second := sha512.Sum512(combined)
	block, err := aes.NewCipher(second[:aes.BlockSize])
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(address)
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, bytes.Repeat([]byte{byte(padding)}, padding)...)
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, second[aes.BlockSize:aes.BlockSize*2]).CryptBlocks(encrypted, plain)
	envelope := append([]byte{168, 0, 1, 0}, encrypted...)
	return base64.StdEncoding.EncodeToString(envelope)
}

func TestHongguoSpadeAKeySupportsCurrentOfficialEnvelope(t *testing.T) {
	want, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	encoded := fixtureHongguoSpadeA(want)
	got, err := hongguoSpadeAKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("spade_a key=%x, want %x", got, want)
	}
}

func TestHongguoDecryptVideoURLSupportsKeySeedEnvelope(t *testing.T) {
	seed := []byte("D-TV red fruit fixture key seed")
	const want = "https://cdn.example.test/video/main.m3u8"
	encrypted := fixtureHongguoEncryptedURL(t, want, seed)
	got, err := hongguoDecryptVideoURL(encrypted, base64.StdEncoding.EncodeToString(seed))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("decrypted URL=%q, want %q", got, want)
	}
}

func TestSelectHongguoAppMediaKeepsBackupURLListAndCENCKey(t *testing.T) {
	wantKey, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	spade := fixtureHongguoSpadeA(wantKey)
	model := map[string]any{
		"video_duration": "80.5",
		"video_list": []any{
			map[string]any{
				"main_url": "https://cdn.example.test/main.mp4",
				"backup_url_list": []any{
					"https://cdn.example.test/backup-1.mp4",
					"https://cdn.example.test/backup-2.mp4",
				},
				"spade_a": spade,
				"video_meta": map[string]any{
					"codec_type": "h264",
					"vwidth": "720",
					"vheight": "1280",
					"definition": "1280P",
				},
			},
		},
	}
	media, err := selectHongguoAppMedia(model)
	if err != nil {
		t.Fatal(err)
	}
	if media.URL != "https://cdn.example.test/main.mp4" {
		t.Fatalf("primary URL=%q", media.URL)
	}
	if len(media.Variants) != 3 {
		t.Fatalf("variants=%d, want primary plus 2 backups", len(media.Variants))
	}
	for _, variant := range media.Variants {
		if !bytes.Equal(variant.CENCKey, wantKey) {
			t.Fatalf("variant key=%x, want %x", variant.CENCKey, wantKey)
		}
		if variant.Quality != 720 {
			t.Fatalf("portrait quality=%d, want short edge 720", variant.Quality)
		}
	}
}
