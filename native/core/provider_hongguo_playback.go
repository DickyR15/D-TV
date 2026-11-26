package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"bytes"
	"net/url"
	"fmt"
	"math/bits"
	"regexp"
	"strconv"
	"strings"
)

var hongguoNumericID = regexp.MustCompile(`^[0-9]{1,32}$`)

var hongguoQualityNumber = regexp.MustCompile(`[0-9]+`)

func decodeHongguoBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.Strict().DecodeString(value)
}


var hongguoEncryptedURLKeyMaterial, _ = hex.DecodeString(
	"4dd4c2e6b83162090e52b3c7a6733ba41cb2462b829ab58a196b39db57177524f49baf7f08e8d68d26a72e37c1a95a2f1f05a51892aef2949732b62a38aadd58",
)

func decodeHongguoFlexibleBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "-", "+")
	value = strings.ReplaceAll(value, "_", "/")
	if decoded, err := base64.StdEncoding.Strict().DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.Strict().DecodeString(strings.TrimRight(value, "=")); err == nil {
		return decoded, nil
	}
	return nil, errors.New("紅果 Base64 資料格式無效")
}

// hongguoSpadeAKey supports the current official App spade_a envelope.
// The previous contentKey decoder remains as a compatibility fallback.
func hongguoSpadeAKey(value string) ([]byte, error) {
	raw, err := decodeHongguoFlexibleBase64(value)
	if err != nil || len(raw) < 34 {
		return nil, errors.New("紅果 spade_a 金鑰資料無效")
	}
	length := len(raw) - int(raw[0]^raw[1]^raw[2]) + 47
	if length > len(raw)-1 {
		length = len(raw)-1
	}
	if length < 33 {
		return nil, errors.New("紅果 spade_a 金鑰長度無效")
	}
	decoded := append([]byte(nil), raw[1:1+length]...)
	previousEven, previousOdd := byte(246), byte(85)
	for index := range decoded {
		current := decoded[index]
		previous := previousEven
		if index&1 == 0 {
			previousEven = current
		} else {
			previous = previousOdd
			previousOdd = current
		}
		decoded[index] = byte(-21 - bits.OnesCount(uint(index)) + int(previous^current))
	}
	if len(decoded) < 33 {
		return nil, errors.New("紅果 spade_a 內容長度不足")
	}
	keyText := strings.TrimSpace(string(decoded[1:33]))
	key, err := hex.DecodeString(keyText)
	if err != nil || len(key) != aes.BlockSize {
		return nil, errors.New("紅果 spade_a 未包含有效 AES-128 金鑰")
	}
	return key, nil
}

// hongguoDecryptVideoURL decodes the encrypted App URL envelope using the
// per-model key_seed. It accepts already-public HTTPS URLs unchanged.
func hongguoDecryptVideoURL(raw, keySeed string) (string, error) {
	raw = strings.TrimSpace(raw)
	if isProviderHTTPMediaURL(raw) {
		parsed, err := url.Parse(raw)
		if err == nil && parsed.User == nil && parsed.Scheme == "https" {
			return raw, nil
		}
		return "", errors.New("紅果播放網址格式無效")
	}
	if strings.TrimSpace(keySeed) == "" {
		decoded, err := decodeHongguoFlexibleBase64(raw)
		if err == nil {
			candidate := strings.TrimSpace(string(decoded))
			parsed, parseErr := url.Parse(candidate)
			if parseErr == nil && parsed.Scheme == "https" && parsed.User == nil && parsed.Host != "" {
				return candidate, nil
			}
		}
		return "", errors.New("紅果加密播放網址缺少 key_seed")
	}
	encrypted, err := decodeHongguoFlexibleBase64(raw)
	if err != nil || len(encrypted) < 20 || encrypted[0] != 168 || encrypted[2] != 1 || encrypted[3] != 0 {
		return "", errors.New("紅果加密播放網址標頭無效")
	}
	completeLength := (len(encrypted)-4)/aes.BlockSize*aes.BlockSize
	if completeLength < aes.BlockSize {
		return "", errors.New("紅果加密播放網址區塊長度無效")
	}
	seed, err := decodeHongguoFlexibleBase64(keySeed)
	if err != nil || len(seed) == 0 {
		return "", errors.New("紅果 key_seed 編碼無效")
	}
	first := sha512.Sum512(seed)
	combined := append(append([]byte(nil), first[:]...), hongguoEncryptedURLKeyMaterial...)
	second := sha512.Sum512(combined)
	block, err := aes.NewCipher(second[:aes.BlockSize])
	if err != nil {
		return "", err
	}
	cipherText := encrypted[4:4+completeLength]
	plain := make([]byte, len(cipherText))
	mode := cipher.NewCBCDecrypter(block, second[aes.BlockSize:aes.BlockSize*2])
	mode.CryptBlocks(plain, cipherText)
	if len(plain) > 0 {
		padding := int(plain[len(plain)-1])
		if padding >= 1 && padding <= aes.BlockSize && padding <= len(plain) {
			valid := true
			for _, value := range plain[len(plain)-padding:] {
				if int(value) != padding {
					valid = false
					break
				}
			}
			if valid {
				plain = plain[:len(plain)-padding]
			}
		}
	}
	plain = bytes.TrimRight(plain, "\x00")
	address := strings.TrimSpace(string(plain))
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("紅果解密後的播放網址無效")
	}
	return address, nil
}

func hongguoContentKey(value string) ([]byte, error) {
	if len(value) > 1024 {
		return nil, errors.New("紅果媒體金鑰資料過長")
	}
	raw, err := decodeHongguoBase64(value)
	if err != nil || len(raw) < 3 {
		return nil, errors.New("紅果媒體金鑰編碼無效")
	}
	tagLength := int(raw[0]^raw[1]^raw[2]) - 48
	contentLength := len(raw) - tagLength - 1
	if tagLength < 1 || contentLength < 33 || contentLength >= len(raw) {
		return nil, errors.New("紅果媒體金鑰結構無效")
	}
	seed := raw[len(raw)-tagLength-2] ^ raw[len(raw)-tagLength-1]
	tag := make([]byte, tagLength)
	for index := range tag {
		tag[index] = raw[len(raw)-tagLength+index] ^ seed
	}
	if string(tag) == "app_v2" || string(tag) == "web_v2" {
		return nil, errors.New("紅果媒體金鑰版本目前不受支援")
	}
	decoded := make([]byte, contentLength)
	previousEven, previousOdd := byte(250), byte(85)
	for index, current := range raw[1 : 1+contentLength] {
		previous := previousEven
		if index%2 == 0 {
			previousEven = current
		} else {
			previous = previousOdd
			previousOdd = current
		}
		decoded[index] = byte(int(previous^current) - 21 - bits.OnesCount(uint(index)))
	}
	padding, err := strconv.ParseUint(string(decoded[:1]), 36, 8)
	if err != nil || contentLength-int(padding)-1 != 32 {
		return nil, errors.New("紅果媒體金鑰內容無效")
	}
	key, err := hex.DecodeString(string(decoded[1:33]))
	if err != nil || len(key) != aes.BlockSize {
		return nil, fmt.Errorf("紅果媒體金鑰不是有效的 AES-128 金鑰")
	}
	return key, nil
}
