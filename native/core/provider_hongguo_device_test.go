package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHongguoAppBaseURLMatchesUpstreamFallbackHost(t *testing.T) {
	const want = "https://api5-normal-sinfonlineb.fqnovel.com"
	if hongguoAppBaseURL != want {
		t.Fatalf("Hongguo App API host = %q, want upstream fallback host %q", hongguoAppBaseURL, want)
	}
}

func TestHongguoDeviceRegistrationMatchesGuoAppModuleV102(t *testing.T) {
	var calls int
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodPost || request.URL.Host != "log.snssdk.com" || request.URL.Path != "/service/2/device_register/" {
			t.Fatalf("unexpected registration request: %s %s", request.Method, request.URL.String())
		}
		if got := request.Header.Get("User-Agent"); got != hongguoAppUserAgent {
			t.Fatalf("registration user-agent = %q", got)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Fatalf("registration content-type = %q", got)
		}
		for _, name := range []string{"X-Khronos", "X-Gorgon", "X-Argus"} {
			if request.Header.Get(name) == "" {
				t.Errorf("registration missing signed header %s", name)
			}
		}
		query := request.URL.Query()
		expected := map[string]string{
			"aid": "8662", "app_name": "novelread", "version_code": "74332", "version_name": "7.4.3.32",
			"manifest_version_code": "74332", "update_version_code": "74332", "channel": "update_64",
			"device_platform": "android", "os": "android", "ssmix": "a", "device_type": "25053RT47C",
			"device_brand": "Redmi", "language": "zh", "os_api": "36", "os_version": "16",
			"resolution": "1280*2772", "dpi": "520", "ac": "wifi",
		}
		for name, want := range expected {
			if got := query.Get(name); got != want {
				t.Errorf("registration query %s = %q, want %q", name, got, want)
			}
		}
		if query.Get("device_id") != "" || query.Get("iid") != "" {
			t.Error("registration query must not contain a prior device id or install id")
		}
		if len(query.Get("req_id")) != 36 || len(query.Get("_rticket")) < 13 {
			t.Errorf("registration request identifiers are invalid: req_id=%q _rticket=%q", query.Get("req_id"), query.Get("_rticket"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			MagicTag string         "json:\"magic_tag\""
			Header   map[string]any "json:\"header\""
			GenTime  int64          "json:\"_gen_time\""
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("registration body is not JSON: %v", err)
		}
		if payload.MagicTag != "ss_app_log" || payload.GenTime <= 0 {
			t.Fatalf("registration envelope mismatch: %#v", payload)
		}
		for name, want := range map[string]string{
			"display_name": "红果免费短剧", "app_version": "7.4.3.32", "channel": "update_64",
			"package": "com.phoenix.read", "os": "Android", "os_version": "16",
			"device_model": "25053RT47C", "device_brand": "Redmi", "device_manufacturer": "Xiaomi",
			"cpu_abi": "arm64-v8a", "resolution": "2772x1280", "tz_name": "Asia/Shanghai",
			"sim_region": "cn", "device_platform": "android", "sdk_flavor": "china",
			"rom": "OS2.0", "rom_version": "BP2A.250605.031.A3",
		} {
			if got, _ := payload.Header[name].(string); got != want {
				t.Errorf("registration header %s = %q, want %q", name, got, want)
			}
		}
		ordered := []string{
			"\"magic_tag\":", "\"header\":", "\"display_name\":", "\"aid\":", "\"app_version\":",
			"\"version_code\":", "\"manifest_version_code\":", "\"update_version_code\":", "\"channel\":",
			"\"package\":", "\"app_language\":", "\"os\":", "\"os_version\":", "\"os_api\":",
			"\"device_model\":", "\"device_brand\":", "\"device_manufacturer\":", "\"cpu_abi\":",
			"\"density_dpi\":", "\"display_density\":", "\"resolution\":", "\"language\":", "\"timezone\":",
			"\"access\":", "\"openudid\":", "\"clientudid\":", "\"cdid\":", "\"region\":",
			"\"tz_name\":", "\"tz_offset\":", "\"sim_region\":", "\"device_platform\":",
			"\"not_request_sender\":", "\"is_system_app\":", "\"sdk_flavor\":", "\"rom\":",
			"\"rom_version\":", "\"_gen_time\":",
		}
		position := -1
		for _, key := range ordered {
			next := strings.Index(string(body), key)
			if next <= position {
				t.Fatalf("registration body field order differs from upstream JSON.stringify order at %s", key)
			}
			position = next
		}
		return sourceFixtureResponse(request, http.StatusOK, "{\"device_id_str\":\"1001\",\"install_id_str\":\"10019\"}"), nil
	})
	d.hongguoClient().state.Registered = false
	if err := d.ensureHongguoDevice(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("registration request count = %d, want 1", calls)
	}
	state := d.hongguoCatalogSnapshot()
	if !state.Registered || state.DeviceID != "1001" || state.InstallID != "10019" {
		t.Fatalf("registered state = %#v", state)
	}
}

func TestHongguoDeviceRegistrationFallsBackToLegacyHostAfterAlternateHTTP444(t *testing.T) {
	var hosts []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != hongguoDeviceRegisterPath {
			t.Fatalf("unexpected registration request: %s %s", request.Method, request.URL)
		}
		hosts = append(hosts, request.URL.Host)
		if request.URL.Host == "log.snssdk.com" {
			return sourceFixtureResponse(request, 444, `{"message":"request blocked by edge","code":"device_policy"}`), nil
		}
		if request.URL.Host != "applog.snssdk.com" {
			t.Fatalf("unexpected fallback host %q", request.URL.Host)
		}
		return sourceFixtureResponse(request, http.StatusOK, `{"device_id_str":"1001","install_id_str":"10019"}`), nil
	})
	d.hongguoClient().state.Registered = false
	if err := d.ensureHongguoDevice(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0] != "log.snssdk.com" || hosts[1] != "applog.snssdk.com" {
		t.Fatalf("registration host order = %#v, want legacy log then applog", hosts)
	}
	state := d.hongguoCatalogSnapshot()
	if !state.Registered || state.DeviceID != "1001" || state.InstallID != "10019" {
		t.Fatalf("alternate registration did not persist: %#v", state)
	}
}


func TestHongguoDeviceRegistrationFallsBackAfterTengineTLB404(t *testing.T) {
	var hosts []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != hongguoDeviceRegisterPath {
			t.Fatalf("unexpected registration request: %s %s", request.Method, request.URL)
		}
		hosts = append(hosts, request.URL.Host)
		if request.URL.Host == "log.snssdk.com" {
			response := sourceFixtureResponse(request, http.StatusNotFound, "<html><body>404 Not Found TLB</body></html>")
			response.Header.Set("Server", "Tengine")
			response.Header.Set("Content-Type", "text/html")
			response.Header.Set("Via", "cache12.edge[13,0]")
			return response, nil
		}
		if request.URL.Host != "applog.snssdk.com" {
			t.Fatalf("unexpected fallback host %q", request.URL.Host)
		}
		return sourceFixtureResponse(request, http.StatusOK, `{"device_id_str":"2001","install_id_str":"20019"}`), nil
	})
	d.hongguoClient().state.Registered = false
	if err := d.ensureHongguoDevice(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0] != "log.snssdk.com" || hosts[1] != "applog.snssdk.com" {
		t.Fatalf("registration host order = %#v, want legacy log then applog", hosts)
	}
}

func TestHongguoDeviceRegistrationDoesNotFailoverForApplication404(t *testing.T) {
	var hosts []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		hosts = append(hosts, request.URL.Host)
		return sourceFixtureResponse(request, http.StatusNotFound, `{"code":404,"message":"endpoint not found"}`), nil
	})
	d.hongguoClient().state.Registered = false
	if err := d.ensureHongguoDevice(context.Background(), ""); err == nil {
		t.Fatal("application 404 must remain a hard failure")
	}
	if len(hosts) != 1 || hosts[0] != "log.snssdk.com" {
		t.Fatalf("application 404 unexpectedly triggered host failover: %#v", hosts)
	}
}

func TestHongguoRegistrationHTTP444WritesPerHostDiagnostics(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		response := sourceFixtureResponse(request, 444, `{"message":"request blocked by edge","code":"device_policy"}`)
		response.Header.Set("Server", "edge-filter")
		response.Header.Set("Content-Type", "application/json")
		response.Header.Set("CF-Ray", "abc123-HKG")
		response.Header.Set("X-Tt-Logid", "trace-456")
		return response, nil
	})
	logPath := filepath.Join(t.TempDir(), "app.log")
	d.diagnostics = &diagnosticLog{path: logPath, limit: diagnosticLogBytes}
	d.hongguoClient().state.Registered = false
	err := d.ensureHongguoDevice(context.Background(), "")
	if err == nil {
		t.Fatal("HTTP 444 registration rejection was accepted")
	}
	for _, hint := range []string{"applog.snssdk.com", "log.snssdk.com", "HTTP 444", "device_policy"} {
		if !strings.Contains(err.Error(), hint) {
			t.Errorf("registration error omitted %q: %v", hint, err)
		}
	}
	logged, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, hint := range []string{
		`"event":"hongguo.device_registration"`,
		`"host":"applog.snssdk.com"`,
		`"host":"log.snssdk.com"`,
		`"httpStatus":444`,
		`"attempt":1`,
		"stage=http_444_edge_reject", "elapsedMs=", "server=edge-filter",
		"request blocked by edge", "abc123-HKG", "trace-456",
	} {
		if !strings.Contains(string(logged), hint) {
			t.Errorf("registration log omitted %q: %s", hint, logged)
		}
	}
	if strings.Contains(string(logged), `"magic_tag"`) ||
		strings.Contains(string(logged), `"clientudid"`) ||
		strings.Contains(string(logged), `"openudid"`) {
		t.Fatal("registration payload or generated identifiers leaked into diagnostics")
	}
}

func TestHongguoInvalidRegistrationPayloadDoesNotCreateDeviceRejectionCooldown(t *testing.T) {
	var calls int
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls++
		if calls <= 2 {
			return sourceFixtureResponse(request, http.StatusOK, "not-json"), nil
		}
		return sourceFixtureResponse(request, http.StatusOK, `{"device_id_str":"1001","install_id_str":"10019"}`), nil
	})
	d.hongguoClient().state.Registered = false
	if err := d.ensureHongguoDevice(context.Background(), ""); err == nil {
		t.Fatal("invalid registration payload was accepted")
	}
	if err := d.ensureHongguoDevice(context.Background(), ""); err != nil {
		t.Fatalf("invalid response must not create device-rejection cooldown: %v", err)
	}
	if calls != 3 {
		t.Fatalf("registration calls = %d, want 3 (two bad hosts, then immediate retry)", calls)
	}
}

func TestHongguoRegistrationTransportStageClassifiesNetworkFailures(t *testing.T) {
	cases := []struct {
		name string
		err error
		want string
	}{
		{name: "dns", err: errors.New("紅果設備註冊 DNS 解析失敗"), want: "dns"},
		{name: "tls", err: errors.New("x509: certificate signed by unknown authority"), want: "tls"},
		{name: "timeout", err: context.DeadlineExceeded, want: "timeout"},
		{name: "connect", err: errors.New("dial tcp: connection refused"), want: "connect"},
		{name: "other", err: errors.New("unexpected response"), want: "network"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hongguoRegistrationTransportStage(tc.err); got != tc.want {
				t.Fatalf("failure stage = %q, want %q", got, tc.want)
			}
		})
	}
}

func hongguoDeviceFixture(t *testing.T, rejected map[string]bool, registerFails bool) (*Downloader, *[]string) {
	var requests []string
	registered := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == hongguoDeviceRegisterPath {
			if request.URL.Host != "applog.snssdk.com" && request.URL.Host != "log.snssdk.com" {
				t.Fatalf("unexpected registration host %q", request.URL.Host)
			}
			registered++
			requests = append(requests, "register")
			if request.URL.Query().Get("device_id") != "" || request.URL.Query().Get("iid") != "" {
				t.Error("device registration leaked a prior device identity")
			}
			for _, name := range []string{"X-Khronos", "X-Gorgon", "X-Argus"} {
				if request.Header.Get(name) == "" {
					t.Errorf("device registration omitted %s", name)
				}
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

func TestHongguoTransientRegistrationFailureCanRetryImmediately(t *testing.T) {
	requests := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return nil, context.DeadlineExceeded
		}
		return sourceFixtureResponse(request, http.StatusOK, `{"device_id_str":"1001","install_id_str":"10019"}`), nil
	})
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "7", "8", false
	if err := d.ensureHongguoDevice(context.Background(), ""); err == nil {
		t.Fatal("first transient registration failure was hidden")
	}
	if err := d.ensureHongguoDevice(context.Background(), ""); err != nil {
		t.Fatalf("transient failure must retry immediately: %v", err)
	}
	if requests != 2 {
		t.Fatalf("registration requests = %d, want 2", requests)
	}
	if state := d.hongguoCatalogSnapshot(); !state.Registered || state.DeviceID != "1001" {
		t.Fatalf("retry did not register device: %#v", state)
	}
}

// Rate limits and upstream 5xx responses are transient, not evidence of a device ban.
func TestHongguoTemporaryHTTPRegistrationFailuresCanRetryWithoutDeviceCooldown(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			requests := 0
			d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
				requests++
				if requests == 1 {
					return sourceFixtureResponse(request, status, `{"message":"temporary upstream failure"}`), nil
				}
				return sourceFixtureResponse(request, http.StatusOK, `{"device_id_str":"1001","install_id_str":"10019"}`), nil
			})
			client := d.hongguoClient()
			client.state.DeviceID, client.state.InstallID, client.state.Registered = "7", "8", false
			if err := d.ensureHongguoDevice(context.Background(), ""); err == nil {
				t.Fatal("first temporary registration failure was hidden")
			}

			// HTTP 429 must keep the host-level Retry-After backoff, but must not
			// create the separate ten-minute device-rejection cooldown. Expire the
			// host backoff here to test the next eligible registration attempt
			// without sleeping for the server's full default one-minute window.
			if status == http.StatusTooManyRequests {
				client.deviceMu.Lock()
				paused := client.registerPausedUntil
				client.deviceMu.Unlock()
				if !paused.IsZero() {
					t.Fatalf("HTTP 429 incorrectly created device cooldown until %v", paused)
				}
				d.limiter.mu.Lock()
				backoff := d.limiter.backoffs["log.snssdk.com"]
				if backoff == nil || backoff.status != http.StatusTooManyRequests {
					d.limiter.mu.Unlock()
					t.Fatal("HTTP 429 Retry-After backoff was not preserved")
				}
				backoff.until = time.Now().Add(-time.Second)
				d.limiter.mu.Unlock()
			}

			if err := d.ensureHongguoDevice(context.Background(), ""); err != nil {
				t.Fatalf("temporary HTTP %d must be retryable after any host Retry-After: %v", status, err)
			}
			if requests != 2 {
				t.Fatalf("registration requests = %d, want 2", requests)
			}
		})
	}
}

func TestHongguoExplicitRegistrationRejectionIsPausedWithReason(t *testing.T) {
	requests := 0
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		requests++
		return sourceFixtureResponse(request, http.StatusForbidden, `{"message":"device blocked by policy"}`), nil
	})
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "7", "8", false
	if err := d.ensureHongguoDevice(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("missing explicit rejection reason: %v", err)
	}
	if err := d.ensureHongguoDevice(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "遭拒絕") {
		t.Fatalf("missing paused rejection message: %v", err)
	}
	if requests != 1 {
		t.Fatalf("rejected registration should be paused, requests=%d", requests)
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


func TestHongguoRegistrationClientLabelUsesActualRequestUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name string
		ua string
		want string
	}{
		{
			name: "legacy app profile",
			ua: "com.phoenix.read/74332 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3)",
			want: "Android 16 / Redmi 25053RT47C / App 7.4.3.32",
		},
		{
			name: "SDK current player profile",
			ua: "com.phoenix.read/73932 (Linux; U; Android 14; zh_CN; 23127PN0CC; Build/UKQ1.230804.001)",
			want: "Android 14 / Xiaomi 23127PN0CC / App 7.3.9.32",
		},
		{name: "unknown client", ua: "Mozilla/5.0", want: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, "https://log5-applog-lq.fqnovel.com/service/2/device_register/", nil)
			if err != nil { t.Fatal(err) }
			request.Header.Set("User-Agent", tc.ua)
			response := &http.Response{Request: request}
			if got := hongguoRegistrationClientLabel(response); got != tc.want {
				t.Fatalf("client label = %q, want %q", got, tc.want)
			}
		})
	}
}

type hongguoSDKFixtureRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip hongguoSDKFixtureRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestHongguoSDKRegistrationEdgeRejectionDoesNotRetryLegacyRegistrationHosts(t *testing.T) {
	var mu sync.Mutex
	var requestedHosts []string
	d := sourceFixtureDownloader(t, nil)
	// SDK requests use Downloader.client directly; replace the fixture's
	// prepared-catalog transport so registration and the web fallbacks share
	// one deterministic request recorder.
	d.client = &http.Client{Transport: hongguoSDKFixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		host := strings.ToLower(request.URL.Hostname())
		mu.Lock()
		requestedHosts = append(requestedHosts, host)
		mu.Unlock()

		switch host {
		case "log5-applog-lq.fqnovel.com":
			if request.Method != http.MethodPost || request.URL.Path != "/service/2/device_register/" {
				t.Errorf("unexpected current SDK registration request: %s %s", request.Method, request.URL)
			}
			return sourceFixtureResponse(request, 444, "Access Denied"), nil
		case "log.snssdk.com", "applog.snssdk.com":
			t.Errorf("legacy registration host must not be retried after SDK edge rejection: %s", host)
			return sourceFixtureResponse(request, 444, "Access Denied"), nil
		default:
			// Let non-registration quick/web fallbacks fail deterministically.
			return sourceFixtureResponse(request, http.StatusServiceUnavailable, "unavailable"), nil
		}
	})}
	t.Cleanup(d.client.CloseIdleConnections)

	_, err := d.resolveHongguoMedia(context.Background(), Task{
		DramaID: "hongguo:100",
		Chapter: Chapter{VideoURL: "hongguo-cenc://200"},
	})
	if err == nil {
		t.Fatal("expected SDK registration edge rejection and unavailable web fallbacks")
	}
	mu.Lock()
	hosts := append([]string(nil), requestedHosts...)
	mu.Unlock()
	if !containsString(hosts, "log5-applog-lq.fqnovel.com") {
		t.Fatalf("new SDK registration host was not used: %#v", hosts)
	}
	for _, host := range []string{"log.snssdk.com", "applog.snssdk.com"} {
		if containsString(hosts, host) {
			t.Fatalf("legacy registration host %s was requested after SDK rejection: %#v", host, hosts)
		}
	}
}

func TestHongguoPlaybackDoesNotCallRetiredThirdPartyAPI(t *testing.T) {
	var requested []string
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		requested = append(requested, request.URL.Host+request.URL.Path)
		switch request.URL.Host {
		case "novelquickapp.com":
			return sourceFixtureResponse(request, http.StatusServiceUnavailable, "unavailable"), nil
		case "api5-normal-sinfonlinea.fqnovel.com":
			return sourceFixtureResponse(request, http.StatusServiceUnavailable, "unavailable"), nil
		case "hongguoduanju.com":
			return sourceFixtureResponse(request, http.StatusServiceUnavailable, "unavailable"), nil
		case "djapi.999888456.xyz":
			t.Fatal("retired third-party playback API must not be called")
		}
		return sourceFixtureResponse(request, http.StatusServiceUnavailable, "unavailable"), nil
	})
	_, err := d.resolveHongguoMedia(context.Background(), Task{
		DramaID: "hongguo:100",
		Chapter: Chapter{VideoURL: "hongguo-cenc://200"},
	})
	if err == nil {
		t.Fatal("expected first-party route failures")
	}
	for _, host := range requested {
		if strings.Contains(host, "djapi.999888456.xyz") {
			t.Fatal("retired third-party API was requested")
		}
	}
	if !strings.Contains(err.Error(), "官網網頁") || strings.Contains(err.Error(), "最終備用取流") {
		t.Fatalf("unexpected user-facing error: %v", err)
	}
}
