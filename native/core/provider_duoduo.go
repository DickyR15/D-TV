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
	switch rows := value.(type) {
	case []map[string]any:
		return rows
	case []any:
		out := make([]map[string]any, 0, len(rows))
		for _, item := range rows {
			if row, ok := item.(map[string]any); ok {
				out = append(out, row)
			}
		}
		return out
	case map[string]any:
		// Some mirrors wrap list data in a named collection rather than
		// returning the array directly. Accept the common public API shapes.
		for _, key := range []string{"list", "items", "results", "videos", "vod"} {
			if nested := duoduoRows(rows[key]); len(nested) > 0 {
				return nested
			}
		}
	}
	return nil
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

func duoduoPosterAddressesFromRow(row map[string]any) []string {
	var addresses []string
	for _, field := range []string{
		"vod_pic", "vod_pic_thumb", "vod_pic_url", "vod_pic_slide",
		"vod_pic_screenshot", "vod_pic_local", "vod_pic_original",
		"vod_pic_big", "vod_pic_small", "vod_pic_720", "vod_pic_1080",
		"vod_pic_cover", "vod_pic_poster", "vod_pic_img",
		"cover", "cover_url", "cover_image", "poster", "poster_url",
		"image", "image_url", "thumbnail", "thumb", "pic", "pic_url", "img",
	} {
		addresses = append(addresses, providerCoverAddresses(row[field], duoduoPublicEntryURL)...)
	}
	return nativeUniqueCoverAddresses(addresses...)
}

func duoduoSynopsisFromRow(row map[string]any) string {
	for _, key := range []string{"vod_content", "vod_blurb"} {
		if description := providerHTMLPlainText(duoduoString(row[key])); description != "" {
			return description
		}
	}
	return ""
}

func duoduoDramaFromRow(row map[string]any) Drama {
	id := firstNonEmpty(duoduoString(row["vod_id"]), duoduoString(row["id"]))
	title := firstNonEmpty(duoduoString(row["vod_name"]), duoduoString(row["title"]), duoduoString(row["name"]))
	if !webProviderNumericID.MatchString(id) || title == "" {
		return Drama{}
	}
	// Preserve the source's alternate poster fields so the cover cache can try
	// them when the preferred CDN is unavailable, rather than losing the title's
	// only other image candidates at the initial JSON mapping stage.
	posterAddresses := duoduoPosterAddressesFromRow(row)
	cover := firstNonEmpty(posterAddresses...)
	var coverCandidates []string
	if len(posterAddresses) > 1 {
		coverCandidates = append(coverCandidates, posterAddresses[1:]...)
	}
	description := duoduoSynopsisFromRow(row)
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
		Cover: cover, CoverURL: cover, CoverCandidates: coverCandidates, ChannelName: "多多追劇",
		CategoryName: category,
		Remark: truncate(remarks, 96), Score: duoduoString(row["vod_douban_score"]),
		Heat: duoduoString(row["vod_hits"]), OnlineDate: year,
		DoubanID: duoduoString(row["vod_douban_id"]),
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

func (d *Downloader) fetchDuoduoHomeData(ctx context.Context) (map[string]any, error) {
	payload, _, err := d.fetchDuoduoAPI(ctx, "/api.php/web/index/home", nil)
	if err != nil {
		return nil, err
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return nil, errors.New("多多追劇首頁沒有回傳有效資料")
	}
	return data, nil
}

func duoduoCategoriesFromHome(data map[string]any) []nativeCategory {
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
	return categories
}

func (d *Downloader) fetchDuoduoCategories(ctx context.Context) ([]nativeCategory, error) {
	data, err := d.fetchDuoduoHomeData(ctx)
	if err != nil {
		return nil, err
	}
	categories := duoduoCategoriesFromHome(data)
	if len(categories) == 0 {
		return nil, errors.New("多多追劇目前沒有可用的影片分類")
	}
	return categories, nil
}

func duoduoCategoryName(category string) string {
	switch strings.TrimSpace(category) {
	case "1":
		return "电影"
	case "2":
		return "剧集"
	case "3":
		return "动漫"
	case "4":
		return "综艺"
	default:
		return ""
	}
}

// duoduoHomeSupplement returns the curated videos shown by the source website's
// home page. Those rows are separate from /filter/vod pagination and otherwise
// never appear in D TV's catalog, even though they are visible on the website.
func duoduoHomeSupplement(data map[string]any, category string) []Drama {
	seen := map[string]bool{}
	var items []Drama
	addRows := func(rows []map[string]any) {
		for _, row := range rows {
			drama := duoduoDramaFromRow(row)
			if drama.ID == "" || seen[drama.ID] {
				continue
			}
			seen[drama.ID] = true
			items = append(items, drama)
		}
	}
	for _, group := range duoduoRows(data["categories"]) {
		if category != "" && duoduoString(group["type_id"]) != category {
			continue
		}
		addRows(duoduoRows(group["videos"]))
	}
	if category == "" {
		addRows(duoduoRows(data["recommend"]))
		addRows(duoduoRows(data["videos"]))
	}
	return items
}

func (d *Downloader) fetchDuoduoCatalogPage(ctx context.Context, page int, category, query string, requestedSort ...string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 || len(query) > 256 || category != "" && len(category) > 16 {
		return nil, false, errors.New("多多追劇目錄查詢參數無效")
	}
	sortValue := "hits"
	if len(requestedSort) > 0 && strings.TrimSpace(requestedSort[0]) != "" {
		sortValue = strings.TrimSpace(requestedSort[0])
	}
	switch sortValue {
	case "hits", "time", "score", "year":
	default:
		return nil, false, errors.New("多多追劇排序選項無效")
	}
	var route string
	params := url.Values{"page": {strconv.Itoa(page)}}
	var homeData map[string]any
	if query == "" && (page == 1 || category != "" && duoduoCategoryName(category) == "") {
		// Keep the site's curated home rows available as part of the first
		// catalog page, even when its independent category endpoint omits them.
		homeData, _ = d.fetchDuoduoHomeData(ctx)
	}
	if query != "" {
		route = "/api.php/web/search/index"
		params.Set("wd", query)
		params.Set("limit", strconv.Itoa(duoduoPageSize))
	} else {
		route = "/api.php/web/filter/vod"
		params.Set("sort", sortValue)
		params.Set("pageSize", "24")
		if category != "" {
			// The upstream web API filters by type_name; type_id is only the
			// identifier used by its category menu and is ignored by the filter.
			name := duoduoCategoryName(category)
			if homeData != nil {
				for _, item := range duoduoCategoriesFromHome(homeData) {
					if item.ID == category {
						name = item.Name
						break
					}
				}
			}
			if name == "" {
				return nil, false, errors.New("多多追劇分類名稱無效")
			}
			params.Set("type_name", name)
		}
	}
	payload, _, err := d.fetchDuoduoAPI(ctx, route, params)
	if err != nil {
		return nil, false, err
	}
	rows := duoduoRows(payload["data"])
	items := make([]Drama, 0, len(rows)+24)
	seen := make(map[string]bool, len(rows)+24)
	appendRows := func(values []map[string]any) {
		for _, row := range values {
			drama := duoduoDramaFromRow(row)
			if drama.ID == "" || seen[drama.ID] {
				continue
			}
			seen[drama.ID] = true
			items = append(items, drama)
		}
	}
	appendRows(rows)
	if query == "" && page == 1 && homeData != nil && sortValue == "hits" {
		// Curated homepage rows are only suitable for the default Hot list.
		// Appending them to latest/score/year results would corrupt the upstream
		// order and inject items that do not satisfy the selected ranking.
		supplement := duoduoHomeSupplement(homeData, category)
		for _, drama := range supplement {
			if drama.ID != "" && !seen[drama.ID] {
				seen[drama.ID] = true
				items = append(items, drama)
			}
		}
	}
	// The source API's pageCount field is known to be unreliable (often 1).
	// Its public client uses >=18 results as the continuation signal as well.
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

func (d *Downloader) fetchDuoduoDetailRow(ctx context.Context, sourceID string) (Drama, map[string]any, map[string]any, string, error) {
	if !webProviderNumericID.MatchString(sourceID) {
		return Drama{}, nil, nil, "", errors.New("多多追劇影片 ID 無效")
	}
	payload, base, err := d.fetchDuoduoAPI(ctx, "/api.php/web/vod/get_detail", url.Values{"vod_id": {sourceID}})
	if err != nil {
		return Drama{}, nil, nil, "", err
	}
	rows := duoduoRows(payload["data"])
	if len(rows) == 0 {
		return Drama{}, nil, nil, "", errors.New("多多追劇目前沒有回傳這部影片的詳情")
	}
	row := rows[0]
	drama := duoduoDramaFromRow(row)
	if drama.ID == "" || drama.SourceID != sourceID {
		return Drama{}, nil, nil, "", errors.New("多多追劇詳情與查詢影片不符")
	}
	return drama, row, payload, base, nil
}

func duoduoMergePlayLines(primary, supplemental []duoduoPlayLine) []duoduoPlayLine {
	merged := append([]duoduoPlayLine(nil), primary...)
	indexes := make(map[string]int, len(merged)*2)
	addAliases := func(line duoduoPlayLine, index int) {
		for _, alias := range []string{line.Key, line.Name} {
			alias = strings.ToLower(strings.TrimSpace(alias))
			if alias != "" {
				indexes[alias] = index
			}
		}
	}
	for index, line := range merged {
		addAliases(line, index)
	}
	for _, candidate := range supplemental {
		index, found := -1, false
		for _, alias := range []string{candidate.Key, candidate.Name} {
			alias = strings.ToLower(strings.TrimSpace(alias))
			if existing, ok := indexes[alias]; alias != "" && ok {
				index, found = existing, true
				break
			}
		}
		if !found {
			index = len(merged)
			merged = append(merged, candidate)
			addAliases(candidate, index)
			continue
		}
		existing := &merged[index]
		seenEpisodes := make(map[string]bool, len(existing.Episodes)+len(candidate.Episodes))
		for _, episode := range existing.Episodes {
			key := fmt.Sprintf("%d|%s", duoduoEpisodeNumber(episode.Title, episode.Index), episode.URL)
			seenEpisodes[key] = true
		}
		for _, episode := range candidate.Episodes {
			key := fmt.Sprintf("%d|%s", duoduoEpisodeNumber(episode.Title, episode.Index), episode.URL)
			if !seenEpisodes[key] {
				seenEpisodes[key] = true
				existing.Episodes = append(existing.Episodes, episode)
			}
		}
		if existing.Name == "" {
			existing.Name = candidate.Name
		}
		addAliases(*existing, index)
	}
	return merged
}

func (d *Downloader) fetchDuoduoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
  return d.fetchDuoduoDetailWithCategory(ctx, sourceID, "")
}

func (d *Downloader) fetchDuoduoDetailWithCategory(ctx context.Context, sourceID, catalogCategory string) (Drama, []Chapter, error) {
	drama, detailRow, payload, base, err := d.fetchDuoduoDetailRow(ctx, sourceID)
	if err != nil {
		return Drama{}, nil, err
	}
  if drama.CategoryName == "" && catalogCategory != "" {
    drama.CategoryName = catalogCategory
  }
	playerRows := duoduoRows(payload["vodplayer"])
	// Keep detail-page direct URLs first, then supplement them with the full
	// aggregate list. A detail response can expose one working line while
	// omitting other public lines; fetching the aggregate is what lets the
	// canonical episode retain all same-episode fallbacks.
	detailLines := duoduoParseDetailLines(detailRow, playerRows)
	aggregateLines, aggregateErr := d.fetchDuoduoAggregate(ctx, sourceID)
	lines := duoduoMergePlayLines(detailLines, aggregateLines)
	if len(lines) == 0 {
		if aggregateErr != nil {
			return Drama{}, nil, fmt.Errorf("多多追劇已取得影片資料，但目前沒有可直接播放的線路：%w", aggregateErr)
		}
		return Drama{}, nil, errors.New("多多追劇目前沒有可直接播放的影片線路")
	}
	// Merge every direct playback line into one canonical chapter per episode.
	// The first available line is the preferred URL; other lines for the same
	// episode are retained as automatic fallbacks and are never displayed as
	// duplicate episode buttons.
	type episodeChoice struct {
		episode  duoduoPlayEpisode
		number   int
		variants []string
		seen     map[string]bool
	}
	choices := make(map[int]*episodeChoice)
	for _, line := range lines {
		for _, episode := range line.Episodes {
			number := duoduoEpisodeNumber(episode.Title, episode.Index)
			if number < 1 || number > 100000 || !duoduoDirectMediaURL(episode.URL) {
				continue
			}
			choice := choices[number]
			if choice == nil {
				choices[number] = &episodeChoice{
					episode: episode,
					number: number,
					seen: map[string]bool{episode.URL: true},
				}
				continue
			}
			if !choice.seen[episode.URL] {
				choice.seen[episode.URL] = true
				choice.variants = append(choice.variants, episode.URL)
			}
		}
	}
	if len(choices) == 0 {
		return Drama{}, nil, errors.New("多多追劇目前沒有可用的直接播放集數")
	}
	numbers := make([]int, 0, len(choices))
	maxEpisodes := 0
	visiblePlayLine := ""
	if len(lines) == 1 {
		visiblePlayLine = truncate(lines[0].Name, 96)
	}
	for number := range choices {
		numbers = append(numbers, number)
		maxEpisodes = max(maxEpisodes, number)
	}
	sort.Ints(numbers)
	chapters := make([]Chapter, 0, len(numbers))
	for _, number := range numbers {
		choice := choices[number]
		chapters = append(chapters, Chapter{
			ID: providerChapterID(sourceDuoduo, sourceID, strconv.Itoa(number)),
			Source: sourceDuoduo,
			Title: truncate(choice.episode.Title, 128),
			CurrentEpisode: rawEpisode(number),
			VideoURL: choice.episode.URL,
			Referer: base + "/",
			// With multiple source lines, playback is automatic and the UI should
			// not offer a manual per-line selector. Preserve the label for a
			// single-line title where it remains useful metadata.
			PlayLine: visiblePlayLine,
			PlaybackVariants: duoduoUniqueStrings(choice.variants),
		})
	}
	if duoduoShouldCollapseMovie(drama, detailRow) {
    chapters = duoduoMovieChapter(sourceID, chapters)
    maxEpisodes = len(chapters)
  }
	drama.TotalEpisode, drama.EpisodeCount, drama.Episodes = maxEpisodes, maxEpisodes, maxEpisodes
	drama.EpisodesVerified = maxEpisodes > 0
	return drama, chapters, nil
}

func duoduoIsMovieCategory(category string) bool {
  switch strings.ToLower(strings.TrimSpace(category)) {
  case "电影", "電影", "movie", "movies", "film", "影片":
    return true
  default:
    return false
  }
}

// duoduoMovieChapter treats Mandarin/Cantonese/quality labels as versions of
// the same film, not sequential episodes. Keep the remaining URLs as same-episode
// fallbacks so a movie always exposes exactly one playable chapter.
// duoduoShouldCollapseMovie uses the catalog category first, then only
// explicit film-style remarks when old cache data has no category and the
// detail endpoint omits type_name.
func duoduoShouldCollapseMovie(drama Drama, detailRow map[string]any) bool {
  category := firstNonEmpty(
    duoduoString(detailRow["type_name"]),
    drama.CategoryName,
    drama.Category,
  )
  if category != "" {
    return duoduoIsMovieCategory(category)
  }
  remark := strings.ToLower(strings.Join(strings.Fields(
    firstNonEmpty(duoduoString(detailRow["vod_remarks"]), drama.Remark),
  ), ""))
  for _, signal := range []string{
    "更新至hd", "正片", "hd中字", "hd国语", "hd粵語", "hd粤语",
    "bd高清", "tc中字", "蓝光高清", "藍光高清",
  } {
    if strings.Contains(remark, strings.ToLower(signal)) {
      return true
    }
  }
  return false
}

func duoduoMovieChapter(sourceID string, chapters []Chapter) []Chapter {
  if len(chapters) == 0 {
    return nil
  }
  primary := chapters[0]
  primary.ID = providerChapterID(sourceDuoduo, sourceID, "1")
  primary.Source = sourceDuoduo
  primary.Title = "正片"
  primary.CurrentEpisode = rawEpisode(1)

  seen := map[string]bool{}
  if primary.VideoURL != "" {
    seen[primary.VideoURL] = true
  }
  variants := make([]string, 0, len(chapters)*2)
  addVariant := func(address string) {
    address = strings.TrimSpace(address)
    if address == "" || seen[address] || !duoduoDirectMediaURL(address) {
      return
    }
    seen[address] = true
    if len(variants) < 11 {
      variants = append(variants, address)
    }
  }
  for _, chapter := range chapters {
    if chapter.VideoURL != primary.VideoURL {
      addVariant(chapter.VideoURL)
    }
    for _, address := range chapter.PlaybackVariants {
      addVariant(address)
    }
  }
  primary.PlaybackVariants = variants
  return []Chapter{primary}
}

func duoduoChapterEpisodeCount(chapters []Chapter) int {
	maxEpisode := 0
	for index, chapter := range chapters {
		number, err := strconv.Atoi(chapter.EpisodeString(index + 1))
		if err == nil && number > maxEpisode {
			maxEpisode = number
		}
	}
	return maxEpisode
}

func (d *Downloader) resolveDuoduoMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(source, sourceID, "")
	if !valid || source != sourceDuoduo || task.Chapter.Source != source || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("多多追劇播放分集資訊無效，請重新整理詳情")
	}
	key := strings.TrimPrefix(task.Chapter.ID, prefix)
	lineOrdinal := 0
	episodeKey := key
	if lineKey, episodeValue, found := strings.Cut(key, "-"); found {
		parsedLine, parseErr := strconv.Atoi(lineKey)
		if parseErr != nil || parsedLine < 1 || parsedLine > 1000 {
			return providerMedia{}, errors.New("多多追劇播放線路編號無效")
		}
		lineOrdinal = parsedLine
		episodeKey = episodeValue
	}
	episodeIndex, err := strconv.Atoi(episodeKey)
	if err != nil || episodeIndex < 1 || episodeIndex > 100000 {
		return providerMedia{}, errors.New("多多追劇播放集數無效")
	}
	// Keep Duoduo on the dedicated stream transport used by the 1.0.1 player.
	// It pins media requests to HTTP/1.1 and retains the CDN/TLS fallback path;
	// routing these HLS URLs through the general HTTP client caused Android TV
	// to fail opening otherwise playable playlists and repeatedly switch lines.
	// New detail responses contain direct public media URLs. Use that validated
	// URL immediately instead of querying the aggregate API a second time;
	// this keeps playback working when the API mirror becomes temporarily busy.
	if duoduoDirectMediaURL(task.Chapter.VideoURL) {
		referer := firstNonEmpty(task.Chapter.Referer, d.providerBaseURL(sourceDuoduo)+"/")
		primary := providerMedia{
			URL: task.Chapter.VideoURL, Referer: referer, StandardHTTP: false,
		}
		seen := map[string]bool{primary.URL: true}
		for _, address := range task.Chapter.PlaybackVariants {
			if !duoduoDirectMediaURL(address) || seen[address] {
				continue
			}
			seen[address] = true
			primary.Variants = append(primary.Variants, providerMedia{
				URL: address, Referer: referer, StandardHTTP: false,
			})
			if len(primary.Variants) >= 11 {
				break
			}
		}
		return primary, nil
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
	for lineIndex, line := range lines {
		if lineOrdinal > 0 && lineIndex+1 != lineOrdinal {
			// Keep the original selected line first for legacy pseudo URLs;
			// other lines remain eligible only as same-episode fallbacks.
			continue
		}
		var selected *duoduoPlayEpisode
		targetTitle := strings.TrimSpace(task.Chapter.Title)
		targetEpisodeNumber := duoduoEpisodeNumber(targetTitle, 0)
		for index := range line.Episodes {
			episode := &line.Episodes[index]
			candidateTitle := strings.TrimSpace(episode.Title)
			if targetTitle != "" && strings.EqualFold(candidateTitle, targetTitle) {
				selected = episode
				break
			}
			if targetEpisodeNumber > 0 && duoduoEpisodeNumber(candidateTitle, 0) == targetEpisodeNumber {
				selected = episode
				break
			}
		}
		// Only use the episode's position when the saved chapter has no title.
		// Falling back by position when a title exists can turn episode 2 into
		// a playback variant for episode 1 on another source line.
		if selected == nil && targetTitle == "" {
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
			URL: selected.URL, Referer: base + "/", StandardHTTP: false,
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
