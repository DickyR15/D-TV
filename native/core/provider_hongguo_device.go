package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type hongguoRegisterRejection struct{ error }
type hongguoHelperDeviceKey struct{}

const (
	hongguoDeviceRegisterURL   = "https://log.snssdk.com/service/2/device_register/"
	hongguoDeviceRegisterPath  = "/service/2/device_register/"
	hongguoDeviceRotations     = 3
	hongguoDeviceRegisterPause = 10 * time.Minute
)

func (client *hongguoAppClient) deviceLocked(helper bool) (string, string, bool) {
	if helper {
		return client.state.HelperDeviceID, client.state.HelperInstallID, client.state.HelperRegistered
	}
	return client.state.DeviceID, client.state.InstallID, client.state.Registered
}

func (client *hongguoAppClient) device(helper bool) (string, string, bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.deviceLocked(helper)
}

func (downloader *Downloader) ensureHongguoDevice(ctx context.Context, rejected string) error {
	return downloader.ensureHongguoDeviceFor(ctx, false, rejected)
}

func (downloader *Downloader) ensureHongguoHelperDevice(ctx context.Context) error {
	return downloader.ensureHongguoDeviceFor(ctx, true, "")
}

func (downloader *Downloader) ensureHongguoDeviceFor(ctx context.Context, helper bool, rejected string) error {
	client := downloader.hongguoClient()
	settled := func() bool {
		current, install, registered := client.device(helper)
		valid := registered && hongguoNumericID.MatchString(current) && hongguoNumericID.MatchString(install) && current != "0" && install != "0"
		return valid && (rejected == "" || rejected != current)
	}
	if settled() {
		return nil
	}
	client.deviceMu.Lock()
	defer client.deviceMu.Unlock()
	if settled() {
		return nil
	}
	if time.Now().Before(client.registerPausedUntil) {
		if client.registerPauseErr != nil {
			return fmt.Errorf("紅果裝置註冊暫不可用：%w", client.registerPauseErr)
		}
		return errors.New("紅果裝置註冊暫不可用，請檢查網路設定後重試")
	}
	deviceID, installID, err := downloader.registerHongguoDevice(ctx)
	if err != nil {
		var rejection hongguoRegisterRejection
		if ctx.Err() == nil && errors.As(err, &rejection) {
			client.registerPausedUntil = time.Now().Add(hongguoDeviceRegisterPause)
			client.registerPauseErr = fmt.Errorf("紅果裝置註冊遭拒絕：%w", err)
			return client.registerPauseErr
		}
		client.registerPausedUntil = time.Time{}
		client.registerPauseErr = nil
		return err
	}
	client.registerPausedUntil = time.Time{}
	client.registerPauseErr = nil
	client.mu.Lock()
	if helper {
		client.state.HelperDeviceID, client.state.HelperInstallID, client.state.HelperRegistered = deviceID, installID, true
	} else {
		client.state.DeviceID, client.state.InstallID, client.state.Registered = deviceID, installID, true
	}
	client.mu.Unlock()
	return nil
}

type hongguoRegistrationHeader struct {
	DisplayName        string `json:"display_name"`
	Aid                int    `json:"aid"`
	AppVersion         string `json:"app_version"`
	VersionCode        int    `json:"version_code"`
	ManifestVersionCode int   `json:"manifest_version_code"`
	UpdateVersionCode  int    `json:"update_version_code"`
	Channel            string `json:"channel"`
	Package            string `json:"package"`
	AppLanguage        string `json:"app_language"`
	OS                 string `json:"os"`
	OSVersion          string `json:"os_version"`
	OSAPI              int    `json:"os_api"`
	DeviceModel        string `json:"device_model"`
	DeviceBrand        string `json:"device_brand"`
	DeviceManufacturer string `json:"device_manufacturer"`
	CPUABI             string `json:"cpu_abi"`
	DensityDPI         int    `json:"density_dpi"`
	DisplayDensity     string `json:"display_density"`
	Resolution         string `json:"resolution"`
	Language           string `json:"language"`
	Timezone           int    `json:"timezone"`
	Access             string `json:"access"`
	OpenUDID           string `json:"openudid"`
	ClientUDID         string `json:"clientudid"`
	CDID               string `json:"cdid"`
	Region             string `json:"region"`
	TimezoneName       string `json:"tz_name"`
	TimezoneOffset     int    `json:"tz_offset"`
	SimRegion          string `json:"sim_region"`
	DevicePlatform     string `json:"device_platform"`
	NotRequestSender   int    `json:"not_request_sender"`
	IsSystemApp        int    `json:"is_system_app"`
	SDKFlavor          string `json:"sdk_flavor"`
	ROM                string `json:"rom"`
	ROMVersion         string `json:"rom_version"`
}

type hongguoRegistrationPayload struct {
	MagicTag string `json:"magic_tag"`
	Header   hongguoRegistrationHeader `json:"header"`
	GenTime  int64  `json:"_gen_time"`
}

var hongguoRegistrationHTMLTags = regexp.MustCompile(`(?s)<[^>]*>`)

func hongguoDeviceRegisterHosts() []string {
	// The working reference registers against the legacy log hostname. Keep
	// applog as a single bounded fallback for edge-route differences.
	return []string{"log.snssdk.com", "applog.snssdk.com"}
}

func hongguoDeviceRegisterURLForHost(host string) string {
	return "https://" + host + hongguoDeviceRegisterPath
}

func hongguoRegistrationBodySummary(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&payload) == nil && payload != nil {
		var hints []string
		for _, key := range []string{"message", "msg", "error", "detail", "error_description", "code", "error_code"} {
			value, ok := payload[key]
			if !ok || value == nil {
				continue
			}
			valueText := strings.TrimSpace(fmt.Sprint(value))
			if valueText != "" && valueText != "<nil>" {
				hints = append(hints, key+"="+valueText)
			}
		}
		if len(hints) > 0 {
			return truncate(redactErrorString(strings.Join(hints, " ")), 180)
		}
	}
	text := html.UnescapeString(string(body))
	text = hongguoRegistrationHTMLTags.ReplaceAllString(text, " ")
	return truncate(redactErrorString(strings.Join(strings.Fields(text), " ")), 180)
}

func hongguoRegistrationResponseDetails(response *http.Response, body []byte) string {
	if response == nil {
		return ""
	}
	var details []string
	for _, header := range []string{"Server", "Via", "Content-Type", "Retry-After", "X-Tt-Logid", "X-Request-Id"} {
		value := strings.Join(strings.Fields(response.Header.Get(header)), " ")
		if value != "" {
			details = append(details, strings.ToLower(header)+"="+truncate(value, 96))
		}
	}
	if location := strings.TrimSpace(response.Header.Get("Location")); location != "" {
		if parsed, err := url.Parse(location); err == nil && parsed.Hostname() != "" {
			details = append(details, "redirectHost="+strings.ToLower(parsed.Hostname()))
		}
	}
	if hint := hongguoRegistrationBodySummary(body); hint != "" {
		details = append(details, "body="+hint)
	}
	return strings.Join(details, "; ")
}

func hongguoRegistrationHTTPStage(status int) string {
	switch status {
	case http.StatusOK:
		return "http_200"
	case http.StatusUnauthorized:
		return "http_401_auth"
	case http.StatusForbidden:
		return "http_403_policy"
	case http.StatusTooManyRequests:
		return "http_429_rate_limit"
	case 444:
		return "http_444_edge_reject"
	default:
		if status >= 500 {
			return fmt.Sprintf("http_%d_server_error", status)
		}
		return fmt.Sprintf("http_%d", status)
	}
}

func hongguoRegistrationIsTLBNotFound(response *http.Response, body []byte) bool {
	if response == nil || response.StatusCode != http.StatusNotFound {
		return false
	}
	bodyHint := strings.ToLower(hongguoRegistrationBodySummary(body))
	server := strings.ToLower(response.Header.Get("Server"))
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	via := strings.ToLower(response.Header.Get("Via"))
	return strings.Contains(bodyHint, "tlb") &&
		(strings.Contains(server, "tengine") ||
			strings.Contains(contentType, "text/html") ||
			strings.Contains(via, "cache"))
}

func hongguoShouldTryAlternateRegistrationResponse(response *http.Response, body []byte) bool {
	if response == nil {
		return false
	}
	return response.StatusCode == 444 || hongguoRegistrationIsTLBNotFound(response, body)
}

func hongguoRegistrationTransportStage(err error) string {
	if err == nil {
		return "unknown"
	}
	message := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(message, "timeout"), strings.Contains(message, "timed out"):
		return "timeout"
	case strings.Contains(message, "dns"), strings.Contains(message, "no such host"), strings.Contains(message, "name resolution"):
		return "dns"
	case strings.Contains(message, "tls"), strings.Contains(message, "certificate"), strings.Contains(message, "x509"):
		return "tls"
	case strings.Contains(message, "connect"), strings.Contains(message, "connection"), strings.Contains(message, "連線"):
		return "connect"
	default:
		return "network"
	}
}

func (downloader *Downloader) recordHongguoRegistrationTransportFailure(host string, attempt int, elapsed time.Duration, err error) {
	if err == nil {
		return
	}
	downloader.recordDiagnostic(diagnosticEvent{
		Level: "warning", Event: "hongguo.device_registration_attempt",
		Source: sourceHongguo, Host: strings.ToLower(host), Attempt: attempt,
		Message: fmt.Sprintf(
			"stage=%s elapsedMs=%d error=%s",
			hongguoRegistrationTransportStage(err),
			elapsed.Milliseconds(),
			truncate(redactErrorString(err.Error()), 512),
		),
	})
}

func hongguoRegistrationClientLabel(response *http.Response) string {
	if response == nil || response.Request == nil {
		return "unknown"
	}
	ua := response.Request.UserAgent()
	switch {
	case strings.Contains(ua, "com.phoenix.read/74332") &&
		strings.Contains(ua, "Android 16") && strings.Contains(ua, "25053RT47C"):
		return "Android 16 / Redmi 25053RT47C / App 7.4.3.32"
	case strings.Contains(ua, "com.phoenix.read/73932") &&
		strings.Contains(ua, "Android 14") && strings.Contains(ua, "23127PN0CC"):
		return "Android 14 / Xiaomi 23127PN0CC / App 7.3.9.32"
	default:
		return "unknown"
	}
}

func (downloader *Downloader) recordHongguoRegistrationResponse(
	response *http.Response,
	body []byte,
	attempt int,
	elapsed time.Duration,
	stage string,
	successful bool,
) {
	if response == nil {
		return
	}
	host := "log.snssdk.com"
	if response.Request != nil && response.Request.URL != nil && response.Request.URL.Hostname() != "" {
		host = strings.ToLower(response.Request.URL.Hostname())
	}
	level := "warning"
	message := fmt.Sprintf("stage=%s elapsedMs=%d", stage, elapsed.Milliseconds())
	if successful {
		level = "info"
		message += " registration=success"
	} else if details := hongguoRegistrationResponseDetails(response, body); details != "" {
		message += "；" + details
	}
	downloader.recordDiagnostic(diagnosticEvent{
		Level: level, Event: "hongguo.device_registration", Source: sourceHongguo,
		Host: host, HTTPStatus: response.StatusCode, Attempt: attempt,
		Client: hongguoRegistrationClientLabel(response),
		CFRay: truncate(strings.TrimSpace(response.Header.Get("CF-Ray")), 128),
		ResponseType: truncate(strings.TrimSpace(response.Header.Get("Content-Type")), 128),
		Message: message,
	})
}

func hongguoShouldTryAlternateRegistrationHost(err error) bool {
	switch hongguoRegistrationTransportStage(err) {
	case "dns", "tls", "connect":
		return true
	default:
		return false
	}
}

func (downloader *Downloader) registerHongguoDevice(ctx context.Context) (string, string, error) {
	header := hongguoRegistrationHeader{
		DisplayName: "红果免费短剧", Aid: 8662, AppVersion: "7.4.3.32", VersionCode: 74332,
		ManifestVersionCode: 74332, UpdateVersionCode: 74332, Channel: "update_64", Package: "com.phoenix.read",
		AppLanguage: "zh", OS: "Android", OSVersion: "16", OSAPI: 36, DeviceModel: "25053RT47C",
		DeviceBrand: "Redmi", DeviceManufacturer: "Xiaomi", CPUABI: "arm64-v8a", DensityDPI: 520,
		DisplayDensity: "xxhdpi", Resolution: "2772x1280", Language: "zh", Timezone: 8, Access: "wifi",
		OpenUDID: randomHex(8), ClientUDID: uuidLike(), CDID: uuidLike(),
		Region: "CN", TimezoneName: "Asia/Shanghai", TimezoneOffset: 28800, SimRegion: "cn",
		DevicePlatform: "android", NotRequestSender: 0, IsSystemApp: 0, SDKFlavor: "china",
		ROM: "OS2.0", ROMVersion: "BP2A.250605.031.A3",
	}
	body, err := json.Marshal(hongguoRegistrationPayload{MagicTag: "ss_app_log", Header: header, GenTime: time.Now().UnixMilli()})
	if err != nil {
		return "", "", err
	}

	var failures []error
	hosts := hongguoDeviceRegisterHosts()
	for hostIndex, host := range hosts {
		attempt := hostIndex + 1
		started := time.Now()
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		query := hongguoAppQuery("", "")
		query.Del("device_id")
		query.Del("iid")
		query.Set("req_id", uuidLike())
		registerURL := hongguoDeviceRegisterURLForHost(host)
		if hostIndex == 0 {
			registerURL = downloader.providerCompatibilityEndpointURL(sourceHongguo, "deviceRegister", registerURL)
		}
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			registerURL,
			bytes.NewReader(body),
		)
		if err != nil {
			return "", "", err
		}
		request.Header.Set("User-Agent", hongguoAppUserAgent)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		var signatureErr error
		response, requestErr := downloader.doPreparedCatalogRequest(request, 20*time.Second, func(prepared *http.Request) {
			now := time.Now()
			query.Set("_rticket", strconv.FormatInt(now.UnixMilli(), 10))
			prepared.URL.RawQuery = query.Encode()
			signatureErr = signHongguoDeviceRegistration(prepared, body, now)
		})
		if signatureErr != nil {
			return "", "", fmt.Errorf("紅果裝置註冊簽名失敗：%w", signatureErr)
		}
		if requestErr != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			elapsed := time.Since(started)
			downloader.recordHongguoRegistrationTransportFailure(host, attempt, elapsed, requestErr)
			failures = append(failures, fmt.Errorf("%s: %w", host, requestErr))
			if hostIndex+1 < len(hosts) && hongguoShouldTryAlternateRegistrationHost(requestErr) {
				continue
			}
			return "", "", errors.Join(failures...)
		}

		content, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		if readErr != nil {
			readFailure := fmt.Errorf("讀取註冊回應失敗：%w", readErr)
			elapsed := time.Since(started)
			downloader.recordHongguoRegistrationTransportFailure(host, attempt, elapsed, readFailure)
			return "", "", errors.Join(append(failures, fmt.Errorf("%s: %w", host, readFailure))...)
		}
		if response.StatusCode != http.StatusOK {
			statusErr := fmt.Errorf("紅果裝置註冊 HTTP %d", response.StatusCode)
			elapsed := time.Since(started)
			stage := hongguoRegistrationHTTPStage(response.StatusCode)
			if hongguoRegistrationIsTLBNotFound(response, content) {
				stage = "http_404_tlb_edge_route"
			}
			downloader.recordHongguoRegistrationResponse(response, content, attempt, elapsed, stage, false)
			if details := hongguoRegistrationResponseDetails(response, content); details != "" {
				statusErr = fmt.Errorf("%w（%s）", statusErr, details)
			}
			failures = append(failures, fmt.Errorf("%s: %w", host, statusErr))

			// HTTP 444 and a Tengine/TLB HTML 404 can indicate a failed edge
			// route for this hostname. Try the known first-party alias once
			// with the same registration identity. Do not fail over arbitrary
			// application 404s or explicit 401/403 policy rejections.
			if hongguoShouldTryAlternateRegistrationResponse(response, content) && hostIndex+1 < len(hosts) {
				continue
			}
			if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden ||
				response.StatusCode == 444 || hongguoRegistrationIsTLBNotFound(response, content) {
				// Both eligible registration routes have now failed. Keep the
				// existing cooldown instead of starting another registration
				// batch for every background catalogue request.
				return "", "", hongguoRegisterRejection{errors.Join(failures...)}
			}
			return "", "", errors.Join(failures...)
		}

		var result map[string]any
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.UseNumber()
		if err := decoder.Decode(&result); err != nil {
			allFailures := fmt.Errorf("%s: 紅果裝置註冊回傳格式異常", host)
			elapsed := time.Since(started)
			downloader.recordHongguoRegistrationResponse(response, content, attempt, elapsed, "invalid_json", false)
			failures = append(failures, allFailures)
			if hostIndex+1 < len(hosts) {
				continue
			}
			return "", "", errors.Join(failures...)
		}
		deviceID := mapString(result, "device_id_str", "device_id")
		installID := mapString(result, "install_id_str", "install_id")
		if !hongguoNumericID.MatchString(deviceID) || !hongguoNumericID.MatchString(installID) || deviceID == "0" || installID == "0" {
			identityFailure := fmt.Errorf("%s: 紅果裝置註冊未提供有效裝置編號", host)
			elapsed := time.Since(started)
			downloader.recordHongguoRegistrationResponse(response, content, attempt, elapsed, "invalid_device_identity", false)
			failures = append(failures, identityFailure)
			if hostIndex+1 < len(hosts) {
				continue
			}
			return "", "", errors.Join(failures...)
		}
		downloader.recordHongguoRegistrationResponse(response, content, attempt, time.Since(started), "registered", true)
		return deviceID, installID, nil
	}
	return "", "", errors.Join(failures...)
}
