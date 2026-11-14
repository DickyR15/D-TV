package core

import (
	"context"
	"errors"
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type providerTextAcceptLanguageKey struct{}

func supjavTraditionalContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, providerTextAcceptLanguageKey{}, "zh-TW,zh-Hant;q=0.9,zh;q=0.8")
}

const (
	missavDefaultBaseURL = "https://missav.ws"
	supjavDefaultBaseURL = "https://supjav.com"
	supjavPlaybackAPI    = "https://lk1.supremejav.com/supjav.php"
)

var (
	reMissavSourceID = regexp.MustCompile(`(?i)^[a-z0-9]+-[0-9]+(?:-[a-z0-9]+)*$`)
	reMissavMediaURL = regexp.MustCompile(`(?i)(?:https?:)?//[^\s"'<>\\]+\.(?:m3u8|mp4)(?:\?[^\s"'<>\\]*)?`)
	reSupjavDetailID = regexp.MustCompile(`(?i)/(?:[^/?#]+/)?([0-9]+)\.html(?:[?#].*)?$`)
	reSupjavPlayerURL = regexp.MustCompile(`(?is)urlPlay.{0,1000}?(https?://[^\s"'<>\\]+?\.m3u8(?:\?[^\s"'<>\\]*)?)`)
)

func (d *Downloader) fetchAdultProviderDocument(ctx context.Context, address, referer string) (*html.Node, string, string, error) {
	responses := &playbackResponseURLs{}
	pageContext := context.WithValue(ctx, playbackResponseURLsKey{}, responses)
	body, err := d.fetchProviderText(pageContext, address, referer)
	if err != nil {
		return nil, "", "", err
	}
	if len(body) > 4<<20 {
		return nil, "", "", errors.New("片源頁面過大")
	}
	if actual, found := responses.values.Load(address); found {
		address = actual.(string)
	}
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, "", "", err
	}
	return document, address, body, nil
}

func adultAdditionalCoverCandidates(covers []string) []string {
	if len(covers) < 2 {
		return nil
	}
	return append([]string(nil), covers[1:]...)
}

func adultPageMeta(document *html.Node, keys ...string) string {
	wanted := map[string]bool{}
	for _, key := range keys {
		wanted[strings.ToLower(key)] = true
	}
	for _, node := range providerHTMLNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "meta"
	}) {
		key := strings.ToLower(strings.TrimSpace(firstNonEmpty(
			providerHTMLAttr(node, "property"),
			providerHTMLAttr(node, "name"),
		)))
		if wanted[key] {
			if value := strings.TrimSpace(providerHTMLAttr(node, "content")); value != "" {
				return htmlstd.UnescapeString(value)
			}
		}
	}
	return ""
}

func adultPageTitle(document *html.Node) string {
	if title := adultPageMeta(document, "og:title", "twitter:title"); title != "" {
		return strings.TrimSpace(title)
	}
	for _, node := range providerHTMLNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && (node.Data == "h1" || node.Data == "title")
	}) {
		if value := strings.TrimSpace(providerHTMLText(node)); value != "" {
			return value
		}
	}
	return ""
}

func adultPageDescription(document *html.Node) string {
	return providerHTMLPlainText(adultPageMeta(document, "og:description", "description", "twitter:description"))
}

func adultImageCandidates(node *html.Node, pageURL string) []string {
	if node == nil {
		return nil
	}
	var addresses []string
	for _, key := range []string{"src", "data-src", "data-original", "data-lazy-src", "data-lazyload", "data-poster", "data-srcset", "srcset"} {
		value := strings.TrimSpace(providerHTMLAttr(node, key))
		if value == "" {
			continue
		}
		if strings.Contains(key, "srcset") {
			for _, candidate := range strings.Split(value, ",") {
				fields := strings.Fields(strings.TrimSpace(candidate))
				if len(fields) > 0 {
					addresses = append(addresses, providerCoverAddresses(fields[0], pageURL)...)
				}
			}
			continue
		}
		addresses = append(addresses, providerCoverAddresses(value, pageURL)...)
	}
	return nativeUniqueCoverAddresses(addresses...)
}

func missavIDFromURL(raw, baseURL string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !parsed.IsAbs() {
		base, baseErr := url.Parse(baseURL)
		if baseErr != nil {
			return ""
		}
		parsed = base.ResolveReference(parsed)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "missav.ws" && host != "www.missav.ws" {
		return ""
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for index := len(segments) - 1; index >= 0; index-- {
		segment := strings.TrimSpace(segments[index])
		if reMissavSourceID.MatchString(segment) {
			return strings.ToLower(segment)
		}
	}
	return ""
}

func missavDramaFromCard(anchor *html.Node, pageURL string) (Drama, bool) {
	href := strings.TrimSpace(providerHTMLAttr(anchor, "href"))
	sourceID := missavIDFromURL(href, pageURL)
	if sourceID == "" {
		return Drama{}, false
	}
	title := strings.TrimSpace(providerHTMLAttr(anchor, "title"))
	var covers []string
	for _, image := range providerHTMLNodes(anchor, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "img"
	}) {
		if title == "" {
			title = strings.TrimSpace(firstNonEmpty(providerHTMLAttr(image, "alt"), providerHTMLAttr(image, "title")))
		}
		covers = append(covers, adultImageCandidates(image, pageURL)...)
	}
	if title == "" {
		title = strings.TrimSpace(providerHTMLText(anchor))
	}
	if title == "" {
		title = strings.ToUpper(sourceID)
	}
	covers = nativeUniqueCoverAddresses(covers...)
	return Drama{
		ID: providerDramaID(sourceMissav, sourceID), Source: sourceMissav, SourceID: sourceID,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Cover: firstNonEmpty(covers...), CoverURL: firstNonEmpty(covers...),
		CoverCandidates: adultAdditionalCoverCandidates(covers),
		ChannelName: "MissAV", CategoryName: "影片", TotalEpisode: 1, EpisodeCount: 1,
	}, true
}

func parseMissavCards(document *html.Node, pageURL string) []Drama {
	var items []Drama
	seen := map[string]bool{}
	for _, anchor := range providerHTMLNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a"
	}) {
		drama, ok := missavDramaFromCard(anchor, pageURL)
		if !ok || seen[drama.ID] {
			continue
		}
		seen[drama.ID] = true
		items = append(items, drama)
		if len(items) >= 500 {
			break
		}
	}
	return items
}

func (d *Downloader) missavCatalogAddress(page int, query string) string {
	base := strings.TrimRight(d.providerBaseURL(sourceMissav), "/")
	if query != "" {
		return base + "/search/" + url.PathEscape(strings.TrimSpace(query))
	}
	address := base + "/zh/release"
	if page > 1 {
		address += "?page=" + strconv.Itoa(page)
	}
	return address
}

func (d *Downloader) missavDetailAddress(sourceID string) string {
	sourceID = strings.ToLower(strings.TrimSpace(sourceID))
	return strings.TrimRight(d.providerBaseURL(sourceMissav), "/") + "/" + url.PathEscape(sourceID) + "/"
}

func (d *Downloader) fetchMissavCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 10000 || strings.TrimSpace(category) != "" || len(query) > 160 {
		return nil, false, errors.New("MissAV 目錄查詢參數無效")
	}
	query = strings.TrimSpace(query)
	if query != "" && reMissavSourceID.MatchString(query) {
		drama, _, err := d.fetchMissavDetail(ctx, strings.ToLower(query))
		if err != nil {
			return nil, false, err
		}
		return []Drama{drama}, false, nil
	}
	address := d.missavCatalogAddress(page, query)
	document, pageURL, _, err := d.fetchAdultProviderDocument(ctx, address, d.providerBaseURL(sourceMissav)+"/zh/")
	if err != nil {
		return nil, false, err
	}
	items := parseMissavCards(document, pageURL)
	return items, len(items) >= 20, nil
}

func (d *Downloader) fetchMissavDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.ToLower(strings.TrimSpace(sourceID))
	if !reMissavSourceID.MatchString(sourceID) {
		return Drama{}, nil, errors.New("MissAV 影片片號無效")
	}
	site := strings.TrimRight(d.providerBaseURL(sourceMissav), "/")
	address := d.missavDetailAddress(sourceID)
	document, pageURL, _, err := d.fetchAdultProviderDocument(ctx, address, site+"/zh/")
	if err != nil {
		return Drama{}, nil, err
	}
	title := strings.TrimSpace(adultPageTitle(document))
	if title == "" {
		title = strings.ToUpper(sourceID)
	}
	cover := adultPageMeta(document, "og:image", "twitter:image")
	covers := providerCoverAddresses(cover, pageURL)
	description := adultPageDescription(document)
	drama := Drama{
		ID: providerDramaID(sourceMissav, sourceID), Source: sourceMissav, SourceID: sourceID,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Desc: truncate(description, 12000), Intro: truncate(description, 12000),
		Cover: firstNonEmpty(covers...), CoverURL: firstNonEmpty(covers...),
		ChannelName: "MissAV", CategoryName: "影片", TotalEpisode: 1, EpisodeCount: 1,
	}
	chapters := []Chapter{{
		ID: providerChapterID(sourceMissav, sourceID, "1"), Source: sourceMissav,
		Title: "正片", VideoURL: "missav-player://" + sourceID,
		PageURL: pageURL, Referer: site + "/zh/",
	}}
	return drama, chapters, nil
}

func adultMediaCandidates(document *html.Node, rawBody, pageURL string) []string {
	seen := map[string]bool{}
	var candidates []string
	add := func(raw string) {
		raw = strings.TrimSpace(htmlstd.UnescapeString(strings.ReplaceAll(raw, "\\/", "/")))
		if raw == "" {
			return
		}
		address := resolveProviderURL(pageURL, raw)
		if !isProviderHTTPMediaURL(address) || seen[address] {
			return
		}
		parsed, err := url.Parse(address)
		if err != nil {
			return
		}
		path := strings.ToLower(parsed.Path)
		if !strings.HasSuffix(path, ".m3u8") && !strings.HasSuffix(path, ".mp4") {
			return
		}
		seen[address] = true
		candidates = append(candidates, address)
	}
	for _, node := range providerHTMLNodes(document, func(node *html.Node) bool {
		if node.Type != html.ElementNode {
			return false
		}
		return (node.Data == "video" || node.Data == "source") &&
			(providerHTMLAttr(node, "src") != "" || providerHTMLAttr(node, "data-src") != "")
	}) {
		add(firstNonEmpty(providerHTMLAttr(node, "src"), providerHTMLAttr(node, "data-src")))
	}

	// Scan raw page text for inline JavaScript URL fallbacks. Extract every
	// absolute URL token, then let add() validate protocol and media extension.
	body := strings.ReplaceAll(rawBody, "\\/", "/")
	body = strings.ReplaceAll(body, "\\u0026", "&")
	mediaToken := regexp.MustCompile(`(?i)https?://[^[:space:]"'<>\\]+`)
	for _, match := range mediaToken.FindAllString(body, -1) {
		add(strings.TrimRight(match, ".,);]"))
	}
	return candidates
}
func (d *Downloader) resolveMissavMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	if !valid || source != sourceMissav || !reMissavSourceID.MatchString(sourceID) {
		return providerMedia{}, errors.New("MissAV 播放資訊無效，請重新整理詳情")
	}
	site := strings.TrimRight(d.providerBaseURL(sourceMissav), "/")
	pageURL := strings.TrimSpace(task.Chapter.PageURL)
	if pageURL == "" {
		pageURL = d.missavDetailAddress(sourceID)
	}
	document, actualURL, body, err := d.fetchAdultProviderDocument(ctx, pageURL, site+"/zh/")
	if err != nil {
		return providerMedia{}, err
	}
	candidates := adultMediaCandidates(document, body, actualURL)
	if len(candidates) == 0 {
		return providerMedia{}, errors.New("MissAV 目前未回傳可用的 MP4／M3U8 播放網址；可能是來源限制或播放器格式已更新")
	}
	return d.prepareWebProviderMedia(ctx, providerMedia{
		URL: candidates[0], Referer: actualURL,
	}, "MissAV")
}

func supjavIDFromURL(raw, baseURL string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !parsed.IsAbs() {
		base, baseErr := url.Parse(baseURL)
		if baseErr != nil {
			return ""
		}
		parsed = base.ResolveReference(parsed)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "supjav.com" && host != "www.supjav.com" {
		return ""
	}
	match := reSupjavDetailID.FindStringSubmatch(parsed.Path)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func supjavDramaFromCard(anchor *html.Node, pageURL string) (Drama, bool) {
	sourceID := supjavIDFromURL(providerHTMLAttr(anchor, "href"), pageURL)
	if !webProviderNumericID.MatchString(sourceID) {
		return Drama{}, false
	}
	title := strings.TrimSpace(providerHTMLAttr(anchor, "title"))
	var covers []string
	for _, image := range providerHTMLNodes(anchor, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "img"
	}) {
		if title == "" {
			title = strings.TrimSpace(firstNonEmpty(providerHTMLAttr(image, "alt"), providerHTMLAttr(image, "title")))
		}
		covers = append(covers, adultImageCandidates(image, pageURL)...)
	}
	if title == "" {
		title = strings.TrimSpace(providerHTMLText(anchor))
	}
	if title == "" {
		title = "Supjav 影片 " + sourceID
	}
	covers = nativeUniqueCoverAddresses(covers...)
	return Drama{
		ID: providerDramaID(sourceSupjav, sourceID), Source: sourceSupjav, SourceID: sourceID,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Cover: firstNonEmpty(covers...), CoverURL: firstNonEmpty(covers...),
		CoverCandidates: adultAdditionalCoverCandidates(covers),
		ChannelName: "Supjav", CategoryName: "影片", TotalEpisode: 1, EpisodeCount: 1,
	}, true
}

func parseSupjavCards(document *html.Node, pageURL string) []Drama {
	var items []Drama
	seen := map[string]bool{}
	for _, anchor := range providerHTMLNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a"
	}) {
		drama, ok := supjavDramaFromCard(anchor, pageURL)
		if !ok || seen[drama.ID] {
			continue
		}
		seen[drama.ID] = true
		items = append(items, drama)
		if len(items) >= 500 {
			break
		}
	}
	return items
}

func (d *Downloader) supjavCatalogAddress(page int, query string) string {
	base := strings.TrimRight(d.providerBaseURL(sourceSupjav), "/") + "/zh/"
	if query != "" {
		parsed, _ := url.Parse(base)
		values := parsed.Query()
		values.Set("s", strings.TrimSpace(query))
		parsed.RawQuery = values.Encode()
		return parsed.String()
	}
	if page > 1 {
		return base + "page/" + strconv.Itoa(page) + "/"
	}
	return base
}

func (d *Downloader) fetchSupjavCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	ctx = supjavTraditionalContext(ctx)
	if page < 1 || page > 10000 || strings.TrimSpace(category) != "" || len(query) > 160 {
		return nil, false, errors.New("Supjav 目錄查詢參數無效")
	}
	address := d.supjavCatalogAddress(page, strings.TrimSpace(query))
	document, pageURL, _, err := d.fetchAdultProviderDocument(ctx, address, d.providerBaseURL(sourceSupjav)+"/zh/")
	if err != nil {
		return nil, false, err
	}
	items := parseSupjavCards(document, pageURL)
	return items, len(items) >= 20, nil
}

func (d *Downloader) fetchSupjavDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	ctx = supjavTraditionalContext(ctx)
	sourceID = strings.TrimSpace(sourceID)
	if !webProviderNumericID.MatchString(sourceID) {
		return Drama{}, nil, errors.New("Supjav 影片 ID 無效")
	}
	site := strings.TrimRight(d.providerBaseURL(sourceSupjav), "/")
	address := site + "/" + sourceID + ".html"
	document, pageURL, _, err := d.fetchAdultProviderDocument(ctx, address, site+"/zh/")
	if err != nil {
		return Drama{}, nil, err
	}
	title := strings.TrimSpace(adultPageTitle(document))
	if title == "" {
		title = "Supjav 影片 " + sourceID
	}
	covers := providerCoverAddresses(adultPageMeta(document, "og:image", "twitter:image"), pageURL)
	description := adultPageDescription(document)
	drama := Drama{
		ID: providerDramaID(sourceSupjav, sourceID), Source: sourceSupjav, SourceID: sourceID,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Desc: truncate(description, 12000), Intro: truncate(description, 12000),
		Cover: firstNonEmpty(covers...), CoverURL: firstNonEmpty(covers...),
		ChannelName: "Supjav", CategoryName: "影片", TotalEpisode: 1, EpisodeCount: 1,
	}
	chapters := []Chapter{{
		ID: providerChapterID(sourceSupjav, sourceID, "1"), Source: sourceSupjav,
		Title: "正片", VideoURL: "supjav-player://" + sourceID,
		PageURL: pageURL, Referer: site + "/zh/",
	}}
	return drama, chapters, nil
}

func supjavTVLink(document *html.Node) string {
	var fallback string
	for _, anchor := range providerHTMLNodes(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && providerHTMLAttr(node, "data-link") != ""
	}) {
		value := strings.TrimSpace(providerHTMLAttr(anchor, "data-link"))
		if value == "" {
			continue
		}
		label := strings.ToUpper(strings.TrimSpace(providerHTMLText(anchor)))
		if label == "TV" || strings.Contains(label, "TV") {
			return value
		}
		if fallback == "" {
			fallback = value
		}
	}
	return fallback
}

func (d *Downloader) resolveSupjavMedia(ctx context.Context, task Task) (providerMedia, error) {
	ctx = supjavTraditionalContext(ctx)
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	if !valid || source != sourceSupjav || !webProviderNumericID.MatchString(sourceID) {
		return providerMedia{}, errors.New("Supjav 播放資訊無效，請重新整理詳情")
	}
	site := strings.TrimRight(d.providerBaseURL(sourceSupjav), "/")
	pageURL := strings.TrimSpace(task.Chapter.PageURL)
	if pageURL == "" {
		pageURL = site + "/" + sourceID + ".html"
	}
	document, actualURL, body, err := d.fetchAdultProviderDocument(ctx, pageURL, site+"/zh/")
	if err != nil {
		return providerMedia{}, err
	}
	if direct := adultMediaCandidates(document, body, actualURL); len(direct) > 0 {
		return d.prepareWebProviderMedia(ctx, providerMedia{
			URL: direct[0], Referer: actualURL,
		}, "Supjav")
	}
	encoded := supjavTVLink(document)
	if encoded == "" {
		return providerMedia{}, errors.New("Supjav 播放頁沒有提供 TV 播放線路")
	}
	reversed := []rune(encoded)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	endpoint, _ := url.Parse(supjavPlaybackAPI)
	values := endpoint.Query()
	values.Set("c", string(reversed))
	endpoint.RawQuery = values.Encode()
	playerBody, err := d.fetchProviderText(ctx, endpoint.String(), site+"/zh/")
	if err != nil {
		return providerMedia{}, fmt.Errorf("Supjav 取得播放線路失敗：%w", err)
	}
	playerBody = strings.ReplaceAll(playerBody, "\\/", "/")
	playerBody = strings.ReplaceAll(playerBody, "\\u0026", "&")
	match := reSupjavPlayerURL.FindStringSubmatch(playerBody)
	if len(match) != 2 {
		candidates := adultMediaCandidates(nil, playerBody, endpoint.String())
		if len(candidates) == 0 {
			return providerMedia{}, errors.New("Supjav 播放伺服器未回傳有效的 M3U8 網址")
		}
		match = []string{"", candidates[0]}
	}
	mediaURL := resolveProviderURL(endpoint.String(), htmlstd.UnescapeString(match[1]))
	return d.prepareWebProviderMedia(ctx, providerMedia{
		URL: mediaURL, Referer: site+"/zh/",
	}, "Supjav")
}

func adultProviderSourceID(source, raw string) string {
	sourceID := strings.TrimSpace(raw)
	if source == sourceMissav {
		return strings.ToLower(sourceID)
	}
	return sourceID
}

func validateAdultProviderIdentity(source, sourceID string) error {
	sourceID = adultProviderSourceID(source, sourceID)
	switch canonicalProviderSource(source) {
	case sourceMissav:
		if !reMissavSourceID.MatchString(sourceID) {
			return fmt.Errorf("MissAV 影片片號無效")
		}
	case sourceSupjav:
		if !webProviderNumericID.MatchString(sourceID) {
			return fmt.Errorf("Supjav 影片 ID 無效")
		}
	default:
		return fmt.Errorf("未知的進階影片來源")
	}
	return nil
}
