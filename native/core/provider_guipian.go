package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// 鬼片网（guipian）原生站源：MacCMS 风格的 HTML 站点，鬼片/电视剧/动漫三大类。
//
// 上游页面（实测 2026-09）：
//   GET /list/{id}.html          分类首页（第 1 页）
//   GET /list/{id}_{page}.html   分类翻页
//   GET /nv/{id}.html            详情（含多线路播放列表）
//   GET /play/{id}-{line}-{ep}.html  播放页（var now="<m3u8>"）
//   GET /xml/rss.xml             最新影片 RSS（站点搜索已停用，用它做标题匹配）
//
// 站点搜索接口已停用（/index.php/search/wd/*.html 恒等于首页），因此搜索改为
// 在 RSS 最新条目里做标题匹配，命中范围限于最新一批影片。

const (
	guipianSiteBaseURL = "https://guipianwu.com"
	guipianUserAgent   = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
	guipianPageSize    = 24
)

// guipianCategories 是站点各分类页给出的可翻页叶子分类（实测均能正常翻页）。
// 大类聚合页（1/2/4）不提供翻页且与叶子分类内容重叠，故不单独列出。
var guipianCategories = []nativeCategory{
	{ID: "3", Name: "恐怖片"},
	{ID: "6", Name: "大陆鬼片"},
	{ID: "7", Name: "日韩鬼片"},
	{ID: "8", Name: "林正英鬼片"},
	{ID: "9", Name: "港台鬼片"},
	{ID: "10", Name: "泰国鬼片"},
	{ID: "11", Name: "欧美鬼片"},
	{ID: "31", Name: "剧情片"},
	{ID: "12", Name: "国产剧"},
	{ID: "13", Name: "美剧"},
	{ID: "14", Name: "韩剧"},
	{ID: "15", Name: "日剧"},
	{ID: "16", Name: "泰剧"},
	{ID: "17", Name: "港台剧"},
	{ID: "18", Name: "其他剧"},
	{ID: "23", Name: "日韩动漫"},
	{ID: "24", Name: "国产动漫"},
	{ID: "25", Name: "欧美动漫"},
	{ID: "26", Name: "港台动漫"},
}

var (
	reGuipianMainList   = regexp.MustCompile(`(?is)<ul class="row video-list video-film-list[^"]*">(.*?)</ul>`)
	reGuipianCard       = regexp.MustCompile(`(?is)<a class="video-link" href="/nv/(\d+)\.html"[^>]*title="([^"]*)"(.*?)</a>`)
	reGuipianCover      = regexp.MustCompile(`(?is)data-(?:original|background)="([^"]+)"`)
	reGuipianDoubanLink = regexp.MustCompile(`(?i)douban\.com/(?:movie/)?subject/(\d{6,12})`)
	reGuipianDoubanPoster = regexp.MustCompile(`(?i)/(?:douban|subject)/(\d{6,12})(?:\.[a-z0-9]{2,5})?(?:[/?#]|$)`)
	reGuipianArtworkTag    = regexp.MustCompile(`(?is)<(img|source|div|span|a|figure|meta)\b[^>]*>`)
	reGuipianArtworkAttr   = regexp.MustCompile(`(?is)([a-z_:][-a-z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	reGuipianArtworkCSSURL = regexp.MustCompile(`(?is)url\(\s*["']?([^"')]+)["']?\s*\)`)
	reGuipianDetailArtwork = regexp.MustCompile(`(?is)<(?:div|figure)\b[^>]*(?:class|id)=["'][^"']*(?:detail-img|detail-cover|detail-pic|detail-poster|video-cover|media-cover)[^"']*["'][^>]*>`)
	reGuipianRemark     = regexp.MustCompile(`(?is)<div class="video-duration">(.*?)</div>`)
	reGuipianTitle      = regexp.MustCompile(`(?is)<h1 class="media-title[^"]*">(.*?)</h1>`)
	reGuipianTitleTag   = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
	reGuipianDetailPic  = regexp.MustCompile(`(?is)<div class="detail-img"><img[^>]+src="([^"]+)"`)
	reGuipianDetailPic2 = regexp.MustCompile(`(?is)<div class="detail-cover[^"]*"[^>]*data-original="([^"]+)"`)
	reGuipianContent    = regexp.MustCompile(`(?is)简介：(.*?)</li>`)
	reGuipianYear       = regexp.MustCompile(`(?is)年份：<span>(.*?)</span>`)
	reGuipianDirector   = regexp.MustCompile(`(?is)导演：(.*?)</span>`)
	reGuipianActor      = regexp.MustCompile(`(?is)主演：(.*?)</span>`)
	reGuipianAnchor     = regexp.MustCompile(`(?is)<a[^>]*>([^<]+)</a>`)
	reGuipianArea       = regexp.MustCompile(`(?is)地区：<span><a[^>]*>(.*?)</a>`)
	reGuipianStatus     = regexp.MustCompile(`(?is)状态：(.*?)</span>`)
	reGuipianTab        = regexp.MustCompile(`(?is)<li class="swiper-slide ewave-tab[^"]*" data-target="#(ewave-playlist-\d+)">([^<]+)</li>`)
	reGuipianEpisode    = regexp.MustCompile(`(?is)<li><a title='([^']*)' href='([^']+)'`)
	reGuipianNow        = regexp.MustCompile(`(?is)var\s+now\s*=\s*["']([^"']+)["']`)
	reGuipianMediaAny   = regexp.MustCompile(`(?i)https?://[^"'\s<>\\]+\.(?:m3u8|mp4)(?:\?[^"'\s<>\\]*)?`)
	reGuipianLeadNum    = regexp.MustCompile(`(\d+)`)
	reGuipianRSSItem    = regexp.MustCompile(`(?is)<item>(.*?)</item>`)
	reGuipianRSSTitle   = regexp.MustCompile(`(?is)<title>\s*(?:<!\[CDATA\[)?(.*?)(?:\]\]>)?\s*</title>`)
	reGuipianRSSLink    = regexp.MustCompile(`(?is)/nv/(\d+)\.html`)
	reGuipianTag        = regexp.MustCompile(`<[^>]+>`)
)

func validGuipianCategory(category string) bool {
	if category == "" {
		return true
	}
	if len(category) > 16 || strings.ContainsAny(category, "|/\\\x00\r\n") {
		return false
	}
	for _, entry := range guipianCategories {
		if entry.ID == category {
			return true
		}
	}
	return false
}

func guipianNumericID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 12 {
		return false
	}
	number, err := strconv.Atoi(value)
	return err == nil && number > 0
}

// guipianClean 去标签、解 HTML 实体、压缩空白。
func guipianClean(text string) string {
	if text == "" {
		return ""
	}
	text = reGuipianTag.ReplaceAllString(text, "")
	text = guipianUnescape(text)
	text = strings.ReplaceAll(text, "\u3000", " ")
	return strings.Join(strings.Fields(text), " ")
}

func guipianUnescape(text string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", "\"",
		"&apos;", "'", "&#39;", "'", "&nbsp;", " ",
	)
	for i := 0; i < 3; i++ {
		next := replacer.Replace(text)
		next = regexp.MustCompile(`&#x([0-9a-fA-F]+);`).ReplaceAllStringFunc(next, func(m string) string {
			hex := m[3 : len(m)-1]
			if code, err := strconv.ParseInt(hex, 16, 32); err == nil {
				return string(rune(code))
			}
			return m
		})
		next = regexp.MustCompile(`&#(\d+);`).ReplaceAllStringFunc(next, func(m string) string {
			if code, err := strconv.Atoi(m[2 : len(m)-1]); err == nil {
				return string(rune(code))
			}
			return m
		})
		if next == text {
			break
		}
		text = next
	}
	return text
}

func guipianDoubanID(values ...string) string {
	for _, value := range values {
		for _, pattern := range []*regexp.Regexp{reGuipianDoubanLink, reGuipianDoubanPoster} {
			if match := pattern.FindStringSubmatch(value); len(match) > 1 && validDoubanSubjectID(match[1]) {
				return match[1]
			}
		}
	}
	return ""
}

func guipianFixURL(raw string) string {
	raw = strings.TrimSpace(guipianUnescape(raw))
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !parsed.IsAbs() {
		base, _ := url.Parse(guipianSiteBaseURL + "/")
		parsed = base.ResolveReference(parsed)
	}
	if !validNativeCoverURL(parsed) {
		return ""
	}
	return parsed.String()
}

// guipianPosterCandidates reads lazy-loading, responsive-image and CSS
// artwork attributes in their intended priority order. It deliberately ignores
// unrelated anchor URLs and non-artwork layout elements.
func guipianPosterCandidates(markup string) []string {
	var original, lazy, background, regular, responsive, css, meta []string
	for _, match := range reGuipianArtworkTag.FindAllStringSubmatch(markup, -1) {
		if len(match) < 2 {
			continue
		}
		tagName := strings.ToLower(match[1])
		tag := match[0]
		attrs := map[string]string{}
		for _, attr := range reGuipianArtworkAttr.FindAllStringSubmatch(tag, -1) {
			if len(attr) < 5 {
				continue
			}
			value := firstNonEmpty(attr[2], attr[3], attr[4])
			attrs[strings.ToLower(attr[1])] = guipianUnescape(strings.TrimSpace(value))
		}
		if tagName == "meta" {
			imageMeta := strings.ToLower(attrs["property"] + " " + attrs["name"])
			if strings.Contains(imageMeta, "og:image") || strings.Contains(imageMeta, "twitter:image") {
				if address := guipianFixURL(attrs["content"]); address != "" {
					meta = append(meta, address)
				}
			}
			continue
		}
		className := strings.ToLower(attrs["class"] + " " + attrs["id"])
		imageTag := tagName == "img" || tagName == "source"
		artworkContainer := false
		for _, marker := range []string{"cover", "poster", "detail-img", "detail-cover", "detail-pic", "video-img", "media-img", "thumbnail", "thumb", "picture"} {
			if strings.Contains(className, marker) {
				artworkContainer = true
				break
			}
		}
		if !imageTag && !artworkContainer {
			// Older list templates put data-original directly on an otherwise
			// unclassified div instead of using an img or poster class.
			artworkContainer = attrs["data-original"] != "" ||
				attrs["data-original-src"] != "" ||
				attrs["data-background"] != "" || attrs["data-bg"] != ""
		}
		if !imageTag && !artworkContainer {
			continue
		}
		for _, key := range []string{"data-original", "data-original-src"} {
			if address := guipianFixURL(attrs[key]); address != "" {
				original = append(original, address)
			}
		}
		for _, key := range []string{"data-src", "data-lazy-src", "data-echo"} {
			if address := guipianFixURL(attrs[key]); address != "" {
				lazy = append(lazy, address)
			}
		}
		for _, key := range []string{"data-background", "data-bg"} {
			if address := guipianFixURL(attrs[key]); address != "" {
				background = append(background, address)
			}
		}
		if address := guipianFixURL(attrs["src"]); address != "" {
			regular = append(regular, address)
		}
		for _, value := range strings.Split(attrs["srcset"], ",") {
			fields := strings.Fields(strings.TrimSpace(value))
			if len(fields) > 0 {
				if address := guipianFixURL(fields[0]); address != "" {
					responsive = append(responsive, address)
				}
			}
		}
		for _, styleURL := range reGuipianArtworkCSSURL.FindAllStringSubmatch(attrs["style"], -1) {
			if len(styleURL) > 1 {
				if address := guipianFixURL(styleURL[1]); address != "" {
					css = append(css, address)
				}
			}
		}
	}
	return nativeUniqueCoverAddresses(append(append(append(append(append(append(original, lazy...), background...), regular...), responsive...), css...), meta...)...)
}

func guipianDetailPosterCandidates(body string) []string {
	var candidates []string
	for _, match := range reGuipianDetailArtwork.FindAllStringIndex(body, -1) {
		start := match[0]
		end := min(len(body), start+4096)
		block := body[start:end]
		if close := strings.Index(strings.ToLower(block), "</div>"); close >= 0 {
			block = block[:close+len("</div>")]
		}
		candidates = append(candidates, guipianPosterCandidates(block)...)
	}
	// Preserve the original detail-page selectors even when Open Graph metadata
	// is present; provider-native artwork always has priority over external art.
	if match := reGuipianDetailPic.FindStringSubmatch(body); len(match) > 1 {
		if address := guipianFixURL(match[1]); address != "" {
			candidates = append(candidates, address)
		}
	}
	if match := reGuipianDetailPic2.FindStringSubmatch(body); len(match) > 1 {
		if address := guipianFixURL(match[1]); address != "" {
			candidates = append(candidates, address)
		}
	}
	// Open Graph artwork is the final provider-page fallback when the visible
	// cover markup is incomplete or populated only after JavaScript executes.
	for _, tag := range reGuipianArtworkTag.FindAllString(body, -1) {
		if strings.HasPrefix(strings.ToLower(tag), "<meta") {
			candidates = append(candidates, guipianPosterCandidates(tag)...)
		}
	}
	return nativeUniqueCoverAddresses(candidates...)
}

func guipianCategoryKind(category string) string {
	switch category {
	case "3", "6", "7", "8", "9", "10", "11", "31":
		return "電影"
	case "12", "13", "14", "15", "16", "17", "18":
		return "劇集"
	case "23", "24", "25", "26":
		return "動漫"
	default:
		return ""
	}
}

func guipianAppendTag(tags []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return tags
	}
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag), value) {
			return tags
		}
	}
	return append(tags, value)
}

func (d *Downloader) fetchGuipianCategories() []nativeCategory {
	return append([]nativeCategory(nil), guipianCategories...)
}

func (d *Downloader) guipianGet(ctx context.Context, path string) (string, error) {
	site := d.providerBaseURL(sourceGuipian)
	address := path
	if !strings.HasPrefix(address, "http") {
		address = site + path
	}
	pageContext := context.WithValue(ctx, providerTextUserAgentKey{}, guipianUserAgent)
	return d.fetchProviderText(pageContext, address, site+"/")
}

// parseGuipianCards 从主列表容器解析卡片。
func parseGuipianCards(body string) []Drama {
	segment := body
	if match := reGuipianMainList.FindStringSubmatch(body); len(match) > 1 {
		segment = match[1]
	}
	items := make([]Drama, 0, guipianPageSize)
	seen := map[string]bool{}
	for _, match := range reGuipianCard.FindAllStringSubmatch(segment, -1) {
		id := match[1]
		if !guipianNumericID(id) || seen[id] {
			continue
		}
		title := guipianClean(match[2])
		chunk := match[3]
		if title == "" {
			continue
		}
		seen[id] = true
		coverCandidates := guipianPosterCandidates(chunk)
		cover := ""
		var coverAlternatives []string
		if len(coverCandidates) > 0 {
			cover = coverCandidates[0]
			coverAlternatives = append(coverAlternatives, coverCandidates[1:]...)
		} else if coverMatch := reGuipianCover.FindStringSubmatch(chunk); len(coverMatch) > 1 {
			cover = guipianFixURL(coverMatch[1])
		}
		remark := ""
		if remarkMatch := reGuipianRemark.FindStringSubmatch(chunk); len(remarkMatch) > 1 {
			remark = guipianClean(remarkMatch[1])
		}
		items = append(items, Drama{
			ID: providerDramaID(sourceGuipian, id), Source: sourceGuipian, SourceID: id,
			DoubanID: guipianDoubanID(append(coverCandidates, chunk)...),
			Title: truncate(title, 512), Name: truncate(title, 512),
			Cover: cover, CoverURL: cover, ImageURL: coverAlternatives, ChannelName: "鬼片网",
			CategoryName: "影视", Remark: truncate(remark, 64),
		})
		if len(items) > 500 {
			break
		}
	}
	return items
}

// guipianPageCount 从分页链接推断末页；拿不到时按当前页是否满页保守判断。
func guipianPageCount(body, category string, page, count int) int {
	pattern := regexp.MustCompile(fmt.Sprintf(`(?is)/list/%s_(\d+)\.html`, regexp.QuoteMeta(category)))
	maxPage := page
	for _, match := range pattern.FindAllStringSubmatch(body, -1) {
		if value, err := strconv.Atoi(match[1]); err == nil && value > maxPage && value < 100000 {
			maxPage = value
		}
	}
	if maxPage > page {
		return maxPage
	}
	if count >= guipianPageSize {
		return page + 1
	}
	return page
}

func (d *Downloader) fetchGuipianCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 {
		return nil, false, errors.New("鬼片网目录页码无效")
	}
	if query != "" {
		return d.searchGuipian(ctx, page, query)
	}
	if !validGuipianCategory(category) {
		return nil, false, errors.New("鬼片网内容分类无效")
	}
	if category == "" {
		category = guipianCategories[0].ID
	}
	path := "/list/" + category + ".html"
	if page > 1 {
		path = fmt.Sprintf("/list/%s_%d.html", category, page)
	}
	body, err := d.guipianGet(ctx, path)
	if err != nil {
		return nil, false, err
	}
	items := parseGuipianCards(body)
	if len(items) == 0 {
		return []Drama{}, false, nil
	}
	// Keep the unified catalogue category, but retain the work type as
	// metadata so strict external poster matching can distinguish films,
	// series and animation without adding a redundant Guipian category menu.
	kind := guipianCategoryKind(category)
	for index := range items {
		items[index].Tags = guipianAppendTag(items[index].Tags, kind)
	}
	pageCount := guipianPageCount(body, category, page, len(items))
	return items, page < pageCount, nil
}

func guipianExtractField(body, label string, re *regexp.Regexp) string {
	if match := re.FindStringSubmatch(body); len(match) > 1 {
		return guipianClean(match[1])
	}
	return ""
}

func guipianActors(body string) string {
	match := reGuipianActor.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	names := make([]string, 0, 8)
	for _, anchor := range reGuipianAnchor.FindAllStringSubmatch(match[1], -1) {
		if name := guipianClean(anchor[1]); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return guipianClean(match[1])
	}
	return strings.Join(names, " ")
}

// guipianPickPlaylist 选出分集最多的一条线路。
func guipianPickPlaylist(body string) (string, []Chapter, error) {
	tabs := reGuipianTab.FindAllStringSubmatch(body, -1)
	if len(tabs) == 0 {
		return "", nil, errors.New("鬼片网未找到可用播放线路")
	}
	site := guipianSiteBaseURL
	bestName := ""
	var best []Chapter
	seen := map[string]bool{}
	for _, tab := range tabs {
		anchor, name := tab[1], strings.TrimSpace(tab[2])
		if anchor == "" || seen[anchor] {
			continue
		}
		seen[anchor] = true
		// Go RE2 不支持 lookahead，改用显式字符串截断：从本容器标记之后
		// 截到下一个 playlist-slide 或 </section>，避免消耗式分组吃掉下一条线路的开头。
		marker := fmt.Sprintf(`id="%s"`, anchor)
		start := strings.Index(body, marker)
		if start < 0 {
			continue
		}
		segment := body[start+len(marker):]
		if end := strings.Index(segment, `<div class="playlist-slide`); end >= 0 {
			segment = segment[:end]
		} else if end := strings.Index(segment, `</section>`); end >= 0 {
			segment = segment[:end]
		}
		var chapters []Chapter
		seenPath := map[string]bool{}
		for index, ep := range reGuipianEpisode.FindAllStringSubmatch(segment, -1) {
			epTitle := guipianClean(ep[1])
			href := strings.TrimSpace(ep[2])
			if href == "" {
				continue
			}
			playURL := guipianPlayURL(href)
			if playURL == "" || seenPath[playURL] {
				continue
			}
			seenPath[playURL] = true
			order := index + 1
			if num := reGuipianLeadNum.FindString(epTitle); num != "" {
				if parsed, err := strconv.Atoi(num); err == nil && parsed > 0 && parsed <= 100000 {
					order = parsed
				}
			}
			if epTitle == "" {
				epTitle = fmt.Sprintf("第 %d 集", order)
			}
			chapters = append(chapters, Chapter{
				ID:             providerChapterID(sourceGuipian, "", playURL),
				Source:         sourceGuipian,
				Title:          truncate(epTitle, 128),
				CurrentEpisode: rawEpisode(order),
				VideoURL:       "guipian-play://" + playURL,
				PageURL:        playURL,
				Referer:        site + "/",
			})
		}
		if len(chapters) > len(best) {
			best, bestName = chapters, name
		}
	}
	if len(best) == 0 {
		return "", nil, errors.New("鬼片网线路暂无可播放分集")
	}
	sort.SliceStable(best, func(i, j int) bool {
		left, _ := strconv.Atoi(best[i].EpisodeString(i + 1))
		right, _ := strconv.Atoi(best[j].EpisodeString(j + 1))
		return left < right
	})
	return bestName, best, nil
}

// guipianPlayURL 把播放锚点归一成绝对 URL，并校验同源。
func guipianPlayURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if !strings.HasPrefix(href, "http") {
		href = guipianSiteBaseURL + "/" + strings.TrimPrefix(href, "/")
	}
	parsed, err := url.Parse(href)
	if err != nil || parsed.User != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "guipianwu.com" && host != "www.guipianwu.com" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(parsed.Path), ".html") || !strings.Contains(parsed.Path, "/play/") {
		return ""
	}
	return parsed.String()
}

// parseGuipianCoverMetadata extracts poster and matching metadata without
// requiring a playable playlist. Artwork remains available when player markup
// is incomplete or the source has not exposed episode links.
func parseGuipianCoverMetadata(sourceID, body string) (Drama, error) {
	if !guipianNumericID(sourceID) {
		return Drama{}, errors.New("鬼片網影片 ID 無效")
	}
	name := guipianExtractField(body, "title", reGuipianTitle)
	if name == "" {
		if match := reGuipianTitleTag.FindStringSubmatch(body); len(match) > 1 {
			raw := guipianClean(match[1])
			if strings.Contains(raw, "提示信息") || strings.Contains(raw, "影片不存在") {
				return Drama{}, errors.New("鬼片網影片不存在")
			}
			name = strings.TrimSpace(regexp.MustCompile("《(.*?)》.*").ReplaceAllString(raw, "$1"))
		}
	}
	if name == "" {
		return Drama{}, errors.New("鬼片網詳情缺少影片名稱")
	}
	coverCandidates := guipianDetailPosterCandidates(body)
	cover := ""
	var alternatives []string
	if len(coverCandidates) > 0 {
		cover = coverCandidates[0]
		alternatives = append(alternatives, coverCandidates[1:]...)
	}
	year := guipianExtractField(body, "year", reGuipianYear)
	drama := Drama{
		ID: providerDramaID(sourceGuipian, sourceID), Source: sourceGuipian, SourceID: sourceID,
		DoubanID: guipianDoubanID(append(coverCandidates, body)...),
		Title: truncate(name, 512), Name: truncate(name, 512),
		Cover: cover, CoverURL: cover, ImageURL: alternatives,
		ChannelName: "鬼片網", CategoryName: "影視", OnlineDate: year,
	}
	if year != "" {
		drama.Tags = append(drama.Tags, year+"年")
	}
	return drama, nil
}

func (d *Downloader) fetchGuipianCoverMetadata(ctx context.Context, sourceID string) (Drama, error) {
	if !guipianNumericID(sourceID) {
		return Drama{}, errors.New("鬼片網影片 ID 無效")
	}
	body, err := d.guipianGet(ctx, "/nv/"+sourceID+".html")
	if err != nil {
		return Drama{}, err
	}
	return parseGuipianCoverMetadata(sourceID, body)
}

func (d *Downloader) fetchGuipianDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !guipianNumericID(sourceID) {
		return Drama{}, nil, errors.New("鬼片网视频 ID 无效")
	}
	body, err := d.guipianGet(ctx, "/nv/"+sourceID+".html")
	if err != nil {
		return Drama{}, nil, err
	}
	name := guipianExtractField(body, "title", reGuipianTitle)
	if name == "" {
		if match := reGuipianTitleTag.FindStringSubmatch(body); len(match) > 1 {
			raw := guipianClean(match[1])
			if strings.Contains(raw, "提示信息") || strings.Contains(raw, "影片不存在") {
				return Drama{}, nil, errors.New("鬼片网影片不存在")
			}
			name = strings.TrimSpace(regexp.MustCompile(`《(.*?)》.*`).ReplaceAllString(raw, "$1"))
		}
	}
	if name == "" {
		return Drama{}, nil, errors.New("鬼片网详情缺少视频名称")
	}
	coverCandidates := guipianDetailPosterCandidates(body)
	cover := ""
	var coverAlternatives []string
	if len(coverCandidates) > 0 {
		cover = coverCandidates[0]
		coverAlternatives = append(coverAlternatives, coverCandidates[1:]...)
	}
	description := guipianExtractField(body, "content", reGuipianContent)
	year := guipianExtractField(body, "year", reGuipianYear)
	director := guipianExtractField(body, "director", reGuipianDirector)
	area := guipianExtractField(body, "area", reGuipianArea)
	status := guipianExtractField(body, "status", reGuipianStatus)
	actors := guipianActors(body)

	drama := Drama{
		ID: providerDramaID(sourceGuipian, sourceID), Source: sourceGuipian, SourceID: sourceID,
		DoubanID: guipianDoubanID(append(coverCandidates, body)...),
		Title: truncate(name, 512), Name: truncate(name, 512),
		Desc: truncate(description, 12000), Intro: truncate(description, 12000),
		Cover: cover, CoverURL: cover, ImageURL: coverAlternatives,
		ChannelName: "鬼片网", CategoryName: "影视", OnlineDate: year,
		Remark: truncate(status, 64),
	}
	tags := make([]string, 0, 8)
	seenTag := map[string]bool{}
	addTag := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" && !seenTag[value] && len([]rune(value)) <= 48 && len(tags) < 16 {
			seenTag[value] = true
			tags = append(tags, value)
		}
	}
	if year != "" {
		addTag(year + "年")
	}
	if area != "" {
		addTag(area)
	}
	if director != "" {
		addTag("导演 " + truncate(director, 32))
	}
	if actors != "" {
		addTag("主演 " + truncate(actors, 48))
	}
	drama.Tags = tags

	_, chapters, err := guipianPickPlaylist(body)
	if err != nil {
		return Drama{}, nil, err
	}
	// 分集 ID 里带的是完整播放页 URL，回填 sourceID 前缀便于播放阶段校验归属。
	for index := range chapters {
		chapters[index].ID = providerChapterID(sourceGuipian, sourceID, chapters[index].PageURL)
	}
	drama.TotalEpisode, drama.EpisodeCount = len(chapters), len(chapters)
	return drama, chapters, nil
}

func (d *Downloader) resolveGuipianMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceGuipian, sourceID, "")
	if !valid || source != sourceGuipian || !guipianNumericID(sourceID) || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("鬼片网播放分集信息无效，请刷新详情")
	}
	playURL := strings.TrimPrefix(task.Chapter.ID, prefix)
	if playURL == "" {
		playURL = task.Chapter.PageURL
	}
	if guipianPlayURL(playURL) == "" {
		return providerMedia{}, errors.New("鬼片网播放页地址无效")
	}
	body, err := d.guipianGet(ctx, playURL)
	if err != nil {
		return providerMedia{}, err
	}
	address := ""
	if match := reGuipianNow.FindStringSubmatch(body); len(match) > 1 {
		address = strings.TrimSpace(match[1])
	}
	if address == "" {
		address = reGuipianMediaAny.FindString(body)
	}
	if address == "" {
		return providerMedia{}, errors.New("鬼片网该集暂无可用播放地址")
	}
	if strings.HasPrefix(address, "//") {
		address = "https:" + address
	}
	if !isProviderHTTPMediaURL(address) {
		return providerMedia{}, errors.New("鬼片网播放地址无效")
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return providerMedia{}, errors.New("鬼片网播放地址无效")
	}
	referer := guipianSiteBaseURL + "/"
	media := providerMedia{URL: address, Referer: referer}
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return d.prepareWebProviderMedia(ctx, media, "鬼片网")
	}
	return media, nil
}

// searchGuipian 在 RSS 最新条目里做标题匹配（站点搜索接口已停用）。
func (d *Downloader) searchGuipian(ctx context.Context, page int, query string) ([]Drama, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 256 {
		return nil, false, errors.New("鬼片网搜索关键词无效")
	}
	body, err := d.guipianGet(ctx, "/xml/rss.xml")
	if err != nil {
		return nil, false, err
	}
	needle := strings.ToLower(query)
	var matched []Drama
	seen := map[string]bool{}
	for _, item := range reGuipianRSSItem.FindAllStringSubmatch(body, -1) {
		segment := item[1]
		titleMatch := reGuipianRSSTitle.FindStringSubmatch(segment)
		idMatch := reGuipianRSSLink.FindStringSubmatch(segment)
		if len(titleMatch) < 2 || len(idMatch) < 2 {
			continue
		}
		title := guipianClean(titleMatch[1])
		id := idMatch[1]
		if title == "" || !guipianNumericID(id) || seen[id] {
			continue
		}
		if !strings.Contains(strings.ToLower(title), needle) {
			continue
		}
		seen[id] = true
		matched = append(matched, Drama{
			ID: providerDramaID(sourceGuipian, id), Source: sourceGuipian, SourceID: id,
			Title: truncate(title, 512), Name: truncate(title, 512),
			ChannelName: "鬼片网", CategoryName: "影视",
		})
	}
	start := (page - 1) * guipianPageSize
	if start >= len(matched) {
		return []Drama{}, false, nil
	}
	end := start + guipianPageSize
	if end > len(matched) {
		end = len(matched)
	}
	return matched[start:end], end < len(matched), nil
}
