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

	work, cancel := context.WithTimeout(ctx, 8*time.Second)
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
	searchCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
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
	sourceID   string
	preferred  bool
	titleAliases []string
}

// These two assets were independently identified from programme-owner /
// official-platform materials. They are used only for an exact Chinese title
// and the variety/real-world work type, after normal provider artwork fails.
// "這是我的西遊 第二季" intentionally has no year gate: Duoduo currently
// labels this exact second-season title as 2025, while Youku's official
// catalogue identifies the second season as a 2026 programme.
var nativeVerifiedOfficialPosters = []nativeVerifiedOfficialPoster{
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
		// This exact Guipian record is titled Fast Cash / 歹路無可退.
		// The provider's own image URL is broken; use the sales agent's
		// official campaign artwork, restricted to this source ID and title.
		// Guipian's detail currently reports 2027, while Apple TV and LiTV
		// list the same production as 2026, so this exact-ID mapping has no
		// source-year gate.
		title: "歹路無可退",
		source: sourceGuipian,
		sourceID: "77333",
		address: "https://jingafilms.com/wp-content/uploads/2026/02/FastCash.jpg",
	},
	{
		// The source's 4.5 KB PNG is returned as a successful image but is not
		// what the user sees in D TV. Prefer Netflix's Season 2 press still for
		// this exact Guipian anime record, never for another title or source ID.
		title: "獨角獸學院秘密揭開第二章",
		titleAliases: []string{"独角兽学院秘密揭开第二章"},
		source: sourceGuipian,
		sourceID: "77681",
		preferred: true,
		address: "https://www.whats-on-netflix.com/wp-content/uploads/2026/09/Unicorn-Academy-Secrets-Revealed-n-S2-E1-00-10-43-15-1536x864.jpg",
	},
	{
		// Duoduo ID 1192 is the 2001 Fuji TV series できちゃった結婚
		// (Shotgun Marriage). Prefer the programme distributor's official
		// artwork because the source CDN is unavailable and Douban is blocked.
		// The mapping is restricted to this exact provider ID and title.
		title: "奉子成婚",
		source: sourceDuoduo,
		sourceID: "1192",
		preferred: true,
		address: "https://fujicreative.co.jp/forBuyers/wp-content/uploads/2022/06/2_0227.jpg",
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
		if candidate.source != "" || candidate.sourceID != "" {
			// Provider-specific artwork is safe only for the exact source record
			// and exact title; it does not broaden matching to unrelated sources.
			if !hasSourceID || source != candidate.source || sourceID != candidate.sourceID {
				continue
			}
		} else {
			// A Bangumi type-6 result can be either a movie or a television
			// programme, so general curated programme artwork is variety-only.
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
	const queryText = "query ($search: String!) { Page(page: 1, perPage: 25) { media(search: $search, type: ANIME, isAdult: false) { id title { native romaji english } startDate { year } coverImage { extraLarge large } } } }"
	searchCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
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
			if id == "" || !nativePosterTitleMatches(
				drama.Title,
				duoduoString(title["native"]),
				duoduoString(title["romaji"]),
				duoduoString(title["english"]),
			) {
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
	searchCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	matches := map[int]string{}
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
			id := intValue(show["id"])
			if id <= 0 || !nativePosterTitleMatches(drama.Title, duoduoString(show["name"])) {
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

// externalPosterAddress prefers Duoduo's exact Douban subject ID. If Douban
// denies the request or has no usable poster, Bangumi is a guarded fallback:
// its result must still match the exact title, allowed work type and release
// year, and ambiguous matches are rejected.
func (d *Downloader) externalPosterAddress(ctx context.Context, drama nativeDrama) (string, error) {
	// Prefer exact, source-verified programme artwork over a third-party search.
	// The original provider poster is still attempted first by the cover cache.
	if address, ok := nativeVerifiedOfficialPosterAddress(drama); ok {
		return address, nil
	}
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
		bangumiAddress, bangumiErr := d.bangumiPosterAddress(ctx, drama)
		if bangumiErr == nil {
			return bangumiAddress, nil
		}
		return "", errors.Join(
			fmt.Errorf("豆瓣精準海報查詢失敗：%w", doubanErr),
			fmt.Errorf("Bangumi 備援查詢失敗：%w", bangumiErr),
		)
	}
	return d.bangumiPosterAddress(ctx, drama)
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
	searchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
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
	"综", "綜", "艺", "藝", "实", "實", "境", "境",
	"脱", "脫", "说", "說", "剧", "劇", "电", "電",
	"视", "視", "传", "傳", "华", "華", "国", "國",
	"乐", "樂", "门", "門",
	"云", "雲", "龙", "龍", "风", "風", "无", "無",
	"独", "獨", "学", "學",
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
