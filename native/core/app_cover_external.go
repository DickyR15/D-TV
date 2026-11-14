package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
)

const (
	bangumiSubjectSearchURL = "https://api.bgm.tv/v0/search/subjects"
	bangumiPosterSearchLimit = 20
)

// bangumiPosterBaseTitle allows an explicitly season-suffixed source title to
// match a database entry for the parent series. The caller still enforces
// exact normalized title, allowed media type, release year and uniqueness.
func bangumiPosterBaseTitle(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if index := strings.LastIndex(value, "第"); index >= 0 && strings.HasSuffix(value, "季") {
		season := strings.TrimSpace(value[index+len("第") : len(value)-len("季")])
		valid := season != ""
		for _, character := range season {
			if !strings.ContainsRune("0123456789一二三四五六七八九十百零〇两兩", character) {
				valid = false
				break
			}
		}
		if valid {
			value = strings.TrimSpace(value[:index])
			lower = strings.ToLower(value)
		}
	}
	if index := strings.LastIndex(lower, "season"); index >= 0 {
		season := strings.TrimSpace(value[index+len("season"):])
		valid := season != ""
		for _, character := range season {
			if character < '0' || character > '9' {
				valid = false
				break
			}
		}
		if valid {
			value = strings.TrimSpace(value[:index])
			lower = strings.ToLower(value)
		}
	}
	if index := strings.LastIndex(lower, "s"); index >= 0 {
		season := lower[index+1:]
		valid := season != ""
		for _, character := range season {
			if character < '0' || character > '9' {
				valid = false
				break
			}
		}
		if valid {
			value = strings.TrimSpace(value[:index])
		}
	}
	// Some Chinese catalogues write a sequel number without the word "季"
	// (for example, "這是我的西遊2"). Treat one/two trailing digits as a
	// season marker only when the preceding character is Han.
	runes := []rune(strings.TrimSpace(value))
	digitStart := len(runes)
	for digitStart > 0 && runes[digitStart-1] >= '0' && runes[digitStart-1] <= '9' {
		digitStart--
	}
	if digitStart > 0 && len(runes)-digitStart > 0 && len(runes)-digitStart <= 2 {
		prefix := strings.TrimSpace(string(runes[:digitStart]))
		prefixRunes := []rune(prefix)
		if len(prefixRunes) > 0 && unicode.Is(unicode.Han, prefixRunes[len(prefixRunes)-1]) {
			value = prefix
		}
	}
	return strings.TrimSpace(value)
}

type bangumiPosterSubject struct {
	ID     int               `json:"id"`
	Type   int               `json:"type"`
	Name   string            `json:"name"`
	NameCN string            `json:"name_cn"`
	Date   string            `json:"date"`
	Images map[string]string `json:"images"`
}

type bangumiPosterSearchResponse struct {
	Data []bangumiPosterSubject `json:"data"`
}

type bangumiPosterSearchRequest struct {
	Keyword string `json:"keyword"`
	Sort    string `json:"sort"`
	Filter  struct {
		Type []int `json:"type,omitempty"`
		NSFW bool  `json:"nsfw"`
	} `json:"filter"`
}

const (
	doubanSubjectPageURL = "https://movie.douban.com/subject/"
	doubanMobileDetailURL = "https://m.douban.com/rexxar/api/v2/movie/"
	doubanMobileUserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1"
)

// validDoubanSubjectID rejects empty, placeholder-zero and malformed IDs.
// Duoduo uses the string "0" when no Douban subject is linked to a title.
func validDoubanSubjectID(id string) bool {
	id = strings.TrimSpace(id)
	if len(id) < 6 || len(id) > 12 || strings.Trim(id, "0") == "" {
		return false
	}
	for _, character := range id {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// doubanPosterAddress uses the mobile Rexxar detail endpoint for the exact
// Douban ID supplied by Duoduo. The desktop subject page is a Vue shell and
// may return an anti-bot page without og:image even when the ID is valid.
func (d *Downloader) doubanPosterAddress(ctx context.Context, drama nativeDrama) (string, error) {
	id := strings.TrimSpace(drama.DoubanID)
	if !validDoubanSubjectID(id) {
		return "", errors.New("豆瓣作品 ID 無效")
	}
	if d == nil || d.client == nil {
		return "", errors.New("豆瓣海報查詢用戶端尚未初始化")
	}

	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	endpoint := doubanMobileDetailURL + url.PathEscape(id)
	request, err := http.NewRequestWithContext(
		context.WithValue(work, nativeCoverNetworkKey{}, true),
		http.MethodGet, endpoint, nil,
	)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", doubanMobileUserAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Referer", "https://m.douban.com/movie/subject/"+id+"/")
	request.Header.Set("Origin", "https://m.douban.com")

	client := *d.client
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("讀取豆瓣行動版詳情失敗：%w", publicError(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("豆瓣行動版詳情回應 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	if err != nil {
		return "", err
	}
	if len(body) > 2<<20 {
		return "", errors.New("豆瓣行動版詳情超過大小限制")
	}
	var detail map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&detail); err != nil {
		return "", errors.New("豆瓣行動版詳情不是有效 JSON")
	}

	// Refuse a response for a different subject before reading any artwork.
	if returnedID := duoduoString(detail["id"]); returnedID != "" && returnedID != id {
		return "", errors.New("豆瓣詳情 ID 與官網提供的作品 ID 不符")
	}
	pic, _ := detail["pic"].(map[string]any)
	candidates := []string{
		duoduoString(pic["normal"]),
		duoduoString(detail["cover_url"]),
	}
	for _, raw := range candidates {
		address := validDoubanPosterAddress(raw)
		if address != "" {
			return address, nil
		}
	}
	return "", errors.New("豆瓣行動版詳情沒有可用的官方海報")
}

// Only allow public Douban photo CDN poster paths. The poster URL is later
// fetched by the native downloader, so accepting arbitrary hosts here would
// turn metadata parsing into an SSRF vector.
func validDoubanPosterAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Scheme != "https" ||
		parsed.User != nil || parsed.Port() != "" ||
		!strings.HasPrefix(parsed.Path, "/view/photo/") {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(host, ".doubanio.com") {
		return ""
	}
	cdnLabel := strings.TrimSuffix(host, ".doubanio.com")
	if cdnLabel != "img" {
		if !strings.HasPrefix(cdnLabel, "img") {
			return ""
		}
		for _, character := range strings.TrimPrefix(cdnLabel, "img") {
			if character < '0' || character > '9' {
				return ""
			}
		}
	}
	ext := strings.ToLower(path.Ext(parsed.Path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
		return parsed.String()
	default:
		return ""
	}
}

// posterTitleVariants adds only conservative, title-like alternatives. It
// does not accept fuzzy similarity: every external candidate must still equal
// one of these normalized variants exactly.
func posterTitleVariants(value string) []string {
	value = strings.TrimSpace(value)
	withoutYear := value
	for _, pair := range [][2]string{{"(", ")"}, {"（", "）"}, {"[", "]"}, {"【", "】"}} {
		if !strings.HasSuffix(withoutYear, pair[1]) {
			continue
		}
		index := strings.LastIndex(withoutYear, pair[0])
		if index < 0 {
			continue
		}
		suffix := strings.TrimSpace(withoutYear[index+len(pair[0]) : len(withoutYear)-len(pair[1])])
		if len(suffix) == 4 && bangumiPosterYear(suffix) != "" {
			withoutYear = strings.TrimSpace(withoutYear[:index])
			break
		}
	}
	candidates := []string{value, bangumiPosterBaseTitle(value), withoutYear}
	for _, separator := range []string{" / ", "／", "｜", " | "} {
		if strings.Contains(value, separator) {
			for _, part := range strings.Split(value, separator) {
				candidates = append(candidates, strings.TrimSpace(part))
			}
		}
	}
	seen := map[string]bool{}
	var result []string
	for _, candidate := range candidates {
		normalized := normalizeBangumiPosterTitle(candidate)
		if normalized == "" || len([]rune(normalized)) < 2 || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, strings.TrimSpace(candidate))
		if len(result) >= 4 {
			break
		}
	}
	return result
}

func nativePosterTitleMatches(sourceTitle string, candidates ...string) bool {
	wanted := map[string]bool{}
	for _, variant := range posterTitleVariants(sourceTitle) {
		wanted[normalizeBangumiPosterTitle(variant)] = true
	}
	for _, candidate := range candidates {
		if normalized := normalizeBangumiPosterTitle(candidate); normalized != "" && wanted[normalized] {
			return true
		}
	}
	return false
}

func nativePosterTypeCompatible(drama nativeDrama, kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "movie" && kind != "tv" && kind != "tv_show" && kind != "tv series" {
		return false
	}
	category := strings.ToLower(strings.TrimSpace(drama.Category + " " + strings.Join(drama.Tags, " ")))
	isMovie := bangumiPosterContainsAny(category, "電影", "电影", "movie", "film")
	isTV := bangumiPosterContainsAny(category, "劇集", "剧集", "電視劇", "电视剧", "真人劇", "真人剧", "韓劇", "韩剧", "日劇", "日剧", "美劇", "美剧", "短劇", "短剧", "series", "tv")
	isAnime := bangumiPosterContainsAny(category, "動漫", "動畫", "动画", "番劇", "番剧", "漫劇", "漫剧", "anime")
	isVariety := bangumiPosterContainsAny(category, "綜藝", "综艺", "真人秀", "實境秀", "真人實境", "variety")
	if isAnime || isVariety || (!isMovie && !isTV) {
		return true
	}
	if isMovie {
		return kind == "movie"
	}
	return kind == "tv" || kind == "tv_show" || kind == "tv series"
}

// doubanSuggestedPosterAddress uses Douban's public title-suggestion endpoint
// when a provider catalogue omitted its Douban ID. It only accepts exact
// normalized title matches with a compatible work type and release year.
func (d *Downloader) doubanSuggestedPosterAddress(ctx context.Context, drama nativeDrama) (string, error) {
	if d == nil || d.client == nil {
		return "", errors.New("豆瓣片名搜尋用戶端尚未初始化")
	}
	if strings.TrimSpace(drama.Title) == "" {
		return "", errors.New("缺少作品名稱，略過豆瓣片名搜尋")
	}
	searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	matches := map[string]string{}
	var lastErr error
	for _, keyword := range posterTitleVariants(drama.Title) {
		if err := searchCtx.Err(); err != nil {
			return "", err
		}
		endpoint, err := url.Parse("https://movie.douban.com/j/subject_suggest")
		if err != nil {
			return "", err
		}
		query := endpoint.Query()
		query.Set("q", keyword)
		endpoint.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(
			context.WithValue(searchCtx, nativeCoverNetworkKey{}, true),
			http.MethodGet, endpoint.String(), nil,
		)
		if err != nil {
			return "", err
		}
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Referer", "https://movie.douban.com/")
		request.Header.Set("Origin", "https://movie.douban.com")
		request.Header.Set("X-Requested-With", "XMLHttpRequest")
		client := *d.client
		response, requestErr := client.Do(request)
		if requestErr != nil {
			lastErr = requestErr
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("豆瓣片名搜尋回應 HTTP %d", response.StatusCode)
			continue
		}
		if len(body) > 1<<20 {
			lastErr = errors.New("豆瓣片名搜尋回應超過大小限制")
			continue
		}
		var rows []map[string]any
		if err := json.Unmarshal(body, &rows); err != nil {
			lastErr = errors.New("豆瓣片名搜尋回應格式無效")
			continue
		}
		for _, row := range rows {
			id := strings.TrimSpace(duoduoString(row["id"]))
			title := strings.TrimSpace(duoduoString(row["title"]))
			kind := strings.TrimSpace(duoduoString(row["type"]))
			year := strings.TrimSpace(duoduoString(row["year"]))
			if !validDoubanSubjectID(id) || title == "" ||
				!nativePosterTypeCompatible(drama, kind) ||
				!nativePosterTitleMatches(drama.Title, title) {
				continue
			}
			sourceYear := bangumiPosterYear(drama.OnlineDate)
			candidateYear := bangumiPosterYear(year)
			if sourceYear != "" && candidateYear != "" && sourceYear != candidateYear {
				continue
			}
			address := validDoubanPosterAddress(firstNonEmpty(
				duoduoString(row["img"]), duoduoString(row["pic"]),
			))
			if address != "" {
				matches[id] = address
			}
		}
	}
	if len(matches) == 1 {
		for _, address := range matches {
			return address, nil
		}
	}
	if len(matches) > 1 {
		return "", errors.New("豆瓣片名搜尋有多個同名候選，為避免錯配已略過")
	}
	if lastErr != nil {
		return "", fmt.Errorf("豆瓣片名搜尋失敗：%w", lastErr)
	}
	return "", errors.New("豆瓣找不到片名、類型與年份相符的海報")
}

type nativeVerifiedOfficialPoster struct {
	title      string
	year       string
	address    string
	source     string
	sourceID        string
	preferred       bool
	overrideProvider bool
	workType        string
	titleAliases    []string
}

// These two assets were independently identified from programme-owner /
// official-platform materials. They are used only for an exact Chinese title
// and the variety/real-world work type, after normal provider artwork fails.
// "這是我的西遊 第二季" intentionally has no year gate: Duoduo currently
// labels this exact second-season title as 2025, while Youku's official
// catalogue identifies the second season as a 2026 programme.
var nativeVerifiedOfficialPosters = []nativeVerifiedOfficialPoster{
	{
		// Duoduo's image for 永生 returns a valid anti-hotlink notice graphic.
		// Override only the exact Duoduo anime title and 2022 series record with
		// Bilibili's official programme artwork.
		title: "永生",
		titleAliases: []string{"永生之無盡仙途", "永生之无尽仙途"},
		source: sourceDuoduo,
		year: "2022",
		workType: "anime",
		preferred: true,
		overrideProvider: true,
		address: "https://i0.hdslb.com/bfs/bangumi/image/1c697e41f5e2524b2610ec895cead5c25f494e87.png",
	},
	{
		title: "花兒與少年同心季",
		year: "2025",
		address: "https://k.sinaimg.cn/n/sinakd20250816s/450/w800h450/20250816/ec1c-4c5fc3ef466113eb1d27a75cc8c37fcf.jpg/w700d1q75cms.jpg?by=cms_fixed_width",
	},
	{
		title: "這是我的西遊第二季",
		address: "https://live.ykimg.com/image/545880B90349426B93A8696CA9CC578B?x-oss-process=image%2Fresize%2Cm_fixed%2Cw_1278%2Ch_720%2Climit_0",
	},
	{
		// Duoduo ID 87221 is Single's Inferno Season 5 (Netflix, 2026).
		// Use the season-specific Netflix key art, restricted to this exact
		// source ID and title to prevent another season from matching.
		title: "單身即地獄第五季",
		titleAliases: []string{"单身即地狱第五季", "單身即地獄 第五季", "单身即地狱 第五季"},
		source: sourceDuoduo,
		sourceID: "87221",
		preferred: true,
		address: "https://resource02.ulifestyle.com.hk/ulcms/content/article/image/w1280/2026/01/20260106180130_adf32c82e8e9ede7a5f96d795e9f224666da43bd.jpg",
	},
	{
		// Duoduo ID 59902 is the exact MangoTV programme record for
		// 明星大侦探 第二季; use that season's programme artwork.
		title: "明星大偵探第二季",
		titleAliases: []string{"明星大侦探第二季", "明星大偵探 第二季", "明星大侦探 第二季"},
		source: sourceDuoduo,
		sourceID: "59902",
		preferred: true,
		address: "https://4img.hitv.com/preview/internettv/sp_images/ott/2018/zongyi/309556/20180328100639870-new.jpg",
	},
	{
		// Duoduo ID 78011 is the 2014 Korean film Obsessed (인간중독).
		// Use the distributor's poster, restricted to this exact source ID.
		title: "人間中毒",
		titleAliases: []string{"人间中毒"},
		source: sourceDuoduo,
		sourceID: "78011",
		preferred: true,
		address: "https://img.its-new.co.kr/uploaded/poster/264/poster_368.jpg",
	},
	{
		// Duoduo ID 88129 is the exact 9-1-1 Season 1 catalogue entry (2018).
		// TVmaze lists this season separately and provides season-specific key art.
		// Restrict the poster to this exact source ID, title, TV category and year.
		title: "緊急呼救第一季",
		titleAliases: []string{"紧急呼救第一季", "緊急呼救 第一季", "紧急呼救 第一季"},
		source: sourceDuoduo,
		sourceID: "88129",
		year: "2018",
		workType: "tv",
		preferred: true,
		overrideProvider: true,
		address: "https://static.tvmaze.com/uploads/images/medium_portrait/213/533462.jpg",
	},
	{
		// Duoduo ID 1192 is the 2025 Japanese anime デキちゃうまで婚.
		// Use the official programme site's key visual because the source CDN
		// has no public DNS record. Require the exact ID, anime category and year.
		title: "奉子成婚",
		source: sourceDuoduo,
		sourceID: "1192",
		year: "2025",
		workType: "anime",
		preferred: true,
		address: "https://dekichaumade-kon.af-original.com/wp-content/themes/template/img/visual.jpg",
	},
	{
		// Duoduo ID 66452 is the 2014 micro-film 逆愛2後會無期.
		// The archive page matches the exact title, synopsis, director and cast.
		// Keep this image as a fallback only for this exact source record.
		title: "逆愛2後會無期",
		titleAliases: []string{"逆爱2后会无期", "逆愛2：後會無期", "逆爱2：后会无期"},
		source: sourceDuoduo,
		sourceID: "66452",
		address: "https://www.jendow.com.tw/img/5/105/nBnauM3X1gTO1AzM4MzMzIzM3QTM5YzNyITOzQTNwAzMwIzLzMzL3YzLt92YucmbvRWdo5Cd0FmL0E2LvoDc0RHa.jpg",
	},
}

func normalizeBangumiPosterExactTitle(value string) string {
	value = bangumiPosterChineseVariants.Replace(strings.TrimSpace(value))
	var normalized strings.Builder
	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}

func nativeVerifiedOfficialPosterEntry(drama nativeDrama) (nativeVerifiedOfficialPoster, bool) {
	title := normalizeBangumiPosterExactTitle(drama.Title)
	year := bangumiPosterYear(drama.OnlineDate)
	category := strings.ToLower(strings.TrimSpace(drama.Category + " " + strings.Join(drama.Tags, " ")))
	isVariety := bangumiPosterContainsAny(
		category,
		"綜藝", "综艺", "綜藝節目", "综艺节目",
		"真人秀", "實境秀", "真人實境", "真人实境",
		"脫口秀", "脱口秀", "variety",
	)
	source, sourceID, hasSourceID := splitProviderDramaID(drama.ID)
	for _, candidate := range nativeVerifiedOfficialPosters {
		matchesTitle := title == normalizeBangumiPosterExactTitle(candidate.title)
		if !matchesTitle {
			for _, alias := range candidate.titleAliases {
				if title == normalizeBangumiPosterExactTitle(alias) {
					matchesTitle = true
					break
				}
			}
		}
		if !matchesTitle {
			continue
		}
		if candidate.source != "" && (!hasSourceID || source != candidate.source) {
			continue
		}
		if candidate.sourceID != "" && (!hasSourceID || sourceID != candidate.sourceID) {
			continue
		}
		if candidate.workType == "anime" {
			// Anime artwork must match the exact source, programme title,
			// category and release year to avoid selecting a different season.
			if !nativePosterIsAnime(drama) || year == "" || year != candidate.year {
				continue
			}
		} else if candidate.workType == "tv" {
			// Season-specific TV artwork is accepted only for the exact TV
			// catalogue entry and its verified premiere year.
			if !nativePosterIsTVSeries(drama) || year == "" || year != candidate.year {
				continue
			}
		} else if candidate.source == "" && candidate.sourceID == "" {
			// Unscoped curated programme artwork remains variety-only.
			if !isVariety {
				continue
			}
			if candidate.year != "" && year != candidate.year {
				continue
			}
		}
		parsed, err := url.Parse(candidate.address)
		if err != nil || !validNativeCoverURL(parsed) {
			continue
		}
		return candidate, true
	}
	return nativeVerifiedOfficialPoster{}, false
}

func nativeVerifiedOfficialPosterAddress(drama nativeDrama) (string, bool) {
	candidate, ok := nativeVerifiedOfficialPosterEntry(drama)
	if !ok {
		return "", false
	}
	return candidate.address, true
}

func nativePreferredOfficialPosterAddress(drama nativeDrama) (string, bool) {
	candidate, ok := nativeVerifiedOfficialPosterEntry(drama)
	if !ok || !candidate.preferred {
		return "", false
	}
	return candidate.address, true
}

func nativePosterIsAnime(drama nativeDrama) bool {
	category := strings.ToLower(strings.TrimSpace(drama.Category + " " + strings.Join(drama.Tags, " ")))
	return bangumiPosterContainsAny(category, "動漫", "動畫", "动画", "番劇", "番剧", "漫劇", "漫剧", "anime")
}

func nativePosterIsTVSeries(drama nativeDrama) bool {
	category := strings.ToLower(strings.TrimSpace(drama.Category + " " + strings.Join(drama.Tags, " ")))
	if nativePosterIsAnime(drama) || bangumiPosterContainsAny(category, "電影", "电影", "movie", "film") {
		return false
	}
	return bangumiPosterContainsAny(
		category,
		"劇集", "剧集", "電視劇", "电视剧", "真人劇", "真人剧",
		"韓劇", "韩剧", "日劇", "日剧", "美劇", "美剧",
		"台劇", "台剧", "港劇", "港剧", "短劇", "短剧",
		"綜藝", "综艺", "真人秀", "實境秀", "variety", "series", "tv",
	)
}

func validAniListPosterAddress(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Scheme != "https" ||
		parsed.User != nil || parsed.Port() != "" ||
		!strings.HasPrefix(parsed.Path, "/file/anilistcdn/") {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "anilist.co" && !strings.HasSuffix(host, ".anilist.co") {
		return ""
	}
	return parsed.String()
}

func (d *Downloader) aniListPosterAddress(ctx context.Context, drama nativeDrama) (string, error) {
	if d == nil || d.client == nil {
		return "", errors.New("AniList 海報搜尋用戶端尚未初始化")
	}
	if !nativePosterIsAnime(drama) {
		return "", errors.New("作品類型不是動畫，略過 AniList 搜尋")
	}
	const queryText = "query ($search: String!) { Page(page: 1, perPage: 25) { media(search: $search, type: ANIME, isAdult: false) { id title { native romaji english } synonyms startDate { year } coverImage { extraLarge large } } } }"
	searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	matches := map[string]string{}
	var lastErr error
	for _, keyword := range posterTitleVariants(drama.Title) {
		if err := searchCtx.Err(); err != nil {
			return "", err
		}
		payload, err := json.Marshal(map[string]any{
			"query": queryText,
			"variables": map[string]any{"search": keyword},
		})
		if err != nil {
			return "", err
		}
		request, err := http.NewRequestWithContext(
			context.WithValue(searchCtx, nativeCoverNetworkKey{}, true),
			http.MethodPost, "https://graphql.anilist.co", strings.NewReader(string(payload)),
		)
		if err != nil {
			return "", err
		}
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/json")
		client := *d.client
		response, requestErr := client.Do(request)
		if requestErr != nil {
			lastErr = requestErr
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("AniList 搜尋回應 HTTP %d", response.StatusCode)
			continue
		}
		if len(body) > 2<<20 {
			lastErr = errors.New("AniList 搜尋回應超過大小限制")
			continue
		}
		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			lastErr = errors.New("AniList 搜尋回應格式無效")
			continue
		}
		page := nestedMap(result, "data", "Page")
		for _, candidate := range duoduoRows(page["media"]) {
			id := strings.TrimSpace(duoduoString(candidate["id"]))
			title := nestedMap(candidate, "title")
			aliases := []string{
				duoduoString(title["native"]),
				duoduoString(title["romaji"]),
				duoduoString(title["english"]),
			}
			aliases = append(aliases, duoduoStrings(candidate["synonyms"])...)
			if id == "" || !nativePosterTitleMatches(drama.Title, aliases...) {
				continue
			}
			date := nestedMap(candidate, "startDate")
			sourceYear := bangumiPosterYear(drama.OnlineDate)
			candidateYear := bangumiPosterYear(duoduoString(date["year"]))
			if sourceYear != "" && candidateYear != "" && sourceYear != candidateYear {
				continue
			}
			cover := nestedMap(candidate, "coverImage")
			address := validAniListPosterAddress(firstNonEmpty(
				duoduoString(cover["extraLarge"]), duoduoString(cover["large"]),
			))
			if address != "" {
				matches[id] = address
			}
		}
	}
	if len(matches) == 1 {
		for _, address := range matches {
			return address, nil
		}
	}
	if len(matches) > 1 {
		return "", errors.New("AniList 有多個同名候選，為避免錯配已略過")
	}
	if lastErr != nil {
		return "", fmt.Errorf("AniList 海報搜尋失敗：%w", lastErr)
	}
	return "", errors.New("AniList 找不到片名與年份相符的動畫海報")
}

func validTVMazePosterAddress(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Scheme != "https" ||
		parsed.User != nil || parsed.Port() != "" ||
		!strings.HasPrefix(parsed.Path, "/uploads/images/") {
		return ""
	}
	if !strings.EqualFold(parsed.Hostname(), "static.tvmaze.com") {
		return ""
	}
	return parsed.String()
}

func (d *Downloader) tvMazePosterAddress(ctx context.Context, drama nativeDrama) (string, error) {
	if d == nil || d.client == nil {
		return "", errors.New("TVmaze 海報搜尋用戶端尚未初始化")
	}
	if !nativePosterIsTVSeries(drama) {
		return "", errors.New("作品類型不是電視劇或綜藝，略過 TVmaze 搜尋")
	}
	searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	matches := map[string]string{}
	var lastErr error
	for _, keyword := range posterTitleVariants(drama.Title) {
		if err := searchCtx.Err(); err != nil {
			return "", err
		}
		endpoint, err := url.Parse("https://api.tvmaze.com/search/shows")
		if err != nil {
			return "", err
		}
		query := endpoint.Query()
		query.Set("q", keyword)
		endpoint.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(
			context.WithValue(searchCtx, nativeCoverNetworkKey{}, true),
			http.MethodGet, endpoint.String(), nil,
		)
		if err != nil {
			return "", err
		}
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Accept", "application/json")
		client := *d.client
		response, requestErr := client.Do(request)
		if requestErr != nil {
			lastErr = requestErr
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("TVmaze 搜尋回應 HTTP %d", response.StatusCode)
			continue
		}
		if len(body) > 1<<20 {
			lastErr = errors.New("TVmaze 搜尋回應超過大小限制")
			continue
		}
		var rows []map[string]any
		if err := json.Unmarshal(body, &rows); err != nil {
			lastErr = errors.New("TVmaze 搜尋回應格式無效")
			continue
		}
		for _, row := range rows {
			show := nestedMap(row, "show")
			id := strings.TrimSpace(duoduoString(show["id"]))
			if id == "" || id == "0" || !nativePosterTitleMatches(drama.Title, duoduoString(show["name"])) {
				continue
			}
			sourceYear := bangumiPosterYear(drama.OnlineDate)
			candidateYear := bangumiPosterYear(duoduoString(show["premiered"]))
			if sourceYear != "" && candidateYear != "" && sourceYear != candidateYear {
				continue
			}
			image := nestedMap(show, "image")
			address := validTVMazePosterAddress(firstNonEmpty(
				duoduoString(image["original"]), duoduoString(image["medium"]),
			))
			if address != "" {
				matches[id] = address
			}
		}
	}
	if len(matches) == 1 {
		for _, address := range matches {
			return address, nil
		}
	}
	if len(matches) > 1 {
		return "", errors.New("TVmaze 有多個同名候選，為避免錯配已略過")
	}
	if lastErr != nil {
		return "", fmt.Errorf("TVmaze 海報搜尋失敗：%w", lastErr)
	}
	return "", errors.New("TVmaze 找不到片名與年份相符的影集海報")
}

// externalPosterAddress tries independent catalogues in a conservative order.
// Each catalogue must still return an exact title match, a compatible work type,
// and a non-conflicting release year. Ambiguous titles are never auto-selected.
func (d *Downloader) externalPosterAddress(ctx context.Context, drama nativeDrama) (string, error) {
	// Prefer exact, source-verified programme artwork over a third-party search.
	// The original provider poster is still attempted first by the cover cache.
	if address, ok := nativeVerifiedOfficialPosterAddress(drama); ok {
		return address, nil
	}
	var failures []error
	if validDoubanSubjectID(drama.DoubanID) {
		address, doubanErr := d.doubanPosterAddress(ctx, drama)
		if doubanErr == nil {
			return address, nil
		}
		// A response for a different Douban subject is a hard identity mismatch,
		// not an availability failure. Never replace that exact ID with a
		// title-only candidate when the source metadata itself conflicts.
		if strings.Contains(doubanErr.Error(), "豆瓣詳情 ID 與官網提供的作品 ID 不符") {
			return "", doubanErr
		}
		failures = append(failures, fmt.Errorf("豆瓣精準海報查詢失敗：%w", doubanErr))
	}
	if address, err := d.bangumiPosterAddress(ctx, drama); err == nil {
		return address, nil
	} else {
		failures = append(failures, fmt.Errorf("Bangumi 備援查詢失敗：%w", err))
	}
	if address, err := d.doubanSuggestedPosterAddress(ctx, drama); err == nil {
		return address, nil
	} else {
		failures = append(failures, fmt.Errorf("豆瓣片名備援查詢失敗：%w", err))
	}
	if nativePosterIsAnime(drama) {
		if address, err := d.aniListPosterAddress(ctx, drama); err == nil {
			return address, nil
		} else {
			failures = append(failures, fmt.Errorf("AniList 備援查詢失敗：%w", err))
		}
	}
	if nativePosterIsTVSeries(drama) {
		if address, err := d.tvMazePosterAddress(ctx, drama); err == nil {
			return address, nil
		} else {
			failures = append(failures, fmt.Errorf("TVmaze 備援查詢失敗：%w", err))
		}
	}
	return "", errors.Join(failures...)
}

func (d *Downloader) bangumiPosterAddress(ctx context.Context, drama nativeDrama) (string, error) {
	title := strings.TrimSpace(drama.Title)
	if title == "" {
		return "", errors.New("缺少作品名稱，略過外部海報搜尋")
	}
	types := bangumiPosterSubjectTypes(drama)
	if len(types) == 0 {
		return "", errors.New("作品類型不適合外部海報配對")
	}
	if d == nil || d.client == nil {
		return "", errors.New("外部海報搜尋用戶端尚未初始化")
	}

	endpoint, err := url.Parse(bangumiSubjectSearchURL)
	if err != nil {
		return "", err
	}
	query := endpoint.Query()
	query.Set("limit", fmt.Sprint(bangumiPosterSearchLimit))
	query.Set("offset", "0")
	endpoint.RawQuery = query.Encode()

	payload := bangumiPosterSearchRequest{Keyword: title, Sort: "match"}
	payload.Filter.Type = types
	payload.Filter.NSFW = false
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	searchCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(
		context.WithValue(searchCtx, nativeCoverNetworkKey{}, true),
		http.MethodPost,
		endpoint.String(),
		strings.NewReader(string(body)),
	)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")

	client := *d.client
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("外部海報搜尋回應 HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", err
	}
	var result bangumiPosterSearchResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return "", errors.New("外部海報搜尋回應格式無效")
	}

	queryTitle := normalizeBangumiPosterTitle(title)
	matches := make([]bangumiPosterSubject, 0, len(result.Data))
	allowedTypes := make(map[int]bool, len(types))
	for _, subjectType := range types {
		allowedTypes[subjectType] = true
	}
	for _, candidate := range result.Data {
		if candidate.ID <= 0 || !allowedTypes[candidate.Type] {
			continue
		}
		if normalizeBangumiPosterTitle(candidate.NameCN) != queryTitle &&
			normalizeBangumiPosterTitle(candidate.Name) != queryTitle {
			continue
		}
		matches = append(matches, candidate)
	}

	// A source release year can disambiguate same-title seasons or versions.
	// When both sides expose a year, a mismatch is never accepted.
	if year := bangumiPosterYear(drama.OnlineDate); year != "" {
		var yearMatches, unknownYearMatches []bangumiPosterSubject
		for _, candidate := range matches {
			candidateYear := bangumiPosterYear(candidate.Date)
			switch candidateYear {
			case year:
				yearMatches = append(yearMatches, candidate)
			case "":
				unknownYearMatches = append(unknownYearMatches, candidate)
			}
		}
		switch {
		case len(yearMatches) > 0:
			matches = yearMatches
		case len(unknownYearMatches) > 0:
			matches = unknownYearMatches
		default:
			return "", errors.New("外部作品年份與原片資料不符")
		}
	}
	if len(matches) != 1 {
		if len(matches) == 0 {
			return "", errors.New("找不到片名與類型完全相符的外部海報")
		}
		return "", errors.New("外部海報有多個同名候選，為避免錯配已略過")
	}

	candidate := matches[0]
	for _, key := range []string{"large", "common", "medium", "small"} {
		address := strings.TrimSpace(candidate.Images[key])
		if strings.HasPrefix(address, "//") {
			address = "https:" + address
		}
		parsed, parseErr := url.Parse(address)
		if parseErr != nil || !validNativeCoverURL(parsed) {
			continue
		}
		if strings.Contains(strings.ToLower(parsed.Path), "no_icon_subject") {
			continue
		}
		return parsed.String(), nil
	}
	return "", errors.New("外部作品條目沒有可用海報")
}

func bangumiPosterSubjectTypes(drama nativeDrama) []int {
	category := strings.ToLower(strings.TrimSpace(drama.Category + " " + strings.Join(drama.Tags, " ")))
	switch {
	case bangumiPosterContainsAny(category, "綜藝", "综艺", "綜藝節目", "综艺节目", "脫口秀", "脱口秀", "真人秀", "實境秀", "真人實境", "真人实境", "variety"):
		// Bangumi classifies variety programmes under real-world media (type 6).
		// Exact title/year checks below still prevent unrelated matches.
		return []int{6}
	case bangumiPosterContainsAny(category, "動漫", "動畫", "动画", "番劇", "番剧", "漫劇", "漫剧", "anime"):
		return []int{2}
	case bangumiPosterContainsAny(category, "電影", "电影", "劇集", "剧集", "電視劇", "电视剧", "真人劇", "真人剧", "短劇", "短剧", "movie", "tv"):
		return []int{6}
	default:
		return []int{2, 6}
	}
}

// Bangumi stores many Chinese subject names in Simplified Chinese while
// source catalogues and the UI may use Traditional Chinese. Normalize the
// common title characters before strict equality matching.
var bangumiPosterChineseVariants = strings.NewReplacer(
	"与", "與", "儿", "兒", "这", "這", "游", "遊",
	"综", "綜", "艺", "藝", "实", "實",
	"脱", "脫", "说", "說", "剧", "劇", "电", "電",
	"视", "視", "传", "傳", "华", "華", "国", "國",
	"乐", "樂", "门", "門", "云", "雲", "龙", "龍",
	"风", "風", "无", "無", "独", "獨", "学", "學",
	"万", "萬", "业", "業", "东", "東", "丝", "絲",
	"两", "兩", "严", "嚴", "个", "個", "丰", "豐",
	"临", "臨", "为", "為", "丽", "麗", "举", "舉",
	"义", "義", "乌", "烏", "乔", "喬", "习", "習",
	"乡", "鄉", "书", "書", "买", "買", "乱", "亂",
	"争", "爭", "亚", "亞", "产", "產", "亲", "親",
	"仅", "僅", "从", "從", "仓", "倉", "仪", "儀",
	"们", "們", "价", "價", "众", "眾", "优", "優",
	"会", "會", "伟", "偉", "伤", "傷", "伦", "倫",
	"体", "體", "余", "餘", "侣", "侶", "侧", "側",
	"侨", "僑", "侦", "偵", "俩", "倆", "俭", "儉",
	"债", "債", "倾", "傾", "储", "儲", "兑", "兌",
	"党", "黨", "兰", "蘭", "关", "關", "兴", "興",
	"养", "養", "兽", "獸", "冈", "岡", "册", "冊",
	"写", "寫", "军", "軍", "农", "農", "冯", "馮",
	"冲", "沖", "决", "決", "况", "況", "冻", "凍",
	"净", "淨", "凉", "涼", "减", "減", "凤", "鳳",
	"凭", "憑", "凯", "凱", "击", "擊", "凿", "鑿",
	"刘", "劉", "则", "則", "刚", "剛", "创", "創",
	"删", "刪", "别", "別", "剂", "劑", "剑", "劍",
	"剧", "劇", "劝", "勸", "办", "辦", "务", "務",
	"动", "動", "励", "勵", "劲", "勁", "劳", "勞",
	"势", "勢", "勋", "勳", "区", "區", "医", "醫",
	"卖", "賣", "厉", "厲", "压", "壓", "厂", "廠",
	"厅", "廳", "历", "歷", "厉", "厲", "县", "縣",
	"双", "雙", "发", "發", "变", "變", "叹", "嘆",
	"叶", "葉", "号", "號", "听", "聽", "启", "啟",
	"员", "員", "呐", "吶", "呗", "唄", "咏", "詠",
	"咙", "嚨", "响", "響", "唤", "喚", "啸", "嘯",
	"喷", "噴", "团", "團", "园", "園", "围", "圍",
	"图", "圖", "圆", "圓", "圣", "聖", "场", "場",
	"坏", "壞", "块", "塊", "坚", "堅", "坛", "壇",
	"坝", "壩", "坟", "墳", "坠", "墜", "垄", "壟",
	"垒", "壘", "垦", "墾", "墙", "牆", "壮", "壯",
	"声", "聲", "处", "處", "备", "備", "复", "複",
	"够", "夠", "头", "頭", "夹", "夾", "夺", "奪",
	"奖", "獎", "奋", "奮", "妇", "婦", "妈", "媽",
	"孙", "孫", "学", "學", "宁", "寧", "实", "實",
	"宝", "寶", "审", "審", "宪", "憲", "宫", "宮",
	"宽", "寬", "宾", "賓", "寻", "尋", "导", "導",
	"寿", "壽", "将", "將", "层", "層", "属", "屬",
	"岁", "歲", "岂", "豈", "岛", "島", "岭", "嶺",
	"币", "幣", "师", "師", "帐", "帳", "带", "帶",
	"帮", "幫", "干", "幹", "庄", "莊", "庆", "慶",
	"庙", "廟", "库", "庫", "应", "應", "废", "廢",
	"开", "開", "异", "異", "弃", "棄", "张", "張",
	"弥", "彌", "弯", "彎", "弹", "彈", "归", "歸",
	"当", "當", "录", "錄", "彻", "徹", "忆", "憶",
	"怀", "懷", "态", "態", "总", "總", "恋", "戀",
	"恶", "惡", "惊", "驚", "惧", "懼", "惨", "慘",
	"惯", "慣", "戏", "戲", "战", "戰", "户", "戶",
	"扑", "撲", "执", "執", "扩", "擴", "扫", "掃",
	"扬", "揚", "扰", "擾", "抚", "撫", "抢", "搶",
	"护", "護", "报", "報", "担", "擔", "拟", "擬",
	"拢", "攏", "拣", "揀", "拥", "擁", "拨", "撥",
	"择", "擇", "挂", "掛", "挚", "摯", "损", "損",
	"换", "換", "据", "據", "掷", "擲", "摆", "擺",
	"摇", "搖", "摊", "攤", "撵", "攆", "敌", "敵",
	"数", "數", "斋", "齋", "斩", "斬", "断", "斷",
	"无", "無", "旧", "舊", "时", "時", "晓", "曉",
	"暂", "暫", "术", "術", "机", "機", "杀", "殺",
	"杂", "雜", "权", "權", "条", "條", "来", "來",
	"杨", "楊", "极", "極", "构", "構", "枪", "槍",
	"标", "標", "样", "樣", "树", "樹", "桥", "橋",
	"欢", "歡", "欧", "歐", "汉", "漢", "汤", "湯",
	"沟", "溝", "没", "沒", "泪", "淚", "洁", "潔",
	"浅", "淺", "浆", "漿", "泽", "澤", "济", "濟",
	"浓", "濃", "涛", "濤", "润", "潤", "涨", "漲",
	"湾", "灣", "湿", "濕", "灭", "滅", "灯", "燈",
	"灵", "靈", "灾", "災", "炉", "爐", "热", "熱",
	"爱", "愛", "爷", "爺", "牵", "牽", "牺", "犧",
	"状", "狀", "独", "獨", "猎", "獵", "猫", "貓",
	"现", "現", "环", "環", "电", "電", "画", "畫",
	"疗", "療", "疯", "瘋", "盘", "盤", "尽", "盡",
	"监", "監", "盖", "蓋", "盗", "盜", "睁", "睜",
	"矫", "矯", "矿", "礦", "码", "碼", "砖", "磚",
	"礼", "禮", "种", "種", "积", "積", "称", "稱",
	"稳", "穩", "穷", "窮", "竞", "競", "笔", "筆",
	"笼", "籠", "签", "簽", "简", "簡", "粮", "糧",
	"紧", "緊", "纪", "紀", "约", "約", "红", "紅",
	"纤", "纖", "纯", "純", "纷", "紛", "纸", "紙",
	"线", "線", "组", "組", "细", "細", "织", "織",
	"终", "終", "经", "經", "绿", "綠", "缀", "綴",
	"网", "網", "罗", "羅", "罚", "罰", "美", "美",
	"联", "聯", "聪", "聰", "肃", "肅", "肠", "腸",
	"肤", "膚", "脑", "腦", "腾", "騰", "舆", "輿",
	"舰", "艦", "苏", "蘇", "药", "藥", "获", "獲",
	"莲", "蓮", "蚀", "蝕", "补", "補", "装", "裝",
	"观", "觀", "规", "規", "觉", "覺", "览", "覽",
	"计", "計", "订", "訂", "认", "認", "让", "讓",
	"训", "訓", "议", "議", "讯", "訊", "记", "記",
	"讲", "講", "许", "許", "论", "論", "设", "設",
	"访", "訪", "证", "證", "评", "評", "识", "識",
	"诈", "詐", "诉", "訴", "诊", "診", "词", "詞",
	"译", "譯", "试", "試", "诗", "詩", "诚", "誠",
	"话", "話", "该", "該", "详", "詳", "语", "語",
	"误", "誤", "说", "說", "请", "請", "读", "讀",
	"课", "課", "谁", "誰", "调", "調", "谈", "談",
	"谋", "謀", "谢", "謝", "谣", "謠", "谦", "謙",
	"谨", "謹", "谱", "譜", "谷", "谷", "贝", "貝",
	"负", "負", "财", "財", "责", "責", "贤", "賢",
	"败", "敗", "账", "帳", "货", "貨", "质", "質",
	"贩", "販", "贫", "貧", "购", "購", "贮", "貯",
	"贯", "貫", "贴", "貼", "贵", "貴", "贸", "貿",
	"费", "費", "贺", "賀", "资", "資", "赛", "賽",
	"赞", "讚", "赵", "趙", "赶", "趕", "轻", "輕",
	"载", "載", "较", "較", "辅", "輔", "辆", "輛",
	"辉", "輝", "辩", "辯", "边", "邊", "迁", "遷",
	"过", "過", "还", "還", "进", "進", "远", "遠",
	"违", "違", "连", "連", "迟", "遲", "适", "適",
	"选", "選", "遗", "遺", "邮", "郵", "邻", "鄰",
	"郑", "鄭", "释", "釋", "鉴", "鑑", "针", "針",
	"钉", "釘", "钟", "鐘", "钢", "鋼", "钱", "錢",
	"铁", "鐵", "铃", "鈴", "铜", "銅", "银", "銀",
	"锅", "鍋", "键", "鍵", "锁", "鎖", "镜", "鏡",
	"长", "長", "门", "門", "闪", "閃", "闭", "閉",
	"问", "問", "闯", "闖", "闲", "閒", "间", "間",
	"闻", "聞", "阀", "閥", "阅", "閱", "队", "隊",
	"阳", "陽", "阴", "陰", "阵", "陣", "阶", "階",
	"际", "際", "陆", "陸", "陈", "陳", "降", "降",
	"险", "險", "随", "隨", "隐", "隱", "难", "難",
	"雾", "霧", "静", "靜", "顶", "頂", "项", "項",
	"顺", "順", "须", "須", "领", "領", "颂", "頌",
	"预", "預", "颁", "頒", "颇", "頗", "领", "領",
	"颈", "頸", "频", "頻", "颗", "顆", "题", "題",
	"颜", "顏", "额", "額", "风", "風", "飞", "飛",
	"饭", "飯", "饮", "飲", "饰", "飾", "饱", "飽",
	"馆", "館", "马", "馬", "驭", "馭", "驮", "馱",
	"驾", "駕", "骂", "罵", "骄", "驕", "验", "驗",
	"骑", "騎", "骗", "騙", "骚", "騷", "鱼", "魚",
	"鸟", "鳥", "鸡", "雞", "鸭", "鴨", "鹅", "鵝",
	"麦", "麥", "黄", "黃", "齐", "齊", "齿", "齒",
	"龙", "龍", "龟", "龜",
)

func normalizeBangumiPosterTitle(value string) string {
	value = bangumiPosterBaseTitle(value)
	value = bangumiPosterChineseVariants.Replace(value)
	var normalized strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}

func bangumiPosterYear(value string) string {
	value = strings.TrimSpace(value)
	for index := 0; index+4 <= len(value); index++ {
		year := value[index : index+4]
		if (strings.HasPrefix(year, "19") || strings.HasPrefix(year, "20")) &&
			year[2] >= '0' && year[2] <= '9' &&
			year[3] >= '0' && year[3] <= '9' {
			return year
		}
	}
	return ""
}

func bangumiPosterContainsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
