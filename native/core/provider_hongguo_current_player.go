package core

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"net/http"
	"strconv"
	"strings"
)

type hongguoCurrentProfile struct {
	material  []byte
	header    []byte
	boxes     [4][]byte
	initial   [4][]byte
	checksum  [256]uint64
	factorIV  [8]uint32
	factorTab [64]uint32
	padding   []byte
}

func loadHongguoCurrentProfile() hongguoCurrentProfile {
	raw, err := base64.StdEncoding.DecodeString(hongguoCurrentProfileBase64)
	if err != nil || len(raw) != 3428 {
		panic("invalid Hongguo current-player protocol profile")
	}
	var p hongguoCurrentProfile
	p.material = append([]byte(nil), raw[0:32]...)
	p.header = append([]byte(nil), raw[32:48]...)
	for i := 0; i < 4; i++ {
		p.boxes[i] = append([]byte(nil), raw[48+i*256:48+(i+1)*256]...)
		p.initial[i] = append([]byte(nil), raw[1072+i*4:1076+i*4]...)
	}
	for i := 0; i < 256; i++ {
		p.checksum[i] = binary.LittleEndian.Uint64(raw[1088+i*8 : 1096+i*8])
	}
	for i := 0; i < 8; i++ {
		p.factorIV[i] = binary.LittleEndian.Uint32(raw[3136+i*4 : 3140+i*4])
	}
	for i := 0; i < 64; i++ {
		p.factorTab[i] = binary.LittleEndian.Uint32(raw[3168+i*4 : 3172+i*4])
	}
	p.padding = append([]byte(nil), raw[3424:3428]...)
	return p
}

var hongguoCurrentProfileData = loadHongguoCurrentProfile()

type hongguoQueryPair struct {
	key   string
	value string
}

func hongguoCurrentEncodeQuery(pairs []hongguoQueryPair) string {
	const allowed = "-_.!~*'()"
	var out strings.Builder
	for i, pair := range pairs {
		if i > 0 {
			out.WriteByte('&')
		}
		write := func(value string) {
			for _, ch := range []byte(value) {
				if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.IndexByte(allowed, ch) >= 0 {
					out.WriteByte(ch)
				} else {
					out.WriteString(fmt.Sprintf("%%%02X", ch))
				}
			}
		}
		write(pair.key)
		out.WriteByte('=')
		write(pair.value)
	}
	return out.String()
}

func hongguoCurrentGetIV(iv uint32, data []byte) uint32 {
	value := iv
	for i, b := range data {
		if i&1 == 0 {
			value = (value >> 4) ^ value ^ (value << 6) ^ uint32(b)
		} else {
			value = ^((value >> 7) ^ value ^ (uint32(b) | (value << 12)))
		}
	}
	return value
}

func hongguoCurrentFactor(input []byte, seconds uint32, selector int) ([]byte, error) {
	if len(input) != 52 || selector != 0 && selector != 3 && selector != 6 && selector != 9 && selector != 12 && selector != 15 {
		return nil, errors.New("invalid Hongguo current-player factor input")
	}
	p := hongguoCurrentProfileData
	configs := [6][2]int{{15, 100}, {13, 96}, {62, 94}, {79, 97}, {66, 96}, {127, 99}}
	cfg := configs[selector/3]
	rotation := seconds + uint32(cfg[0]) - 15 + 0xee47628f
	ini := make([]uint32, 8)
	for i := range ini {
		ini[i] = bits.RotateLeft32(p.factorIV[i], -int(rotation&31))
	}
	table := make([]uint32, 64)
	for i := range table {
		table[i] = bits.RotateLeft32(p.factorTab[i], -int((rotation+1)&31))
	}
	block := make([]byte, 64)
	copy(block, input)
	shift := (rotation + 2) & 7
	for i := 0; i < 4; i++ {
		x := uint32(p.padding[i])
		block[52+i] = byte(x>>shift | x<<((8-shift)&7))
	}
	block[62], block[63] = 1, 0xa0

	var words [128]uint32
	for i := 0; i < 16; i++ {
		words[i] = binary.BigEndian.Uint32(block[i*4 : i*4+4])
	}
	for i := 16; i < 128; i++ {
		x, y := words[i-15], words[i-2]
		sigmaX := bits.RotateLeft32(x, -7) ^ bits.RotateLeft32(x, -18) ^ (x >> 3)
		sigmaY := bits.RotateLeft32(y, -17) ^ bits.RotateLeft32(y, -19) ^ (y >> 10)
		words[i] = words[i-16] + sigmaX + words[i-7] + sigmaY
	}
	v := append([]uint32(nil), ini...)
	sig := func(x uint32) uint32 {
		return bits.RotateLeft32(x, -6) ^ bits.RotateLeft32(x, -11) ^ bits.RotateLeft32(x, -25)
	}
	upper := func(x uint32) uint32 {
		return bits.RotateLeft32(x, -2) ^ bits.RotateLeft32(x, -13) ^ bits.RotateLeft32(x, -22)
	}
	for round := 0; round < cfg[1]; round++ {
		a, b, c, d := v[0], v[1], v[2], v[3]
		e, f, g, h := v[4], v[5], v[6], v[7]
		kw := table[(cfg[0]+round)&63] + words[(cfg[0]+round)&127]
		switch selector {
		case 0:
			t := kw + (c ^ (e & (f ^ c))) + sig(e) + g
			v = []uint32{t + h, a, b, c, d, e, f, ((a & b) | ((b | a) & d)) + upper(a) + t}
		case 3:
			t := kw + (h ^ (a & (c ^ h))) + sig(a) + b
			v = []uint32{h, a, ((f & d) | ((d | f) & g)) + upper(f) + t, c, d, t + e, f, g}
		case 6:
			t := kw + (a ^ (d & (b ^ a))) + sig(d) + c
			v = []uint32{h, a, b, ((h & g) | ((g | h) & f)) + upper(h) + t, d, t + e, f, g}
		case 9:
			t := kw + (b ^ (c & (f ^ b))) + sig(c) + d
			v = []uint32{h, t + a, b, c, ((g & h) | ((h | g) & e)) + upper(g) + t, e, f, g}
		case 12:
			t := kw + (e ^ (b & (a ^ e))) + sig(b) + g
			v = []uint32{t + h, a, b, c, d, e, f, ((d & f) | ((f | d) & c)) + upper(d) + t}
		default:
			t := kw + (a ^ (b & (d ^ a))) + sig(b) + f
			v = []uint32{h, a, b, c, d, t + e, ((g & c) | ((c | g) & h)) + upper(g) + t, g}
		}
	}
	digest := make([]byte, 32)
	for i := 0; i < 8; i++ {
		binary.BigEndian.PutUint32(digest[i*4:i*4+4], ini[i]+v[i])
	}
	out := make([]byte, 20)
	for i := 0; i < 16; i++ {
		out[i] = digest[i] ^ digest[i+16]
	}
	state := uint32(0x20220420)
	for i := 0; i < 8; i++ {
		x := uint32(out[i])
		if i&1 == 1 {
			state = ^(state ^ ((state << 11) | x) ^ (state >> 5))
		} else {
			state = state ^ (state << 7) ^ x ^ (state >> 3)
		}
	}
	binary.LittleEndian.PutUint32(out[16:], (state &^ 0x24) ^ 0x01000000)
	return out, nil
}

func hongguoCurrentReadLE32(data []byte, at int) uint32 {
	return binary.LittleEndian.Uint32(data[at : at+4])
}

func hongguoCurrentReadBE32(data []byte, at int) uint32 {
	return binary.BigEndian.Uint32(data[at : at+4])
}

func hongguoCurrentReadLE64(data []byte, at int) uint64 {
	return binary.LittleEndian.Uint64(data[at : at+8])
}

func hongguoCurrentLe32(v uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, v)
	return out
}

func hongguoCurrentBe32(v uint32) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, v)
	return out
}

func hongguoCurrentHex(data []byte) string { return hex.EncodeToString(data) }

func hongguoCurrentGorgon(query string, digest []byte, seconds uint64, nonce, flags uint16) string {
	key := [8]byte{0x4a, byte(flags), 0x16, byte(nonce >> 8), 0x47, 0x6c, byte(flags >> 8), byte(nonce)}
	var table [256]byte
	for i := range table {
		table[i] = byte(i)
	}
	j := 0
	for i := 0; i < 256; i++ {
		j = (j + int(table[i]) + int(key[i&7])) & 255
		table[i] = table[j]
	}
	queryHash := md5.Sum([]byte(query))
	seed := make([]byte, 0, 20)
	seed = append(seed, queryHash[:4]...)
	seed = append(seed, digest[:4]...)
	seed = append(seed, 0, 0, 0, 0, 0, 9, 9, 4)
	seed = append(seed, hongguoCurrentBe32(uint32(seconds))...)
	out := make([]byte, 20)
	j = 0
	for n := 0; n < 20; n++ {
		i := n + 1
		j = (j + int(table[i&255])) & 255
		y := table[j&255]
		table[i&255] = y
		out[n] = seed[n] ^ table[(int(y)+int(y))&255]
	}
	for i := 0; i < 20; i++ {
		x := out[i]
		swapped := (x >> 4) | (x << 4)
		v := out[(i+1)%20] ^ swapped
		out[i] = ^(bits.Reverse8(v) ^ 20)
	}
	header := []byte{0x84, 0x04, byte(nonce), byte(nonce >> 8), byte(flags), byte(flags >> 8)}
	return hex.EncodeToString(append(header, out...))
}

func hongguoCurrentHelios(seconds uint64, license, aid string, random uint32) string {
	keyHash := md5.Sum(append(hongguoCurrentLe32(random), []byte(aid)...))
	keyASCII := []byte(hex.EncodeToString(keyHash[:]))
	keys := make([]uint64, 35)
	keys[0] = hongguoCurrentReadLE64(keyASCII, 0)
	a, b := keys[0], hongguoCurrentReadLE64(keyASCII, 8)
	queue := []uint64{hongguoCurrentReadLE64(keyASCII, 16), hongguoCurrentReadLE64(keyASCII, 24)}
	for i := 0; i < 34; i++ {
		x := (bits.RotateLeft64(b, -8) + a) ^ uint64(i)
		queue = append(queue, x)
		x ^= bits.RotateLeft64(a, -61)
		keys[i+1] = x
		a, b = x, queue[0]
		queue = queue[1:]
	}
	raw := []byte(strconv.FormatUint(seconds, 10) + "-" + license + "-" + aid)
	padding := 16 - len(raw)%16
	plain := append(append([]byte(nil), raw...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	prefix := hongguoCurrentLe32(random)
	out := append([]byte(nil), prefix...)
	for at := 0; at < len(plain); at += 16 {
		l, r := hongguoCurrentReadLE64(plain, at), hongguoCurrentReadLE64(plain, at+8)
		for i := 0; i < 34; i++ {
			r = keys[i] ^ (l + bits.RotateLeft64(r, -8))
			l = r ^ bits.RotateLeft64(l, -61)
		}
		var block [16]byte
		binary.LittleEndian.PutUint64(block[:8], l)
		binary.LittleEndian.PutUint64(block[8:], r)
		out = append(out, block[:]...)
	}
	return base64.StdEncoding.EncodeToString(out)
}

func hongguoCurrentBuildPB(seconds uint32, did, version string, nonce uint32, factor, hash6 []byte) []byte {
	n := func(tag int, v int64) hongguoV5Field { return hongguoV5Field{tag: tag, value: v} }
	s := func(tag int, v string) hongguoV5Field { return hongguoV5Field{tag: tag, value: v} }
	b := func(tag int, v []byte) hongguoV5Field { return hongguoV5Field{tag: tag, value: v} }
	p := hongguoCurrentProfileData
	env := hongguoV5Proto([]hongguoV5Field{
		n(1, 100), n(5, 7), s(6, "v04.10.03.04-bugfix"), n(7, 10001), s(14, version),
	})
	return hongguoV5Proto([]hongguoV5Field{
		b(1, p.header),
		n(2, 5), n(3, int64(nonce)), s(4, "8662"), s(5, did), s(6, "646637794"),
		s(7, version), s(8, "v04.10.03-ml-android"), n(9, 135529984/2),
		b(10, make([]byte, 8)), n(12, int64(seconds)),
		b(13, factor), b(14, hash6),
		b(15, hongguoV5Proto([]hongguoV5Field{n(1, 1), n(2, 1), n(3, 1)})),
		n(17, int64(seconds)), s(20, "none"), n(21, 377), b(23, env),
		s(24, `{"cmr":16777216,"cmr2":16777216,"vpn":0,"kd":0,"dyn":"","do":0,"tk":false}`),
	})
}

func hongguoCurrentMid(pb []byte) (uint16, error) {
	var first [2]int
	first[0], first[1] = -1, -1
	read := func(at *int) (uint64, error) {
		var value uint64
		for i := 0; i < 10; i++ {
			if *at >= len(pb) {
				return 0, errors.New("truncated current-player protobuf varint")
			}
			b := pb[*at]
			*at++
			if i == 9 && b > 1 {
				return 0, errors.New("overflow current-player protobuf varint")
			}
			value |= uint64(b&0x7f) << uint(7*i)
			if b&0x80 == 0 {
				return value, nil
			}
		}
		return 0, errors.New("invalid current-player protobuf varint")
	}
	at := 0
	for at < len(pb) {
		tag, err := read(&at)
		if err != nil || tag>>3 == 0 {
			return 0, errors.New("invalid current-player protobuf tag")
		}
		field, wire := int(tag>>3), int(tag&7)
		switch wire {
		case 0:
			if _, err := read(&at); err != nil { return 0, err }
		case 1:
			at += 8
		case 5:
			at += 4
		case 2:
			length, err := read(&at)
			if err != nil || length > uint64(len(pb)-at) {
				return 0, errors.New("invalid current-player protobuf length")
			}
			n := int(length)
			if (field == 13 || field == 14) && n > 0 {
				first[field-13] = int(pb[at])
			}
			at += n
		default:
			return 0, errors.New("unsupported current-player protobuf wire type")
		}
		if at > len(pb) {
			return 0, errors.New("truncated current-player protobuf field")
		}
	}
	if first[0] < 0 || first[1] < 0 {
		return 0, errors.New("current-player protobuf missing factor/hash fields")
	}
	return uint16(first[0]&63) | uint16(first[1]&63)<<6, nil
}

func hongguoCurrentKS(pb, material, t4 []byte) []byte {
	box := hongguoSM3(append(append(append([]byte(nil), material...), t4...), material...))
	n := len(pb)
	out := make([]byte, n)
	for i := range pb {
		a := uint32(box[(4*i)%32])
		c := uint32(box[(4*i+1)%32])
		x := uint32(pb[i])
		w := ^((((x >> 4) | (x << 4)) + a) ^ c)
		rot := ((w << 3) & 0xfffffff8) | ((w >> 5) & 7)
		out[n-1-i] = byte(^(a ^ (rot + c)))
	}
	last, previous := out[n-1], out[n-2]
	out[0] += ^(last ^ previous)
	out[1] = (out[0] ^ last ^ 254) + out[1]
	for j := 2; j < n-1; j++ {
		v := uint32(out[j-1])
		rot := ((v >> 5) & 7) | (v << 3)
		out[j] += byte(^(uint32(out[j-2]) ^ rot ^ uint32(j)))
	}
	out[n-1] ^= out[n-2]
	var sum byte
	for _, b := range out[1:] { sum += b }
	out[0] = (out[0] ^ out[1]) + sum
	reg := ^(((uint32(t4[2]) << 11) | uint32(t4[3])) ^ uint32(t4[2]) ^ (uint32(t4[2]) >> 5))
	mask := hongguoCurrentBe32(reg)
	stream := append(make([]byte, 8), out...)
	for i := range stream {
		stream[i] = mask[i%4] ^ stream[len(stream)-1-i]
	}
	return stream
}

var hongguoCurrentCon = [4][4]int{{2, 0, 3, 1}, {1, 3, 2, 0}, {3, 2, 1, 0}, {2, 3, 1, 0}}
var hongguoCurrentOrder = [4][16]int{
	{0, 9, 14, 15, 4, 13, 2, 7, 8, 1, 6, 3, 12, 5, 10, 11},
	{0, 9, 14, 11, 4, 13, 2, 3, 8, 1, 6, 15, 12, 5, 10, 7},
	{0, 9, 14, 3, 4, 13, 2, 15, 8, 1, 6, 11, 12, 5, 10, 7},
	{0, 9, 14, 15, 4, 13, 2, 3, 8, 1, 6, 11, 12, 5, 10, 7},
}

func hongguoCurrentTransform(plain, key, iv []byte, branch int) ([]byte, error) {
	if len(key) != 16 || len(iv) != 16 || branch < 0 || branch > 3 {
		return nil, errors.New("invalid current-player transform input")
	}
	p := hongguoCurrentProfileData
	box := p.boxes[branch]
	initial := p.initial[branch]
	initialWord := binary.LittleEndian.Uint32(initial)
	expanded := make([]byte, 48)
	for i := 0; i < 16; i++ {
		expanded[i] = key[i] ^ initial[i%4]
	}
	rounds := 8
	for i := 4; i < 12; i++ {
		at := 4 * (i - 1)
		a, b, c, d := expanded[at], expanded[at+1], expanded[at+2], expanded[at+3]
		if i&3 == 0 {
			old := a
			a = byte((initialWord >> uint(rounds&24)) ^ uint32(box[b]))
			b, c, d = box[c], box[d], box[old]
		}
		rounds += 2
		expanded[at+4] = a ^ expanded[at-12]
		expanded[at+5] = b ^ expanded[at-11]
		expanded[at+6] = c ^ expanded[at-10]
		expanded[at+7] = d ^ expanded[at-9]
	}
	pad := 16 - len(plain)%16
	source := append(append([]byte(nil), plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	previous := append([]byte(nil), iv...)
	out := make([]byte, 0, len(source))
	co := hongguoCurrentCon[branch]
	order := hongguoCurrentOrder[branch]
	for at := 0; at < len(source); at += 16 {
		state := make([]byte, 16)
		for i := 0; i < 16; i++ {
			keyIndex := 4*(i/4) + co[i%4]
			state[i] = source[at+i] ^ previous[i] ^ expanded[keyIndex]
		}
		for round := 1; round <= 2; round++ {
			old := append([]byte(nil), state...)
			for i := 0; i < 16; i++ {
				index := co[i/4]*4 + i%4
				state[i] = box[old[index]]
			}
			old = append(old[:0], state...)
			for i := 0; i < 16; i++ {
				state[i] = old[order[i]]
			}
			if round == 1 {
				old = append(old[:0], state...)
				for i := 0; i < 16; i++ {
					index := 4*(i/4) + co[i%4]
					state[i] = old[index]
				}
				for column := 0; column < 4; column++ {
					total := state[column] ^ state[4+column] ^ state[8+column] ^ state[12+column]
					first := state[column]
					for row := 0; row < 4; row++ {
						next := first
						if row < 3 { next = state[(row+1)*4+column] }
						x := state[row*4+column] ^ next
						xt := byte(x << 1)
						if x&0x80 != 0 { xt ^= 0x1b }
						state[row*4+column] ^= total ^ xt
					}
				}
			}
			for i := 0; i < 16; i++ {
				state[i] ^= expanded[16*round+4*(i/4)+co[i%4]]
			}
		}
		previous = make([]byte, 16)
		for i := 0; i < 16; i++ {
			previous[i] = state[i] ^ expanded[16+i]
		}
		out = append(out, previous...)
	}
	return out, nil
}

func hongguoCurrentMedusa(pb, material []byte, seconds uint64, random1, random2 uint32) (string, error) {
	mid, err := hongguoCurrentMid(pb)
	if err != nil { return "", err }
	t4 := hongguoCurrentLe32(random1)
	var prefix uint32
	for i, value := range material {
		x := uint32(value)
		if i&1 == 0 {
			prefix ^= (prefix << 7) ^ x ^ (prefix >> 3)
		} else {
			prefix ^= ^((prefix << 11) ^ x ^ (prefix >> 5))
		}
	}
	pre := []byte{byte(prefix)}
	pre = append(pre, hongguoCurrentLe32(random2)...)
	pre = append(pre, 1, byte(mid), byte(mid>>8), 0x18)
	pre = append(pre, hongguoCurrentKS(pb, material, t4)...)
	pre = append(pre, t4[2:4]...)
	if len(pre) < 248 {
		return "", errors.New("current-player Medusa envelope is too short")
	}
	lane := [8]uint{6, 5, 3, 4, 2, 7, 1, 0}
	bitPositions := [8]uint{1, 7, 3, 6, 2, 0, 4, 5}
	plain := make([]byte, 31)
	for i := 0; i < 248; i++ {
		plain[i/8] |= ((pre[i] >> lane[i%8]) & 1) << bitPositions[i%8]
	}
	keyHash := md5.Sum(material[:16])
	ivHash := md5.Sum(material[16:32])
	channel, err := hongguoCurrentTransform(plain, keyHash[:], ivHash[:], int(seconds&3))
	if err != nil { return "", err }
	body := append([]byte(nil), pre...)
	for i := 0; i < 248; i++ {
		mask := byte(1 << lane[i%8])
		body[i] = (body[i] &^ mask) | (((channel[i/8] >> bitPositions[i%8]) & 1) << lane[i%8])
	}
	header := append(hongguoCurrentLe32(5), hongguoCurrentProfileData.header...)
	stamp := hongguoCurrentLe32(uint32(seconds))
	for i := range header { header[i] ^= stamp[i%4] }
	out := append([]byte(nil), header...)
	out = append(out, t4[:2]...)
	out = append(out, 0, 1, channel[31])
	out = append(out, body...)
	return base64.StdEncoding.EncodeToString(out), nil
}

type hongguoCurrentComputeInput struct {
	Query       string
	Digest      []byte
	PB          []byte
	Material    []byte
	License     string
	AID         string
	Seconds     uint64
	Nonce       uint16
	Flags       uint16
	HeliosRandom uint32
	Random1     uint32
	Random2     uint32
	Local       uint64
	Previous    uint32
}

func hongguoCurrentCompute(input hongguoCurrentComputeInput) (http.Header, uint32, error) {
	if len(input.Material) != 32 || len(input.Digest) != 16 || len(input.PB) < 2 || len(input.PB) > 32768 {
		return nil, 0, errors.New("invalid current-player compute input")
	}
	var crc uint64
	for _, ch := range []byte(input.License) {
		crc = hongguoCurrentProfileData.checksum[byte(crc)^ch] ^ (crc >> 8)
	}
	state := uint32(bits.RotateLeft64(crc^input.Seconds^input.Local, 4)) ^ input.Previous
	helios := hongguoCurrentHelios(input.Seconds, input.License, input.AID, input.HeliosRandom)
	medusa, err := hongguoCurrentMedusa(input.PB, input.Material, input.Seconds, input.Random1, input.Random2)
	if err != nil { return nil, 0, err }
	headers := make(http.Header)
	headers.Set("X-Khronos", strconv.FormatUint(input.Seconds, 10))
	headers.Set("X-Argus", base64.StdEncoding.EncodeToString(hongguoCurrentLe32(uint32(input.Seconds))))
	headers.Set("X-Ladon", base64.StdEncoding.EncodeToString(hongguoCurrentBe32(state)))
	headers.Set("X-Gorgon", hongguoCurrentGorgon(input.Query, input.Digest, input.Seconds, input.Nonce, input.Flags))
	headers.Set("X-Helios", helios)
	headers.Set("X-Medusa", medusa)
	return headers, state, nil
}

func hongguoCurrentRandomEntropy() ([]byte, error) {
	out := make([]byte, 24)
	if _, err := rand.Read(out); err != nil { return nil, err }
	return out, nil
}

func signHongguoCurrentPlayer(pairs []hongguoQueryPair, nowMillis int64, did, version string, body []byte, entropy []byte) (string, http.Header, error) {
	if nowMillis < 1000 || nowMillis > int64(^uint32(0)>>1)*1000-128 || did == "" || len(did) > 64 || version == "" || len(version) > 32 {
		return "", nil, errors.New("invalid current-player signing parameters")
	}
	if entropy == nil {
		var err error
		entropy, err = hongguoCurrentRandomEntropy()
		if err != nil { return "", nil, err }
	}
	if len(entropy) != 24 {
		return "", nil, errors.New("invalid current-player signing entropy")
	}
	filtered := make([]hongguoQueryPair, 0, len(pairs)+2)
	for _, p := range pairs {
		if p.key != "ts" && p.key != "_rticket" {
			filtered = append(filtered, p)
		}
	}
	bodyMD5 := md5.Sum(body)
	for offset := int64(0); offset < 128; offset++ {
		ticket := nowMillis + offset
		seconds := uint32(ticket / 1000)
		queryPairs := append(append([]hongguoQueryPair(nil), filtered...),
			hongguoQueryPair{key: "ts", value: strconv.FormatUint(uint64(seconds), 10)},
			hongguoQueryPair{key: "_rticket", value: strconv.FormatInt(ticket, 10)})
		query := hongguoCurrentEncodeQuery(queryPairs)
		if len(query) > 32768 { return "", nil, errors.New("current-player query is too long") }
		queryHash := hongguoSM3([]byte(query))
		input := append(append(append([]byte(nil), queryHash[:]...), bodyMD5[:]...), hongguoCurrentLe32(seconds)...)
		selector := int(hongguoCurrentGetIV(0x20230928, input) & 15)
		if selector%3 != 0 { continue }
		factor, err := hongguoCurrentFactor(input, seconds, selector)
		if err != nil { return "", nil, err }
		nonce := binary.LittleEndian.Uint32(entropy[:4])
		pb := hongguoCurrentBuildPB(seconds, did, version, nonce, factor, queryHash[:6])
		headers, _, err := hongguoCurrentCompute(hongguoCurrentComputeInput{
			Query: query, Digest: bodyMD5[:], PB: pb, Material: hongguoCurrentProfileData.material,
			License: "646637794", AID: "8662", Seconds: uint64(seconds),
			Nonce: binary.LittleEndian.Uint16(entropy[:2]), Flags: 0x101,
			HeliosRandom: binary.LittleEndian.Uint32(entropy[4:8]),
			Random1: binary.LittleEndian.Uint32(entropy[8:12]),
			Random2: binary.LittleEndian.Uint32(entropy[12:16]),
			Local: 0x10000000, Previous: binary.LittleEndian.Uint32(entropy[16:20]),
		})
		if err != nil { return "", nil, err }
		headers.Set("Accept", "application/json; charset=utf-8,application/x-protobuf")
		headers.Set("Content-Type", "application/json; charset=utf-8")
		headers.Set("X-XS-From-Web", "0")
		headers.Set("Sdk-Version", "2")
		headers.Set("X-SS-Req-Ticket", strconv.FormatInt(ticket, 10))
		headers.Set("X-SS-STUB", strings.ToUpper(hex.EncodeToString(bodyMD5[:])))
		return query, headers, nil
	}
	return "", nil, errors.New("current-player selector search exhausted")
}
