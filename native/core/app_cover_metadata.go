package core

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var providerDoubanSubjectPattern = regexp.MustCompile(`(?i)(?:douban\.com/(?:movie/)?subject/|/(?:douban|subject)/)(\d{6,12})(?:\.[a-z0-9]{2,5})?(?:[/?#]|$)`)

func providerDoubanSubjectID(values ...string) string {
	for _, value := range values {
		for _, match := range providerDoubanSubjectPattern.FindAllStringSubmatch(value, -1) {
			if len(match) > 1 && validDoubanSubjectID(match[1]) {
				return match[1]
			}
		}
	}
	return ""
}

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

// nativeCoverRecoveryMetadata loads provider-native artwork and IDs in one
// detail request. Catalog pages may omit Douban IDs even when the detail API
// exposes them, so fallback recovery must use the authoritative detail row.
func (d *Downloader) nativeCoverRecoveryMetadata(ctx context.Context, drama *nativeDrama) ([]string, string, error) {
	if drama == nil {
		return nil, "", errors.New("海報資料無效")
	}
	source, id, valid := splitProviderDramaID(drama.ID)
	if !valid {
		return nil, drama.DoubanID, errors.New("無效的劇集 ID")
	}

	// The external matcher needs the same authoritative title, work type,
	// release year and provider identifiers that are available on each source's
	// detail page. Earlier implementations merged details for only a subset of providers;
	// other sources discarded the detail row after extracting image URLs, which
	// caused otherwise searchable titles to be rejected as ambiguous or unknown.
	var fresh Drama
	var err error
	switch source {
	case sourceHongguo:
		if !hongguoNumericID.MatchString(id) {
			return nil, drama.DoubanID, errors.New("無效的紅果劇集 ID")
		}
		result, requestErr := d.hongguoAppRequest(
			ctx, http.MethodPost, "/novel/player/video_detail/v1/", nil,
			map[string]any{"series_id": id},
		)
		if requestErr != nil {
			return nil, drama.DoubanID, requestErr
		}
		row := nestedMap(result, "data", "video_data")
		if mapString(row, "series_id_str", "series_id") != id {
			return nil, drama.DoubanID, errors.New("紅果詳情與請求劇集不符")
		}
		fresh = hongguoDramaFromAny(row, "短劇")
	case sourceHuangdou:
		var row map[string]any
		row, err = d.huangdouDetail(ctx, id)
		if err == nil {
			fresh = huangdouDramaFromMap(row)
		}
	case sourceHuangju:
		fresh, _, err = d.fetchHuangjuDetail(ctx, id)
	case sourceYeguo:
		fresh, _, err = d.fetchYeguoDetail(ctx, id)
	case sourceDSD:
		fresh, _, err = d.fetchDSDDetail(ctx, id)
	case sourceHanxiaoquan:
		fresh, _, err = d.fetchHanxiaoquanDetail(ctx, id)
	case sourceHeitao:
		fresh, _, err = d.fetchHeitaoDetail(ctx, id)
	case sourceDuoduo:
		fresh, _, _, _, err = d.fetchDuoduoDetailRow(ctx, id)
	case sourceCloudFront:
		if !rankingSourceID.MatchString(id) {
			return nil, drama.DoubanID, errors.New("無效的黃果劇集 ID")
		}
		var raw Drama
		err = d.fetchAPI(ctx, "/api/app/playlet/detail/"+url.PathEscape(id), nil, &raw)
		if err == nil {
			if raw.ID != "" && raw.ID != id && raw.ID != drama.ID {
				return nil, drama.DoubanID, errors.New("黃果詳情與請求劇集不符")
			}
			raw.ID, raw.Source, raw.SourceID = providerDramaID(source, id), source, id
			fresh = raw
		}
	case sourceHuangguoAI, sourceHuangguoVideo:
		route := "/detail/" + url.PathEscape(id) + "/"
		if source == sourceHuangguoAI && !rankingSourceID.MatchString(id) {
			return nil, drama.DoubanID, errors.New("無效的黃果劇集 ID")
		}
		if source == sourceHuangguoVideo {
			parts := strings.Split(id, "/")
			if len(parts) == 1 {
				parts = []string{"series", id}
			}
			if len(parts) != 2 || (parts[0] != "series" && parts[0] != "video") ||
				!rankingSourceID.MatchString(parts[1]) {
				return nil, drama.DoubanID, errors.New("無效的黃果劇集 ID")
			}
			route = "/" + parts[0] + "/" + url.PathEscape(parts[1])
		}
		pageURL := d.providerBaseURL(source) + route
		var body string
		body, err = d.fetchProviderText(ctx, pageURL, d.providerBaseURL(source)+"/")
		if err == nil {
			fresh, err = parseHuangguoSortDetail(
				body, pageURL,
				Drama{ID: drama.ID, Source: source, SourceID: id},
			)
		}
	default:
		addresses, fallbackErr := d.nativeCoverAddresses(ctx, *drama)
		return addresses, strings.TrimSpace(drama.DoubanID), fallbackErr
	}
	if err != nil {
		return nil, strings.TrimSpace(drama.DoubanID), err
	}
	if fresh.ID == "" {
		fresh.ID, fresh.Source, fresh.SourceID = providerDramaID(source, id), source, id
	}
	if normalized := nativeNormalize(fresh); normalized.ID != drama.ID {
		return nil, strings.TrimSpace(drama.DoubanID), errors.New("站源詳情與請求劇集不符")
	}
	addresses := nativeMergeCoverRecoveryMetadata(drama, fresh)
	return addresses, strings.TrimSpace(drama.DoubanID), nil
}

// nativeMergeCoverRecoveryMetadata propagates matching hints from a provider
// detail row to the same drama used by the external poster matcher. Without
// this, its release year and detail-only identifiers are silently discarded.
func nativeMergeCoverRecoveryMetadata(drama *nativeDrama, fresh Drama) []string {
	if drama == nil {
		return nil
	}
	normalized := nativeNormalize(fresh)
	if (drama.Title == "" || drama.Title == "短劇") && normalized.Title != "" {
		drama.Title = normalized.Title
	}
	freshDoubanID := strings.TrimSpace(fresh.DoubanID)
	if !validDoubanSubjectID(freshDoubanID) {
		freshDoubanID = strings.TrimSpace(normalized.DoubanID)
	}
	if !validDoubanSubjectID(freshDoubanID) {
		var freshCoverValues []string
		for _, value := range []any{fresh.Cover, fresh.CoverURL, fresh.CoverURLSnake, fresh.Image, fresh.ImageURL, fresh.ImageURLSnake} {
			freshCoverValues = append(freshCoverValues, nativeCoverValueStrings(value)...)
		}
		freshDoubanID = providerDoubanSubjectID(freshCoverValues...)
	}
	if !validDoubanSubjectID(drama.DoubanID) && validDoubanSubjectID(freshDoubanID) {
		drama.DoubanID = freshDoubanID
	}
	freshYear := firstNonEmpty(strings.TrimSpace(fresh.OnlineDate), strings.TrimSpace(normalized.OnlineDate))
	if freshYear != "" {
		drama.OnlineDate = providerReleaseDate(freshYear)
		if drama.OnlineDate == "" {
			drama.OnlineDate = freshYear
		}
	}
	if (drama.Category == "" || drama.Category == "影视" || drama.Category == "影視") && normalized.Category != "" {
		drama.Category = normalized.Category
	}
	seenTags := make(map[string]bool, len(drama.Tags)+len(normalized.Tags))
	for _, tag := range drama.Tags {
		seenTags[strings.ToLower(strings.TrimSpace(tag))] = true
	}
	for _, tag := range normalized.Tags {
		tag = strings.TrimSpace(tag)
		key := strings.ToLower(tag)
		if tag != "" && !seenTags[key] {
			drama.Tags = append(drama.Tags, tag)
			seenTags[key] = true
		}
	}
	if drama.Cover == "" {
		drama.Cover = normalized.Cover
	}
	drama.CoverCandidates = nativeUniqueCoverAddresses(append(
		drama.CoverCandidates, normalized.CoverCandidates...,
	)...)
	addresses := append([]string{}, drama.CoverCandidates...)
	if drama.Cover != "" {
		addresses = append([]string{drama.Cover}, addresses...)
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
