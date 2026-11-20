package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type hongguoRegisterRejection struct{ error }
type hongguoHelperDeviceKey struct{}

const (
	hongguoDeviceRegisterURL   = "https://log.snssdk.com/service/2/device_register/"
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
			return fmt.Errorf("紅果設備註冊暫不可用：%w", client.registerPauseErr)
		}
		return errors.New("紅果設備註冊暫不可用，請檢查網路設定後重試")
	}
	deviceID, installID, err := downloader.registerHongguoDevice(ctx)
	if err != nil {
		var rejection hongguoRegisterRejection
		if ctx.Err() == nil && errors.As(err, &rejection) {
			client.registerPausedUntil = time.Now().Add(hongguoDeviceRegisterPause)
			client.registerPauseErr = fmt.Errorf("紅果設備註冊遭拒絕：%w", err)
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

func (downloader *Downloader) registerHongguoDevice(ctx context.Context) (string, string, error) {
	header := hongguoRegistrationHeader{
		DisplayName: "红果免费短剧", Aid: 8662, AppVersion: "7.3.5.32", VersionCode: 73532,
		ManifestVersionCode: 73532, UpdateVersionCode: 73532, Channel: "update_64", Package: "com.phoenix.read",
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
	query := hongguoAppQuery("", "")
	query.Del("device_id")
	query.Del("iid")
	query.Set("req_id", uuidLike())
	registerURL := downloader.providerCompatibilityEndpointURL(sourceHongguo, "deviceRegister", hongguoDeviceRegisterURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, registerURL, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	request.Header.Set("User-Agent", hongguoAppUserAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	// GuoApp app-v2.0.0-112 executes module host.fetch through the shared
	// Host HTTP client. Keep registration on that same route rather than
	// introducing a separate DNS transport just for log.snssdk.com.
	response, err := downloader.doPreparedCatalogRequest(request, 20*time.Second, func(prepared *http.Request) {
		query.Set("_rticket", strconv.FormatInt(time.Now().UnixMilli(), 10))
		prepared.URL.RawQuery = query.Encode()
	})
	if err != nil {
		return "", "", err
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	response.Body.Close()
	if err != nil {
		return "", "", err
	}
	if response.StatusCode != http.StatusOK {
		statusErr := fmt.Errorf("紅果設備註冊 HTTP %d", response.StatusCode)
		// 只有明確的客戶端拒絕才進入 10 分鐘冷卻；逾時、限流及伺服器錯誤都應允許下一次請求立即重試。
		if response.StatusCode == http.StatusUnauthorized ||
			response.StatusCode == http.StatusForbidden ||
			(response.StatusCode >= 400 && response.StatusCode < 500 &&
				response.StatusCode != http.StatusRequestTimeout &&
				response.StatusCode != http.StatusTooEarly &&
				response.StatusCode != http.StatusTooManyRequests &&
				response.StatusCode != 444) {
			return "", "", hongguoRegisterRejection{statusErr}
		}
		return "", "", statusErr
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return "", "", hongguoRegisterRejection{errors.New("紅果裝置註冊回傳格式異常")}
	}
	deviceID, installID := mapString(result, "device_id_str", "device_id"), mapString(result, "install_id_str", "install_id")
	if !hongguoNumericID.MatchString(deviceID) || !hongguoNumericID.MatchString(installID) || deviceID == "0" || installID == "0" {
		return "", "", hongguoRegisterRejection{errors.New("紅果裝置註冊未提供裝置編號")}
	}
	return deviceID, installID, nil
}
