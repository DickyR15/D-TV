package core

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Independently check the official website's explicit public episode scope.
// Never substitute another episode or request protected media.
func (d *Downloader) resolveHongguoPublicWebEpisode(ctx context.Context, seriesID, videoID string) (providerMedia, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	body, err := d.fetchProviderText(ctx, hongguoBaseURL+"/detail?series_id="+url.QueryEscape(seriesID), hongguoBaseURL+"/")
	if err != nil {
		return providerMedia{}, err
	}
	if !hongguoPublicWebEpisodeAllowed(body, seriesID, videoID) {
		return providerMedia{}, errors.New("官網未明確開放此集")
	}
	body, err = d.fetchProviderText(ctx, hongguoBaseURL+"/player/"+url.PathEscape(seriesID)+"/"+url.PathEscape(videoID), hongguoBaseURL+"/")
	if err != nil {
		return providerMedia{}, err
	}
	return hongguoPublicWebMediaFromPage(body, seriesID, videoID)
}

func hongguoPublicWebEpisodeAllowed(body, seriesID, videoID string) bool {
	if hongguoExactID(seriesID) == "" || hongguoExactID(videoID) == "" {
		return false
	}
	page := routerLoaderMap(parseRouterData(body), "detail_page", "detail_")
	if success, ok := page["isSuccess"].(bool); !ok || !success {
		return false
	}
	detail, _ := page["seriesDetail"].(map[string]any)
	if mapString(detail, "series_id") != seriesID {
		return false
	}
	vids := anyList(detail["vid_list"])
	count, err := strconv.Atoi(mapString(detail, "episode_cnt"))
	if err != nil || count < 1 || count != len(vids) {
		return false
	}
	accessible, err := strconv.Atoi(mapString(detail, "accessible_episode_cnt"))
	if err != nil || accessible < 1 || accessible > count {
		return false
	}
	seen := make(map[string]bool, count)
	allowed := false
	for i, raw := range vids {
		id := hongguoWebChapterID(raw)
		if id == "" || seen[id] {
			return false
		}
		seen[id] = true
		if id == videoID && i < accessible {
			allowed = true
		}
	}
	return allowed
}

func hongguoPublicWebMediaFromPage(body, seriesID, videoID string) (providerMedia, error) {
	page := routerLoaderMap(parseRouterData(body), "player_", "player_page")
	if mapString(page, "vid") != videoID || mapString(page, "series_id") != seriesID {
		return providerMedia{}, errors.New("官網回傳的劇集或分集不符")
	}
	info, _ := page["video_player_info"].(map[string]any)
	for _, key := range []string{"spade_a", "encrypt_info", "drm", "license_url", "decryptionKey", "cenc", "preview", "is_preview"} {
		if _, exists := info[key]; exists {
			return providerMedia{}, errors.New("官網未提供可確認的完整未加密影片")
		}
	}
	width, _ := strconv.Atoi(mapString(info, "width", "vwidth"))
	height, _ := strconv.Atoi(mapString(info, "height", "vheight"))
	quality := hongguoMediaShortEdgeQuality(width, height, 0)
	if width < 0 || width > 16384 || height < 0 || height > 16384 || (quality > 0 && quality < 720) {
		return providerMedia{}, errors.New("官網公開影片畫質低於要求或無效")
	}
	seconds, _ := strconv.ParseFloat(mapString(info, "duration"), 64)
	var duration time.Duration
	if seconds > 0 && seconds <= 86400 {
		duration = time.Duration(seconds * float64(time.Second))
	}
	// Only consider explicit, unencrypted URLs in this already validated
	// public episode response. Do not synthesize a CDN URL or change tokens.
	var variants []providerMedia
	seen := make(map[string]bool)
	add := func(raw any) {
		address, ok := raw.(string)
		if !ok {
			return
		}
		address = strings.TrimSpace(address)
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" ||
			(u.Port() != "" && u.Port() != "443") ||
			!strings.HasSuffix(strings.ToLower(u.Hostname()), ".qznovelvod.com") ||
			!isProviderHTTPMediaURL(address) || seen[address] {
			return
		}
		seen[address] = true
		variants = append(variants, providerMedia{URL: address, Referer: hongguoBaseURL + "/", Quality: quality, Duration: duration})
	}
	add(info["main_url"])
	// The primary must still be a valid public URL before accepting backups.
	if len(variants) == 0 {
		return providerMedia{}, errors.New("官網未提供有效的公開影片網址")
	}
	for _, key := range []string{"backup_url", "backup_url_1", "backup_url_2"} {
		add(info[key])
	}
	for _, key := range []string{"backup_url_list", "backup_urls"} {
		for _, raw := range anyList(info[key]) {
			if len(variants) >= 8 {
				break
			}
			add(raw)
		}
	}
	selected := variants[0]
	selected.Variants = variants
	return selected, nil
}
