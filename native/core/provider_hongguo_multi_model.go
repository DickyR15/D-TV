package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// resolveHongguoMultiModelMedia is a second, official App playback path. It is
// tried only when the current video_model/v1 path fails, keeping successful
// play starts fast while adding the multi_video_model + fallback_api route.
var errHongguoH266Only = errors.New("紅果目前只回傳 H.266（bytevc2）媒體")

func (downloader *Downloader) resolveHongguoMultiModelMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("紅果備用播放 ID 無效")
	}

	// Prefer the documented multi_video_model API and response shape used by
	// the upstream downloader. Keep the existing preload route as a fallback
	// because it may still be accepted by newer App versions.
	officialPayload := map[string]any{
		"biz_param": map[string]any{
			"detail_page_version": 0,
			"device_level": 3,
			"disable_digg_stat": false,
			"disable_video_relate_book": false,
			"need_all_video_definition": true,
			"need_mp4_align": false,
			"use_os_player": false,
			"use_server_dns": false,
			"video_platform": 1024,
		},
		"mixed_video_id_map": map[string]any{"1": []string{videoID}},
	}
	officialCtx, cancelOfficial := context.WithTimeout(ctx, 8*time.Second)
	officialResult, officialErr := downloader.hongguoAppRequest(
		officialCtx, http.MethodPost, "/novel/player/multi_video_model/v1/", nil, officialPayload,
	)
	cancelOfficial()
	if officialErr == nil {
		media, parseErr := downloader.resolveHongguoMultiModelResult(ctx, officialResult, videoID)
		if parseErr == nil {
			return media, nil
		}
		officialErr = parseErr
	}
	if err := ctx.Err(); err != nil {
		return providerMedia{}, err
	}

	preloadPayload := map[string]any{
		"biz_param": map[string]any{
			"detail_page_version": 0,
			"device_level": 3,
			"disable_digg_stat": false,
			"need_all_video_definition": true,
			"need_mp4_align": false,
			"use_os_player": false,
			"use_server_dns": false,
			"video_platform": 1024,
		},
		"dr_scene": "preload",
		"mixed_video_id_map": map[string]any{"1004": []string{videoID}},
	}
	preloadResult, preloadErr := downloader.hongguoAppRequest(
		ctx, http.MethodPost, "/novel/player/multi_video_model/preload/v1", nil, preloadPayload,
	)
	if preloadErr != nil {
		if officialErr != nil {
			return providerMedia{}, fmt.Errorf(
				"標準多影片模型取流失敗：%v；預載備用取流失敗：%w",
				publicError(officialErr), preloadErr,
			)
		}
		return providerMedia{}, preloadErr
	}
	media, preloadParseErr := downloader.resolveHongguoMultiModelResult(ctx, preloadResult, videoID)
	if preloadParseErr != nil && officialErr != nil {
		return providerMedia{}, fmt.Errorf(
			"標準多影片模型取流失敗：%v；預載備用取流失敗：%w",
			publicError(officialErr), preloadParseErr,
		)
	}
	return media, preloadParseErr
}


func (downloader *Downloader) resolveHongguoHelperMedia(ctx context.Context, seriesID, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(seriesID) || !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("紅果輔助設備劇集 ID 無效")
	}
	if err := downloader.ensureHongguoHelperDevice(ctx); err != nil {
		return providerMedia{}, err
	}
	payload := map[string]any{
		"video_id": videoID,
		"content_type": 1,
		"biz_param": map[string]any{"need_all_video_definition": true, "video_platform": 3},
	}
	result, err := downloader.hongguoHelperAppRequest(ctx, http.MethodPost, "/novel/player/video_model/v1/", nil, payload)
	if err == nil {
		model := hongguoVideoModelFromAppResult(result)
		media, parseErr := selectHongguoAppMedia(model)
		if parseErr == nil {
			return media, nil
		}
		err = parseErr
	}
	if ctx.Err() != nil {
		return providerMedia{}, ctx.Err()
	}
	multiPayload := map[string]any{
		"biz_param": map[string]any{
			"detail_page_version": 0,
			"device_level": 3,
			"disable_digg_stat": false,
			"disable_video_relate_book": false,
			"need_all_video_definition": true,
			"need_mp4_align": false,
			"use_os_player": false,
			"use_server_dns": false,
			"video_platform": 1024,
		},
		"mixed_video_id_map": map[string]any{"1": []string{videoID}},
	}
	multiResult, multiErr := downloader.hongguoHelperAppRequest(ctx, http.MethodPost, "/novel/player/multi_video_model/v1/", nil, multiPayload)
	if multiErr == nil {
		media, parseErr := downloader.resolveHongguoMultiModelResult(ctx, multiResult, videoID)
		if parseErr == nil {
			return media, nil
		}
		multiErr = parseErr
	}
	if errors.Is(err, errHongguoH266Only) && errors.Is(multiErr, errHongguoH266Only) {
		return providerMedia{}, errHongguoH266Only
	}
	return providerMedia{}, fmt.Errorf("紅果輔助設備取流失敗：主 App 路徑：%v；多影片模型：%w", publicError(err), multiErr)
}

func hongguoVideoModelFromAppResult(result map[string]any) map[string]any {
	data := nestedMap(result, "data")
	model, _ := data["video_model"].(map[string]any)
	if encoded, ok := data["video_model"].(string); ok && encoded != "" {
		if decoded, decodeErr := decodeHongguoBase64(encoded); decodeErr == nil && len(decoded) > 0 {
			encoded = string(decoded)
		}
		decoder := json.NewDecoder(strings.NewReader(encoded))
		decoder.UseNumber()
		_ = decoder.Decode(&model)
	}
	if len(model) == 0 && hongguoHasVideoList(data["video_list"]) {
		model = map[string]any{
			"video_list": data["video_list"],
			"video_duration": data["video_duration"],
			"duration": data["duration"],
		}
	}
	return model
}

func (downloader *Downloader) resolveHongguoMultiModelResult(
	ctx context.Context, result map[string]any, videoID string,
) (providerMedia, error) {
	model, err := hongguoMultiModelFromResponse(result, videoID)
	if err != nil {
		return providerMedia{}, err
	}
	if hongguoHasVideoList(model["video_list"]) {
		return selectHongguoAppMedia(model)
	}

	fallbackURL := hongguoFallbackAPI(model["fallback_api"])
	if fallbackURL == "" || !isProviderHTTPMediaURL(fallbackURL) {
		return providerMedia{}, errors.New("紅果備用播放模型沒有可用的 fallback_api")
	}
	body, err := downloader.fetchProviderText(ctx, fallbackURL, "https://novelquickapp.com/")
	if err != nil {
		return providerMedia{}, fmt.Errorf("取得紅果備用播放資訊失敗：%w", err)
	}
	var response map[string]any
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil || response == nil {
		return providerMedia{}, errors.New("紅果備用播放資訊格式異常")
	}
	return hongguoMediaFromFallbackResponse(response)
}

func hongguoMultiModelFromResponse(response map[string]any, videoID string) (map[string]any, error) {
	data := nestedMap(response, "data")
	entry := nestedMap(data, videoID)
	if len(entry) == 0 && (data["video_model"] != nil || data["video_list"] != nil || data["fallback_api"] != nil) {
		entry = data
	}
	if len(entry) == 0 {
		return nil, errors.New("紅果備用播放 API 沒有對應的集數")
	}
	value := entry["video_model"]
	if value == nil {
		value = entry
	}
	model := decodeHongguoVideoModel(value)
	if len(model) == 0 {
		return nil, errors.New("紅果備用播放模型格式異常")
	}
	return model, nil
}

func decodeHongguoVideoModel(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	raw, ok := value.(string)
	if !ok {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if !strings.HasPrefix(raw, "{") {
		if decoded, err := decodeHongguoBase64(raw); err == nil {
			candidate := strings.TrimSpace(string(decoded))
			if strings.HasPrefix(candidate, "{") {
				raw = candidate
			}
		}
	}
	var model map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&model) != nil {
		return nil
	}
	return model
}

func hongguoFallbackAPI(value any) string {
	switch current := value.(type) {
	case string:
		raw := strings.TrimSpace(current)
		if strings.HasPrefix(strings.ToLower(raw), "https://") || strings.HasPrefix(strings.ToLower(raw), "http://") {
			return raw
		}
		var decoded any
		if json.Unmarshal([]byte(raw), &decoded) == nil {
			return hongguoFallbackAPI(decoded)
		}
	case map[string]any:
		for _, key := range []string{"fallback_api", "url", "api"} {
			if url := hongguoFallbackAPI(current[key]); url != "" {
				return url
			}
		}
	case []any:
		for _, item := range current {
			if url := hongguoFallbackAPI(item); url != "" {
				return url
			}
		}
	}
	return ""
}

func hongguoHasVideoList(value any) bool {
	switch current := value.(type) {
	case []any:
		return len(current) > 0
	case map[string]any:
		return len(current) > 0
	}
	return false
}

func hongguoMediaFromFallbackResponse(response map[string]any) (providerMedia, error) {
	info := nestedMap(response, "video_info", "data")
	if len(info) == 0 {
		info = nestedMap(response, "data", "video_info", "data")
	}
	if len(info) == 0 && hongguoHasVideoList(response["video_list"]) {
		info = response
	}
	if len(info) == 0 {
		return providerMedia{}, errors.New("紅果備用播放資訊沒有 video_list")
	}
	return selectHongguoAppMedia(info)
}
