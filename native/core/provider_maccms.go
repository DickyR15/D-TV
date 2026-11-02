package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
)

const (
	sourceMacCMSDytt  = "maccms-dytt"
	sourceMacCMSSubo  = "maccms-subo"
	sourceMacCMSLzi   = "maccms-lzi"
	sourceMacCMSFfzy  = "maccms-ffzy"
	sourceMacCMSDouban = "maccms-douban"
	sourceMacCMSZuida = "maccms-zuida"
)

type macCMSProvider struct {
	Name    string
	APIs    []string
	Referer string
}

var macCMSProviders = map[string]macCMSProvider{
	sourceMacCMSDytt: {
		Name:    "天堂資源",
		Referer: "https://dyttzy.tv/",
		APIs: []string{
			"https://caiji.dyttzyapi.com/api.php/provide/vod/from/dyttm3u8/at/json/",
			"https://caiji.dyttzyapi.com/api.php/provide/vod/at/json/",
			"http://caiji.dyttzyapi.com/api.php/provide/vod/",
		},
	},
	sourceMacCMSSubo: {
		Name:    "速播資源",
		Referer: "https://subocj.com/",
		APIs: []string{
			"https://subocj.com/api.php/provide/vod/from/subm3u8/at/json/",
			"https://subocj.com/api.php/provide/vod/at/json/",
		},
	},
	sourceMacCMSLzi: {
		Name:    "量子資源",
		Referer: "https://www.lzizy.net/",
		APIs: []string{
			"https://cj.lziapi.com/api.php/provide/vod/from/lzm3u8/at/json/",
			"https://cj.lzcaiji.com/api.php/provide/vod/from/lzm3u8/at/json/",
			"https://cj.lziapi.com/api.php/provide/vod/",
			"https://cj.lzcaiji.com/api.php/provide/vod/",
		},
	},
	sourceMacCMSFfzy: {
		Name:    "非凡資源",
		Referer: "https://cj.ffzyapi.com/",
		APIs: []string{
			"https://api.ffzyapi.com/api.php/provide/vod/from/ffm3u8/at/json/",
			"https://cj2.ffzyapi.com/api.php/provide/vod/from/ffm3u8/at/json/",
			"https://api.ffzyapi.com/api.php/provide/vod/at/json/",
			"http://api.ffzyapi.com/api.php/provide/vod/",
			"http://cj2.ffzyapi.com/api.php/provide/vod/",
			"https://cj.ffzyapi.com/api.php/provide/vod/",
		},
	},
	sourceMacCMSDouban: {
		Name:    "豆瓣資源",
		Referer: "https://dbzy.tv/",
		APIs: []string{
			"https://caiji.dbzy5.com/api.php/provide/vod/from/dbm3u8/at/josn/",
			"http://caiji.dbzy5.com/api.php/provide/vod/from/dbm3u8/at/josn/",
			"https://caiji.dbzy5.com/api.php/provide/vod/at/josn/",
			"http://caiji.dbzy5.com/api.php/provide/vod/at/josn/",
			"https://dbzy.tv/api.php/provide/vod/at/josn/",
		},
	},
	sourceMacCMSZuida: {
		Name:    "最大資源",
		Referer: "https://api.zuidapi.com/",
		APIs: []string{
			"https://api.zuidapi.com/api.php/provide/vod/",
			"http://api.zuidapi.com/api.php/provide/vod/",
		},
	},
}

func isMacCMSSource(source string) bool {
	_, ok := macCMSProviders[canonicalProviderSource(source)]
	return ok
}

func macCMSProviderBaseURL(source string) string {
	provider, ok := macCMSProviders[canonicalProviderSource(source)]
	if !ok {
		return ""
	}
	if len(provider.APIs) == 0 {
		return ""
	}
	parsed, err := url.Parse(provider.APIs[0])
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func (d *Downloader) macCMSReferer(source string) string {
	source = canonicalProviderSource(source)
	if provider, ok := macCMSProviders[source]; ok && provider.Referer != "" {
		return provider.Referer
	}
	d.providerMu.Lock()
	preferred := d.providerHosts[source]
	d.providerMu.Unlock()
	if preferred != "" {
		return strings.TrimRight(preferred, "/") + "/"
	}
	if base := macCMSProviderBaseURL(source); base != "" {
		return base + "/"
	}
	return ""
}

func macCMSValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func macCMSObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func macCMSRows(payload map[string]any) []map[string]any {
	for _, container := range []map[string]any{payload, macCMSObject(payload["data"])} {
		if container == nil {
			continue
		}
		for _, key := range []string{"list", "vod_list", "items"} {
			rows, ok := container[key].([]any)
			if !ok {
				continue
			}
			result := make([]map[string]any, 0, len(rows))
			for _, value := range rows {
				if row := macCMSObject(value); row != nil {
					result = append(result, row)
				}
			}
			if len(result) > 0 {
				return result
			}
		}
		if row := macCMSObject(container["vod"]); row != nil {
			return []map[string]any{row}
		}
	}
	return nil
}

func macCMSClasses(payload map[string]any) []map[string]any {
	for _, container := range []map[string]any{payload, macCMSObject(payload["data"])} {
		if container == nil {
			continue
		}
		for _, key := range []string{"class", "classes", "type_list"} {
			rows, ok := container[key].([]any)
			if !ok {
				continue
			}
			result := make([]map[string]any, 0, len(rows))
			for _, value := range rows {
				if row := macCMSObject(value); row != nil {
					result = append(result, row)
				}
			}
			if len(result) > 0 {
				return result
			}
		}
	}
	return nil
}

func macCMSInteger(payload map[string]any, keys ...string) int {
	for _, key := range keys {
		if value := macCMSValue(payload[key]); value != "" {
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
				return parsed
			}
		}
	}
	return 0
}

func (d *Downloader) fetchMacCMSJSON(ctx context.Context, source string, params url.Values) (map[string]any, error) {
	source = canonicalProviderSource(source)
	provider, ok := macCMSProviders[source]
	if !ok || len(provider.APIs) == 0 {
		return nil, errors.New("未知的 MacCMS 片源")
	}
	var lastErr error
	for _, api := range provider.APIs {
		endpoint, err := url.Parse(api)
		if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
			lastErr = errors.New("MacCMS API 位址無效")
			continue
		}
		query := endpoint.Query()
		for key, values := range params {
			query.Del(key)
			for _, value := range values {
				query.Add(key, value)
			}
		}
		endpoint.RawQuery = query.Encode()
		referer := provider.Referer
		if referer == "" {
			referer = endpoint.Scheme + "://" + endpoint.Host + "/"
		}
		body, requestErr := d.fetchProviderText(ctx, endpoint.String(), referer)
		if requestErr != nil {
			lastErr = requestErr
			continue
		}
		if len(body) > providerMaxBodyBytes {
			return nil, errors.New("MacCMS API 回應過大")
		}
		decoder := json.NewDecoder(strings.NewReader(body))
		decoder.UseNumber()
		var payload map[string]any
		if decodeErr := decoder.Decode(&payload); decodeErr != nil {
			lastErr = decodeErr
			continue
		}
		if payload == nil {
			lastErr = errors.New("API 沒有回傳 JSON 物件")
			continue
		}
		// Cache the working API host for subsequent URL/Referer handling.
		d.providerMu.Lock()
		if d.providerHosts == nil {
			d.providerHosts = map[string]string{}
		}
		d.providerHosts[source] = endpoint.Scheme + "://" + endpoint.Host
		d.providerMu.Unlock()
		return payload, nil
	}
	if lastErr == nil {
		lastErr = errors.New("沒有可用的 API 位址")
	}
	return nil, fmt.Errorf("%s API 請求失敗：%w", provider.Name, lastErr)
}

func macCMSDramaFromRow(source string, row map[string]any) Drama {
	provider, ok := macCMSProviders[canonicalProviderSource(source)]
	if !ok {
		return Drama{}
	}
	id := macCMSValue(row["vod_id"])
	name := html.UnescapeString(macCMSValue(row["vod_name"]))
	if !webProviderNumericID.MatchString(id) || name == "" {
		return Drama{}
	}
	cover := html.UnescapeString(macCMSValue(row["vod_pic"]))
	if strings.HasPrefix(cover, "//") {
		cover = "https:" + cover
	}
	description := html.UnescapeString(firstNonEmpty(
		macCMSValue(row["vod_content"]),
		macCMSValue(row["vod_blurb"]),
		macCMSValue(row["vod_en"]),
	))
	var tags []string
	for _, raw := range strings.FieldsFunc(macCMSValue(row["vod_class"]), func(r rune) bool {
		return r == ',' || r == '，' || r == '/'
	}) {
		if tag := strings.TrimSpace(raw); tag != "" && len(tags) < 16 {
			tags = append(tags, tag)
		}
	}
	return Drama{
		ID: providerDramaID(source, id), Source: source, SourceID: id,
		Title: truncate(name, 512), Name: truncate(name, 512),
		Desc: truncate(description, 12000), Intro: truncate(description, 12000),
		Cover: cover, CoverURL: cover, ChannelName: provider.Name,
		CategoryName: firstNonEmpty(macCMSValue(row["type_name"]), "影視"),
		Remark: truncate(macCMSValue(row["vod_remarks"]), 96),
		Score: macCMSValue(row["vod_score"]), Views: macCMSValue(row["vod_hits"]),
		Heat: macCMSValue(row["vod_hits"]), OnlineDate: macCMSValue(row["vod_year"]),
		TotalEpisode: macCMSValue(row["vod_total"]), EpisodeCount: macCMSValue(row["vod_continu"]),
		Tags: tags,
	}
}

func macCMSNormalizeMediaURL(raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if decoded, err := url.QueryUnescape(raw); err == nil && strings.HasPrefix(decoded, "http") {
		return decoded
	}
	return raw
}

func macCMSDirectMediaURL(raw string) bool {
	raw = macCMSNormalizeMediaURL(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return false
	}
	path := strings.ToLower(parsed.Path)
	return strings.HasSuffix(path, ".m3u8") || strings.HasSuffix(path, ".mp4") || strings.HasSuffix(path, ".m4v")
}

func macCMSPlayLineNames(raw string) []string {
	parts := strings.Split(raw, "$$$")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		names = append(names, strings.TrimSpace(part))
	}
	return names
}

func (d *Downloader) macCMSParseChapters(source, sourceID string, row map[string]any) []Chapter {
	names := macCMSPlayLineNames(macCMSValue(row["vod_play_from"]))
	blocks := strings.Split(macCMSValue(row["vod_play_url"]), "$$$")
	var chapters []Chapter
	for lineIndex, block := range blocks {
		lineName := fmt.Sprintf("線路 %d", lineIndex+1)
		if lineIndex < len(names) && names[lineIndex] != "" {
			lineName = names[lineIndex]
		}
		for episodeIndex, part := range strings.Split(block, "#") {
			title, rawURL, found := strings.Cut(strings.TrimSpace(part), "$")
			if !found {
				continue
			}
			title = strings.TrimSpace(title)
			rawURL = macCMSNormalizeMediaURL(rawURL)
			if !macCMSDirectMediaURL(rawURL) {
				continue
			}
			if title == "" {
				title = fmt.Sprintf("%s 第 %d 集", lineName, episodeIndex+1)
			}
			chapters = append(chapters, Chapter{
				ID: providerChapterID(source, sourceID, fmt.Sprintf("%d-%d", lineIndex+1, episodeIndex+1)),
				Source: source, Title: truncate(title, 256),
				VideoURL: rawURL, Referer: d.macCMSReferer(source),
				CurrentEpisode: json.RawMessage(strconv.Itoa(episodeIndex + 1)),
			})
		}
	}
	return chapters
}

func (d *Downloader) fetchMacCMSCatalogPage(ctx context.Context, source string, page int, category, query string) ([]Drama, bool, error) {
	params := url.Values{}
	params.Set("pg", strconv.Itoa(max(1, page)))
	if query != "" {
		params.Set("ac", "detail")
		params.Set("wd", query)
	} else {
		params.Set("ac", "list")
		if category != "" {
			params.Set("t", category)
		}
	}
	payload, err := d.fetchMacCMSJSON(ctx, source, params)
	if err != nil {
		return nil, false, err
	}
	rows := macCMSRows(payload)
	items := make([]Drama, 0, len(rows))
	for _, row := range rows {
		if drama := macCMSDramaFromRow(source, row); drama.ID != "" {
			items = append(items, drama)
		}
	}
	totalPages := macCMSInteger(payload, "pagecount", "page_count", "total_pages")
	if data := macCMSObject(payload["data"]); totalPages == 0 {
		totalPages = macCMSInteger(data, "pagecount", "page_count", "total_pages")
	}
	hasMore := totalPages > page
	if totalPages == 0 {
		hasMore = len(rows) >= 20
	}
	if len(items) == 0 && page == 1 {
		return nil, hasMore, fmt.Errorf("%s 目前沒有回傳可用影片資料", macCMSProviders[source].Name)
	}
	return items, hasMore, nil
}

func (d *Downloader) fetchMacCMSCategories(ctx context.Context, source string) ([]nativeCategory, error) {
	payload, err := d.fetchMacCMSJSON(ctx, source, url.Values{"ac": []string{"list"}, "pg": []string{"1"}})
	if err != nil {
		return nil, err
	}
	classes := macCMSClasses(payload)
	categories := make([]nativeCategory, 0, len(classes))
	seen := map[string]bool{}
	for _, row := range classes {
		id := macCMSValue(row["type_id"])
		name := html.UnescapeString(macCMSValue(row["type_name"]))
		if !webProviderNumericID.MatchString(id) || name == "" || seen[id] {
			continue
		}
		seen[id] = true
		categories = append(categories, nativeCategory{ID: id, Name: truncate(name, 48)})
	}
	return categories, nil
}

func (d *Downloader) fetchMacCMSDetail(ctx context.Context, source, sourceID string) (Drama, []Chapter, error) {
	if !webProviderNumericID.MatchString(sourceID) {
		return Drama{}, nil, errors.New("MacCMS 影片編號無效")
	}
	payload, err := d.fetchMacCMSJSON(ctx, source, url.Values{"ac": []string{"detail"}, "ids": []string{sourceID}})
	if err != nil {
		return Drama{}, nil, err
	}
	rows := macCMSRows(payload)
	if len(rows) == 0 {
		return Drama{}, nil, fmt.Errorf("%s 找不到這部影片的詳情", macCMSProviders[source].Name)
	}
	drama := macCMSDramaFromRow(source, rows[0])
	chapters := d.macCMSParseChapters(source, sourceID, rows[0])
	drama.EpisodesVerified = len(chapters) > 0
	if len(chapters) == 0 {
		return drama, nil, fmt.Errorf("%s 詳情已取得，但沒有可直接播放的 M3U8／MP4 分集", macCMSProviders[source].Name)
	}
	return drama, chapters, nil
}
