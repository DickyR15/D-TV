package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// resolveHongguoMultiModelMedia is a second, official App playback path. It is
// tried only when the current video_model/v1 path fails, keeping successful
// play starts fast while adding the multi_video_model + fallback_api route.
func (downloader *Downloader) resolveHongguoMultiModelMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("紅果備用播放 ID 無效")
	}
	payload := map[string]any{
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
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/multi_video_model/preload/v1", nil, payload)
	if err != nil {
		return providerMedia{}, err
	}
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
