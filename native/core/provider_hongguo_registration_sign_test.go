package core

import (
	"net/http"
	"testing"
	"time"
)

func TestHongguoRegistrationSignatureMatchesReferenceVector(t *testing.T) {
	const query = "aid=8662&app_name=novelread&version_code=73532&version_name=7.3.5.32&channel=update_64&_rticket=1700000000123"
	body := []byte(`{"magic_tag":"ss_app_log","header":{"aid":8662},"_gen_time":1700000000}`)
	now := time.Unix(1700000000, 0)

	headers := hongguoRegistrationSignatureHeaders(query, body, now, 0x1234)
	if got, want := headers.Get("X-Khronos"), "1700000000"; got != want {
		t.Fatalf("X-Khronos = %q, want %q", got, want)
	}
	if got, want := headers.Get("X-Gorgon"), "84041234448147be9afa058991e0b1159cfa1daa141e5e044e68"; got != want {
		t.Fatalf("X-Gorgon = %q, want %q", got, want)
	}
	if got, want := headers.Get("X-Argus"), "APFTZQ=="; got != want {
		t.Fatalf("X-Argus = %q, want %q", got, want)
	}
}

func TestSignHongguoDeviceRegistrationAddsOnlyBasicRegistrationHeaders(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://log.snssdk.com/service/2/device_register/?aid=8662&_rticket=1700000000123", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"magic_tag":"ss_app_log"}`)
	if err := signHongguoDeviceRegistration(request, body, time.Unix(1700000000, 0)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"X-Khronos", "X-Gorgon", "X-Argus"} {
		if request.Header.Get(name) == "" {
			t.Errorf("missing %s", name)
		}
	}
	for _, name := range []string{"X-Helios", "X-Medusa", "X-SS-STUB"} {
		if request.Header.Get(name) != "" {
			t.Errorf("unexpected playback-only header %s", name)
		}
	}
}
