package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (downloader *Downloader) resolveHongguoAppMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("紅果影片 ID 無效")
	}
	payload := map[string]any{
		"video_id": videoID, "content_type": 1,
		"biz_param": map[string]any{"need_all_video_definition": true, "video_platform": 3},
	}
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_model/v1/", nil, payload)
	if err != nil {
		return providerMedia{}, err
	}
	data := nestedMap(result, "data")
	model, _ := data["video_model"].(map[string]any)
	if encoded, ok := data["video_model"].(string); ok && encoded != "" {
		if decoded, err := decodeHongguoBase64(encoded); err == nil && len(decoded) > 0 {
			encoded = string(decoded)
		}
		decoder := json.NewDecoder(strings.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&model); err != nil {
			return providerMedia{}, errors.New("紅果 App 播放資訊格式異常")
		}
	}
	if len(model) == 0 {
		if rows := anyList(data["video_list"]); len(rows) > 0 {
			model = map[string]any{
				"video_list":     data["video_list"],
				"video_duration": data["video_duration"],
				"duration":       data["duration"],
			}
		}
	}
	return selectHongguoAppMedia(model)
}



// resolveHongguoV5Media uses the playback-specific V5 endpoint from
// guoapp-modules/hongguo v1.0.2. Catalog/detail API requests deliberately
// continue to use hongguoAppRequest's legacy signing mode.
type hongguoPlaybackModelCacheEntry struct {
	model map[string]any
	expiresAt time.Time
}

type hongguoPlaybackRoute struct {
	name string
	fallbackURL string
	single bool
}

var hongguoPlaybackRoutes = map[string]hongguoPlaybackRoute{
	"playbackPrimary": {
		name: "playbackPrimary",
		fallbackURL: "https://api5-normal-sinfonlineb.fqnovel.com/novel/player/multi_video_model/v1/",
	},
	"playbackReadingMulti": {
		name: "playbackReadingMulti",
		fallbackURL: "https://reading.snssdk.com/novel/player/multi_video_model/v1/",
	},
	"playbackReadingSingle": {
		name: "playbackReadingSingle",
		fallbackURL: "https://reading.snssdk.com/novel/player/video_model/v1/",
		single: true,
	},
	"playbackFqMulti": {
		name: "playbackFqMulti",
		fallbackURL: "https://api5-normal.fqnovel.com/novel/player/multi_video_model/v1/",
	},
	"playbackLegacy": {
		name: "playbackLegacy",
		fallbackURL: hongguoPlaybackBaseURL + hongguoPlaybackPath,
	},
}

func validHongguoPlaybackEndpoint(raw string) bool {
	address, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || address.Scheme != "https" || address.User != nil ||
		address.RawQuery != "" || address.Fragment != "" ||
		(address.Port() != "" && address.Port() != "443") {
		return false
	}
	host := strings.ToLower(address.Hostname())
	return host == "reading.snssdk.com" ||
		strings.HasSuffix(host, ".snssdk.com") ||
		host == "fqnovel.com" ||
		strings.HasSuffix(host, ".fqnovel.com")
}

func (downloader *Downloader) hongguoSignedPlaybackRequest(
	ctx context.Context, endpoint string, payload map[string]any,
) (map[string]any, error) {
	if !validHongguoPlaybackEndpoint(endpoint) {
		return nil, errors.New("紅果播放 API 網址不受信任")
	}
	if err := downloader.ensureHongguoDevice(ctx, ""); err != nil {
		return nil, fmt.Errorf("紅果設備初始化失敗：%w", err)
	}
	client := downloader.hongguoClient()
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		deviceID, installID, registered := client.device(false)
		if !registered || !hongguoNumericID.MatchString(deviceID) ||
			!hongguoNumericID.MatchString(installID) || deviceID == "0" || installID == "0" {
			return nil, errors.New("紅果播放設備尚未完成註冊")
		}
		values := hongguoPlaybackQuery(deviceID, installID)
		requestURL, headers, signErr := signHongguoPlaybackV5(endpoint, values, body, time.Now())
		if signErr != nil {
			return nil, signErr
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", hongguoPlaybackUserAgent)
		request.Header.Set("Accept", "application/json, application/x-protobuf")
		for key, items := range headers {
			request.Header[key] = append([]string(nil), items...)
		}
		response, err := downloader.doPreparedCatalogRequest(request, 4*time.Second, nil)
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
		response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(content) > providerMaxBodyBytes {
			return nil, errors.New("紅果播放 API 回應過大")
		}
		if response.StatusCode != http.StatusOK {
			statusErr := fmt.Errorf("紅果 V5 API HTTP %d", response.StatusCode)
			if attempt == 0 &&
				(response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) &&
				catalogResponseBlockReason(response, content) == "" {
				if rotateErr := downloader.ensureHongguoDeviceFor(ctx, false, deviceID); rotateErr == nil {
					continue
				}
			}
			return nil, statusErr
		}
		if len(content) == 0 {
			return nil, errors.New("紅果 V5 API 回傳空內容")
		}
		var result map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(content)))
		decoder.UseNumber()
		if err := decoder.Decode(&result); err != nil || result == nil {
			return nil, errors.New("紅果 V5 API 回應格式異常")
		}
		code := firstNonEmpty(mapString(result, "code", "Code", "status_code"))
		if code != "" && code != "0" {
			return nil, fmt.Errorf("紅果 V5 API 業務回應 %s", truncate(code, 24))
		}
		return result, nil
	}
	return nil, errors.New("紅果播放設備更新後仍遭拒絕")
}

func hongguoPlaybackPayload(videoID string, single bool) map[string]any {
	biz := map[string]any{
		"detail_page_version": 0,
		"device_level": 3,
		"disable_digg_stat": false,
		"disable_video_relate_book": false,
		"need_all_video_definition": true,
		"need_mp4_align": false,
		"use_os_player": false,
		"use_server_dns": false,
		"video_platform": 1024,
	}
	if single {
		return map[string]any{"video_id": videoID, "content_type": 1, "biz_param": biz}
	}
	return map[string]any{"biz_param": biz, "mixed_video_id_map": map[string]any{"1004": []string{videoID}}}
}

func hongguoPlaybackEpisodeEntry(result map[string]any, videoID string, single bool) map[string]any {
	data := nestedMap(result, "data")
	if single {
		return data
	}
	entry := nestedMap(data, videoID)
	if len(entry) == 0 {
		entry = nestedMap(nestedMap(data, "1004"), videoID)
	}
	return entry
}

func hongguoPlaybackModelExpiry(model, entry map[string]any, now time.Time) time.Time {
	var candidates []time.Time
	for _, value := range []any{
		entry["expire_time"], entry["url_expire"], model["expire_time"],
		model["url_expire"], model["video_expire_time"],
	} {
		number, err := strconv.ParseInt(nativeText(value), 10, 64)
		if err != nil || number < 1_000_000_000 {
			continue
		}
		if number < 1_000_000_000_000 {
			number *= 1000
		}
		if number > 0 && number < 8_640_000_000_000_000 {
			candidates = append(candidates, time.UnixMilli(number))
		}
	}
	expiry := now.Add(30 * time.Second)
	if len(candidates) > 0 {
		expiry = candidates[0]
		for _, candidate := range candidates[1:] {
			if candidate.Before(expiry) {
				expiry = candidate
			}
		}
	}
	maximum := now.Add(5 * time.Minute)
	if expiry.After(maximum) {
		expiry = maximum
	}
	return expiry
}

func (downloader *Downloader) cachedHongguoPlaybackModel(videoID string) (map[string]any, bool) {
	client := downloader.hongguoClient()
	deviceID, installID, _ := client.device(false)
	cacheKey := deviceID + ":" + installID + ":" + videoID
	downloader.hongguoPlaybackMu.Lock()
	defer downloader.hongguoPlaybackMu.Unlock()
	entry, found := downloader.hongguoPlaybackModels[cacheKey]
	if !found || !entry.expiresAt.After(time.Now().Add(5*time.Second)) {
		delete(downloader.hongguoPlaybackModels, cacheKey)
		return nil, false
	}
	return entry.model, true
}

func (downloader *Downloader) cacheHongguoPlaybackModel(videoID string, model map[string]any, expiresAt time.Time) {
	client := downloader.hongguoClient()
	deviceID, installID, _ := client.device(false)
	cacheKey := deviceID + ":" + installID + ":" + videoID
	downloader.hongguoPlaybackMu.Lock()
	defer downloader.hongguoPlaybackMu.Unlock()
	if downloader.hongguoPlaybackModels == nil {
		downloader.hongguoPlaybackModels = make(map[string]hongguoPlaybackModelCacheEntry)
	}
	for key, entry := range downloader.hongguoPlaybackModels {
		if !entry.expiresAt.After(time.Now()) {
			delete(downloader.hongguoPlaybackModels, key)
		}
	}
	if len(downloader.hongguoPlaybackModels) >= 8 {
		for key := range downloader.hongguoPlaybackModels {
			delete(downloader.hongguoPlaybackModels, key)
			break
		}
	}
	downloader.hongguoPlaybackModels[cacheKey] = hongguoPlaybackModelCacheEntry{
		model: model,
		expiresAt: expiresAt,
	}
}

func (downloader *Downloader) resolveHongguoV5Media(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("紅果 V5 播放 ID 無效")
	}
	if model, ok := downloader.cachedHongguoPlaybackModel(videoID); ok {
		media, err := selectHongguoAppMedia(model)
		if err == nil {
			return media, nil
		}
	}

	defaultOrder := []string{
		"playbackPrimary",
		"playbackReadingMulti",
		"playbackReadingSingle",
		"playbackFqMulti",
		"playbackLegacy",
	}
	order := downloader.providerCompatibilityPlaybackRouteOrder(sourceHongguo, defaultOrder)
	downloader.hongguoPlaybackMu.Lock()
	preferred := downloader.hongguoPlaybackPreferredRoute
	preferredUntil := downloader.hongguoPlaybackPreferredUntil
	downloader.hongguoPlaybackMu.Unlock()
	if preferred != "" && time.Now().Before(preferredUntil) {
		reordered := []string{preferred}
		for _, name := range order {
			if name != preferred {
				reordered = append(reordered, name)
			}
		}
		order = reordered
	}

	requestCtx, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()
	var failures []string
	for _, name := range order {
		route, ok := hongguoPlaybackRoutes[name]
		if !ok {
			continue
		}
		endpoint := downloader.providerCompatibilityEndpointURL(sourceHongguo, name, route.fallbackURL)
		if !validHongguoPlaybackEndpoint(endpoint) {
			failures = append(failures, name+": 不受信任的端點")
			continue
		}
		routeCtx, routeCancel := context.WithTimeout(requestCtx, 4*time.Second)
		result, err := downloader.hongguoSignedPlaybackRequest(routeCtx, endpoint, hongguoPlaybackPayload(videoID, route.single))
		routeCancel()
		if err != nil {
			failures = append(failures, name+": "+publicError(err).Error())
			if requestCtx.Err() != nil {
				break
			}
			continue
		}
		entry := hongguoPlaybackEpisodeEntry(result, videoID, route.single)
		if len(entry) == 0 {
			failures = append(failures, name+": 沒有指定集數")
			continue
		}
		model := decodeHongguoVideoModel(entry["video_model"])
		if len(model) == 0 && hongguoHasVideoList(entry["video_list"]) {
			model = entry
		}
		if len(model) == 0 {
			failures = append(failures, name+": 播放模型格式無效")
			continue
		}
		media, mediaErr := selectHongguoAppMedia(model)
		if mediaErr != nil {
			// Some official responses contain only fallback_api. Reuse the
			// existing bounded JSON parser and host validation for that route.
			fallbackResponse := map[string]any{"data": map[string]any{
				videoID: map[string]any{"video_model": model},
			}}
			media, mediaErr = downloader.resolveHongguoMultiModelResult(requestCtx, fallbackResponse, videoID)
		}
		if mediaErr != nil {
			failures = append(failures, name+": "+publicError(mediaErr).Error())
			continue
		}
		expiry := hongguoPlaybackModelExpiry(model, entry, time.Now())
		if expiry.After(time.Now().Add(5*time.Second)) {
			downloader.cacheHongguoPlaybackModel(videoID, model, expiry)
		}
		downloader.hongguoPlaybackMu.Lock()
		downloader.hongguoPlaybackPreferredRoute = name
		downloader.hongguoPlaybackPreferredUntil = time.Now().Add(5*time.Minute)
		downloader.hongguoPlaybackMu.Unlock()
		return media, nil
	}
	if ctx.Err() != nil {
		return providerMedia{}, ctx.Err()
	}
	if len(failures) == 0 {
		return providerMedia{}, errors.New("紅果目前沒有可用的播放 API 路線")
	}
	return providerMedia{}, fmt.Errorf("紅果 V5 多路線取流失敗：%s", strings.Join(failures, "；"))
}
func hongguoMediaShortEdgeQuality(width, height, definition int) int {
	// The visible resolution label is the shorter image edge. Prefer actual
	// dimensions over definition metadata, which may describe the long edge
	// for portrait video (for example, 720x1280 reported as "1280P").
	if width > 0 && height > 0 {
		return min(width, height)
	}
	if definition > 0 {
		return definition
	}
	if width > 0 {
		return width
	}
	if height > 0 {
		return height
	}
	return 0
}

func selectHongguoAppMedia(model map[string]any) (providerMedia, error) {
	variants := anyList(model["video_list"])
	if rows, ok := model["video_list"].(map[string]any); ok && len(variants) == 0 {
		keys := make([]string, 0, len(rows))
		for key := range rows {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			variants = append(variants, rows[key])
		}
	}
	duration, _ := strconv.ParseFloat(mapString(model, "video_duration", "duration"), 64)
	modelKeySeed := mapString(model, "key_seed")
	var keyErr error
	hasH266Variant := false
	type scoredMedia struct {
		media providerMedia
		score int
	}
	var choices []scoredMedia
	for _, row := range variants {
		variant, _ := row.(map[string]any)
		if len(variant) == 0 {
			continue
		}
		meta := nestedMap(variant, "video_meta")
		codec := strings.ToLower(mapString(variant, "codec_type", "codec"))
		if codec == "" {
			codec = strings.ToLower(mapString(meta, "codec_type", "codec"))
		}
		if strings.Contains(codec, "bytevc2") ||
			strings.Contains(strings.ToLower(mapString(variant, "gear_des_key")), "bytevc2") {
			hasH266Variant = true
			continue
		}
		addresses := hongguoMediaAddressesForModel(variant, firstNonEmpty(mapString(variant, "key_seed"), modelKeySeed))
		if len(addresses) == 0 {
			continue
		}
		media := providerMedia{
			Referer: "https://novelquickapp.com/",
			Duration: time.Duration(duration * float64(time.Second)),
		}
		encryption := nestedMap(variant, "encrypt_info")
		spade := firstNonEmpty(mapString(variant, "spade_a"), mapString(encryption, "spade_a"))
		encrypted := encryption["encrypt"] == true ||
			mapString(encryption, "encryption_method") == "cenc-aes-ctr" ||
			spade != ""
		if encrypted {
			if spade == "" {
				keyErr = errors.New("紅果加密影片缺少 spade_a")
				continue
			}
			key, err := hongguoSpadeAKey(spade)
			if err != nil {
				key, err = hongguoContentKey(spade)
			}
			if err != nil {
				keyErr = err
				continue
			}
			media.CENCKey = key
		}
		width, _ := strconv.Atoi(mapString(variant, "vwidth", "width"))
		height, _ := strconv.Atoi(mapString(variant, "vheight", "height"))
		if width <= 0 {
			width, _ = strconv.Atoi(mapString(meta, "vwidth", "width"))
		}
		if height <= 0 {
			height, _ = strconv.Atoi(mapString(meta, "vheight", "height"))
		}
		definitionText := mapString(variant, "quality_desc", "definition")
		if definitionText == "" {
			definitionText = mapString(meta, "quality_desc", "definition")
		}
		definition, _ := strconv.Atoi(hongguoQualityNumber.FindString(definitionText))
		media.Quality = hongguoMediaShortEdgeQuality(width, height, definition)
		codecRank := 0
		switch codec {
		case "h264", "avc", "avc1":
			codecRank = 3
		case "hevc", "h265", "hvc1", "hev1", "bytevc1":
			codecRank = 2
		case "", "mp4", "mpeg":
			codecRank = 1
		default:
			codecRank = 0
		}
		score := media.Quality*100 + codecRank
		if codecRank == 0 {
			continue
		}
		for _, address := range addresses {
			candidate := media
			candidate.URL = address
			candidate.CENCKey = append([]byte(nil), media.CENCKey...)
			choices = append(choices, scoredMedia{media: candidate, score: score})
		}
	}
	if len(choices) > 0 {
		sort.SliceStable(choices, func(i, j int) bool { return choices[i].score > choices[j].score })
		selected := choices[0].media
		for _, choice := range choices {
			selected.Variants = append(selected.Variants, choice.media)
		}
		return selected, nil
	}
	if keyErr != nil {
		return providerMedia{}, fmt.Errorf("紅果 App 媒體金鑰無法使用：%w", keyErr)
	}
	if hasH266Variant {
		return providerMedia{}, errHongguoH266Only
	}
	return providerMedia{}, errors.New("紅果 App 未提供相容的媒體，已略過不支援的編碼")
}

func hongguoMediaAddressesForModel(info map[string]any, keySeed string) []string {
	var addresses []string
	seen := map[string]bool{}
	var add func(any)
	add = func(value any) {
		switch current := value.(type) {
		case string:
			raw := strings.TrimSpace(current)
			if raw == "" || len(raw) > 32<<10 {
				return
			}
			address, err := hongguoDecryptVideoURL(raw, keySeed)
			if err != nil {
				// Some older responses Base64-wrap a public URL without using
				// the encrypted spade URL envelope.
				if decoded, decodeErr := decodeHongguoFlexibleBase64(raw); decodeErr == nil {
					candidate := strings.TrimSpace(string(decoded))
					if parsed, parseErr := url.Parse(candidate); parseErr == nil &&
						parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil {
						address = candidate
					}
				}
			}
			if address != "" && !seen[address] {
				if parsed, parseErr := url.Parse(address); parseErr == nil &&
					parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil {
					seen[address] = true
					addresses = append(addresses, address)
				}
			}
		case []any:
			for _, item := range current {
				add(item)
			}
		}
	}
	for _, key := range []string{
		"main_url", "backup_url", "backup_url_1", "backup_url_2", "backup_url_3",
		"backup_url_list", "backup_urls", "url_list",
	} {
		add(info[key])
	}
	return addresses
}

func hongguoMediaAddresses(info map[string]any) []string {
	var addresses []string
	seen := map[string]bool{}
	var add func(any)
	add = func(value any) {
		switch value := value.(type) {
		case string:
			address := strings.TrimSpace(value)
			if len(address) > 8192 {
				return
			}
			if !isProviderHTTPMediaURL(address) {
				decoded, err := decodeHongguoBase64(address)
				if err != nil {
					return
				}
				address = strings.TrimSpace(string(decoded))
			}
			if isProviderHTTPMediaURL(address) && !seen[address] {
				seen[address] = true
				addresses = append(addresses, address)
			}
		case []any:
			for _, item := range value {
				add(item)
			}
		}
	}
	for _, key := range []string{"main_url", "backup_url", "backup_url_1", "backup_url_2", "backup_urls", "url_list"} {
		add(info[key])
	}
	return addresses
}
