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
	address, _ := info["main_url"].(string)
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" ||
		!strings.HasSuffix(strings.ToLower(u.Hostname()), ".qznovelvod.com") ||
		!isProviderHTTPMediaURL(address) {
		return providerMedia{}, errors.New("官網未提供有效的公開影片網址")
	}
	quality, _ := strconv.Atoi(mapString(info, "height"))
	if quality < 0 || quality > 16384 || (quality > 0 && quality < 720) {
		return providerMedia{}, errors.New("官網公開影片畫質低於要求或無效")
	}
	seconds, _ := strconv.ParseFloat(mapString(info, "duration"), 64)
	var duration time.Duration
	if seconds > 0 && seconds <= 86400 {
		duration = time.Duration(seconds * float64(time.Second))
	}
	return providerMedia{URL: address, Referer: hongguoBaseURL+"/", Quality: quality, Duration: duration}, nil
}
