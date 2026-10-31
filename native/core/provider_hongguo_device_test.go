package core

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestHongguoAppBaseURLMatchesUpstreamFallbackHost(t *testing.T) {
	const want = "https://api5-normal-sinfonlinea.fqnovel.com"
	if hongguoAppBaseURL != want {
		t.Fatalf("Hongguo App API host = %q, want upstream fallback host %q", hongguoAppBaseURL, want)
	}
}

func hongguoDeviceFixture(t *testing.T, rejected map[string]bool, registerFails bool) (*Downloader, *[]string) {
	var requests []string
	registered := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == hongguoDeviceRegisterURL+"?"+request.URL.RawQuery {
			registered++
			requests = append(requests, "register")
			if request.URL.Query().Get("device_id") != "" || request.Header.Get("X-Argus") != "" {
				t.Error("device registration leaked an old device or a signature")
			}
			if registerFails {
				return sourceFixtureResponse(request, http.StatusOK, `{"device_id":0,"install_id":0}`), nil
			}
			id := strconv.Itoa(1000 + registered)
			return sourceFixtureResponse(request, http.StatusOK, `{"device_id_str":"`+id+`","install_id_str":"`+id+`9"}`), nil
		}
		device := request.URL.Query().Get("device_id")
		requests = append(requests, "detail@"+device)
		if rejected[device] {
			return sourceFixtureResponse(request, http.StatusOK, ""), nil
		}
		return sourceFixtureResponse(request, http.StatusOK, `{"code":0,"data":{}}`), nil
	})
	d.cfg.Retries = 2
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "7", "8", false
	return d, &requests
}

func TestHongguoSignedRequestRegistersDeviceBeforeRequest(t *testing.T) {
	d, requests := hongguoDeviceFixture(t, nil, false)
	if _, err := d.hongguoAppRequest(context.Background(), http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if state := d.hongguoCatalogSnapshot(); state.DeviceID != "1001" || state.InstallID != "10019" || !state.Registered {
		t.Fatal("signed request did not use the registered device", state)
	}
	if got := *requests; len(got) != 2 || got[0] != "register" || got[1] != "detail@1001" {
		t.Fatal("unexpected request sequence", got)
	}
}

func TestHongguoSignedRequestRotatesRejectedDeviceOnEmptyResponse(t *testing.T) {
	d, requests := hongguoDeviceFixture(t, map[string]bool{"1001": true}, false)
	d.cfg.Retries = 1
	if _, err := d.hongguoAppRequest(context.Background(), http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if state := d.hongguoCatalogSnapshot(); state.DeviceID != "1002" || state.InstallID != "10029" || !state.Registered {
		t.Fatal("rejected device was not rotated", state)
	}
	if got := *requests; len(got) != 4 || got[0] != "register" || got[1] != "detail@1001" || got[2] != "register" || got[3] != "detail@1002" {
		t.Fatal("unexpected request sequence", got)
	}
}

func TestHongguoDeviceRegistrationReplacesDevice(t *testing.T) {
	d, requests := hongguoDeviceFixture(t, nil, false)
	if err := d.ensureHongguoDevice(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if err := d.ensureHongguoDevice(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if state := d.hongguoCatalogSnapshot(); !state.Registered || state.DeviceID != "1001" || state.InstallID != "10019" {
		t.Fatal("registered device was not kept", state)
	}
	if err := d.ensureHongguoDevice(context.Background(), "1001"); err != nil {
		t.Fatal(err)
	}
	if state := d.hongguoCatalogSnapshot(); state.DeviceID != "1002" {
		t.Fatal("rejected device was not replaced", state)
	}
	if got := *requests; len(got) != 2 || got[0] != "register" || got[1] != "register" {
		t.Fatal("unexpected request sequence", got)
	}
}

func TestHongguoTabRejectionKeepsDevice(t *testing.T) {
	d, requests := hongguoDeviceFixture(t, map[string]bool{"7": true}, false)
	d.cfg.Retries = 1
	if _, err := d.hongguoAppRequest(context.Background(), http.MethodGet, hongguoBookmallTabPath, nil, nil); err == nil {
		t.Fatal("empty tab response became a success")
	}
	if state := d.hongguoCatalogSnapshot(); state.DeviceID != "7" || state.Registered {
		t.Fatal("unsigned tab request changed the device", state)
	}
	if got := *requests; len(got) != 1 || got[0] != "detail@7" {
		t.Fatal("unexpected request sequence", got)
	}
}

func TestHongguoRegisterFailurePausesAndKeepsOldDevice(t *testing.T) {
	d, requests := hongguoDeviceFixture(t, nil, true)
	for range 2 {
		if err := d.ensureHongguoDevice(context.Background(), ""); err == nil {
			t.Fatal("failed registration reported success")
		}
	}
	if state := d.hongguoCatalogSnapshot(); state.DeviceID != "7" || state.Registered {
		t.Fatal("failed registration changed the device", state)
	}
	if got := *requests; len(got) != 1 || got[0] != "register" {
		t.Fatal("registration was not paused after failure", got)
	}
}

func TestHongguoSignedRequestRotatesRejectedDeviceOnForbiddenResponse(t *testing.T) {
	var requests []string
	registered := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/service/2/device_register/" {
			registered++
			requests = append(requests, "register")
			id := strconv.Itoa(1000 + registered)
			return sourceFixtureResponse(request, http.StatusOK, `{"device_id_str":"`+id+`","install_id_str":"`+id+`9"}`), nil
		}
		device := request.URL.Query().Get("device_id")
		requests = append(requests, "detail@"+device)
		if device == "1001" {
			return sourceFixtureResponse(request, http.StatusForbidden, `{"code":403}`), nil
		}
		return sourceFixtureResponse(request, http.StatusOK, `{"code":0,"data":{}}`), nil
	})
	d.cfg.Retries = 1
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "7", "8", false

	if _, err := d.hongguoAppRequest(context.Background(), http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if state := d.hongguoCatalogSnapshot(); state.DeviceID != "1002" || state.InstallID != "10029" || !state.Registered {
		t.Fatal("forbidden device was not rotated", state)
	}
	if len(requests) != 4 || requests[0] != "register" || requests[1] != "detail@1001" || requests[2] != "register" || requests[3] != "detail@1002" {
		t.Fatal("unexpected request sequence", requests)
	}
}

// TestHongguoDeviceRotationClearsOnlyForbiddenHostCooldown verifies that an
// identity-specific 403 can be retried after registration without removing
// unrelated rate-limit or temporary-error backoffs.
func TestHongguoDeviceRotationClearsOnlyForbiddenHostCooldown(t *testing.T) {
	limiter := newRequestLimiter(2, 0)
	limiter.backoffs["api.example.test"] = &requestBackoff{
		host: "api.example.test", until: time.Now().Add(time.Minute), status: http.StatusForbidden,
	}
	limiter.backoffs["rate.example.test"] = &requestBackoff{
		host: "rate.example.test", until: time.Now().Add(time.Minute), status: http.StatusTooManyRequests,
	}
	limiter.backoffs["blocked.example.test"] = &requestBackoff{
		host: "blocked.example.test", until: time.Now().Add(time.Minute), status: http.StatusForbidden, reason: "browser challenge",
	}

	limiter.clearDeviceRejectionBackoff("API.EXAMPLE.TEST")

	if _, ok := limiter.backoffs["api.example.test"]; ok {
		t.Fatal("rotated device host remained in 403 cooldown")
	}
	if _, ok := limiter.backoffs["rate.example.test"]; !ok {
		t.Fatal("device rotation cleared a 429 rate limit")
	}
	if _, ok := limiter.backoffs["blocked.example.test"]; !ok {
		t.Fatal("device rotation cleared a browser challenge")
	}
}
