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
	"strings"
	"sync"
	"time"
)

const (
	hongguoCurrentPlayerHost = "api5-normal-sinfonlinea.fqnovel.com"
	hongguoCurrentPlayerPath = "/novel/player/video_model/v1/"
	hongguoCurrentPlayerVersion = "7.3.9.32"
	hongguoCurrentPlayerVersionCode = "73932"
	hongguoCurrentPlayerDeviceModel = "23127PN0CC"
	hongguoCurrentPlayerUA = "com.phoenix.read/73932 (Linux; U; Android 14; zh_CN; 23127PN0CC; Build/UKQ1.230804.001; Cronet/TTNetVersion:8d40f833 2026-03-03 QuicVersion:21ac1950 2025-11-18)"
	hongguoCurrentRegisterInterval = 6 * time.Second
)

type hongguoCurrentPlayerIdentity struct {
	DeviceID string
	InstallID string
	CDID string
	OpenUDID string
}

var hongguoCurrentPlayerRegisterState struct {
	sync.Mutex
	next time.Time
	blockedUntil time.Time
	blockedErr error
}

var hongguoCurrentPlayerClock struct {
	sync.Mutex
	offset time.Duration
	refreshAfter time.Time
}

func waitHongguoCurrentPlayerRegisterSlot(ctx context.Context) error {
	hongguoCurrentPlayerRegisterState.Lock()
	now := time.Now()
	if now.Before(hongguoCurrentPlayerRegisterState.blockedUntil) {
		err := hongguoCurrentPlayerRegisterState.blockedErr
		hongguoCurrentPlayerRegisterState.Unlock()
		if err != nil { return fmt.Errorf("紅果播放器註冊冷卻中：%w", err) }
		return errors.New("紅果播放器註冊冷卻中，請稍後再試")
	}
	wait := hongguoCurrentPlayerRegisterState.next.Sub(now)
	if wait < 0 { wait = 0 }
	if wait == 0 {
		hongguoCurrentPlayerRegisterState.next = now.Add(hongguoCurrentRegisterInterval)
	} else {
		hongguoCurrentPlayerRegisterState.next = hongguoCurrentPlayerRegisterState.next.Add(hongguoCurrentRegisterInterval)
	}
	hongguoCurrentPlayerRegisterState.Unlock()
	if wait == 0 { return nil }
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done(): return ctx.Err()
	case <-timer.C: return nil
	}
}

func hongguoCurrentPlayerRegisterQuery(cdid, openudid string) string {
	return "aid=8662&app_name=novelread&version_code=73932&version_name=7.3.9.32" +
		"&device_platform=android&os=android&ssmix=a&language=zh&channel=xiaomi_8662_64" +
		"&device_type=23127PN0CC&device_brand=Xiaomi&resolution=1200*2670&dpi=460" +
		"&host_abi=arm64-v8a&manifest_version_code=73932&update_version_code=73932" +
		"&ac=wifi&app_type=normal&cpu_support64=true&os_api=34&os_version=14" +
		"&cdid=" + url.QueryEscape(cdid) + "&openudid=" + url.QueryEscape(openudid)
}

func hongguoCurrentPlayerRegistrationBody(cdid, openudid string) ([]byte, error) {
	header := map[string]any{
		"display_name": "红果短剧",
		"aid": 8662,
		"channel": "xiaomi_8662_64",
		"package": "com.phoenix.read",
		"app_version": hongguoCurrentPlayerVersion,
		"version_code": 73932,
		"manifest_version_code": 73932,
		"update_version_code": 73932,
		"sdk_version": "3.9.5",
		"sdk_target_version": 34,
		"os": "Android",
		"os_version": "14",
		"os_api": 34,
		"device_model": hongguoCurrentPlayerDeviceModel,
		"device_brand": "Xiaomi",
		"device_manufacturer": "Xiaomi",
		"cpu_abi": "arm64-v8a",
		"release_build": "UKQ1.230804.001",
		"density_dpi": 460,
		"display_density": "xxhdpi",
		"resolution": "2670x1200",
		"language": "zh",
		"timezone": 8,
		"region": "CN",
		"tz_name": "Asia/Shanghai",
		"cdid": cdid,
		"openudid": openudid,
		"clientudid": uuidLike(),
		"google_aid": "",
		"req_id": uuidLike(),
	}
	// encoding/json sorts map keys recursively, matching the reference's
	// deterministic J.encodeSorted representation before the signature is made.
	return json.Marshal(map[string]any{
		"magic_tag": "ss_app_log",
		"header": header,
		"_gen_time": time.Now().Unix(),
	})
}

func (downloader *Downloader) registerHongguoCurrentPlayerIdentity(ctx context.Context) (hongguoCurrentPlayerIdentity, error) {
	if err := waitHongguoCurrentPlayerRegisterSlot(ctx); err != nil {
		return hongguoCurrentPlayerIdentity{}, err
	}
	cdid, openudid := uuidLike(), randomHex(8)
	body, err := hongguoCurrentPlayerRegistrationBody(cdid, openudid)
	if err != nil { return hongguoCurrentPlayerIdentity{}, err }

	var failures []error
	hosts := hongguoDeviceRegisterHosts()
	for index, host := range hosts {
		if err := ctx.Err(); err != nil { return hongguoCurrentPlayerIdentity{}, err }
		now := time.Now()
		query := hongguoCurrentPlayerRegisterQuery(cdid, openudid)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, hongguoDeviceRegisterURLForHost(host), bytes.NewReader(body))
		if err != nil { return hongguoCurrentPlayerIdentity{}, err }
		request.Header.Set("User-Agent", hongguoCurrentPlayerUA)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		request.Header.Set("Sdk-Version", "2")
		var signatureErr error
		response, requestErr := downloader.doPreparedCatalogRequest(request, 15*time.Second, func(prepared *http.Request) {
			signedAt := time.Now()
			queryWithTicket := query + "&_rticket=" + strconv.FormatInt(signedAt.UnixMilli(), 10)
			prepared.URL.RawQuery = queryWithTicket
			signatureErr = signHongguoDeviceRegistration(prepared, body, signedAt)
		})
		if signatureErr != nil { return hongguoCurrentPlayerIdentity{}, fmt.Errorf("紅果播放器註冊簽名失敗：%w", signatureErr) }
		if requestErr != nil {
			if ctx.Err() != nil { return hongguoCurrentPlayerIdentity{}, ctx.Err() }
			downloader.recordHongguoRegistrationTransportFailure(host, index+1, time.Since(now), requestErr)
			failures = append(failures, fmt.Errorf("%s: %w", host, requestErr))
			if index+1 < len(hosts) && hongguoShouldTryAlternateRegistrationHost(requestErr) { continue }
			return hongguoCurrentPlayerIdentity{}, errors.Join(failures...)
		}
		content, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		if readErr != nil {
			return hongguoCurrentPlayerIdentity{}, fmt.Errorf("%s: 讀取播放器註冊回應失敗：%w", host, readErr)
		}
		if response.StatusCode != http.StatusOK {
			stage := hongguoRegistrationHTTPStage(response.StatusCode)
			if hongguoRegistrationIsTLBNotFound(response, content) { stage = "http_404_tlb_edge_route" }
			downloader.recordHongguoRegistrationResponse(response, content, index+1, time.Since(now), stage, false)
			statusErr := fmt.Errorf("紅果播放器設備註冊 HTTP %d", response.StatusCode)
			if details := hongguoRegistrationResponseDetails(response, content); details != "" {
				statusErr = fmt.Errorf("%w（%s）", statusErr, details)
			}
			failures = append(failures, fmt.Errorf("%s: %w", host, statusErr))
			if hongguoShouldTryAlternateRegistrationResponse(response, content) && index+1 < len(hosts) { continue }
			err := errors.Join(failures...)
			if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden || response.StatusCode == 444 {
				hongguoCurrentPlayerRegisterState.Lock()
				hongguoCurrentPlayerRegisterState.blockedUntil = time.Now().Add(30*time.Second)
				hongguoCurrentPlayerRegisterState.blockedErr = err
				hongguoCurrentPlayerRegisterState.Unlock()
			}
			return hongguoCurrentPlayerIdentity{}, err
		}
		var result map[string]any
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.UseNumber()
		if err := decoder.Decode(&result); err != nil || result == nil {
			return hongguoCurrentPlayerIdentity{}, fmt.Errorf("%s: 紅果播放器設備註冊回應格式異常", host)
		}
		did := mapString(result, "device_id_str", "device_id")
		iid := mapString(result, "install_id_str", "install_id", "iid")
		if !hongguoNumericID.MatchString(did) || !hongguoNumericID.MatchString(iid) || did == "0" || iid == "0" {
			return hongguoCurrentPlayerIdentity{}, fmt.Errorf("%s: 紅果播放器設備註冊未返回有效設備身分", host)
		}
		downloader.recordHongguoRegistrationResponse(response, content, index+1, time.Since(now), "http_200", true)
		hongguoCurrentPlayerRegisterState.Lock()
		hongguoCurrentPlayerRegisterState.blockedUntil = time.Time{}
		hongguoCurrentPlayerRegisterState.blockedErr = nil
		hongguoCurrentPlayerRegisterState.Unlock()
		return hongguoCurrentPlayerIdentity{DeviceID: did, InstallID: iid, CDID: cdid, OpenUDID: openudid}, nil
	}
	return hongguoCurrentPlayerIdentity{}, errors.Join(failures...)
}

func (downloader *Downloader) hongguoCurrentPlayerSigningMillis(ctx context.Context) int64 {
	now := time.Now()
	hongguoCurrentPlayerClock.Lock()
	offset := hongguoCurrentPlayerClock.offset
	refresh := !now.Before(hongguoCurrentPlayerClock.refreshAfter)
	if refresh {
		hongguoCurrentPlayerClock.refreshAfter = now.Add(5*time.Minute)
	}
	hongguoCurrentPlayerClock.Unlock()
	if refresh {
		request, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://"+hongguoCurrentPlayerHost+hongguoCurrentPlayerPath, nil)
		if err == nil {
			request.Header.Set("Cache-Control", "no-cache")
			request.Header.Set("User-Agent", hongguoCurrentPlayerUA)
			started := time.Now()
			response, requestErr := downloader.doPreparedCatalogRequest(request, 5*time.Second, nil)
			if requestErr == nil && response != nil {
				if response.Body != nil { response.Body.Close() }
				received := time.Now()
				ageOK := response.Header.Get("Age") == "" || strings.TrimSpace(response.Header.Get("Age")) == "0"
				if (response.StatusCode >= 200 && response.StatusCode < 500) && ageOK {
					if serverTime, parseErr := http.ParseTime(response.Header.Get("Date")); parseErr == nil && received.Sub(started) <= 10*time.Second {
						midpoint := started.Add(received.Sub(started)/2)
						offset = serverTime.Add(500*time.Millisecond).Sub(midpoint)
						hongguoCurrentPlayerClock.Lock()
						hongguoCurrentPlayerClock.offset = offset
						hongguoCurrentPlayerClock.refreshAfter = received.Add(5*time.Minute)
						hongguoCurrentPlayerClock.Unlock()
					}
				}
			}
		}
	}
	return time.Now().Add(offset).UnixMilli()
}

func hongguoCurrentPlayerPairs(identity hongguoCurrentPlayerIdentity) []hongguoQueryPair {
	return []hongguoQueryPair{
		{"iid", identity.InstallID},
		{"device_id", identity.DeviceID},
		{"ac", "wifi"},
		{"channel", "xiaomi_8662_64"},
		{"aid", "8662"},
		{"app_name", "novelread"},
		{"cdid", identity.CDID},
		{"version_code", hongguoCurrentPlayerVersionCode},
		{"version_name", hongguoCurrentPlayerVersion},
		{"device_platform", "android"},
		{"os", "android"},
		{"ssmix", "a"},
		{"device_type", hongguoCurrentPlayerDeviceModel},
		{"device_brand", "Xiaomi"},
		{"language", "zh"},
		{"os_api", "34"},
		{"os_version", "14"},
		{"manifest_version_code", hongguoCurrentPlayerVersionCode},
		{"resolution", "1200*2670"},
		{"dpi", "460"},
		{"update_version_code", hongguoCurrentPlayerVersionCode},
		{"host_abi", "arm64-v8a"},
		{"dragon_device_type", "phone"},
		{"pv_player", hongguoCurrentPlayerVersionCode},
		{"compliance_status", "0"},
		{"need_personal_recommend", "1"},
		{"player_so_load", "1"},
		{"is_android_pad_screen", "0"},
		{"openudid", identity.OpenUDID},
	}
}
