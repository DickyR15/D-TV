package core

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Hongguo V5 playback signing protocol, adapted for D-TV from
// guoapp-modules/hongguo v1.0.2. Catalog/detail requests retain their
// existing App API signing path.
const hongguoPlaybackBaseURL = "https://api5-normal-sinfonlinea.fqnovel.com"
const hongguoPlaybackPath = "/novel/player/multi_video_model/player/v1"
const hongguoPlaybackUserAgent = "com.phoenix.read/73932 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3; Cronet/TTNetVersion:04657795 2026-01-23 QuicVersion:c67e9834 2025-09-08)"

var (
	hongguoV5Magic = mustHongguoHex("2d4b4fca49750d433fb5ae2c226dcc56")
	hongguoV5SignKey = mustHongguoHex("b23c2a470a20d105dc3f8c4438f8272ed46d6c89ffdcdedd5108698d26e9489d")
	hongguoV5Sbox = mustHongguoHex("53290fe9e51f316f90f74a7e034d367784ab49237cd0135cdc632c322fd3be0e2624a417ecd9151a7ff64f60730a87f9445ec6ada2f865d22b696c4188f24c0df37b75510b939f6bc2fe6883e68bc8c722181ba0bdb08c9871b50c9a16b46eea8eed20ba4b8a14e0fcf1468dbbef05a93e5a9d7dcf06898635c1fd99f5de287a80c594daaab19b6279fbd75070cb52eb2555c456e39e7839853874e2f0013ac0b64742d8ca10e4b79c955de8b843ce4ed464a6a72ed508091c92e7b3eedd1e1dd12afac321618fa3d65f9167c982f43d66a53c6a1997585b346dbf9672ff37cdbc073b2dae11453304df00815476b2a13fccdb30b957a8acafe127400248591238922563e36406d37a24100b7902a028044e218453c2a399199c9d13dc40c55ba67e502273850ebdc1a2e2188b8f2d9e3cb909b8c46a2a9f4a6dd93007eafa65b7b3a881aa908cc74f0ad76bbbb58617c37012ca05a469fd44e62eecf1e4272ce860c0ae5eadf99b20d8f0234174006f3f110d7657d01645d4eff8f71471cbfe1cb2f52648727bf3c9de55153e031b4d61757db63dbf7782eeafe9018a8891362fb14759ac32bab03b349aeb5a377c5156be4c8dfbe13a684bda39cf836697c894545fe0f2491acea9fc6252f4ff1e0fe72bd2df6e67a7f635e5bcc6d1a5dd951d89960887a18eb40c785cdb1f80ed42299333ab98cdd67f316cd54346585dccd759fb722789b08807cc0fda293f7e5e1819bb0353517020b31d3d00374952bea2c421e862040a9ec892a05b266b7a0d0cc316f483b81af39715657ffe32ad8de0c13077646035236ee5363e5f900e9a7cf850a81cd9960b6df73317226f99eaa6fff15cec34acb98246639b918ab2851466a7cdfc4ad1b52c2ae95a58d3734e47ddd5b7560193f644ef9c408b7dee39b4d6aabf4de761e3252d4c7ba3843187745d55e6a5db05d42b45caed43c0c5d0b64bfa9d94a4a9d8f2f0e1a108dcfd288fd2bc1fc7bd675412138169cb09249875ba3b68cfb11180afae104fc9789571c6dfce02413886796ae21e3c488cc21b9f423a066c2eabf98e2fdeeb5776e4f5d09a515ba58cbbab3e37b09fd4f6d9244df80512e84cd7956be301cf64f1429d58577cc241f3a2c655144f81aaf95a4e108da894b38970561ae9002619fbbac91fa922ed202bfc62cb08e0de2a7f3631435f8b47c8a3c563b4db4071ce74c46da6fa696a9e282d1e825d663b93bfd56c48dce1884bb10fccea063929f746443fd830a4232e53da21a1f2d1bdfebc3a02c10a1b8ad31d04fd616590d272ac737e98eb33af60528716bee78ec0ee8699031177277d9cff32542c67caf5f44591681335ae0e178f3409a7803db83892dde5b9b7f0c750e24aef077badd678a09b850b84ec1597e4963cb5e675df1c0d6fb618496ecd76c35c798325590c2fb27a5e")
	hongguoV5HashTable = []uint32{0xebb64faf, 0x07aadcc2, 0xcf3187bf, 0xe01138ff, 0x6d0bfcff, 0x5a30a3be, 0xb41ad638, 0x34180eb8, 0xf233eb6f, 0xb1a584cc, 0xccc30dc7, 0x47d1db51, 0xd55653de, 0x70a84fa1, 0x57473c12, 0xf76f0288, 0x2c077f0a, 0xda0dcad0, 0xfbb86f6c, 0xfdc4cf00, 0x688a020d, 0xe676c6a6, 0x8cd6338b, 0x1a3c8d0e, 0xcce8b06b, 0x6ad0ed0b, 0xa0522717, 0xdc71ac83, 0x2285db71, 0xd5b4dda6, 0x736f8650, 0x6560306c, 0x617ce2a6, 0xe423417e, 0x0a40e143, 0x544e4032, 0x88dffb2a, 0x716c1ae0, 0x4c467a88, 0x05b23bb3, 0xe1d0b866, 0xbaa3dcb8, 0xae3374d3, 0xc3381a50, 0x1702f75b, 0xfe6da368, 0xf0b4cf48, 0x4e0ffbb8, 0x72aad10d, 0x26c53a3d, 0xf2bce0f6, 0xb4557581, 0x4a257fdd, 0x8c3182a2, 0xab0b3b86, 0x3d5dfb14, 0x4f103634, 0xd37b52d7, 0x444eff16, 0xeb0a33d1, 0x6ca86f6e, 0x00284ba7, 0x08387cfa, 0x5fb37586}
	hongguoV5ProcessMu sync.Mutex
	hongguoV5Process *hongguoV5ProcessState
)

type hongguoV5ProcessState struct {
	nonce string
	started int64
}

type hongguoV5Field struct {
	tag int
	value any
}

func mustHongguoHex(value string) []byte {
	decoded, err := hex.DecodeString(value)
	if err != nil { panic(err) }
	return decoded
}

func hongguoV5RandomBytes(size int) []byte {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil { panic(err) }
	return value
}

func hongguoV5MD5(value []byte) []byte {
	sum := md5.Sum(value)
	return sum[:]
}

func hongguoV5SM3(value []byte) []byte {
	sum := hongguoSM3(value)
	return sum[:]
}

func hongguoV5Concat(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts { out = append(out, part...) }
	return out
}

func hongguoV5Slice(value []byte, start, end int) []byte {
	if start < 0 { start = 0 }
	if end > len(value) { end = len(value) }
	if start > end { return nil }
	return append([]byte(nil), value[start:end]...)
}

func hongguoV5Le32(value uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, value)
	return out
}

func hongguoV5U32LE(value []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(value[offset:offset+4])
}

func hongguoV5PutUint32BE(value []byte, offset int, number uint32) {
	binary.BigEndian.PutUint32(value[offset:offset+4], number)
}

func hongguoV5Pad16(value []byte) []byte {
	padding := 16 - len(value)%16
	out := append([]byte(nil), value...)
	for i := 0; i < padding; i++ { out = append(out, byte(padding)) }
	return out
}

func hongguoV5AppendVarint(out []byte, value uint64) []byte {
	var buffer [10]byte
	n := binary.PutUvarint(buffer[:], value)
	return append(out, buffer[:n]...)
}

func hongguoV5VarintField(out []byte, field int, value uint64) []byte {
	out = hongguoV5AppendVarint(out, uint64(field*8))
	return hongguoV5AppendVarint(out, value)
}

func hongguoV5BytesField(out []byte, field int, value []byte) []byte {
	out = hongguoV5AppendVarint(out, uint64(field*8+2))
	out = hongguoV5AppendVarint(out, uint64(len(value)))
	return append(out, value...)
}

func hongguoV5Proto(fields []hongguoV5Field) []byte {
	var out []byte
	for _, field := range fields {
		switch value := field.value.(type) {
		case int:
			if value < 0 { out = hongguoV5VarintField(out, field.tag, uint64(-value*2-1)) } else { out = hongguoV5VarintField(out, field.tag, uint64(value*2)) }
		case int64:
			if value < 0 { out = hongguoV5VarintField(out, field.tag, uint64(-value*2-1)) } else { out = hongguoV5VarintField(out, field.tag, uint64(value*2)) }
		case uint32:
			out = hongguoV5VarintField(out, field.tag, uint64(value)*2)
		case string:
			if value != "" { out = hongguoV5BytesField(out, field.tag, []byte(value)) }
		case []byte:
			if len(value) != 0 { out = hongguoV5BytesField(out, field.tag, value) }
		}
	}
	return out
}

func hongguoV5EncodeQuery(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values { keys = append(keys, key) }
	sort.Strings(keys)
	query := url.Values{}
	for _, key := range keys { query.Set(key, values[key]) }
	// url.Values.Encode sorts keys and applies the same application/x-www-form-urlencoded
	// escaping used by the upstream module for this ASCII-only playback query.
	return query.Encode()
}

func hongguoV5ReadU64(value []byte, offset int) uint64 {
	return binary.LittleEndian.Uint64(value[offset:offset+8])
}

func hongguoV5WriteU64(value []byte, offset int, number uint64) {
	binary.LittleEndian.PutUint64(value[offset:offset+8], number)
}

func hongguoV5Rotate8(value byte, shift int) byte { return bits.RotateLeft8(value, shift) }

func hongguoV5IV(queryHash, bodyMD5 []byte, timestamp uint32) uint32 {
	value := uint32(0x20230928)
	inputs := [][]byte{queryHash, bodyMD5, hongguoV5Le32(timestamp)}
	for _, data := range inputs {
		for index, current := range data {
			if index&1 == 0 {
				value = (value >> 4) ^ value ^ (value << 6) ^ uint32(current)
			} else {
				value = ^((value >> 7) ^ value ^ (uint32(current) | (value << 12)))
			}
		}
	}
	return value
}

func hongguoV5Hash(queryHash, bodyMD5 []byte, timestamp uint32) ([]byte, error) {
	if len(queryHash) != 32 || len(bodyMD5) != 16 {
		return nil, errors.New("V5 hash requires a 32-byte query hash and a 16-byte body hash")
	}
	if hongguoV5IV(queryHash, bodyMD5, timestamp)&15 != 12 {
		return nil, errors.New("unsupported V5 hash branch")
	}
	const iv uint32 = 0x9d4a97c2
	count := iv + timestamp
	table := make([]uint32, len(hongguoV5HashTable))
	for i, value := range hongguoV5HashTable {
		table[i] = bits.RotateLeft32(value, -int((count+1)&255))
	}
	shift := (count + 2) & 7
	padding := []byte{
		byte((uint32(0xfa)|uint32(0xfa)<<8)>>shift),
		byte((uint32(0x45)|uint32(0x45)<<8)>>shift),
		byte((uint32(0x61)|uint32(0x61)<<8)>>shift),
		byte((uint32(0xd7)|uint32(0xd7)<<8)>>shift),
	}
	data := hongguoV5Concat(queryHash, bodyMD5, hongguoV5Le32(timestamp), padding, []byte{0,0,0,0,0,0,1,0xa0})
	words := make([]uint32, 0, 128)
	for index := 0; index < len(data); index += 4 {
		words = append(words, binary.BigEndian.Uint32(data[index:index+4]))
	}
	for index := 0; index < 112; index++ {
		x, y := words[index+1], words[index+14]
		sigmaX := bits.RotateLeft32(x,14) ^ bits.RotateLeft32(x,25) ^ (x>>3)
		sigmaY := bits.RotateLeft32(y,13) ^ bits.RotateLeft32(y,15) ^ (y>>10)
		words = append(words, words[index]+words[index+9]+sigmaX+sigmaY)
	}
	initial := [8]uint32{0x7aba4fc8,0x67166507,0x6403fa00,0x340f512f,984304912,3005047866,2874125293,2152413264}
	for i := range initial { initial[i] = bits.RotateLeft32(initial[i], -int(count)) }
	state := initial
	for index := 0; index < 96; index++ {
		base := (iv + uint32(index)) & 127
		choose := ((state[4]^state[0])&state[1]) ^ state[4]
		sigma1 := bits.RotateLeft32(state[1],26) ^ bits.RotateLeft32(state[1],21) ^ bits.RotateLeft32(state[1],7)
		mixed := words[base]+choose+sigma1+table[base&63]+state[6]
		sigma0 := bits.RotateLeft32(state[3],30) ^ bits.RotateLeft32(state[3],19) ^ bits.RotateLeft32(state[3],10)
		majority := (state[3]&state[5]) | ((state[3]|state[5])&state[2])
		old := state[7]
		for i:=7;i>0;i-- { state[i]=state[i-1] }
		state[0]=old
		state[7]=sigma0+majority+mixed
		state[0]=old+mixed
	}
	digest := make([]byte,32)
	for index := 0; index < 8; index++ { hongguoV5PutUint32BE(digest,index*4,state[index]+initial[index]) }
	folded := make([]byte,16)
	for index:=0;index<16;index++ { folded[index]=digest[index]^digest[index+16] }
	check:=uint32(0x20220420)
	for index:=0;index<12;index++ {
		even:=index&1==0
		shift:=uint(5)
		if even { shift=3 }
		temp:=(check>>shift)^check
		if even { check=(uint32(folded[index])^(check<<7))^temp } else { check=^(uint32(folded[index])|(check<<11))^temp }
	}
	return hongguoV5Concat(folded,hongguoV5Le32(((check|4)^0x1000000)|0x20)),nil
}

func hongguoV5CarrierBlock(value []byte, expanded []byte, box []byte, con []int, order []int) []byte {
	var rows [4][4]byte
	for row:=0;row<4;row++ { copy(rows[row][:],value[row*4:row*4+4]) }
	addCon:=func(offset int) {
		for i:=0;i<4;i++ { for j:=0;j<4;j++ { rows[i][j]^=expanded[(offset+i)*4+con[j]] } }
	}
	addCon(0)
	for round:=1;round<=2;round++ {
		for i:=0;i<4;i++ { for j:=0;j<4;j++ { rows[i][j]=box[rows[i][j]] } }
		var before [4][4]byte
		before=rows
		for i:=0;i<4;i++ { rows[i]=before[con[i]] }
		flat:=make([]byte,16)
		for i:=0;i<4;i++ { copy(flat[i*4:i*4+4],rows[i][:]) }
		for i:=0;i<16;i++ { rows[i/4][i%4]=flat[order[i]] }
		if round==1 {
			for i:=0;i<4;i++ {
				var reordered [4]byte
				for j:=0;j<4;j++ { reordered[j]=rows[i][con[j]] }
				rows[i]=reordered
			}
			xt:=func(v byte)byte { result:=uint16(v)<<1;if v&0x80!=0{result^=0x1b};return byte(result&0xff) }
			for column:=0;column<4;column++ {
				mixed:=rows[0][column]^rows[1][column]^rows[2][column]^rows[3][column]
				first:=rows[0][column]
				rows[0][column]^=mixed^xt(rows[0][column]^rows[1][column])
				rows[1][column]^=mixed^xt(rows[1][column]^rows[2][column])
				rows[2][column]^=mixed^xt(rows[2][column]^rows[3][column])
				rows[3][column]^=mixed^xt(rows[3][column]^first)
			}
		}
		addCon(round*4)
	}
	for i:=0;i<4;i++ { for j:=0;j<4;j++ { rows[i][j]^=expanded[(4+i)*4+j] } }
	out:=make([]byte,16)
	for i:=0;i<4;i++ { copy(out[i*4:i*4+4],rows[i][:]) }
	return out
}

func hongguoV5Carrier(key, iv, source []byte, timestamp uint32) ([]byte,error) {
	if len(key)!=16 || len(iv)!=16 || len(source)<248 { return nil,errors.New("V5 carrier requires 16-byte key/IV and at least 248 source bytes") }
	variant:=int(timestamp&3)
	box:=hongguoV5Slice(hongguoV5Sbox,variant*256,(variant+1)*256)
	cons:=[][]int{{2,0,3,1},{1,3,2,0},{3,2,1,0},{2,3,1,0}}
	orders:=[][]int{
		{0,9,14,15,4,13,2,7,8,1,6,3,12,5,10,11},
		{0,9,14,11,4,13,2,3,8,1,6,15,12,5,10,7},
		{0,9,14,3,4,13,2,15,8,1,6,11,12,5,10,7},
		{0,9,14,15,4,13,2,3,8,1,6,11,12,5,10,7},
	}
	initials:=[]uint32{0xbbb5de72,0xd4638d5b,0x92a5f73b,0x89f8a1c9}
	initial:=initials[variant]
	initialBytes:=hongguoV5Le32(initial)
	expanded:=make([]byte,48)
	for index:=0;index<16;index++ { expanded[index]=initialBytes[index&3]^key[index] }
	rounds:=8
	for index:=4;index<12;index++ {
		at:=4*(index-1)
		a,b,c,d:=expanded[at],expanded[at+1],expanded[at+2],expanded[at+3]
		if index&3==0 {
			next:=byte((initial>>uint(rounds&24))^uint32(box[b]))
			b=box[c];c=box[d];d=box[a];a=next
		}
		rounds+=2
		expanded[at+4]=a^expanded[at-12]
		expanded[at+5]=b^expanded[at-11]
		expanded[at+6]=c^expanded[at-10]
		expanded[at+7]=d^expanded[at-9]
	}
	wire:=[]int{6,5,3,4,2,7,1,0}
	compact:=[]int{1,7,3,6,2,0,4,5}
	plaintext:=make([]byte,32)
	for i:=0;i<31;i++ { for j:=0;j<8;j++ { plaintext[i]|=((source[i*8+j]>>uint(wire[j]))&1)<<uint(compact[j]) } }
	plaintext[31]=1
	input:=make([]byte,16)
	for i:=0;i<16;i++ { input[i]=plaintext[i]^iv[i] }
	first:=hongguoV5CarrierBlock(input,expanded,box,cons[variant],orders[variant])
	for i:=0;i<16;i++ { input[i]=plaintext[i+16]^first[i] }
	second:=hongguoV5CarrierBlock(input,expanded,box,cons[variant],orders[variant])
	derived:=hongguoV5Concat(first,second)
	out:=make([]byte,len(source)+1)
	out[0]=derived[31]
	copy(out[1:],source)
	for i:=0;i<31;i++ { for j:=0;j<8;j++ {
		at:=1+i*8+j
		mask:=byte(1<<uint(wire[j]))
		out[at]=(out[at]&^mask)|(((derived[i]>>uint(compact[j]))&1)<<uint(wire[j]))
	} }
	return out,nil
}

func hongguoV5Xmxor(data,key []byte) []byte {
	encoded:=make([]byte,len(data))
	for index:=0;index<len(data);index++ {
		at:=(index*4)&28
		d0,d1:=key[at],key[at+1]
		mixed:=hongguoV5Rotate8(data[index],4)+d0
		mixed=^mixed^d1
		mixed=hongguoV5Rotate8(mixed,3)
		mixed+=d1
		mixed^=d0
		encoded[len(data)-index-1]=^mixed
	}
	lastIndex:=len(encoded)-1
	last:=encoded[lastIndex]^encoded[lastIndex-1]
	first:=encoded[0]
	encoded[0]=byte(^last+first)
	encoded[1]=byte((encoded[0]^encoded[lastIndex]^254)+encoded[1])
	encoded[2]=byte(encoded[2]+((last-first)^hongguoV5Rotate8(encoded[1],3)^2))
	for index:=0;index<len(encoded)-4;index++ {
		mixed:=hongguoV5Rotate8(encoded[index+2],3)^encoded[index+1]^byte(index+3)
		encoded[index+3]=byte(^mixed+encoded[index+3])
	}
	encoded[lastIndex]^=encoded[lastIndex-1]
	sum:=0
	for index:=1;index<len(encoded);index++ { sum+=int(encoded[index]) }
	encoded[0]=byte((encoded[0]^encoded[1])+byte(sum))
	return encoded
}

func hongguoV5Helios(timestamp uint32) string {
	prefix:=hongguoV5RandomBytes(4)
	ascii:=[]byte(hex.EncodeToString(hongguoV5MD5(hongguoV5Concat(prefix,[]byte("8662")))))
	left,right:=hongguoV5ReadU64(ascii,0),hongguoV5ReadU64(ascii,8)
	queue:=[]uint64{hongguoV5ReadU64(ascii,16),hongguoV5ReadU64(ascii,24)}
	keys:=make([]uint64,0,34)
	for index:=0;index<34;index++ {
		keys=append(keys,left)
		mixed:=(bits.RotateLeft64(right,-8)+left)^uint64(index)
		queue=append(queue,mixed)
		left=mixed^bits.RotateLeft64(left,3)
		right=queue[0]
		queue=queue[1:]
	}
	data:=hongguoV5Pad16([]byte(strconv.FormatUint(uint64(timestamp),10)+"-646637794-8662"))
	for offset:=0;offset<len(data);offset+=16 {
		a,b:=hongguoV5ReadU64(data,offset),hongguoV5ReadU64(data,offset+8)
		for _,key:=range keys {
			b=key^(a+bits.RotateLeft64(b,-8))
			a=b^bits.RotateLeft64(a,3)
		}
		hongguoV5WriteU64(data,offset,a)
		hongguoV5WriteU64(data,offset+8,b)
	}
	return base64.StdEncoding.EncodeToString(hongguoV5Concat(prefix,data))
}

func hongguoV5Medusa(queryHash,f13 []byte,timestamp uint32,values map[string]string,now time.Time) string {
	hongguoV5ProcessMu.Lock()
	if hongguoV5Process==nil {
		nonce:=base64.RawURLEncoding.EncodeToString(hongguoV5RandomBytes(18))
		hongguoV5Process=&hongguoV5ProcessState{nonce:nonce,started:now.UnixMilli()}
	}
	process:=*hongguoV5Process
	hongguoV5ProcessMu.Unlock()
	flags:=make([]byte,8)
	deviceInfo:=hongguoV5Proto([]hongguoV5Field{
		{1,1},{2,2},{3,"8662"},{4,values["device_id"]},{5,process.nonce},
		{6,"!noperm!"},{12,"Asia/Shanghai,8"},{13,"zh_CN"},{22,values["os_version"]},
		{25,process.started},{30,values["device_type"]},{31,firstHongguoValue(values["device_brand"],"HONOR")},
		{32,values["device_type"]},{34,firstHongguoValue(values["device_brand"],"HONOR")},
	})
	env:=hongguoV5Proto([]hongguoV5Field{{6,"v04.10.03.04-bugfix"},{8,values["channel"]},{12,deviceInfo},{14,values["version_name"]}})
	message:=hongguoV5Proto([]hongguoV5Field{
		{1,hongguoV5Magic},{2,5},{3,hongguoV5U32LE(hongguoV5RandomBytes(4),0)},{4,"8662"},
		{5,values["device_id"]},{6,"646637794"},{7,values["version_name"]},{8,"v04.10.03-ml-android"},
		{9,0x040a0300},{10,flags},{12,timestamp},{13,f13},{14,hongguoV5Slice(queryHash,0,6)},
		{15,hongguoV5Proto([]hongguoV5Field{{1,1},{2,1},{3,1},{5,1}})},
		{17,timestamp},{20,"none"},{21,377},{23,env},
		{24,`{"cmr":16777216,"cmr2":16777216,"un_h":11,"vpn":0,"kd":0,"dyn":"","do":1,"tk":true}`},
	})
	nonce:=hongguoV5U32LE(hongguoV5RandomBytes(4),0)
	hash:=hongguoV5SM3(hongguoV5Concat(hongguoV5SignKey,hongguoV5Le32(nonce),hongguoV5SignKey))
	high:=(nonce>>16)&255
	seed:=hongguoV5Le32(^(uint32((high<<11)|(nonce>>24))^(high>>5)^high))
	transformed:=hongguoV5Concat(flags,hongguoV5Xmxor(message,hash))
	for left,right:=0,len(transformed)-1;left<right;left,right=left+1,right-1 { transformed[left],transformed[right]=transformed[right],transformed[left] }
	for index:=range transformed { transformed[index]^=seed[(^index)&3] }
	check:=uint32(0x18000001)|((uint32(queryHash[0])&63)<<14)|((uint32(f13[0])&63)<<8)
	packed:=hongguoV5Concat([]byte{0xca},hongguoV5RandomBytes(4),hongguoV5Le32(check),transformed,[]byte{byte(nonce>>16),byte(nonce>>24)})
	encrypted,err:=hongguoV5Carrier(hongguoV5MD5(hongguoV5Slice(hongguoV5SignKey,0,16)),hongguoV5MD5(hongguoV5Slice(hongguoV5SignKey,16,32)),packed,timestamp)
	if err!=nil { return "" }
	prefix:=hongguoV5Concat(hongguoV5Le32(5),hongguoV5Magic)
	stamp:=hongguoV5Le32(timestamp)
	for index:=range prefix { prefix[index]^=stamp[index&3] }
	return base64.StdEncoding.EncodeToString(hongguoV5Concat(prefix,[]byte{byte(nonce),byte(nonce>>8),0,1},encrypted))
}

func firstHongguoValue(values ...string) string { for _,value:=range values { if value!="" { return value } };return "" }

func signHongguoPlaybackV5(base string, values map[string]string, body []byte, now time.Time) (string,http.Header,error) {
	timestamp:=uint32(now.Unix())
	bodyMD5:=make([]byte,16)
	if body!=nil { bodyMD5=hongguoV5MD5(body) }
	for offset:=int64(0);offset<1024;offset++ {
		params:=make(map[string]string,len(values)+1)
		for key,value:=range values { params[key]=value }
		params["_rticket"]=strconv.FormatInt(now.UnixMilli()+offset,10)
		query:=hongguoV5EncodeQuery(params)
		queryHash:=hongguoV5SM3([]byte(query))
		if hongguoV5IV(queryHash,bodyMD5,timestamp)&15!=12 { continue }
		for _,name:=range []string{"device_id","version_name","os_version","device_type","channel"} {
			if params[name]=="" { return "",nil,fmt.Errorf("紅果播放簽章缺少設備欄位：%s",name) }
		}
		f13,err:=hongguoV5Hash(queryHash,bodyMD5,timestamp)
		if err!=nil { continue }
		headers:=make(http.Header)
		headers.Set("X-Khronos",strconv.FormatUint(uint64(timestamp),10))
		headers.Set("X-Helios",hongguoV5Helios(timestamp))
		headers.Set("X-Medusa",hongguoV5Medusa(queryHash,f13,timestamp,params,now))
		if body!=nil {
			headers.Set("X-SS-STUB",strings.ToUpper(hex.EncodeToString(bodyMD5)))
			headers.Set("Content-Type","application/json; charset=utf-8")
		}
		return base+"?"+query,headers,nil
	}
	return "",nil,errors.New("紅果無法在 1024 次 ticket 嘗試內產生 V5 播放簽章")
}

func hongguoPlaybackQuery(deviceID, installID string) map[string]string {
	return map[string]string{
		"aid": "8662",
		"app_name": "novelread",
		"version_code": "73932",
		"version_name": "7.3.9.32",
		"manifest_version_code": "73932",
		"update_version_code": "73932",
		"pv_player": "73932",
		"channel": "update_64",
		"device_platform": "android",
		"os": "android",
		"ssmix": "a",
		"device_type": "25053RT47C",
		"device_brand": "Redmi",
		"device_manufacturer": "Xiaomi",
		"language": "zh",
		"os_api": "36",
		"os_version": "16",
		"resolution": "1280*2772",
		"dpi": "520",
		"ac": "wifi",
		"host_abi": "arm64-v8a",
		"dragon_device_type": "phone",
		"compliance_status": "0",
		"need_personal_recommend": "1",
		"player_so_load": "1",
		"is_android_pad_screen": "0",
		"iid": installID,
		"cdid": uuidLike(),
		"device_id": deviceID,
	}
}
