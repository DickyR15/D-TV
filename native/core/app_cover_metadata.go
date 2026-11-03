package core

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var coverMetaTag = regexp.MustCompile(`(?is)<meta\b[^>]*>`)

func (d *Downloader) nativeCoverAddress(ctx context.Context, drama nativeDrama) (string, error) {
	addresses, err := d.nativeCoverAddresses(ctx, drama)
	if err != nil || len(addresses) == 0 {
		return "", err
	}
	return addresses[0], nil
}

func nativeCoverAddressesFromDrama(drama Drama) []string {
	normalized := nativeNormalize(drama)
	addresses := append([]string{}, normalized.CoverCandidates...)
	if normalized.Cover != "" {
		addresses = append([]string{normalized.Cover}, addresses...)
	}
	return nativeUniqueCoverAddresses(addresses...)
}

func nativeLegacyCoverAddresses(values ...any) []string {
	var addresses []string
	for _, value := range values {
		for _, raw := range nativeCoverValueStrings(value) {
			if address := legacyCoverURL(raw); address != "" {
				addresses = append(addresses, address)
			}
		}
	}
	return nativeUniqueCoverAddresses(addresses...)
}

func (d *Downloader) nativeCoverAddresses(ctx context.Context, drama nativeDrama) ([]string, error) {
	source, id, valid := splitProviderDramaID(drama.ID)
	if !valid {
		return nil, errors.New("無效的劇集 ID")
	}
	switch source {
	case sourceHongguo:
		if !hongguoNumericID.MatchString(id) {
			return nil, errors.New("無效的紅果劇集 ID")
		}
		result, err := d.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]any{"series_id": id})
		if err != nil {
			return nil, err
		}
		row := nestedMap(result, "data", "video_data")
		if mapString(row, "series_id_str", "series_id") != id {
			return nil, errors.New("紅果詳情與請求劇集不符")
		}
		address := hongguoCoverAddress(mapString(row, "series_cover", "cover"))
		return nativeUniqueCoverAddresses(address), nil
	case sourceHuangdou:
		row, err := d.huangdouDetail(ctx, id)
		if err != nil {
			return nil, err
		}
		fresh := huangdouDramaFromMap(row)
		if nativeNormalize(fresh).ID != drama.ID {
			return nil, errors.New("黃豆詳情與請求劇集不符")
		}
		return nativeCoverAddressesFromDrama(fresh), nil
	case sourceHuangju:
		fresh, _, err := d.fetchHuangjuDetail(ctx, id)
		return nativeCoverAddressesFromDrama(fresh), err
	case sourceYeguo:
		fresh, _, err := d.fetchYeguoDetail(ctx, id)
		return nativeCoverAddressesFromDrama(fresh), err
	case sourceDSD:
		fresh, _, err := d.fetchDSDDetail(ctx, id)
		return nativeCoverAddressesFromDrama(fresh), err
	case sourceDuoduo:
		// Poster repair must not depend on finding a directly playable line.
		// A valid catalogue/detail poster should still be usable when the
		// aggregate playback service is unavailable or the title has no
		// supported direct media URL.
		fresh, _, _, _, err := d.fetchDuoduoDetailRow(ctx, id)
		return nativeCoverAddressesFromDrama(fresh), err
	case sourceHanxiaoquan:
		fresh, _, err := d.fetchHanxiaoquanDetail(ctx, id)
		return nativeCoverAddressesFromDrama(fresh), err
	case sourceCloudFront:
		if !rankingSourceID.MatchString(id) {
			return nil, errors.New("無效的黃果劇集 ID")
		}
		var raw Drama
		if err := d.fetchAPI(ctx, "/api/app/playlet/detail/"+url.PathEscape(id), nil, &raw); err != nil {
			return nil, err
		}
		if raw.ID != "" && raw.ID != id && raw.ID != drama.ID {
			return nil, errors.New("黃果詳情與請求劇集不符")
		}
		addresses := nativeLegacyCoverAddresses(
			raw.Cover, raw.CoverURL, raw.CoverURLSnake,
			raw.Image, raw.ImageURL, raw.ImageURLSnake,
			raw.Img, raw.Pic, raw.Picture, raw.Poster, raw.Thumb, raw.Thumbnail,
		)
		if len(addresses) == 0 {
			return nil, errors.New("詳情暫未返回海報地址")
		}
		return addresses, nil
	case sourceHuangguoAI, sourceHuangguoVideo:
		route := "/detail/" + url.PathEscape(id) + "/"
		if source == sourceHuangguoAI && !rankingSourceID.MatchString(id) {
			return nil, errors.New("無效的黃果劇集 ID")
		}
		if source == sourceHuangguoVideo {
			parts := strings.Split(id, "/")
			if len(parts) == 1 {
				parts = []string{"series", id}
			}
			if len(parts) != 2 || (parts[0] != "series" && parts[0] != "video") || !rankingSourceID.MatchString(parts[1]) {
				return nil, errors.New("無效的黃果劇集 ID")
			}
			route = "/" + parts[0] + "/" + url.PathEscape(parts[1])
		}
		pageURL := d.providerBaseURL(source) + route
		body, err := d.fetchProviderText(ctx, pageURL, d.providerBaseURL(source)+"/")
		if err != nil {
			return nil, err
		}
		fresh, err := parseHuangguoSortDetail(body, pageURL, Drama{ID: drama.ID, Source: source, SourceID: id})
		return nativeCoverAddressesFromDrama(fresh), err
	}
	return nil, errors.New("該站源暫無封面補齊介面")
}

func providerCoverAddress(value any, pageURL string) string {
	switch item := value.(type) {
	case []any:
		for _, value := range item {
			if address := providerCoverAddress(value, pageURL); address != "" {
				return address
			}
		}
	case map[string]any:
		for _, key := range []string{"url", "contentUrl", "thumbnailUrl"} {
			if address := providerCoverAddress(item[key], pageURL); address != "" {
				return address
			}
		}
	case string:
		if strings.TrimSpace(item) == "" {
			return ""
		}
		address, err := url.Parse(strings.TrimSpace(item))
		if err != nil {
			return ""
		}
		if base, err := url.Parse(pageURL); err == nil && base.IsAbs() {
			address = base.ResolveReference(address)
		}
		if validNativeCoverURL(address) {
			return address.String()
		}
	}
	return ""
}

func hongguoCoverAddress(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 8192 {
			continue
		}
		if strings.HasPrefix(value, "//") {
			value = "https:" + value
		}
		parsed, err := url.Parse(value)
		if err == nil && validNativeCoverURL(parsed) {
			return value
		}
	}
	return ""
}
