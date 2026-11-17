package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	htmlnet "golang.org/x/net/html"
)

const (
	missAVBaseURL  = "https://missav.live"
	supJavBaseURL  = "https://supjav.com"
	adultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	adultAnchorPattern = regexp.MustCompile("(?is)<a\\b([^>]*)>(.*?)</a>")
	adultImagePattern  = regexp.MustCompile("(?is)<img\\b[^>]*>")
	adultMetaPattern   = regexp.MustCompile("(?is)<meta\\b[^>]*>")
	adultTagPattern    = regexp.MustCompile("(?is)<[^>]+>")
	adultM3U8Pattern   = regexp.MustCompile("(?i)https?://[^\"'<>\\s]+\\.m3u8(?:\\?[^\"'<>\\s]*)?")
	adultPackerPattern = regexp.MustCompile("(?s)eval\\(function\\(p,a,c,k,e,d\\)\\{.*?\\}\\('([^']*)',\\s*(\\d+),\\s*(\\d+),\\s*'([^']*)'\\.split\\('\\|'\\)")
	adultWordPattern   = regexp.MustCompile("\\b([0-9a-zA-Z_]+)\\b")
	adultCSSImagePattern = regexp.MustCompile("(?i)url\\(([^)]+)\\)")
	adultMissAVGenrePath = regexp.MustCompile("(?i)/(?:dm[0-9]+/)?cn/genres/([^/?#]+)/?$")
	adultSlugPattern   = regexp.MustCompile("(?i)^[a-z0-9][a-z0-9._-]{0,127}$")
)

func adultSourceName(source string) string {
	switch canonicalProviderSource(source) {
	case sourceMissAV:
		return "MissAV"
	case sourceSupJav:
		return "SupJav"
	default:
		return "進階片源"
	}
}

func adultProviderBaseURL(source string) string {
	switch canonicalProviderSource(source) {
	case sourceMissAV:
		return missAVBaseURL
	case sourceSupJav:
		return supJavBaseURL
	default:
		return ""
	}
}

func adultProviderCategories(source string) []nativeCategory {
	switch canonicalProviderSource(source) {
	case sourceMissAV:
		return []nativeCategory{
			{ID: "today-hot", Name: "今日熱門"},
			{ID: "weekly-hot", Name: "本週熱門"},
			{ID: "monthly-hot", Name: "本月熱門"},
			{ID: "chinese-subtitle", Name: "中文字幕"},
			{ID: "new", Name: "最近更新"},
			{ID: "release", Name: "新作上市"},
			{ID: "uncensored-leak", Name: "無碼流出"},
		}
	case sourceSupJav:
		return []nativeCategory{
			{ID: "popular", Name: "熱門總榜"},
			{ID: "weekly-hot", Name: "本週熱門"},
			{ID: "monthly-hot", Name: "本月熱門"},
			{ID: "uncensored-jav", Name: "無碼"},
			{ID: "censored-jav", Name: "有碼"},
			{ID: "amateur", Name: "素人"},
			{ID: "chinese-subtitles", Name: "中文字幕"},
			{ID: "english-subtitles", Name: "英文字幕"},
			{ID: "reducing-mosaic", Name: "破壞版"},
		}
	default:
		return nil
	}
}

func validAdultProviderCategory(source, category string) bool {
	if category == "" {
		return true
	}
	if canonicalProviderSource(source) == sourceMissAV {
		return adultSlugPattern.MatchString(category)
	}
	for _, item := range adultProviderCategories(source) {
		if item.ID == category {
			return true
		}
	}
	return false
}

func adultDetailURL(source, sourceID string) string {
	base := adultProviderBaseURL(source)
	sourceID = strings.TrimSpace(sourceID)
	if base == "" || !adultSlugPattern.MatchString(sourceID) {
		return ""
	}
	switch canonicalProviderSource(source) {
	case sourceMissAV:
		return base + "/cn/" + url.PathEscape(sourceID)
	case sourceSupJav:
		if _, err := strconv.Atoi(sourceID); err != nil {
			return ""
		}
		return base + "/zh/" + url.PathEscape(sourceID) + ".html"
	default:
		return ""
	}
}

func adultCategoryURL(source, category string) (string, error) {
	base := adultProviderBaseURL(source)
	if base == "" || !validAdultProviderCategory(source, category) {
		return "", errors.New("片源分類無效")
	}
	if category == "" {
		if source == sourceSupJav {
			return base + "/zh/", nil
		}
		if source == sourceMissAV {
			return base + "/cn/new", nil
		}
		return base + "/", nil
	}
	if source == sourceMissAV {
		return base + "/cn/genres/" + url.PathEscape(category), nil
	}
	if source == sourceSupJav {
		paths := map[string]string{
			"popular": "/popular",
			"weekly-hot": "/popular?sort=week",
			"monthly-hot": "/popular?sort=month",
			"uncensored-jav": "/category/uncensored-jav",
			"censored-jav": "/category/censored-jav",
			"amateur": "/category/amateur",
			"chinese-subtitles": "/category/chinese-subtitles",
			"english-subtitles": "/category/english-subtitles",
			"reducing-mosaic": "/category/reducing-mosaic",
		}
		if path, ok := paths[category]; ok {
			if strings.HasPrefix(path, "/popular") {
				return base + path, nil
			}
			return base + "/zh" + path, nil
		}
	}
	return "", errors.New("片源分類目前沒有對應網址")
}

func adultAttribute(tag, wanted string) string {
	pattern := regexp.MustCompile("(?is)\\b" + regexp.QuoteMeta(wanted) + "\\s*=\\s*['\\\"]([^'\\\"]*)['\\\"]")
	match := pattern.FindStringSubmatch(tag)
	if len(match) < 2 {
		return ""
	}
	return html.UnescapeString(strings.TrimSpace(match[1]))
}

func adultClean(value string) string {
	value = adultTagPattern.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return strings.Join(strings.Fields(value), " ")
}

func adultSafeURL(base, raw, source string) string {
	raw = html.UnescapeString(strings.TrimSpace(strings.ReplaceAll(raw, "\\/", "/")))
	if raw == "" {
		return ""
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return ""
	}
	reference, err := url.Parse(raw)
	if err != nil || reference.User != nil {
		return ""
	}
	resolved := baseURL.ResolveReference(reference)
	if resolved.Scheme != "https" || resolved.User != nil || resolved.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(resolved.Hostname())
	switch canonicalProviderSource(source) {
	case sourceMissAV:
		if host != "missav.ai" && host != "www.missav.ai" &&
			host != "missav.ws" && host != "www.missav.ws" &&
			host != "missav.live" && host != "www.missav.live" &&
			host != "missav123.com" && host != "www.missav123.com" {
			return ""
		}
	case sourceSupJav:
		if host != "supjav.com" && host != "www.supjav.com" {
			return ""
		}
	default:
		return ""
	}
	return resolved.String()
}

func adultSafeImageURL(base, raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(strings.ReplaceAll(raw, "\\/", "/")))
	baseURL, err := url.Parse(base)
	if err != nil || raw == "" {
		return ""
	}
	reference, err := url.Parse(raw)
	if err != nil || reference.User != nil {
		return ""
	}
	resolved := baseURL.ResolveReference(reference)
	if resolved.User != nil || !isProviderHTTPMediaURL(resolved.String()) {
		return ""
	}
	return resolved.String()
}

func adultImageCandidates(fragment, base, source string) []string {
	document, err := htmlnet.Parse(strings.NewReader(fragment))
	if err != nil {
		return nil
	}
	return adultNodeImages(document, base, source)
}

func adultMetaValue(body, wanted string) string {
	for _, tag := range adultMetaPattern.FindAllString(body, -1) {
		name := adultAttribute(tag, "property")
		if name == "" {
			name = adultAttribute(tag, "name")
		}
		if strings.EqualFold(name, wanted) {
			return adultAttribute(tag, "content")
		}
	}
	return ""
}

func adultHTMLClass(node *htmlnet.Node, wanted string) bool {
	if node == nil || node.Type != htmlnet.ElementNode {
		return false
	}
	for _, className := range strings.Fields(providerHTMLAttr(node, "class")) {
		if className == wanted {
			return true
		}
	}
	return false
}

func adultHTMLWalk(node *htmlnet.Node, visit func(*htmlnet.Node)) {
	if node == nil {
		return
	}
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		adultHTMLWalk(child, visit)
	}
}

func adultHTMLFirst(node *htmlnet.Node, match func(*htmlnet.Node) bool) *htmlnet.Node {
	var found *htmlnet.Node
	adultHTMLWalk(node, func(candidate *htmlnet.Node) {
		if found == nil && match(candidate) {
			found = candidate
		}
	})
	return found
}

func adultSupJavSourceIDFromHref(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() && !strings.EqualFold(parsed.Hostname(), "supjav.com") &&
		!strings.EqualFold(parsed.Hostname(), "www.supjav.com") {
		return ""
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == "" {
		return ""
	}
	slug := path[strings.LastIndex(path, "/")+1:]
	slug = strings.TrimSuffix(strings.ToLower(slug), ".html")
	if _, err := strconv.Atoi(slug); err != nil {
		return ""
	}
	return slug
}

func adultSourceIDFromHref(raw, base, source string) (string, string) {
	address := adultSafeURL(base, raw, source)
	if address == "" {
		return "", ""
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return "", ""
	}
	if source == sourceSupJav {
		slug := adultSupJavSourceIDFromHref(address)
		if slug == "" {
			return "", ""
		}
		return slug, address
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for index := 0; index < len(segments)-1; index++ {
		if strings.EqualFold(segments[index], "cn") {
			slug := segments[index+1]
			if adultSlugPattern.MatchString(slug) && !strings.HasPrefix(strings.ToLower(slug), "dm") {
				return slug, address
			}
		}
	}
	return "", ""
}

func adultHTMLNodeHasImage(node *htmlnet.Node) bool {
	found := false
	adultHTMLWalk(node, func(candidate *htmlnet.Node) {
		if found || candidate.Type != htmlnet.ElementNode {
			return
		}
		if candidate.Data == "img" || candidate.Data == "source" ||
			providerHTMLAttr(candidate, "style") != "" ||
			providerHTMLAttr(candidate, "data-background-image") != "" ||
			providerHTMLAttr(candidate, "data-bg") != "" {
			found = true
		}
	})
	return found
}

func adultHTMLCardNodes(document *htmlnet.Node, source string) []*htmlnet.Node {
	var cards []*htmlnet.Node
	seen := map[*htmlnet.Node]bool{}
	appendCard := func(node *htmlnet.Node) {
		if node != nil && !seen[node] {
			seen[node] = true
			cards = append(cards, node)
		}
	}
	adultHTMLWalk(document, func(node *htmlnet.Node) {
		if node.Type != htmlnet.ElementNode {
			return
		}
		if source == sourceMissAV {
			if node.Data == "div" && adultHTMLClass(node, "thumbnail") && adultHTMLClass(node, "group") {
				appendCard(node)
			}
		} else if source == sourceSupJav && adultHTMLClass(node, "post") {
			appendCard(node)
		}
	})
	if source == sourceSupJav {
		// Theme-independent fallback: locate numeric SupJav detail links and
		// walk to the nearest ancestor that actually contains an image.
		adultHTMLWalk(document, func(node *htmlnet.Node) {
			if node.Type != htmlnet.ElementNode || node.Data != "a" ||
				adultSupJavSourceIDFromHref(providerHTMLAttr(node, "href")) == "" {
				return
			}
			root := node
			for parent, depth := node, 0; parent != nil && depth < 6; parent, depth = parent.Parent, depth+1 {
				if adultHTMLNodeHasImage(parent) {
					root = parent
					break
				}
				root = parent
			}
			appendCard(root)
		})
	}
	return cards
}

func adultImageSrcsetCandidates(raw string) []string {
	var result []string
	for _, entry := range strings.Split(raw, ",") {
		fields := strings.Fields(strings.TrimSpace(entry))
		if len(fields) == 0 || strings.HasPrefix(strings.ToLower(fields[0]), "data:") {
			continue
		}
		result = append(result, strings.Trim(fields[0], "\\\"'"))
	}
	return result
}

func adultNodeImages(card *htmlnet.Node, base, source string) []string {
	seen := map[string]bool{}
	var covers []string
	adultHTMLWalk(card, func(node *htmlnet.Node) {
		if node.Type != htmlnet.ElementNode {
			return
		}
		attrs := []string{"data-original", "data-src", "data-lazy-src", "data-lazyload", "data-srcset", "srcset", "data-image", "data-url", "data-background-image", "data-bg", "src"}
		if source == sourceMissAV {
			attrs = []string{"data-src", "src", "data-original", "data-lazy-src", "data-lazyload", "data-srcset", "srcset"}
		}
		for _, attr := range attrs {
			raw := providerHTMLAttr(node, attr)
			if raw == "" {
				continue
			}
			values := []string{raw}
			if strings.Contains(attr, "srcset") {
				values = adultImageSrcsetCandidates(raw)
			}
			for _, candidate := range values {
				address := adultSafeImageURL(base, candidate)
				if address != "" && !seen[address] {
					seen[address] = true
					covers = append(covers, address)
				}
			}
		}
		if style := providerHTMLAttr(node, "style"); style != "" {
			for _, match := range adultCSSImagePattern.FindAllStringSubmatch(style, -1) {
				if len(match) > 1 {
					raw := strings.Trim(strings.TrimSpace(match[1]), "\\\"'")
					address := adultSafeImageURL(base, raw)
					if address != "" && !seen[address] {
						seen[address] = true
						covers = append(covers, address)
					}
				}
			}
		}
	})
	return covers
}

func adultImageAlt(card *htmlnet.Node) string {
	var alt string
	adultHTMLWalk(card, func(node *htmlnet.Node) {
		if alt == "" && node.Type == htmlnet.ElementNode && node.Data == "img" {
			alt = strings.TrimSpace(providerHTMLAttr(node, "alt"))
		}
	})
	return alt
}

func adultVideoPoster(body, base string) string {
	document, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		return ""
	}
	for _, node := range providerHTMLNodes(document, func(node *htmlnet.Node) bool {
		return node.Type == htmlnet.ElementNode && node.Data == "video"
	}) {
		if raw := providerHTMLAttr(node, "data-poster"); raw != "" {
			return adultSafeImageURL(base, raw)
		}
	}
	return ""
}

func adultParseCards(body, base, source string) []Drama {
	document, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	var items []Drama
	seen := map[string]bool{}
	for _, card := range adultHTMLCardNodes(document, source) {
		var anchor *htmlnet.Node
		var slug, address string
		for _, candidate := range providerHTMLNodes(card, func(node *htmlnet.Node) bool {
			return node.Type == htmlnet.ElementNode && node.Data == "a"
		}) {
			candidateSlug, candidateAddress := adultSourceIDFromHref(providerHTMLAttr(candidate, "href"), base, source)
			if candidateSlug != "" {
				anchor, slug, address = candidate, candidateSlug, candidateAddress
				break
			}
		}
		if anchor == nil || seen[slug] {
			continue
		}
		title := providerHTMLAttr(anchor, "title")
		if title == "" {
			title = providerHTMLAttr(anchor, "aria-label")
		}
		if source == sourceMissAV {
			titleNode := adultHTMLFirst(card, func(node *htmlnet.Node) bool {
				return node.Type == htmlnet.ElementNode && adultHTMLClass(node, "my-2")
			})
			if titleNode != nil {
				title = providerHTMLText(titleNode)
			}
			if title == "" {
				title = adultImageAlt(card)
			}
			if title == "" {
				title = slug
			}
		} else if title == "" {
			title = providerHTMLText(anchor)
			if title == "" {
				title = providerHTMLText(card)
			}
		}
		title = adultClean(title)
		if title == "" {
			continue
		}
		covers := adultNodeImages(card, base, source)
		cover := ""
		if len(covers) > 0 {
			cover = covers[0]
		}
		seen[slug] = true
		items = append(items, Drama{
			ID: providerDramaID(source, slug), Source: source, SourceID: slug,
			Title: truncate(title, 512), Name: truncate(title, 512),
			Cover: cover, CoverURL: cover, CoverCandidates: append([]string(nil), covers[1:]...),
			ChannelName: adultSourceName(source), CategoryName: adultSourceName(source),
			TotalEpisode: 1, EpisodeCount: 1,
		})
		if len(items) >= 100 {
			break
		}
	}
	return items
}

func (d *Downloader) fetchAdultCategories(ctx context.Context, source string) ([]nativeCategory, error) {
	source = canonicalProviderSource(source)
	if source != sourceMissAV {
		return adultProviderCategories(source), nil
	}
	base := adultProviderBaseURL(source)
	requestContext := context.WithValue(ctx, providerTextUserAgentKey{}, adultUserAgent)
	body, err := d.fetchProviderText(requestContext, base+"/cn/genres", base+"/")
	if err != nil {
		return nil, fmt.Errorf("MissAV 分類讀取失敗：%w", err)
	}
	document, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		return nil, errors.New("MissAV 分類頁解析失敗")
	}
	categories := make([]nativeCategory, 0, 32)
	positions := map[string]int{}
	for _, anchor := range providerHTMLNodes(document, func(node *htmlnet.Node) bool {
		return node.Type == htmlnet.ElementNode && node.Data == "a"
	}) {
		address := adultSafeURL(base, providerHTMLAttr(anchor, "href"), source)
		parsed, parseErr := url.Parse(address)
		if parseErr != nil || address == "" {
			continue
		}
		match := adultMissAVGenrePath.FindStringSubmatch(parsed.Path)
		if len(match) < 2 {
			continue
		}
		slug, unescapeErr := url.PathUnescape(match[1])
		if unescapeErr != nil || !adultSlugPattern.MatchString(slug) {
			continue
		}
		name := strings.TrimSpace(providerHTMLText(anchor))
		if name == "" || len([]rune(name)) > 48 {
			continue
		}
		if index, exists := positions[slug]; exists {
			if len([]rune(name)) < len([]rune(categories[index].Name)) {
				categories[index].Name = name
			}
			continue
		}
		positions[slug] = len(categories)
		categories = append(categories, nativeCategory{ID: slug, Name: name})
		if len(categories) >= 200 {
			break
		}
	}
	if len(categories) == 0 {
		return nil, errors.New("MissAV 尚未提供可辨識的分類")
	}
	return categories, nil
}

func (d *Downloader) fetchAdultCatalog(ctx context.Context, source string, page int, category, query string) ([]Drama, bool, error) {
	source = canonicalProviderSource(source)
	if page < 1 || page > 100000 {
		return nil, false, errors.New("目錄頁碼無效")
	}
	base := adultProviderBaseURL(source)
	var address string
	if strings.TrimSpace(query) != "" {
		if len(query) > 256 {
			return nil, false, errors.New("搜尋關鍵字過長")
		}
		if source == sourceMissAV {
			address = base + "/cn/search/" + url.PathEscape(strings.TrimSpace(query))
		} else {
			address = base + "/zh/?s=" + url.QueryEscape(strings.TrimSpace(query))
		}
	} else {
		var err error
		address, err = adultCategoryURL(source, category)
		if err != nil {
			return nil, false, err
		}
	}
	if page > 1 {
		parsed, err := url.Parse(address)
		if err != nil {
			return nil, false, err
		}
		if source == sourceMissAV {
			q := parsed.Query()
			q.Set("page", strconv.Itoa(page))
			parsed.RawQuery = q.Encode()
		} else {
			parsed.Path = strings.TrimRight(parsed.Path, "/") + "/page/" + strconv.Itoa(page)
		}
		address = parsed.String()
	}
	requestContext := context.WithValue(ctx, providerTextUserAgentKey{}, adultUserAgent)
	body, err := d.fetchProviderText(requestContext, address, base+"/")
	if err != nil {
		return nil, false, fmt.Errorf("%s 目錄讀取失敗：%w", adultSourceName(source), err)
	}
	items := adultParseCards(body, base, source)
	return items, len(items) >= 20, nil
}

func adultEncodeBase(value, base int) string {
	if value == 0 {
		return "0"
	}
	var result []byte
	for value > 0 {
		digit := value % base
		switch {
		case digit > 35:
			result = append(result, byte(digit+29))
		case digit < 10:
			result = append(result, byte('0'+digit))
		default:
			result = append(result, byte('a'+digit-10))
		}
		value /= base
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return string(result)
}

func adultUnpackPacker(script string) string {
	match := adultPackerPattern.FindStringSubmatch(script)
	if len(match) != 5 {
		return ""
	}
	packed := strings.ReplaceAll(match[1], "\\'", "'")
	packed = strings.ReplaceAll(packed, "\\/", "/")
	base, err1 := strconv.Atoi(match[2])
	count, err2 := strconv.Atoi(match[3])
	if err1 != nil || err2 != nil || base < 2 || base > 64 || count < 0 || count > 200000 {
		return ""
	}
	keys := strings.Split(match[4], "|")
	lookup := make(map[string]string, count)
	for index := 0; index < count; index++ {
		key := adultEncodeBase(index, base)
		if index < len(keys) && keys[index] != "" {
			lookup[key] = keys[index]
		} else {
			lookup[key] = key
		}
	}
	return adultWordPattern.ReplaceAllStringFunc(packed, func(word string) string {
		sub := adultWordPattern.FindStringSubmatch(word)
		if len(sub) == 2 {
			if value, ok := lookup[sub[1]]; ok {
				return value
			}
		}
		return word
	})
}

func adultResolvePlaybackURL(baseAddress, raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(strings.ReplaceAll(raw, "\\/", "/")))
	if raw == "" {
		return ""
	}
	reference, err := url.Parse(raw)
	if err != nil || reference.User != nil {
		return ""
	}
	var resolved *url.URL
	if strings.TrimSpace(baseAddress) == "" {
		resolved = reference
	} else {
		base, parseErr := url.Parse(baseAddress)
		if parseErr != nil || base == nil {
			return ""
		}
		resolved = base.ResolveReference(reference)
	}
	if resolved.Scheme != "https" || resolved.User != nil || resolved.Hostname() == "" {
		return ""
	}
	return resolved.String()
}

func adultM3U8FromText(body, pageURL string) string {
	body = strings.ReplaceAll(body, "\\/", "/")
	for _, match := range adultM3U8Pattern.FindAllString(body, -1) {
		candidate := strings.TrimRight(match, ".,;:)]}")
		resolved := adultResolvePlaybackURL(pageURL, candidate)
		if resolved != "" {
			return resolved
		}
	}
	return ""
}

func adultFindM3U8(body string, pageURLs ...string) string {
	pageURL := ""
	if len(pageURLs) > 0 {
		pageURL = pageURLs[0]
	}
	body = strings.ReplaceAll(body, "\\/", "/")
	if match := adultM3U8FromText(body, pageURL); match != "" {
		return match
	}
	scriptPattern := regexp.MustCompile("(?is)<script\\b[^>]*>(.*?)</script>")
	for _, script := range scriptPattern.FindAllStringSubmatch(body, -1) {
		if len(script) < 2 {
			continue
		}
		content := script[1]
		if strings.Contains(content, "eval(function") {
			content = adultUnpackPacker(content)
		}
		if content == "" {
			continue
		}
		sourcePattern := regexp.MustCompile("(?is)\\bsource\\s*[:=]\\s*['\\\"]?((?:https?:)?//[^'\\\"\\s;]+|/[^'\\\"\\s;]+)")
		if source := sourcePattern.FindStringSubmatch(content); len(source) > 1 {
			candidate := adultResolvePlaybackURL(pageURL, source[1])
			if candidate != "" && strings.Contains(strings.ToLower(candidate), ".m3u8") {
				return candidate
			}
		}
		if match := adultM3U8FromText(content, pageURL); match != "" {
			return match
		}
	}
	return ""
}

func adultRot13(value string) string {
	data := []byte(value)
	for index, character := range data {
		switch {
		case character >= 'a' && character <= 'z':
			data[index] = 'a' + (character-'a'+13)%26
		case character >= 'A' && character <= 'Z':
			data[index] = 'A' + (character-'A'+13)%26
		}
	}
	return string(data)
}

// adultSupJavVOEHLS decodes VOE's static JSON player payload without running
// remote JavaScript. The returned URL must still be a public HTTPS media URL.
func adultSupJavVOEHLS(body, embedURL string) string {
	scriptPattern := regexp.MustCompile("(?is)<script\\b([^>]*)>(.*?)</script\\s*>")
	for _, match := range scriptPattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 || !strings.EqualFold(adultAttribute("<script "+match[1]+">", "type"), "application/json") {
			continue
		}
		var payload []string
		if err := json.Unmarshal([]byte(strings.TrimSpace(match[2])), &payload); err != nil || len(payload) == 0 {
			continue
		}
		encoded := adultRot13(payload[0])
		for _, noise := range []string{"@$", "^^", "~@", "%?", "*~", "!!", "#&", "_"} {
			encoded = strings.ReplaceAll(encoded, noise, "")
		}
		shifted, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			shifted, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(encoded, "="))
		}
		if err != nil {
			continue
		}
		for index := range shifted {
			if shifted[index] < 3 {
				err = errors.New("invalid VOE shift")
				break
			}
			shifted[index] -= 3
		}
		if err != nil {
			continue
		}
		for left, right := 0, len(shifted)-1; left < right; left, right = left+1, right-1 {
			shifted[left], shifted[right] = shifted[right], shifted[left]
		}
		decoded, decodeErr := base64.StdEncoding.DecodeString(string(shifted))
		if decodeErr != nil {
			decoded, decodeErr = base64.RawStdEncoding.DecodeString(strings.TrimRight(string(shifted), "="))
		}
		if decodeErr != nil {
			continue
		}
		var config map[string]any
		if json.Unmarshal(decoded, &config) != nil {
			continue
		}
		raw, _ := config["source"].(string)
		if raw == "" {
			continue
		}
		resolved := adultResolvePlaybackURL(embedURL, raw)
		if resolved != "" && strings.Contains(strings.ToLower(resolved), ".m3u8") {
			return resolved
		}
	}
	return ""
}

// adultSupJavFSTHLS follows the FST player's declared source order.
// Its primary source can be a relative or extensionless HLS URL, so requiring
// an absolute .m3u8 URL would silently skip the source selected by the player.
func adultSupJavFSTHLS(body, embedURL string) string {
	scriptPattern := regexp.MustCompile("(?is)<script\\b[^>]*>(.*?)</script\\s*>")
	linksAssignment := regexp.MustCompile("(?is)(?:var|let|const)\\s+links\\s*=\\s*")
	sourcesPattern := regexp.MustCompile("(?is)(?:sources|['\\\"]sources['\\\"])\\s*:\\s*\\[\\s*\\{([^{}]*)\\}")
	filePattern := regexp.MustCompile("(?is)(?:file|['\\\"]file['\\\"])\\s*:\\s*(links\\.hls[234](?:\\s*\\|\\|\\s*links\\.hls[234])*)")
	typePattern := regexp.MustCompile("(?is)(?:type|['\\\"]type['\\\"])\\s*:\\s*['\\\"]hls['\\\"]")
	keyPattern := regexp.MustCompile("links\\.(hls[234])")

	for _, match := range scriptPattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		script := match[1]
		if strings.Contains(script, "eval(function") {
			script = adultUnpackPacker(script)
		}
		if script == "" {
			continue
		}
		assignment := linksAssignment.FindStringIndex(script)
		if assignment == nil {
			continue
		}
		objectStart := strings.Index(script[assignment[1]:], "{")
		if objectStart < 0 {
			continue
		}
		remaining := script[assignment[1]+objectStart:]
		decoder := json.NewDecoder(strings.NewReader(remaining))
		var links map[string]any
		if decoder.Decode(&links) != nil || len(links) == 0 {
			continue
		}
		offset := int(decoder.InputOffset())
		if offset < 0 || offset > len(remaining) {
			continue
		}
		sources := sourcesPattern.FindStringSubmatch(remaining[offset:])
		if len(sources) < 2 {
			continue
		}
		fields := sources[1]
		fileMatch := filePattern.FindStringSubmatch(fields)
		if len(fileMatch) < 2 {
			continue
		}
		explicitHLS := typePattern.MatchString(fields)
		for _, key := range keyPattern.FindAllStringSubmatch(fileMatch[1], -1) {
			if len(key) < 2 {
				continue
			}
			raw, ok := links[key[1]].(string)
			if !ok || strings.TrimSpace(raw) == "" {
				continue
			}
			resolved := adultResolvePlaybackURL(embedURL, raw)
			if resolved == "" {
				continue
			}
			path := strings.ToLower(strings.SplitN(resolved, "?", 2)[0])
			if explicitHLS || strings.HasSuffix(path, ".m3u8") {
				return resolved
			}
		}
	}
	return ""
}

func adultReverse(value string) string {
	runes := []rune(value)
	for left, right := 0, len(runes)-1; left < right; left, right = left+1, right-1 {
		runes[left], runes[right] = runes[right], runes[left]
	}
	return string(runes)
}

func adultSupJavDataLinks(body string) map[string]string {
	result := map[string]string{}
	pattern := regexp.MustCompile("(?is)<a\\b([^>]*data-link[^>]*)>(.*?)</a>")
	for _, match := range pattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 {
			continue
		}
		name := strings.ToUpper(adultClean(match[2]))
		link := adultAttribute("<a "+match[1]+">", "data-link")
		if name != "" && link != "" {
			if _, exists := result[name]; !exists {
				result[name] = link
			}
		}
	}
	return result
}

func (d *Downloader) fetchAdultDetail(ctx context.Context, source, sourceID string) (Drama, []Chapter, error) {
	source = canonicalProviderSource(source)
	pageURL := adultDetailURL(source, sourceID)
	if pageURL == "" {
		return Drama{}, nil, errors.New("作品識別碼無效")
	}
	base := adultProviderBaseURL(source)
	requestContext := context.WithValue(ctx, providerTextUserAgentKey{}, adultUserAgent)
	referer := base + "/"
	if source == sourceMissAV {
		referer = base + "/cn/"
	}
	body, err := d.fetchProviderText(requestContext, pageURL, referer)
	if err != nil {
		return Drama{}, nil, fmt.Errorf("%s 作品詳情讀取失敗：%w", adultSourceName(source), err)
	}
	title := adultClean(adultMetaValue(body, "og:title"))
	if title == "" {
		if match := regexp.MustCompile("(?is)<h1\\b[^>]*>(.*?)</h1>").FindStringSubmatch(body); len(match) > 1 {
			title = adultClean(match[1])
		}
	}
	if title == "" {
		return Drama{}, nil, fmt.Errorf("%s 詳情頁缺少作品標題", adultSourceName(source))
	}
	covers := adultImageCandidates(body, base, source)
	metaCovers := []string{}
	for _, key := range []string{"og:image", "twitter:image"} {
		if raw := adultMetaValue(body, key); raw != "" {
			if cover := adultSafeImageURL(base, raw); cover != "" {
				metaCovers = append(metaCovers, cover)
			}
		}
	}
	if source == sourceMissAV {
		if cover := adultVideoPoster(body, base); cover != "" {
			metaCovers = append(metaCovers, cover)
		}
	}
	covers = append(metaCovers, covers...)
	unique := make([]string, 0, len(covers))
	seen := map[string]bool{}
	for _, cover := range covers {
		if cover != "" && !seen[cover] {
			seen[cover] = true
			unique = append(unique, cover)
		}
	}
	cover := ""
	if len(unique) > 0 {
		cover = unique[0]
	}
	description := adultClean(adultMetaValue(body, "description"))
	drama := Drama{
		ID: providerDramaID(source, sourceID), Source: source, SourceID: sourceID,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Desc: truncate(description, 12000), Intro: truncate(description, 12000),
		Cover: cover, CoverURL: cover, CoverCandidates: append([]string(nil), unique[1:]...),
		ChannelName: adultSourceName(source), CategoryName: adultSourceName(source),
		TotalEpisode: 1, EpisodeCount: 1,
	}
	chapter := Chapter{
		ID: providerChapterID(source, sourceID, pageURL),
		Source: source, Title: "第1集",
		PageURL: pageURL, Referer: base + "/",
		VideoURL: "adult-page://" + pageURL,
		CurrentEpisode: json.RawMessage("1"),
	}
	return drama, []Chapter{chapter}, nil
}

func (d *Downloader) resolveAdultMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	if !valid || (source != sourceMissAV && source != sourceSupJav) {
		return providerMedia{}, errors.New("進階片源作品資訊無效，請重新整理")
	}
	prefix := providerChapterID(source, sourceID, "")
	if !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("進階片源分集資訊無效，請重新整理")
	}
	pageURL := task.Chapter.PageURL
	if pageURL == "" {
		pageURL = strings.TrimPrefix(task.Chapter.ID, prefix)
	}
	base := adultProviderBaseURL(source)
	if adultSafeURL(base, pageURL, source) == "" {
		return providerMedia{}, errors.New("進階片源播放頁網址無效")
	}
	pageResponses := &playbackResponseURLs{}
	requestContext := context.WithValue(ctx, playbackResponseURLsKey{}, pageResponses)
	requestContext = context.WithValue(requestContext, providerTextUserAgentKey{}, adultUserAgent)
	body, err := d.fetchProviderText(requestContext, pageURL, base+"/")
	if err != nil {
		return providerMedia{}, fmt.Errorf("%s 播放頁讀取失敗：%w", adultSourceName(source), err)
	}
	if actual, ok := pageResponses.values.Load(pageURL); ok {
		if finalURL, valid := actual.(string); valid && finalURL != "" {
			pageURL = finalURL
		}
	}
	mediaURL := ""
	referer := pageURL
	var serverErrors []string
	if source == sourceMissAV {
		mediaURL = adultFindM3U8(body, pageURL)
	} else {
		servers := adultSupJavDataLinks(body)
		order := []string{"EVS", "FST", "VAS", "LUC", "VOE", "TV", "ST"}
		for _, name := range order {
			link, ok := servers[name]
			if !ok || mediaURL != "" {
				continue
			}
			embedURL := "https://lk1.supremejav.com/supjav.php?c=" + url.QueryEscape(adultReverse(link))
			embedResponses := &playbackResponseURLs{}
			embedContext := context.WithValue(requestContext, playbackResponseURLsKey{}, embedResponses)
			embed, fetchErr := d.fetchProviderText(embedContext, embedURL, pageURL)
			if fetchErr != nil {
				serverErrors = append(serverErrors, name+": "+truncate(fetchErr.Error(), 120))
				continue
			}
			if actual, ok := embedResponses.values.Load(embedURL); ok {
				if finalURL, valid := actual.(string); valid && finalURL != "" {
					embedURL = finalURL
				}
			}
			found := ""
			switch name {
			case "FST":
				found = adultSupJavFSTHLS(embed, embedURL)
			case "VOE":
				found = adultSupJavVOEHLS(embed, embedURL)
			}
			if found == "" {
				found = adultFindM3U8(embed, embedURL)
			}
			if found != "" {
				mediaURL = found
				referer = embedURL
			} else {
				serverErrors = append(serverErrors, name+": 播放頁未找到可用 HLS 位址")
			}
		}
	}
	if mediaURL == "" || !isProviderHTTPMediaURL(mediaURL) {
		if source == sourceSupJav && len(serverErrors) > 0 {
			return providerMedia{}, fmt.Errorf("%s 沒有可用播放來源：%s", adultSourceName(source), strings.Join(serverErrors, "；"))
		}
		return providerMedia{}, fmt.Errorf("%s 目前沒有可用的 HLS 播放網址；站方可能要求驗證、封鎖請求或已變更播放器", adultSourceName(source))
	}
	credentials := &providerMediaCredentials{referer: referer, userAgent: adultUserAgent}
	media := providerMedia{
		URL: mediaURL, Referer: referer, credentials: credentials,
		StandardHTTP: runtime.GOOS == "android",
	}
	parsed, _ := url.Parse(mediaURL)
	if parsed != nil && strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return d.prepareWebProviderMedia(ctx, media, adultSourceName(source))
	}
	return media, nil
}
