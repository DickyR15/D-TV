package core

import (
	"context"
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
	adultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	adultAnchorPattern = regexp.MustCompile("(?is)<a\\b([^>]*)>(.*?)</a>")
	adultImagePattern  = regexp.MustCompile("(?is)<img\\b[^>]*>")
	adultMetaPattern   = regexp.MustCompile("(?is)<meta\\b[^>]*>")
	adultTagPattern    = regexp.MustCompile("(?is)<[^>]+>")
	adultM3U8Pattern   = regexp.MustCompile("(?i)https?://[^\"'<>\\s]+\\.m3u8(?:\\?[^\"'<>\\s]*)?")
	adultMissAVMediaPattern = regexp.MustCompile("(?i)https?://surrit\\.com/[0-9a-f-]{36}/playlist\\.m3u8(?:\\?[^\\\"'<>\\s]*)?")
	adultPackerPattern = regexp.MustCompile("(?s)eval\\(function\\(p,a,c,k,e,(?:d|r)\\)\\{.*?\\}\\('((?:[^'\\\\]|\\\\.)*)',\\s*(\\d+),\\s*(\\d+),\\s*'((?:[^'\\\\]|\\\\.)*)'\\.split\\('\\|'\\)")
	adultWordPattern   = regexp.MustCompile("\\b([0-9a-zA-Z_]+)\\b")
	adultCSSImagePattern = regexp.MustCompile("(?i)url\\(([^)]+)\\)")
	adultMissAVGenrePath = regexp.MustCompile("(?i)/(?:dm[0-9]+/)?cn/genres/([^/?#]+)/?$")
	adultSlugPattern   = regexp.MustCompile("(?i)^[a-z0-9][a-z0-9._-]{0,127}$")
)

func adultSourceName(source string) string {
	if canonicalProviderSource(source) == sourceMissAV { return "MissAV" }
	return extraAdultSourceName(source)
}

func adultProviderBaseURL(source string) string {
	if canonicalProviderSource(source) == sourceMissAV { return missAVBaseURL }
	return extraAdultBaseURL(source)
}

func adultProviderCategories(source string) []nativeCategory {
	if canonicalProviderSource(source) != sourceMissAV { return extraAdultCategoriesFor(source) }
	return []nativeCategory{
		{ID: "today-hot", Name: "今日熱門"},
		{ID: "weekly-hot", Name: "本週熱門"},
		{ID: "monthly-hot", Name: "本月熱門"},
		{ID: "chinese-subtitle", Name: "中文字幕"},
		{ID: "new", Name: "最近更新"},
		{ID: "release", Name: "新作上市"},
		{ID: "uncensored-leak", Name: "無碼流出"},
	}
}

func validAdultProviderCategory(source, category string) bool {
	source = canonicalProviderSource(source)
	if source != sourceMissAV { return extraAdultValidCategory(source, category) }
	return category == "" || adultSlugPattern.MatchString(category)
}

func adultDetailURL(source, sourceID string) string {
	base := adultProviderBaseURL(source)
	sourceID = strings.TrimSpace(sourceID)
	if base == "" || canonicalProviderSource(source) != sourceMissAV ||
		!adultSlugPattern.MatchString(sourceID) {
		return ""
	}
	for _, category := range adultProviderCategories(sourceMissAV) {
		if sourceID == category.ID { return "" }
	}
	return base + "/cn/" + url.PathEscape(sourceID)
}

func adultCategoryURL(source, category string) (string, error) {
	base := adultProviderBaseURL(source)
	if base == "" || canonicalProviderSource(source) != sourceMissAV ||
		!validAdultProviderCategory(source, category) {
		return "", errors.New("片源分類無效")
	}
	if category == "" { return base + "/cn/new", nil }
	return base + "/cn/genres/" + url.PathEscape(category), nil
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
		if host != "missav.ai" && host != "www.missav.ai" && host != "missav.ws" && host != "www.missav.ws" && host != "missav.live" && host != "www.missav.live" && host != "missav123.com" && host != "www.missav123.com" { return "" }
	case sourceSupJav:
		if host != "supjav.blog" && !strings.HasSuffix(host, ".supjav.blog") { return "" }
	case sourcePPP:
		if host != "ppp.porn" && !strings.HasSuffix(host, ".ppp.porn") && host != "cdnlab.live" && !strings.HasSuffix(host, ".cdnlab.live") { return "" }
	case sourceMadou:
		if host != "madou.club" && !strings.HasSuffix(host, ".madou.club") { return "" }
	case source91Porna:
		if host != "91porna.com" && !strings.HasSuffix(host, ".91porna.com") { return "" }
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

func adultMissAVDMPathPrefix(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "dm") || len(value) <= 2 {
		return false
	}
	for _, character := range value[2:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
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
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	var slug string
	switch {
	case len(segments) == 2 && strings.EqualFold(segments[0], "cn"):
		slug = segments[1]
	case len(segments) == 3 && adultMissAVDMPathPrefix(segments[0]) &&
		strings.EqualFold(segments[1], "cn"):
		slug = segments[2]
	default:
		return "", ""
	}
	if !adultSlugPattern.MatchString(slug) {
		return "", ""
	}
	switch strings.ToLower(slug) {
	case "genres", "search", "new", "videos", "release", "today-hot",
		"weekly-hot", "monthly-hot":
		return "", ""
	}
	return slug, address
}

func adultHTMLCardNodes(document *htmlnet.Node, source string) []*htmlnet.Node {
	var cards []*htmlnet.Node
	seen := map[*htmlnet.Node]bool{}
	adultHTMLWalk(document, func(node *htmlnet.Node) {
		if source == sourceMissAV && node.Type == htmlnet.ElementNode &&
			node.Data == "div" && adultHTMLClass(node, "thumbnail") && adultHTMLClass(node, "group") &&
			!seen[node] {
			seen[node] = true
			cards = append(cards, node)
		}
	})
	return cards
}

// adultMissAVCardCandidates keeps the known selector first, then discovers
// cards from exact detail links when the site's wrapper classes change.
// Every fallback candidate must contain an image, so navigation and genre
// links cannot be mistaken for video cards.
func adultMissAVCardCandidates(document *htmlnet.Node, base string) []*htmlnet.Node {
	cards := adultHTMLCardNodes(document, sourceMissAV)
	seen := make(map[*htmlnet.Node]bool, len(cards))
	for _, card := range cards {
		seen[card] = true
	}
	for _, anchor := range providerHTMLNodes(document, func(node *htmlnet.Node) bool {
		return node.Type == htmlnet.ElementNode && node.Data == "a"
	}) {
		if slug, _ := adultSourceIDFromHref(providerHTMLAttr(anchor, "href"), base, sourceMissAV); slug == "" {
			continue
		}
		candidate := anchor
		for parent := anchor.Parent; parent != nil; parent = parent.Parent {
			if parent.Type != htmlnet.ElementNode {
				continue
			}
			switch parent.Data {
			case "div", "article", "li":
				if len(adultNodeImages(parent, base, sourceMissAV)) > 0 {
					candidate = parent
					break
				}
			}
			if candidate != anchor {
				break
			}
		}
		if len(adultNodeImages(candidate, base, sourceMissAV)) == 0 || seen[candidate] {
			continue
		}
		seen[candidate] = true
		cards = append(cards, candidate)
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
			// Prefer real lazy/original poster URLs over a placeholder src.
			// Some page variants also place the poster in data-bg/style.
			attrs = []string{
				"data-original", "data-src", "data-lazy-src", "data-lazyload",
				"data-srcset", "srcset", "data-image", "data-url",
				"data-background-image", "data-bg", "src",
			}
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
	cards := adultHTMLCardNodes(document, source)
	if source == sourceMissAV {
		cards = adultMissAVCardCandidates(document, base)
	}
	for _, card := range cards {
		var anchor *htmlnet.Node
		var slug string
		if card.Type == htmlnet.ElementNode && card.Data == "a" {
			slug, _ = adultSourceIDFromHref(providerHTMLAttr(card, "href"), base, source)
			if slug != "" {
				anchor = card
			}
		}
		if anchor == nil {
			for _, candidate := range providerHTMLNodes(card, func(node *htmlnet.Node) bool {
				return node.Type == htmlnet.ElementNode && node.Data == "a"
			}) {
				candidateSlug, _ := adultSourceIDFromHref(providerHTMLAttr(candidate, "href"), base, source)
				if candidateSlug != "" {
					anchor, slug = candidate, candidateSlug
					break
				}
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
				for _, headingTag := range []string{"h2", "h3", "h4"} {
					titleNode := adultHTMLFirst(card, func(node *htmlnet.Node) bool {
						return node.Type == htmlnet.ElementNode && node.Data == headingTag
					})
					if titleNode != nil {
						title = providerHTMLText(titleNode)
						if title != "" {
							break
						}
					}
				}
			}
			if title == "" {
				title = adultImageAlt(card)
			}
			if title == "" {
				title = providerHTMLText(anchor)
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
		// Match the upstream MissAV module's 500-card page limit.
		// A 100-card cap silently drops valid entries from larger pages.
		if len(items) >= 500 {
			break
		}
	}
	return items
}

func adultParseMissAVCategories(body, base string) []nativeCategory {
	document, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	categories := make([]nativeCategory, 0, 32)
	positions := map[string]int{}
	for _, anchor := range providerHTMLNodes(document, func(node *htmlnet.Node) bool {
		return node.Type == htmlnet.ElementNode && node.Data == "a"
	}) {
		address := adultSafeURL(base, providerHTMLAttr(anchor, "href"), sourceMissAV)
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
	return categories
}

func (d *Downloader) fetchAdultCategories(ctx context.Context, source string) ([]nativeCategory, error) {
	source = canonicalProviderSource(source)
	if source != sourceMissAV { return parseExtraAdultCategories(source), nil }
	base := adultProviderBaseURL(source)
	requestContext := context.WithValue(ctx, providerTextUserAgentKey{}, adultUserAgent)
	referer := base + "/cn/"
	body, err := d.fetchProviderText(requestContext, base+"/cn/genres", referer)
	if err != nil {
		return nil, fmt.Errorf("MissAV 分類讀取失敗：%w", err)
	}
	categories := adultParseMissAVCategories(body, base)
	if len(categories) == 0 {
		return nil, errors.New("MissAV 尚未提供可辨識的分類")
	}
	return categories, nil
}

// adultHasMorePages follows MissAV's published pagination links (?page=N).
// Do not infer this from the number of parsed cards: valid pages can contain
// fewer than 20 cards, and markup changes must not silently stop pagination.
func adultHasMorePages(body string, page int) bool {
	document, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		return false
	}
	maxPage := 0
	for _, anchor := range providerHTMLNodes(document, func(node *htmlnet.Node) bool {
		return node.Type == htmlnet.ElementNode && node.Data == "a"
	}) {
		href := strings.TrimSpace(providerHTMLAttr(anchor, "href"))
		if href == "" {
			continue
		}
		parsed, parseErr := url.Parse(href)
		if parseErr != nil {
			continue
		}
		query := parsed.Query()
		pageValue := firstNonEmpty(query.Get("page"), query.Get("p"))
		pageNumber, parseErr := strconv.Atoi(pageValue)
		if parseErr == nil && pageNumber > maxPage {
			maxPage = pageNumber
		}
	}
	return maxPage > page
}

func adultCatalogHasMorePages(body string, page, parsedItemCount int) bool {
	// Prefer the site's explicit paginator. A full page is a safe fallback for
	// page variants that omit paginator links; one final empty request simply
	// closes the list instead of hiding later videos.
	return adultHasMorePages(body, page) || parsedItemCount >= 20
}

func (d *Downloader) fetchAdultCatalog(ctx context.Context, source string, page int, category, query string) ([]Drama, bool, error) {
	source = canonicalProviderSource(source)
	if source != sourceMissAV { return d.fetchExtraAdultCatalog(ctx, source, page, category, query) }
	if page < 1 || page > 100000 {
		return nil, false, errors.New("目錄頁碼無效")
	}
	base := adultProviderBaseURL(source)
	var address string
	if strings.TrimSpace(query) != "" {
		if len(query) > 256 {
			return nil, false, errors.New("搜尋關鍵字過長")
		}
		address = base + "/cn/search/" + url.PathEscape(strings.TrimSpace(query))
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
		q := parsed.Query()
		q.Set("page", strconv.Itoa(page))
		parsed.RawQuery = q.Encode()
		address = parsed.String()
	}
	requestContext := context.WithValue(ctx, providerTextUserAgentKey{}, adultUserAgent)
	referer := base + "/cn/"
	body, err := d.fetchProviderText(requestContext, address, referer)
	if err != nil {
		return nil, false, fmt.Errorf("%s 目錄讀取失敗：%w", adultSourceName(source), err)
	}
	items := adultParseCards(body, base, source)
	return items, adultCatalogHasMorePages(body, page, len(items)), nil
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
	packed = strings.ReplaceAll(packed, "\\\\", "\\")
	base, err1 := strconv.Atoi(match[2])
	count, err2 := strconv.Atoi(match[3])
	if err1 != nil || err2 != nil || base < 2 || base > 62 || count < 0 || count > 200000 {
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

// adultFindMissAVMedia mirrors the upstream MissAV module's media selector:
// unpack Packer scripts and prefer the site's current Surrit playlist URL.
// Keep the generic HLS parser as a fallback for older player responses.
func adultFindMissAVMedia(body, pageURL string) string {
	body = strings.ReplaceAll(body, "\\/", "/")
	scriptPattern := regexp.MustCompile("(?is)<script\\b[^>]*>(.*?)</script\\s*>")
	for _, match := range scriptPattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		content := match[1]
		if strings.Contains(content, "eval(function") {
			content = adultUnpackPacker(content)
		}
		if found := adultMissAVMediaPattern.FindString(content); found != "" {
			found = strings.TrimRight(found, ".,;:)]}")
			if resolved := adultResolvePlaybackURL(pageURL, found); resolved != "" {
				return resolved
			}
		}
	}
	if found := adultMissAVMediaPattern.FindString(body); found != "" {
		found = strings.TrimRight(found, ".,;:)]}")
		if resolved := adultResolvePlaybackURL(pageURL, found); resolved != "" {
			return resolved
		}
	}
	return adultFindM3U8(body, pageURL)
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

func (d *Downloader) fetchAdultDetail(ctx context.Context, source, sourceID string) (Drama, []Chapter, error) {
	source = canonicalProviderSource(source)
	if source != sourceMissAV { return d.fetchExtraAdultDetail(ctx, source, sourceID) }
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
	chapterReferer := base + "/"
	if source == sourceMissAV {
		chapterReferer = base + "/cn/"
	}
	chapter := Chapter{
		ID: providerChapterID(source, sourceID, pageURL),
		Source: source, Title: "第1集",
		PageURL: pageURL, Referer: chapterReferer,
		VideoURL: "adult-page://" + pageURL,
		CurrentEpisode: json.RawMessage("1"),
	}
	return drama, []Chapter{chapter}, nil
}

func (d *Downloader) resolveAdultMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	if valid && source != sourceMissAV { return d.resolveExtraAdultMedia(ctx, task) }
	if !valid || source != sourceMissAV {
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
	pageReferer := base + "/"
	if source == sourceMissAV {
		pageReferer = base + "/cn/"
	}
	body, err := d.fetchProviderText(requestContext, pageURL, pageReferer)
	if err != nil {
		return providerMedia{}, fmt.Errorf("%s 播放頁讀取失敗：%w", adultSourceName(source), err)
	}
	if actual, ok := pageResponses.values.Load(pageURL); ok {
		if finalURL, valid := actual.(string); valid && finalURL != "" {
			pageURL = finalURL
		}
	}

	mediaURL := adultFindMissAVMedia(body, pageURL)
	if mediaURL == "" || !isProviderHTTPMediaURL(mediaURL) {
		return providerMedia{}, fmt.Errorf("%s 目前沒有可用的 HLS 播放網址；站方可能要求驗證、封鎖請求或已變更播放器", adultSourceName(source))
	}
	credentials := &providerMediaCredentials{referer: pageURL, userAgent: adultUserAgent}
	media := providerMedia{
		URL: mediaURL, Referer: pageURL, credentials: credentials,
		StandardHTTP: runtime.GOOS == "android",
	}
	parsed, _ := url.Parse(mediaURL)
	if parsed != nil && strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return d.prepareWebProviderMedia(ctx, media, adultSourceName(source))
	}
	return media, nil
}
