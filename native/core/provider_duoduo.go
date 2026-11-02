package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmlstd "html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const (
	duoduoPublicEntryURL = "https://duoduozhuiju.com/"
	duoduoClientID       = "8f3d2a1c7b6e5d4c9a0b1f2e3d4c5b6a"
	duoduoWebSign        = "ddtvf65f3a83d6d9ad6f"
	duoduoPageSize       = 15
)

var duoduoAPIHosts = []string{
	"323433ssdfd.top",
	"duoduosdf12223234334.top",
	"xds2435u23422342342u.top",
	"dduotv01.top",
}

type duoduoPlayEpisode struct {
	Title string
	URL   string
	Index int
}

type duoduoPlayLine struct {
	Key          string
	Name         string
	DecodeStatus string
	Sort         float64
	Episodes     []duoduoPlayEpisode
}

func duoduoUniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func duoduoString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func duoduoRows(value any) []map[string]any {
	rows, ok := value.([]any)
	if !ok {
		if maps, ok := value.([]map[string]any); ok {
			return maps
		}
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, item := range rows {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func duoduoStrings(value any) []string {
	var values []any
	switch typed := value.(type) {
	case []any:
		values = typed
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case string:
		if typed = strings.TrimSpace(typed); typed != "" {
			return []string{typed}
		}
		return nil
	default:
		return nil
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		if text := duoduoString(item); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func duoduoDirectMediaURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	path := strings.ToLower(parsed.Path)
	return strings.HasSuffix(path, ".m3u8") || strings.HasSuffix(path, ".mp4")
}

func duoduoSplitPlayNames(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "$$$")
	if len(parts) == 1 {
		parts = strings.Split(raw, ",")
	}
	var out []string
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func duoduoParseEpisodeBlock(raw string, onlyDirect bool) []duoduoPlayEpisode {
	var episodes []duoduoPlayEpisode
	for index, part := range strings.Split(raw, "#") {
		title, address, found := strings.Cut(strings.TrimSpace(part), "$")
		title, address = strings.TrimSpace(title), strings.TrimSpace(address)
		if !found || title == "" || address == "" || onlyDirect && !duoduoDirectMediaURL(address) {
			continue
		}
		episodes = append(episodes, duoduoPlayEpisode{Title: title, URL: address, Index: index + 1})
	}
	return episodes
}

func duoduoDecodeStatus(value any) string {
	status := duoduoString(value)
	if status == "" {
		return "0"
	}
	switch status {
	case "0", "1", "2":
		return status
	default:
		return "1"
	}
}

func duoduoParseAggregateLines(rows []map[string]any) []duoduoPlayLine {
	lines := make([]duoduoPlayLine, 0, len(rows))
	for index, row := range rows {
		status := duoduoDecodeStatus(row["decode_status"])
		// This app integration uses only direct media lines. Lines requiring
		// the website's private player decoder or account/VIP access are skipped.
		if status != "0" {
			continue
		}
		raw := duoduoString(row["vod_play_url"])
		episodes := duoduoParseEpisodeBlock(raw, true)
		if len(episodes) == 0 {
			continue
		}
		key := firstNonEmpty(duoduoString(row["site_key"]), duoduoString(row["vod_play_from"]), fmt.Sprintf("external_%d", index))
		name := firstNonEmpty(duoduoString(row["site_name"]), key)
		sortValue, _ := strconv.ParseFloat(duoduoString(row["sort"]), 64)
		lines = append(lines, duoduoPlayLine{Key: key, Name: name, DecodeStatus: status, Sort: sortValue, Episodes: episodes})
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Sort < lines[j].Sort })
	return lines
}

func duoduoParseDetailLines(row map[string]any, playerRows []map[string]any) []duoduoPlayLine {
	names := duoduoSplitPlayNames(duoduoString(row["vod_play_from"]))
	blocks := strings.Split(duoduoString(row["vod_play_url"]), "$$$")
	playerByFrom := make(map[string]map[string]any, len(playerRows))
	for _, player := range playerRows {
		if key := duoduoString(player["from"]); key != "" {
			playerByFrom[key] = player
		}
	}
	lines := make([]duoduoPlayLine, 0, min(len(names), len(blocks)))
	for index := 0; index < min(len(names), len(blocks)); index++ {
		name := names[index]
		player := playerByFrom[name]
		status := duoduoDecodeStatus(player["decode_status"])
		if status != "0" {
			continue
		}
		episodes := duoduoParseEpisodeBlock(blocks[index], true)
		if len(episodes) == 0 {
			continue
		}
		sortValue, _ := strconv.ParseFloat(duoduoString(player["sort"]), 64)
		lines = append(lines, duoduoPlayLine{
			Key: name, Name: firstNonEmpty(duoduoString(player["show"]), name),
			DecodeStatus: status, Sort: sortValue, Episodes: episodes,
		})
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Sort < lines[j].Sort })
	return lines
}

func duoduoEpisodeNumber(title string, fallback int) int {
	match := reEpisodeNumber.FindStringSubmatch(title)
	if len(match) == 0 {
		return fallback
	}
	for index := 1; index < len(match); index++ {
		if match[index] != "" {
			if value, err := strconv.Atoi(match[index]); err == nil && value > 0 {
				return value
			}
		}
	}
	return fallback
}

func duoduoDramaFromRow(row map[string]any) Drama {
	id := duoduoString(row["vod_id"])
	title := duoduoString(row["vod_name"])
	if !webProviderNumericID.MatchString(id) || title == "" {
		return Drama{}
	}
	cover := duoduoString(row["vod_pic"])
	if strings.HasPrefix(cover, "//") {
		cover = "https:" + cover
	}
	description := htmlUnescapeDuoduo(firstNonEmpty(
		duoduoString(row["vod_content"]),
		duoduoString(row["vod_blurb"]),
	))
	category := duoduoString(row["type_name"])
	year := duoduoString(row["vod_year"])
	remarks := duoduoString(row["vod_remarks"])
	tags := duoduoStrings(row["vod_class"])
	areas := duoduoStrings(row["vod_area"])
	for _, area := range areas {
		if len(tags) >= 16 {
			break
		}
		tags = append(tags, area)
	}
	episodeCount := 0
	if match := reEpisodeNumber.FindStringSubmatch(remarks); len(match) > 0 {
		for index := 1; index < len(match); index++ {
			if match[index] != "" {
				episodeCount, _ = strconv.Atoi(match[index])
				break
			}
		}
	}
	return Drama{
		ID: providerDramaID(sourceDuoduo, id), Source: sourceDuoduo, SourceID: id,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Desc: truncate(description, 12000), Intro: truncate(description, 12000),
		Cover: cover, CoverURL: cover, ChannelName: "多多追劇",
		CategoryName: firstNonEmpty(category, "影視"),
		Remark: truncate(remarks, 96), Score: duoduoString(row["vod_douban_score"]),
		Heat: duoduoString(row["vod_hits"]), OnlineDate: year,
		TotalEpisode: episodeCount, EpisodeCount: episodeCount, Tags: tags,
	}
}

func htmlUnescapeDuoduo(value string) string {
	return htmlstd.UnescapeString(value)
}

func (d *Downloader) fetchDuoduoAPI(ctx context.Context, route string, params url.Values) (map[string]any, string, error) {
	if !strings.HasPrefix(route, "/api.php/web/") || strings.ContainsAny(route, "?#\x00\r\n") {
		return nil, "", errors.New("多多追劇 API 路徑無效")
	}
	hosts := append([]string(nil), duoduoAPIHosts...)
	d.providerMu.Lock()
	preferred := d.providerHosts[sourceDuoduo]
	d.providerMu.Unlock()
	if preferred != "" {
		preferredHost := strings.TrimPrefix(strings.TrimPrefix(preferred, "https://"), "http://")
		preferredHost = strings.TrimRight(preferredHost, "/")
		for _, host := range duoduoAPIHosts {
			if strings.EqualFold(host, preferredHost) {
				hosts = append([]string{host}, hosts...)
				break
			}
		}
	}
	var lastErr error
	for _, host := range duoduoUniqueStrings(hosts) {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		base := "https://" + host
		address := base + route
		if len(params) > 0 {
			address += "?" + params.Encode()
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, "", err
		}
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-Client", duoduoClientID)
		request.Header.Set("web-sign", duoduoWebSign)
		request.Header.Set("Origin", base)
		request.Header.Set("Referer", base+"/")
		response, err := d.doCatalogRequestWithTimeout(request, providerTimeout)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
		closeErr := response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if closeErr != nil {
			lastErr = closeErr
			continue
		}
		if len(body) > 4<<20 {
			lastErr = errors.New("多多追劇 API 回應超過大小限制")
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = fmt.Errorf("多多追劇 API HTTP %d", response.StatusCode)
			continue
		}
		var payload map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			lastErr = fmt.Errorf("多多追劇 API JSON 格式錯誤：%w", err)
			continue
		}
		if duoduoString(payload["code"]) != "200" {
			lastErr = fmt.Errorf("多多追劇 API：%s", firstNonEmpty(duoduoString(payload["msg"]), "回應失敗"))
			continue
		}
		d.providerMu.Lock()
		if d.providerHosts == nil {
			d.providerHosts = map[string]string{}
		}
		d.providerHosts[sourceDuoduo] = base
		d.providerMu.Unlock()
		return payload, base, nil
	}
	if lastErr == nil {
		lastErr = errors.New("多多追劇目前沒有可用的 API 鏡像站")
	}
	return nil, "", fmt.Errorf("多多追劇連線失敗：%w", lastErr)
}

func (d *Downloader) fetchDuoduoCategories(ctx context.Context) ([]nativeCategory, error) {
	payload, _, err := d.fetchDuoduoAPI(ctx, "/api.php/web/index/home", nil)
	if err != nil {
		return nil, err
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return nil, errors.New("多多追劇首頁沒有回傳分類")
	}
	rows := duoduoRows(data["categories"])
	categories := make([]nativeCategory, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		id, name := duoduoString(row["type_id"]), duoduoString(row["type_name"])
		if id == "" || name == "" || seen[id] || len(id) > 16 {
			continue
		}
		seen[id] = true
		categories = append(categories, nativeCategory{ID: id, Name: truncate(name, 32)})
	}
	if len(categories) == 0 {
		return nil, errors.New("多多追劇目前沒有可用的影片分類")
	}
	return categories, nil
}

func (d *Downloader) fetchDuoduoCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 || len(query) > 256 || category != "" && len(category) > 16 {
		return nil, false, errors.New("多多追劇目錄查詢參數無效")
	}
	var route string
	params := url.Values{"page": {strconv.Itoa(page)}}
	if query != "" {
		route = "/api.php/web/search/index"
		params.Set("wd", query)
		params.Set("limit", strconv.Itoa(duoduoPageSize))
	} else {
		route = "/api.php/web/filter/vod"
		params.Set("sort", "hits")
		if category != "" {
			params.Set("type_id", category)
			categories, err := d.fetchDuoduoCategories(ctx)
			if err != nil {
				return nil, false, err
			}
			for _, item := range categories {
				if item.ID == category {
					params.Set("type_name", item.Name)
					break
				}
			}
		}
	}
	payload, _, err := d.fetchDuoduoAPI(ctx, route, params)
	if err != nil {
		return nil, false, err
	}
	rows := duoduoRows(payload["data"])
	items := make([]Drama, 0, len(rows))
	for _, row := range rows {
		if drama := duoduoDramaFromRow(row); drama.ID != "" {
			items = append(items, drama)
		}
	}
	threshold := 18
	if query != "" {
		threshold = duoduoPageSize
	}
	return items, len(rows) >= threshold, nil
}

func (d *Downloader) fetchDuoduoAggregate(ctx context.Context, sourceID string) ([]duoduoPlayLine, error) {
	if !webProviderNumericID.MatchString(sourceID) {
		return nil, errors.New("多多追劇影片 ID 無效")
	}
	payload, _, err := d.fetchDuoduoAPI(ctx, "/api.php/web/internal/search_aggregate", url.Values{"vod_id": {sourceID}})
	if err != nil {
		return nil, err
	}
	return duoduoParseAggregateLines(duoduoRows(payload["data"])), nil
}

func (d *Downloader) fetchDuoduoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !webProviderNumericID.MatchString(sourceID) {
		return Drama{}, nil, errors.New("多多追劇影片 ID 無效")
	}
	payload, base, err := d.fetchDuoduoAPI(ctx, "/api.php/web/vod/get_detail", url.Values{"vod_id": {sourceID}})
	if err != nil {
		return Drama{}, nil, err
	}
	rows := duoduoRows(payload["data"])
	if len(rows) == 0 {
		return Drama{}, nil, errors.New("多多追劇目前沒有回傳這部影片的詳情")
	}
	drama := duoduoDramaFromRow(rows[0])
	if drama.ID == "" || drama.SourceID != sourceID {
		return Drama{}, nil, errors.New("多多追劇詳情與查詢影片不符")
	}
	playerRows := duoduoRows(payload["vodplayer"])
	lines, aggregateErr := d.fetchDuoduoAggregate(ctx, sourceID)
	if len(lines) == 0 {
		lines = duoduoParseDetailLines(rows[0], playerRows)
	}
	if len(lines) == 0 {
		if aggregateErr != nil {
			return Drama{}, nil, fmt.Errorf("多多追劇已取得影片資料，但目前沒有可直接播放的線路：%w", aggregateErr)
		}
		return Drama{}, nil, errors.New("多多追劇目前沒有可直接播放的影片線路")
	}
	best := lines[0]
	for _, line := range lines[1:] {
		if len(line.Episodes) > len(best.Episodes) {
			best = line
		}
	}
	chapters := make([]Chapter, 0, len(best.Episodes))
	for _, episode := range best.Episodes {
		key := strconv.Itoa(episode.Index)
		chapters = append(chapters, Chapter{
			ID: providerChapterID(sourceDuoduo, sourceID, key), Source: sourceDuoduo,
			Title: truncate(episode.Title, 128),
			CurrentEpisode: rawEpisode(duoduoEpisodeNumber(episode.Title, episode.Index)),
			VideoURL: fmt.Sprintf("duoduo-play://%s/%s", sourceID, key),
			Referer: base + "/",
		})
	}
	drama.TotalEpisode, drama.EpisodeCount, drama.EpisodesVerified = len(chapters), len(chapters), len(chapters) > 0
	return drama, chapters, nil
}

func (d *Downloader) resolveDuoduoMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(source, sourceID, "")
	if !valid || source != sourceDuoduo || task.Chapter.Source != source || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("多多追劇播放分集資訊無效，請重新整理詳情")
	}
	key := strings.TrimPrefix(task.Chapter.ID, prefix)
	episodeIndex, err := strconv.Atoi(key)
	if err != nil || episodeIndex < 1 || episodeIndex > 100000 {
		return providerMedia{}, errors.New("多多追劇播放集數無效")
	}
	lines, err := d.fetchDuoduoAggregate(ctx, sourceID)
	if err != nil {
		return providerMedia{}, err
	}
	d.providerMu.Lock()
	base := d.providerHosts[sourceDuoduo]
	d.providerMu.Unlock()
	if base == "" {
		base = d.providerBaseURL(sourceDuoduo)
	}
	var options []providerMedia
	seen := map[string]bool{}
	for _, line := range lines {
		var selected *duoduoPlayEpisode
		for index := range line.Episodes {
			episode := &line.Episodes[index]
			if task.Chapter.Title != "" && strings.EqualFold(strings.TrimSpace(episode.Title), strings.TrimSpace(task.Chapter.Title)) {
				selected = episode
				break
			}
		}
		if selected == nil {
			for index := range line.Episodes {
				if line.Episodes[index].Index == episodeIndex {
					selected = &line.Episodes[index]
					break
				}
			}
		}
		if selected == nil || !duoduoDirectMediaURL(selected.URL) || seen[selected.URL] {
			continue
		}
		seen[selected.URL] = true
		options = append(options, providerMedia{
			URL: selected.URL, Referer: base + "/", StandardHTTP: true,
		})
		if len(options) >= 12 {
			break
		}
	}
	if len(options) == 0 {
		return providerMedia{}, errors.New("多多追劇目前找不到這一集可直接播放的線路")
	}
	primary := options[0]
	if len(options) > 1 {
		primary.Variants = append([]providerMedia(nil), options[1:]...)
	}
	return primary, nil
}
