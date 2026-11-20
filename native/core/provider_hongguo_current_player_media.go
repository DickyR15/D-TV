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
	"time"
)

var errHongguoCurrentPlayerSoftReject = errors.New("紅果 Current Player HTTP 200 空回應（上游軟拒絕）")

func hongguoCurrentPlayerRequestBody(videoID string, level int) ([]byte, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return nil, errors.New("紅果 Current Player 集數 ID 無效")
	}
	deviceLevel := 3
	if level >= 4 { deviceLevel = 4 }
	payload := map[string]any{
		"video_id": videoID,
		"biz_param": map[string]any{
			"video_id_type": 0,
			"need_all_video_definition": true,
			"video_platform": 3,
			"device_level": deviceLevel,
		},
	}
	return json.Marshal(payload)
}

func (downloader *Downloader) resolveHongguoCurrentPlayerMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("紅果 Current Player 集數 ID 無效")
	}
	var lastErr error
	// The reference's live probe found that one freshly registered player
	// identity is accepted once. Rotate only after a confirmed empty 200,
	// with a six-second registration spacing to avoid upstream rate limits.
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil { return providerMedia{}, err }
		identity, err := downloader.registerHongguoCurrentPlayerIdentity(ctx)
		if err != nil {
			return providerMedia{}, fmt.Errorf("紅果獨立播放器設備註冊失敗：%w", err)
		}
		media, err := downloader.resolveHongguoCurrentPlayerWithIdentity(ctx, videoID, 3, identity)
		if err == nil { return media, nil }
		if errors.Is(err, errHongguoCurrentPlayerSoftReject) {
			lastErr = err
			downloader.recordDiagnostic(diagnosticEvent{
				Level: "warning", Event: "hongguo.current_player_soft_reject",
				Source: sourceHongguo, Message: fmt.Sprintf("attempt=%d deviceSuffix=%s action=rotate_identity",
					attempt+1, suffixHongguoID(identity.DeviceID, 4)),
			})
			continue
		}
		return providerMedia{}, err
	}
	if lastErr == nil { lastErr = errHongguoCurrentPlayerSoftReject }
	return providerMedia{}, fmt.Errorf("紅果 Current Player 已輪替 3 個獨立設備仍被空回應拒絕：%w", lastErr)
}

func suffixHongguoID(value string, count int) string {
	if len(value) <= count { return value }
	return value[len(value)-count:]
}

func (downloader *Downloader) resolveHongguoCurrentPlayerWithIdentity(
	ctx context.Context, videoID string, level int, identity hongguoCurrentPlayerIdentity,
) (providerMedia, error) {
	body, err := hongguoCurrentPlayerRequestBody(videoID, level)
	if err != nil { return providerMedia{}, err }
	now := time.Now()
	nowMillis := downloader.hongguoCurrentPlayerSigningMillis(ctx)
	pairs := hongguoCurrentPlayerPairs(identity)
	query, signedHeaders, err := signHongguoCurrentPlayer(pairs, nowMillis, identity.DeviceID, hongguoCurrentPlayerVersion, body, nil)
	if err != nil { return providerMedia{}, fmt.Errorf("紅果 Current Player 簽名失敗：%w", err) }
	requestURL := "https://" + hongguoCurrentPlayerHost + hongguoCurrentPlayerPath + "?" + query
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil { return providerMedia{}, err }
	request.Header.Set("User-Agent", hongguoCurrentPlayerUA)
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("x-ss-dp", "8662")
	request.Header.Set("lc", "101")
	for name, values := range signedHeaders {
		request.Header[name] = append([]string(nil), values...)
	}
	ticket := signedHeaders.Get("X-SS-Req-Ticket")
	request.Header.Set("x-reading-request", ticket+"-"+strconv.FormatInt(time.Now().UnixNano()&0x7fffffff, 10))

	response, requestErr := downloader.doPreparedCatalogRequest(request, 18*time.Second, nil)
	if requestErr != nil {
		return providerMedia{}, fmt.Errorf("紅果 Current Player 網路請求失敗：%w", requestErr)
	}
	content, readErr := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
	response.Body.Close()
	if readErr != nil { return providerMedia{}, fmt.Errorf("讀取紅果 Current Player 回應失敗：%w", readErr) }
	if len(content) > providerMaxBodyBytes { return providerMedia{}, errors.New("紅果 Current Player 回應超過大小限制") }
	if response.StatusCode != http.StatusOK {
		detail := hongguoRegistrationResponseDetails(response, content)
		if detail != "" {
			return providerMedia{}, fmt.Errorf("紅果 Current Player HTTP %d（%s）", response.StatusCode, detail)
		}
		return providerMedia{}, fmt.Errorf("紅果 Current Player HTTP %d", response.StatusCode)
	}
	if len(content) == 0 { return providerMedia{}, errHongguoCurrentPlayerSoftReject }

	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil || result == nil {
		return providerMedia{}, errors.New("紅果 Current Player 回應 JSON 格式異常")
	}
	if code := mapString(result, "code"); code != "" && code != "0" {
		return providerMedia{}, fmt.Errorf("紅果 Current Player code=%s message=%s", code, mapString(result, "message"))
	}
	if code := mapString(nestedMap(result, "BaseResp"), "StatusCode"); code != "" && code != "0" {
		return providerMedia{}, fmt.Errorf("紅果 Current Player BaseResp=%s", code)
	}
	model := hongguoVideoModelFromAppResult(result)
	if len(model) == 0 {
		return providerMedia{}, errors.New("紅果 Current Player 沒有返回 video_model")
	}
	media, err := selectHongguoAppMedia(model)
	if err != nil { return providerMedia{}, err }
	downloader.recordDiagnostic(diagnosticEvent{
		Level: "info", Event: "hongguo.current_player_success",
		Source: sourceHongguo,
		Message: fmt.Sprintf("level=%d streams=%d deviceSuffix=%s", level, len(media.Variants)+1, suffixHongguoID(identity.DeviceID, 4)),
	})
	return media, nil
}

// Test-only helper used to verify the registered player profile without
// issuing a live upstream request.
func hongguoCurrentPlayerRequestURL(identity hongguoCurrentPlayerIdentity, nowMillis int64, body []byte) (string, http.Header, error) {
	query, headers, err := signHongguoCurrentPlayer(hongguoCurrentPlayerPairs(identity), nowMillis, identity.DeviceID, hongguoCurrentPlayerVersion, body, nil)
	if err != nil { return "", nil, err }
	return "https://" + hongguoCurrentPlayerHost + hongguoCurrentPlayerPath + "?" + query, headers, nil
}

// Keep the function's exact response parsing behavior consistent with the
// existing App player fallback, including JSON-string video_model payloads.
func hongguoCurrentPlayerDecodeResponse(content []byte) (map[string]any, error) {
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil || result == nil {
		return nil, errors.New("紅果 Current Player 回應 JSON 格式異常")
	}
	return result, nil
}
