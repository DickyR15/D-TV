package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const (
	bangumiSubjectSearchURL = "https://api.bgm.tv/v0/search/subjects"
	bangumiPosterSearchLimit = 20
)

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

const doubanSubjectPageURL = "https://movie.douban.com/subject/"

func (d *Downloader) externalPosterAddress(ctx context.Context, drama nativeDrama) (string, error) {
	var doubanErr error
	if strings.TrimSpace(drama.DoubanID) != "" {
		address, err := d.doubanPosterAddress(ctx, drama)
		if err == nil && address != "" {
			return address, nil
		}
		doubanErr = err
	}
	address, bangumiErr := d.bangumiPosterAddress(ctx, drama)
	if bangumiErr == nil && address != "" {
		return address, nil
	}
	return "", errors.Join(doubanErr, bangumiErr)
}

// doubanPosterAddress follows the Douban subject ID supplied by Duoduo's
// official detail API. It never searches by title, and verifies the subject
// page's canonical URL before accepting its og:image poster.
func (d *Downloader) doubanPosterAddress(ctx context.Context, drama nativeDrama) (string, error) {
	id := strings.TrimSpace(drama.DoubanID)
	if len(id) < 6 || len(id) > 12 {
		return "", errors.New("豆瓣作品 ID 無效")
	}
	for _, character := range id {
		if character < '0' || character > '9' {
			return "", errors.New("豆瓣作品 ID 無效")
		}
	}
	if d == nil || d.client == nil {
		return "", errors.New("豆瓣海報搜尋用戶端尚未初始化")
	}

	pageURL := doubanSubjectPageURL + id + "/"
	work, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(
		context.WithValue(work, nativeCoverNetworkKey{}, true),
		http.MethodGet, pageURL, nil,
	)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	request.Header.Set("Referer", "https://movie.douban.com/")

	client := *d.client
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("讀取豆瓣作品頁失敗：%w", publicError(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("豆瓣作品頁回應 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	if err != nil {
		return "", err
	}
	if len(body) > 2<<20 {
		return "", errors.New("豆瓣作品頁超過大小限制")
	}

	canonicalURL := ""
	posterURL := ""
	for _, tag := range coverMetaTag.FindAllString(string(body), -1) {
		property := strings.ToLower(firstNonEmpty(extractAttr(tag, "property"), extractAttr(tag, "name")))
		switch property {
		case "og:url":
			canonicalURL = providerCoverAddress(extractAttr(tag, "content"), pageURL)
		case "og:image", "og:image:url":
			if posterURL == "" {
				posterURL = providerCoverAddress(extractAttr(tag, "content"), pageURL)
			}
		}
	}
	canonical, parseErr := url.Parse(canonicalURL)
	if parseErr != nil || canonical == nil ||
		(!strings.EqualFold(canonical.Hostname(), "movie.douban.com") && !strings.EqualFold(canonical.Hostname(), "www.douban.com")) ||
		strings.TrimRight(canonical.Path, "/") != "/subject/"+id {
		return "", errors.New("豆瓣作品頁 ID 與官方片源提供的 ID 不符")
	}
	if posterURL == "" {
		return "", errors.New("豆瓣作品頁沒有提供海報圖片")
	}
	return posterURL, nil
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
	case bangumiPosterContainsAny(category, "綜藝", "综艺", "脫口秀", "脱口秀", "variety"):
		return nil
	case bangumiPosterContainsAny(category, "動漫", "動畫", "动画", "番劇", "番剧", "漫劇", "漫剧", "anime"):
		return []int{2}
	case bangumiPosterContainsAny(category, "電影", "电影", "劇集", "剧集", "電視劇", "电视剧", "真人劇", "真人剧", "短劇", "短剧", "movie", "tv"):
		return []int{6}
	default:
		return []int{2, 6}
	}
}

func normalizeBangumiPosterTitle(value string) string {
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
