package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

const (
	kanmovieSiteBaseURL = "https://www.kanmovie.com"
	kanmovieUserAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

type kanmovieBackend struct {
	host    string
	apiBase string
	home    string
}

// 西瓜影視是聚合站。作品 ID 內含原始片源站代碼，影片詳情與播放地址
// 必須透過對應的上游 MacCMS Provide API 取得，不能把所有影片當成同一個站源。
var kanmovieBackends = []kanmovieBackend{
	{host: "zuidazy.co", apiBase: "https://api.zuidapi.com/api.php/provide/vod", home: "https://zuidazy.co/"},
	{host: "jszyapi.com", apiBase: "https://jszyapi.com/api.php/provide/vod", home: "https://jszyapi.com/"},
	{host: "bfzy.tv", apiBase: "https://bfzyapi.com/api.php/provide/vod", home: "https://bfzy.tv/"},
	{host: "subozy.com", apiBase: "https://subocj.com/api.php/provide/vod", home: "https://www.subozy.com/"},
}

var kanmovieCategories = []nativeCategory{
	{ID: "movies", Name: "電影"},
	{ID: "tv-series", Name: "電視劇"},
	{ID: "anime", Name: "動漫"},
	{ID: "variety", Name: "綜藝"},
	{ID: "today", Name: "最近更新"},
	{ID: "hot-search", Name: "熱門影視"},
}

var kanmovieDatePattern = regexp.MustCompile(`\d{4}-\d{1,2}-\d{1,2}`)
var kanmovieEpisodeNumber = regexp.MustCompile(`(?i)(?:第\s*0*(\d+)\s*(?:集|話|期)|(?:更新至|共|全)\s*0*(\d+)\s*(?:集|話|期)|(?:episode|ep)\s*#?\s*0*(\d+))`)

type kanmovieAPIResponse struct {
	Code      int              `json:"code"`
	Message   string           `json:"msg"`
	Page      json.RawMessage  `json:"page"`
	PageCount json.RawMessage  `json:"pagecount"`
	Total     json.RawMessage  `json:"total"`
	List      []map[string]any `json:"list"`
}

type kanmovieEpisode struct {
	Title string
	URL   string
	Index int
}

type kanmoviePlayGroup struct {
	Name     string
	Episodes []kanmovieEpisode
}

func validKanmMovieCategory(category string) bool {
	if category == "" {
		return true
	}
	if len(category) > 32 || strings.ContainsAny(category, "|/\\\x00\r\n") {
		return false
	}
	for _, entry := range kanmovieCategories {
		if entry.ID == category {
			return true
		}
	}
	return false
}

func kanmovieBackendForHost(host string) (kanmovieBackend, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	for _, backend := range kanmovieBackends {
		if strings.TrimPrefix(backend.host, "www.") == host {
			return backend, true
		}
	}
	return kanmovieBackend{}, false
}

func kanmovieSourceIdentity(raw string) (kanmovieBackend, string, bool) {
	raw = strings.TrimSpace(raw)
	host, id, ok := strings.Cut(raw, "~")
	if !ok || id == "" || !webProviderNumericID.MatchString(id) {
		return kanmovieBackend{}, "", false
	}
	backend, found := kanmovieBackendForHost(host)
	if !found {
		return kanmovieBackend{}, "", false
	}
	return backend, id, true
}

func kanmovieCategoryPath(category string) (string, bool) {
	if category == "" || category == "today" {
		return "/today", true
	}
	switch category {
	case "movies":
		return "/movies", true
	case "tv-series":
		return "/tv-series", true
	case "anime":
		return "/anime", true
	case "variety":
		return "/variety", true
	case "hot-search":
		return "/hot-search", true
	default:
		return "", false
	}
}

func kanmovieAbsoluteURL(raw, pageURL string) string {
	raw = strings.TrimSpace(htmlstd.UnescapeString(raw))
	if raw == "" {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	reference, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	address := base.ResolveReference(reference)
	if address.User != nil || (address.Scheme != "https" && address.Scheme != "http") {
		return ""
	}
	if strings.EqualFold(address.Hostname(), "kanmovie.com") || strings.EqualFold(address.Hostname(), "www.kanmovie.com") {
		return address.String()
	}
	return ""
}

func kanmoviePageSourceID(reference, pageURL string) string {
	address, err := url.Parse(reference)
	if err != nil {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	address = base.ResolveReference(address)
	if address.User != nil || !strings.EqualFold(address.Hostname(), "www.kanmovie.com") && !strings.EqualFold(address.Hostname(), "kanmovie.com") {
		return ""
	}
	const prefix = "/detail/"
	if !strings.HasPrefix(address.Path, prefix) {
		return ""
	}
	key := strings.Trim(strings.TrimPrefix(address.Path, prefix), "/")
	if _, _, ok := kanmovieSourceIdentity(key); !ok {
		return ""
	}
	return key
}

func kanmovieCardContainer(anchor *html.Node) *html.Node {
	node := anchor
	best := anchor
	for depth := 0; depth < 5 && node != nil; depth++ {
		if node.Parent == nil {
			break
		}
		node = node.Parent
		text := providerHTMLText(node)
		if len([]rune(text)) > 0 && len([]rune(text)) < 320 {
			best = node
		}
		if node.Data == "article" || providerHTMLClass(node, "movie-item") ||
			providerHTMLClass(node, "video-item") || providerHTMLClass(node, "module-item") ||
			providerHTMLClass(node, "media-card") {
			return node
		}
	}
	return best
}

func kanmovieImage(card *html.Node, pageURL string) string {
	for _, image := range providerHTMLNodes(card, func(node *html.Node) bool { return node.Data == "img" }) {
		for _, key := range []string{"data-src", "data-original", "data-lazy-src", "src"} {
			if address := providerCoverAddress(providerHTMLAttr(image, key), pageURL); address != "" {
				return address
			}
		}
	}
	return ""
}

func kanmovieTitle(anchor, card *html.Node) string {
	title := firstNonEmpty(
		providerHTMLAttr(anchor, "title"),
		providerHTMLAttr(anchor, "aria-label"),
		providerHTMLText(anchor),
	)
	if title == "" {
		for _, image := range providerHTMLNodes(card, func(node *html.Node) bool { return node.Data == "img" }) {
			title = strings.TrimSpace(providerHTMLAttr(image, "alt"))
			if title != "" {
				break
			}
		}
	}
	title = strings.TrimSpace(strings.TrimSuffix(title, "在线观看"))
	return truncate(title, 512)
}

func kanmovieCatalogDrama(sourceID, title, cover, category, cardText string) Drama {
	backend, upstreamID, _ := kanmovieSourceIdentity(sourceID)
	remark := strings.TrimSpace(strings.TrimPrefix(cardText, title))
	if len([]rune(remark)) > 96 {
		remark = string([]rune(remark)[:96])
	}
	year := ""
	if match := kanmovieDatePattern.FindString(remark); match != "" {
		year = match
	}
	drama := Drama{
		ID: providerDramaID(sourceKanmMovie, sourceID), Source: sourceKanmMovie, SourceID: sourceID,
		Title: title, Name: title, Cover: cover, CoverURL: cover,
		ChannelName: "西瓜影視", CategoryName: firstNonEmpty(category, "影視"),
		Remark: remark, OnlineDate: year,
	}
	if backend.host == "" || upstreamID == "" {
		return Drama{}
	}
	return drama
}

func parseKanmMovieCards(document *html.Node, pageURL, category string, page int) ([]Drama, bool, error) {
	var items []Drama
	seen := map[string]bool{}
	hasMore := false
	for _, anchor := range providerHTMLNodes(document, func(node *html.Node) bool { return node.Data == "a" }) {
		href := providerHTMLAttr(anchor, "href")
		sourceID := kanmoviePageSourceID(href, pageURL)
		if sourceID == "" {
			address, err := url.Parse(href)
			if err == nil && strings.EqualFold(address.Query().Get("page"), strconv.Itoa(page+1)) {
				hasMore = true
			}
			if strings.EqualFold(providerHTMLAttr(anchor, "rel"), "next") ||
				strings.Contains(strings.ToLower(providerHTMLAttr(anchor, "class")), "next") ||
				strings.Contains(providerHTMLText(anchor), "下一页") {
				hasMore = true
			}
			continue
		}
		if seen[sourceID] {
			continue
		}
		card := kanmovieCardContainer(anchor)
		title := kanmovieTitle(anchor, card)
		if title == "" {
			continue
		}
		cover := kanmovieImage(card, pageURL)
		cardText := providerHTMLText(card)
		drama := kanmovieCatalogDrama(sourceID, title, cover, category, cardText)
		if drama.ID == "" {
			continue
		}
		seen[sourceID] = true
		items = append(items, drama)
		if len(items) > 500 {
			return nil, false, errors.New("西瓜影視目錄項目過多")
		}
	}
	if len(items) == 0 {
		return nil, false, errors.New("西瓜影視頁面未找到影片清單，網站版型可能已變更")
	}
	return items, hasMore, nil
}

func (d *Downloader) kanmovieGet(ctx context.Context, path string) (string, error) {
	address := path
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = strings.TrimRight(d.providerBaseURL(sourceKanmMovie), "/") + "/" + strings.TrimLeft(path, "/")
	}
	pageContext := context.WithValue(ctx, providerTextUserAgentKey{}, kanmovieUserAgent)
	return d.fetchProviderText(pageContext, address, kanmovieSiteBaseURL+"/")
}

func kanmovieRawInteger(raw json.RawMessage) int {
	var number int
	if json.Unmarshal(raw, &number) == nil {
		return number
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		number, _ = strconv.Atoi(strings.TrimSpace(text))
	}
	return number
}

func parseKanmMovieAPI(body string) (kanmovieAPIResponse, error) {
	var payload kanmovieAPIResponse
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return payload, fmt.Errorf("西瓜影視上游 API 回應格式錯誤：%w", err)
	}
	if payload.Code != 1 {
		return payload, fmt.Errorf("西瓜影視上游 API 回應失敗：%s", firstNonEmpty(payload.Message, "未知錯誤"))
	}
	if payload.List == nil {
		return payload, errors.New("西瓜影視上游 API 未回傳影片清單")
	}
	return payload, nil
}

func kanmovieAPIAddress(backend kanmovieBackend, values url.Values) string {
	base, err := url.Parse(backend.apiBase)
	if err != nil {
		return ""
	}
	base.RawQuery = values.Encode()
	return base.String()
}

func (d *Downloader) fetchKanmMovieAPI(ctx context.Context, backend kanmovieBackend, values url.Values) (kanmovieAPIResponse, error) {
	address := kanmovieAPIAddress(backend, values)
	if address == "" {
		return kanmovieAPIResponse{}, errors.New("西瓜影視上游 API 位址無效")
	}
	body, err := d.fetchProviderText(ctx, address, backend.home)
	if err != nil {
		return kanmovieAPIResponse{}, fmt.Errorf("%s 上游連線失敗：%w", backend.host, err)
	}
	payload, err := parseKanmMovieAPI(body)
	if err != nil {
		return kanmovieAPIResponse{}, fmt.Errorf("%s：%w", backend.host, err)
	}
	return payload, nil
}

func kanmovieRecordString(record map[string]any, key string) string {
	return strings.TrimSpace(nativeText(record[key]))
}

func kanmovieDramaFromRecord(backend kanmovieBackend, record map[string]any, category string) Drama {
	upstreamID := kanmovieRecordString(record, "vod_id")
	if !webProviderNumericID.MatchString(upstreamID) {
		return Drama{}
	}
	sourceID := backend.host + "~" + upstreamID
	title := kanmovieRecordString(record, "vod_name")
	if title == "" {
		return Drama{}
	}
	cover := kanmovieRecordString(record, "vod_pic")
	if cover != "" && strings.HasPrefix(cover, "//") {
		cover = "https:" + cover
	}
	if !isProviderHTTPMediaURL(cover) {
		cover = ""
	}
	description := kanmovieRecordString(record, "vod_blurb")
	if description == "" {
		description = kanmovieRecordString(record, "vod_content")
	}
	categoryName := firstNonEmpty(kanmovieRecordString(record, "type_name"), category, "影視")
	remark := kanmovieRecordString(record, "vod_remarks")
	year := kanmovieRecordString(record, "vod_year")
	episodeCount, _ := strconv.Atoi(kanmovieRecordString(record, "vod_total"))
	var tags []string
	for _, tag := range strings.Split(kanmovieRecordString(record, "vod_class"), ",") {
		if tag = strings.TrimSpace(tag); tag != "" && len([]rune(tag)) <= 32 && len(tags) < 16 {
			tags = append(tags, tag)
		}
	}
	return Drama{
		ID: providerDramaID(sourceKanmMovie, sourceID), Source: sourceKanmMovie, SourceID: sourceID,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Desc: truncate(htmlstd.UnescapeString(description), 12000),
		Intro: truncate(html.UnescapeString(description), 12000),
		Cover: cover, CoverURL: cover, ChannelName: "西瓜影視", CategoryName: categoryName,
		Remark: truncate(remark, 64), OnlineDate: year,
		TotalEpisode: episodeCount, EpisodeCount: episodeCount, Tags: tags,
	}
}

func (d *Downloader) fetchKanmMovieCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 || len(query) > 256 || !validKanmMovieCategory(category) {
		return nil, false, errors.New("西瓜影視目錄查詢參數無效")
	}
	if query != "" {
		type backendResult struct {
			items []Drama
			more  bool
			err   error
		}
		results := make([]backendResult, len(kanmovieBackends))
		slots := make(chan struct{}, 3)
		var group sync.WaitGroup
		for index, backend := range kanmovieBackends {
			group.Add(1)
			go func(index int, backend kanmovieBackend) {
				defer group.Done()
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
				case <-ctx.Done():
					results[index].err = ctx.Err()
					return
				}
				values := url.Values{"ac": {"videolist"}, "wd": {query}, "pg": {strconv.Itoa(page)}, "at": {"json"}}
				payload, err := d.fetchKanmMovieAPI(ctx, backend, values)
				if err != nil {
					results[index].err = err
					return
				}
				for _, row := range payload.List {
					drama := kanmovieDramaFromRecord(backend, row, "")
					if drama.ID != "" {
						results[index].items = append(results[index].items, drama)
					}
				}
				totalPages := kanmovieRawInteger(payload.PageCount)
				results[index].more = totalPages > page
			}(index, backend)
		}
		group.Wait()
		var items []Drama
		var failures []error
		hasMore := false
		seen := map[string]bool{}
		for index, result := range results {
			hasMore = hasMore || result.more || result.err != nil
			if result.err != nil {
				failures = append(failures, fmt.Errorf("%s：%w", kanmovieBackends[index].host, result.err))
			}
			for _, drama := range result.items {
				if !seen[drama.ID] {
					seen[drama.ID] = true
					items = append(items, drama)
				}
			}
		}
		if len(items) == 0 && len(failures) > 0 {
			return nil, false, errors.Join(failures...)
		}
		return items, hasMore, errors.Join(failures...)
	}

	path, ok := kanmovieCategoryPath(category)
	if !ok {
		return nil, false, errors.New("西瓜影視分類已變更，請重新整理分類")
	}
	address := fmt.Sprintf("%s%s?page=%d", strings.TrimRight(d.providerBaseURL(sourceKanmMovie), "/"), path, page)
	document, finalURL, err := d.fetchProviderPage(ctx, address, d.providerBaseURL(sourceKanmMovie)+"/", kanmovieUserAgent)
	if err != nil {
		return nil, false, err
	}
	return parseKanmMovieCards(document, finalURL, categoryNameKanmMovie(category), page)
}

func categoryNameKanmMovie(category string) string {
	if category == "" {
		return "最近更新"
	}
	for _, entry := range kanmovieCategories {
		if entry.ID == category {
			return entry.Name
		}
	}
	return "影視"
}

func parseKanmMoviePlayGroups(record map[string]any) []kanmoviePlayGroup {
	froms := strings.Split(kanmovieRecordString(record, "vod_play_from"), "$$$")
	playURLs := strings.Split(kanmovieRecordString(record, "vod_play_url"), "$$$")
	count := min(len(froms), len(playURLs))
	groups := make([]kanmoviePlayGroup, 0, count)
	for groupIndex := 0; groupIndex < count; groupIndex++ {
		name := strings.TrimSpace(froms[groupIndex])
		var episodes []kanmovieEpisode
		for episodeIndex, rawEpisode := range strings.Split(playURLs[groupIndex], "#") {
			parts := strings.SplitN(strings.TrimSpace(rawEpisode), "$", 2)
			if len(parts) != 2 {
				continue
			}
			title, rawURL := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			if title == "" || !isProviderHTTPMediaURL(rawURL) {
				continue
			}
			episodes = append(episodes, kanmovieEpisode{Title: truncate(title, 128), URL: rawURL, Index: episodeIndex + 1})
		}
		if len(episodes) > 0 {
			groups = append(groups, kanmoviePlayGroup{Name: name, Episodes: episodes})
		}
	}
	return groups
}

func kanmovieEpisodeOrder(title string, fallback int) int {
	match := kanmovieEpisodeNumber.FindStringSubmatch(title)
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

func (d *Downloader) fetchKanmMovieDetailRecord(ctx context.Context, sourceID string) (kanmovieBackend, map[string]any, error) {
	backend, upstreamID, ok := kanmovieSourceIdentity(sourceID)
	if !ok {
		return kanmovieBackend{}, nil, errors.New("西瓜影視影片來源代碼無效或目前不支援")
	}
	values := url.Values{"ac": {"videolist"}, "ids": {upstreamID}, "at": {"json"}}
	payload, err := d.fetchKanmMovieAPI(ctx, backend, values)
	if err == nil && len(payload.List) > 0 {
		return backend, payload.List[0], nil
	}
	if err == nil {
		values.Set("ac", "detail")
		payload, err = d.fetchKanmMovieAPI(ctx, backend, values)
		if err == nil && len(payload.List) > 0 {
			return backend, payload.List[0], nil
		}
	}
	if err != nil {
		return kanmovieBackend{}, nil, err
	}
	return kanmovieBackend{}, nil, errors.New("西瓜影視上游目前沒有回傳這部影片")
}

func (d *Downloader) fetchKanmMovieDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	backend, record, err := d.fetchKanmMovieDetailRecord(ctx, sourceID)
	if err != nil {
		return Drama{}, nil, err
	}
	drama := kanmovieDramaFromRecord(backend, record, "")
	if drama.ID == "" {
		return Drama{}, nil, errors.New("西瓜影視上游詳情缺少影片名稱或 ID")
	}
	groups := parseKanmMoviePlayGroups(record)
	if len(groups) == 0 {
		return Drama{}, nil, errors.New("西瓜影視這部影片目前沒有可用的分集或播放網址")
	}
	best := groups[0]
	for _, group := range groups[1:] {
		if len(group.Episodes) > len(best.Episodes) {
			best = group
		}
	}
	chapters := make([]Chapter, 0, len(best.Episodes))
	for _, episode := range best.Episodes {
		key := strconv.Itoa(episode.Index)
		chapters = append(chapters, Chapter{
			ID: providerChapterID(sourceKanmMovie, sourceID, key), Source: sourceKanmMovie,
			Title: episode.Title, CurrentEpisode: rawEpisode(kanmovieEpisodeOrder(episode.Title, episode.Index)),
			VideoURL: fmt.Sprintf("kanmovie-play://%s/%s", sourceID, key),
			PageURL: fmt.Sprintf("%s/video/%s?ep=%s&from=0", kanmovieSiteBaseURL, url.PathEscape(sourceID), key),
			Referer: kanmovieSiteBaseURL + "/",
		})
	}
	drama.TotalEpisode, drama.EpisodeCount = len(chapters), len(chapters)
	drama.EpisodesVerified = len(chapters) > 0
	return drama, chapters, nil
}

func (d *Downloader) resolveKanmMovieMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceKanmMovie, sourceID, "")
	if !valid || source != sourceKanmMovie || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("西瓜影視播放分集資訊無效，請重新整理詳情")
	}
	key := strings.TrimPrefix(task.Chapter.ID, prefix)
	episodeIndex, err := strconv.Atoi(key)
	if err != nil || episodeIndex < 1 || episodeIndex > 100000 {
		return providerMedia{}, errors.New("西瓜影視播放集數無效")
	}
	backend, record, err := d.fetchKanmMovieDetailRecord(ctx, sourceID)
	if err != nil {
		return providerMedia{}, err
	}
	groups := parseKanmMoviePlayGroups(record)
	var options []providerMedia
	seen := map[string]bool{}
	for _, group := range groups {
		var selected *kanmovieEpisode
		for index := range group.Episodes {
			episode := &group.Episodes[index]
			if episode.Index == episodeIndex || strings.EqualFold(strings.TrimSpace(episode.Title), strings.TrimSpace(task.Chapter.Title)) {
				selected = episode
				break
			}
		}
		if selected == nil || !isProviderHTTPMediaURL(selected.URL) || seen[selected.URL] {
			continue
		}
		seen[selected.URL] = true
		options = append(options, providerMedia{URL: selected.URL, Referer: backend.home})
	}
	if len(options) == 0 {
		return providerMedia{}, errors.New("西瓜影視目前找不到這一集的有效播放網址")
	}
	primary := options[0]
	if len(options) > 1 {
		primary.Variants = append([]providerMedia(nil), options[1:]...)
	}
	return primary, nil
}
