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

type nativeVerifiedOfficialPoster struct {
	title   string
	year    string
	address string
	source  string
	sourceID string
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

func nativeVerifiedOfficialPosterAddress(drama nativeDrama) (string, bool) {
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
		if title != normalizeBangumiPosterExactTitle(candidate.title) {
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
		return parsed.String(), true
	}
	return "", false
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
