package core

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/bits"
	"net/http"
	"strconv"
	"time"
)

// hongguoRegistrationXGorgon reproduces the device_register basic X-Gorgon
// dialect used by iDuanJu's HGSign.basicHeaders. Keep this separate from the
// V5 playback signer: registration and playback use different signatures.
func hongguoRegistrationXGorgon(rawQuery string, body []byte, timestamp uint32, variant uint16) string {
	queryDigest := md5.Sum([]byte(rawQuery))
	bodyDigest := md5.Sum(body)
	payload := make([]byte, 20)
	copy(payload[0:4], queryDigest[:4])
	copy(payload[4:8], bodyDigest[:4])
	copy(payload[12:16], []byte{0x00, 0x03, 0x0a, 0x04})
	binary.BigEndian.PutUint32(payload[16:20], timestamp)

	const flags uint16 = 0x4481
	key := [8]byte{
		0x4a, byte(flags >> 8), 0x16, byte(variant),
		0x47, 0x6c, byte(flags), byte(variant >> 8),
	}
	stream := hongguoRegistrationBrokenRC4(key[:], len(payload))
	for i := range payload {
		payload[i] ^= stream[i]
	}
	for i := range payload {
		nibbleSwapped := (payload[i]&0x0f)<<4 | (payload[i]>>4)&0x0f
		next := payload[(i+1)%len(payload)]
		payload[i] = 0x14 ^ ^bits.Reverse8(nibbleSwapped^next)
	}
	result := []byte{0x84, 0x04, byte(variant >> 8), byte(variant), byte(flags >> 8), byte(flags)}
	result = append(result, payload...)
	return hex.EncodeToString(result)
}

// The upstream dialect uses a deliberately non-standard RC4-like KSA/PRGA.
// Do not replace this with crypto/rc4: the one-way state update is intentional.
func hongguoRegistrationBrokenRC4(key []byte, length int) []byte {
	var state [256]byte
	for i := range state {
		state[i] = byte(i)
	}
	j := 0
	for i := 0; i < len(state); i++ {
		j = (j + int(state[i]) + int(key[i&7])) & 0xff
		state[i] = state[j]
	}
	out := make([]byte, length)
	i, j := 0, 0
	for n := range out {
		i = (i + 1) & 0xff
		j = (j + int(state[i])) & 0xff
		value := state[j]
		state[i] = value
		out[n] = state[(int(state[i])+int(value))&0xff]
	}
	return out
}

// hongguoRegistrationSignatureHeaders returns the three headers used by the
// reference implementation for anonymous device_register requests.
func hongguoRegistrationSignatureHeaders(rawQuery string, body []byte, now time.Time, variant uint16) http.Header {
	timestamp := uint32(now.Unix())
	var stamp [4]byte
	binary.LittleEndian.PutUint32(stamp[:], timestamp)
	headers := make(http.Header)
	headers.Set("X-Khronos", formatHongguoTimestamp(timestamp))
	headers.Set("X-Gorgon", hongguoRegistrationXGorgon(rawQuery, body, timestamp, variant))
	headers.Set("X-Argus", base64.StdEncoding.EncodeToString(stamp[:]))
	return headers
}

func formatHongguoTimestamp(timestamp uint32) string {
	return strconv.FormatUint(uint64(timestamp), 10)
}

func signHongguoDeviceRegistration(request *http.Request, body []byte, now time.Time) error {
	var random [2]byte
	if _, err := rand.Read(random[:]); err != nil {
		return errors.New("無法產生紅果註冊簽名隨機值")
	}
	variant := uint16(random[0]&0x0f)<<12 | uint16(random[1])
	for name, values := range hongguoRegistrationSignatureHeaders(request.URL.RawQuery, body, now, variant) {
		request.Header[name] = append([]string(nil), values...)
	}
	return nil
}
