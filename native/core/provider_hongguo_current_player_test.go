package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func hongguoCurrentFixture(t *testing.T, name string, dst any) {
	t.Helper()
	path := filepath.Join("testdata", "hongguo", name)
	raw, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	if err := json.Unmarshal(raw, dst); err != nil { t.Fatalf("decode %s: %v", name, err) }
}

func hongguoCurrentSHA256(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func TestHongguoCurrentPlayerNativeEnvelopeVectors(t *testing.T) {
	type input struct {
		RawQuery string `json:"raw_query"`
		RecordDigest []byte `json:"record_digest"`
		FactorPB []byte `json:"factor_pb"`
		Material []byte `json:"material"`
		LicenseID string `json:"license_id"`
		AID string `json:"aid"`
		Seconds uint64 `json:"seconds"`
		GorgonNonce uint16 `json:"gorgon_nonce"`
		NativeGorgonFlags uint16 `json:"native_gorgon_flags"`
		HeliosRandom uint32 `json:"helios_random"`
		MedusaRandom1 uint32 `json:"medusa_random1"`
		MedusaRandom2 uint32 `json:"medusa_random2"`
		LocalBufferAddress uint64 `json:"local_buffer_address"`
		PreviousThreadState uint32 `json:"previous_thread_state"`
	}
	type whole struct {
		ID string `json:"id"`
		Inputs input `json:"inputs"`
		ExpectedHeaderSHA256 map[string]string `json:"expected_header_sha256"`
		ExpectedThreadState uint32 `json:"expected_thread_state"`
	}
	type transform struct {
		Branch int `json:"branch"`
		Key []byte `json:"key"`
		IV []byte `json:"iv"`
		Plain []byte `json:"plain"`
		ExpectedSHA256 string `json:"expected_sha256"`
	}
	var fixture struct {
		SyntheticOnly bool `json:"synthetic_only"`
		Whole []whole `json:"whole"`
		Transforms []transform `json:"transforms"`
	}
	hongguoCurrentFixture(t, "current_crypto.json", &fixture)
	if !fixture.SyntheticOnly || len(fixture.Whole) != 32 || len(fixture.Transforms) != 22 {
		t.Fatalf("unexpected current-player fixture shape: synthetic=%v whole=%d transforms=%d", fixture.SyntheticOnly, len(fixture.Whole), len(fixture.Transforms))
	}
	for _, vector := range fixture.Whole {
		t.Run(vector.ID, func(t *testing.T) {
			in := vector.Inputs
			headers, state, err := hongguoCurrentCompute(hongguoCurrentComputeInput{
				Query: in.RawQuery, Digest: in.RecordDigest, PB: in.FactorPB, Material: in.Material,
				License: in.LicenseID, AID: in.AID, Seconds: in.Seconds, Nonce: in.GorgonNonce,
				Flags: in.NativeGorgonFlags, HeliosRandom: in.HeliosRandom,
				Random1: in.MedusaRandom1, Random2: in.MedusaRandom2,
				Local: in.LocalBufferAddress, Previous: in.PreviousThreadState,
			})
			if err != nil { t.Fatal(err) }
			if state != vector.ExpectedThreadState {
				t.Fatalf("thread state = %08x, want %08x", state, vector.ExpectedThreadState)
			}
			for name, expected := range vector.ExpectedHeaderSHA256 {
				got := headers.Get(httpCanonicalHeader(name))
				if got == "" { t.Errorf("missing %s header", name); continue }
				if actual := hongguoCurrentSHA256([]byte(got)); actual != expected {
					t.Errorf("%s SHA-256 = %s, want %s", name, actual, expected)
				}
			}
		})
	}
	for _, vector := range fixture.Transforms {
		t.Run(fmt.Sprintf("transform-branch-%d-%s", vector.Branch, vector.ExpectedSHA256[:8]), func(t *testing.T) {
			got, err := hongguoCurrentTransform(vector.Plain, vector.Key, vector.IV, vector.Branch)
			if err != nil { t.Fatal(err) }
			if actual := hongguoCurrentSHA256(got); actual != vector.ExpectedSHA256 {
				t.Fatalf("transform SHA-256 = %s, want %s", actual, vector.ExpectedSHA256)
			}
		})
	}
}

func httpCanonicalHeader(name string) string {
	parts := strings.Split(strings.ToLower(name), "-")
	for i := range parts {
		if len(parts[i]) > 0 { parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:] }
	}
	return strings.Join(parts, "-")
}

func TestHongguoCurrentPlayerFactorVectors(t *testing.T) {
	type vector struct {
		ToySeed int `json:"toy_seed"`
		PublicInputSeed int `json:"public_input_seed"`
		PublicSeconds uint32 `json:"public_seconds"`
		OutputSHA256 string `json:"output_sha256"`
	}
	var vectors []vector
	hongguoCurrentFixture(t, "current_factor.json", &vectors)
	if len(vectors) != 48 { t.Fatalf("factor vector count = %d, want 48", len(vectors)) }
	for _, vector := range vectors {
		t.Run(fmt.Sprintf("selector-%d-seed-%d-time-%d", vector.ToySeed, vector.PublicInputSeed, vector.PublicSeconds), func(t *testing.T) {
			input := make([]byte, 52)
			for i := range input { input[i] = byte(i*37 + vector.PublicInputSeed) }
			got, err := hongguoCurrentFactor(input, vector.PublicSeconds, vector.ToySeed)
			if err != nil { t.Fatal(err) }
			if actual := hongguoCurrentSHA256(got); actual != vector.OutputSHA256 {
				t.Fatalf("factor SHA-256 = %s, want %s", actual, vector.OutputSHA256)
			}
		})
	}
}

func TestSignHongguoCurrentPlayerUsesExactTicketBinding(t *testing.T) {
	pairs := []hongguoQueryPair{
		{"aid", "8662"},
		{"device_id", "1000000000000000001"},
		{"version_name", "7.3.9.32"},
		{"ts", "stale"},
		{"_rticket", "stale"},
		{"device_type", "23127PN0CC"},
		{"channel", "xiaomi_8662_64"},
	}
	entropy := make([]byte, 24)
	for i := range entropy { entropy[i] = byte(i + 1) }
	body := []byte(`{"video_id":"synthetic-1"}`)
	var crossedSecond bool
	for i := 0; i < 32; i++ {
		now := int64(1794827000999) + int64(i)*1000
		query, headers, err := signHongguoCurrentPlayer(pairs, now, pairs[1].value, "7.3.9.32", body, entropy)
		if err != nil { t.Fatal(err) }
		parsed, err := url.ParseQuery(query)
		if err != nil { t.Fatal(err) }
		if len(parsed["ts"]) != 1 || len(parsed["_rticket"]) != 1 { t.Fatalf("query contains duplicate timestamp fields: %s", query) }
		ticket, err := strconv.ParseInt(parsed.Get("_rticket"), 10, 64)
		if err != nil { t.Fatal(err) }
		seconds, err := strconv.ParseInt(parsed.Get("ts"), 10, 64)
		if err != nil { t.Fatal(err) }
		if ticket < now || ticket >= now+128 { t.Fatalf("ticket=%d outside [%d,%d)", ticket, now, now+128) }
		if seconds != ticket/1000 { t.Fatalf("ts=%d, ticket=%d", seconds, ticket) }
		if ticket/1000 != now/1000 { crossedSecond = true }
		for _, header := range []string{"X-Khronos", "X-Argus", "X-Ladon", "X-Gorgon", "X-Helios", "X-Medusa", "X-SS-Stub", "X-SS-Req-Ticket"} {
			if headers.Get(header) == "" { t.Errorf("missing signed header %s", header) }
		}
		if headers.Get("X-SS-Req-Ticket") != parsed.Get("_rticket") { t.Errorf("request ticket header mismatch") }
	}
	if !crossedSecond { t.Fatal("ticket search did not exercise a second boundary") }
}

func TestHongguoCurrentPlayerProfileIsComplete(t *testing.T) {
	p := hongguoCurrentProfileData
	if len(p.material) != 32 || len(p.header) != 16 || len(p.padding) != 4 {
		t.Fatalf("invalid profile fields: material=%d header=%d padding=%d", len(p.material), len(p.header), len(p.padding))
	}
	for i := 0; i < 4; i++ {
		if len(p.boxes[i]) != 256 || len(p.initial[i]) != 4 {
			t.Fatalf("branch %d incomplete: box=%d initial=%d", i, len(p.boxes[i]), len(p.initial[i]))
		}
	}
	if len(p.factorTab) != 64 || len(p.factorIV) != 8 {
		t.Fatalf("factor tables incomplete: iv=%d table=%d", len(p.factorIV), len(p.factorTab))
	}
}
