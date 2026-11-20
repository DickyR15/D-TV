package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"runtime"
	"strconv"
	"strings"
	"time"

	htmlnet "golang.org/x/net/html"
)

const (
	supjavBaseURL = "https://supjav.blog"
	pppBaseURL = "https://ppp.porn"
	madouBaseURL = "https://madou.club"
	madouDashBaseURL = "https://dash.madou.club"
	ninetyOneBaseURL = "https://91porna.com"
	ninetyOneMobileUserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1"
)

var extraAdultCategories = map[string][]nativeCategory{
	sourceSupJav: {{ID:"0",Name:"最新"},{ID:"uncensored",Name:"無碼"},{ID:"uncensored-leaked",Name:"無碼破解"},{ID:"censored",Name:"有碼"},{ID:"chinese",Name:"中文字幕"},{ID:"amateur",Name:"素人"}},
	sourcePPP: {{ID:"new",Name:"最新上架"},{ID:"c-china",Name:"中國"},{ID:"c-japan",Name:"日本"},{ID:"c-taiwan",Name:"台灣"},{ID:"c-korea",Name:"韓國"},{ID:"c-hongkong",Name:"香港"},{ID:"c-se-asia",Name:"東南亞"},{ID:"c-amateur",Name:"素人自拍"},{ID:"c-cosplay",Name:"角色扮演"},{ID:"c-streamer",Name:"直播主"},{ID:"c-first-person-pov",Name:"第一人稱"},{ID:"c-china-av",Name:"中國成人影片"},{ID:"c-japan-producer",Name:"日本片商"}},
	sourceMadou: {{ID:"麻豆传媒",Name:"madou"},{ID:"麻豆番外篇",Name:"麻豆番外篇"},{ID:"麻豆花絮",Name:"麻豆花絮"},{ID:"hongkongdoll",Name:"HongKongDoll"},{ID:"psychoporntw",Name:"PsychopornTW"},{ID:"91制片厂",Name:"91製片廠"},{ID:"果冻传媒",Name:"果凍傳媒"},{ID:"蜜桃影像",Name:"蜜桃影像"},{ID:"天美传媒",Name:"天美傳媒"},{ID:"皇家华人",Name:"皇家華人"},{ID:"兔子先生",Name:"兔子先生"},{ID:"星空无限传媒",Name:"星空無限傳媒"},{ID:"爱豆",Name:"愛豆"},{ID:"麻豆导演系列",Name:"麻豆導演系列"},{ID:"大象传媒",Name:"大象傳媒"},{ID:"猫爪影像",Name:"貓爪影像"},{ID:"精东影业",Name:"精東影業"},{ID:"杏吧",Name:"杏吧"},{ID:"乐播传媒",Name:"樂播傳媒"},{ID:"草莓",Name:"草莓"},{ID:"抖阴",Name:"抖陰"},{ID:"sa国际传媒",Name:"SA國際傳媒"},{ID:"起点传媒-性视界传媒",Name:"起點傳媒／性視界傳媒"},{ID:"大鸟十八",Name:"大鳥十八"},{ID:"小鹏奇啪行",Name:"小鵬奇啪行"},{ID:"女优淫娃培训营",Name:"女優培訓營"},{ID:"淫欲游戏王",Name:"淫慾遊戲王"},{ID:"女神羞羞研究所",Name:"女神羞羞研究所"},{ID:"突袭女优家",Name:"突襲女優家"},{ID:"情趣K歌房",Name:"情趣 K 歌房"},{ID:"kiss糖果屋",Name:"KISS 糖果屋"}},
	source91Porna: {{ID:"play",Name:"正在播放"},{ID:"now_hot",Name:"目前最熱門"},{ID:"new_update",Name:"最近更新"},{ID:"original",Name:"91 原創"},{ID:"now_month_hot",Name:"本月最熱門"},{ID:"ten_minutes",Name:"10 分鐘以上"},{ID:"twenty_minutes",Name:"20 分鐘以上"},{ID:"now_month_collect",Name:"本月收藏"},{ID:"hd",Name:"高畫質"},{ID:"month_hot",Name:"上月最熱門"},{ID:"now_month_comment",Name:"本月討論"},{ID:"max_collect",Name:"收藏最多"}},
}

func extraAdultBaseURL(source string) string {
	switch canonicalProviderSource(source) {
	case sourceSupJav: return supjavBaseURL
	case sourcePPP: return pppBaseURL
	case sourceMadou: return madouBaseURL
	case source91Porna: return ninetyOneBaseURL
	default: return ""
	}
}
func extraAdultSourceName(source string) string {
	switch canonicalProviderSource(source) {
	case sourceSupJav: return "SupJav"
	case sourcePPP: return "ppp"
	case sourceMadou: return "madou"
	case source91Porna: return "91porna"
	default: return "進階片源"
	}
}
func extraAdultValidCategory(source, category string) bool {
	source = canonicalProviderSource(source)
	if category == "" { return true }
	// Keep accepting the legacy "0" selection from saved Madou preferences,
	// but expose only the shared "全部" option in the category picker.
	if source == sourceMadou && category == "0" { return true }
	for _, item := range extraAdultCategories[source] { if item.ID == category { return true } }
	return false
}
func extraAdultCategoriesFor(source string) []nativeCategory {
	return append([]nativeCategory(nil), extraAdultCategories[canonicalProviderSource(source)]...)
}

// parseExtraAdultCategories is the shared category registry used by catalog
// and playback setup for the supported independent adult providers.
func parseExtraAdultCategories(source string) []nativeCategory {
	return extraAdultCategoriesFor(source)
}
func extraAdultCategoryPath(source string, page int, category string) (string,error) {
	source=canonicalProviderSource(source)
	if page<1 || page>100000 || !extraAdultValidCategory(source,category) { return "",errors.New("進階片源分類或頁碼無效") }
	switch source {
	case sourceSupJav:
		if category=="" { category="0" }; return fmt.Sprintf("/%s?page=%d",url.PathEscape(category),page),nil
	case sourcePPP:
		if category=="" || category=="new" { if page==1{return "/new/",nil};return fmt.Sprintf("/new/%d/",page),nil }
		slug:=strings.TrimPrefix(category,"c-");if page==1{return "/categories/"+url.PathEscape(slug)+"/",nil};return fmt.Sprintf("/categories/%s/%d/",url.PathEscape(slug),page),nil
	case sourceMadou:
		if category=="" || category=="0" {
			if page==1 { return "/", nil }
			return fmt.Sprintf("/page/%d/", page), nil
		}
		path := "/category/" + url.PathEscape(category)
		if page > 1 { path += fmt.Sprintf("/page/%d", page) }
		return path, nil
	case source91Porna:
		if category==""||category=="new"{category="play"}
		path := "/comic/index/video?category=" + url.QueryEscape(category)
		if page > 1 { path += "&page=" + strconv.Itoa(page) }
		return path, nil
	}
	return "",errors.New("不支援的進階片源")
}
func extraAdultSearchPath(source string,page int,query string)(string,bool,error){
	query=strings.TrimSpace(query);if query==""||len([]rune(query))>256||page<1||page>100000{return "",false,errors.New("搜尋條件無效")};q:=url.QueryEscape(query)
	switch canonicalProviderSource(source){
	case sourceSupJav:
		p := "/search?keyword=" + q
		if page > 1 { p += "&page=" + strconv.Itoa(page) }
		return p, true, nil
	case sourcePPP:
		p := "/search/?q=" + q
		if page > 1 { p += "&page=" + strconv.Itoa(page) }
		return p, true, nil
	case sourceMadou:
		if page == 1 { return "/?s=" + q, true, nil }
		return fmt.Sprintf("/page/%d/?s=%s", page, q), true, nil
	case source91Porna:
		p := "/comic/index/search?keyword=" + q
		if page > 1 { p += "&page=" + strconv.Itoa(page) }
		return p, true, nil
	};return "",false,errors.New("不支援的進階片源搜尋")
}
func extraAdultNormalizeID(source,raw string)string{
	source=canonicalProviderSource(source);raw=strings.TrimSpace(html.UnescapeString(raw))
	if parsed,err:=url.Parse(raw);err==nil{
		if source==sourceMadou{
			if parsed.IsAbs(){
				host:=strings.ToLower(parsed.Hostname())
				if parsed.Scheme!="https" || parsed.User!=nil || (host!="madou.club" && !strings.HasSuffix(host,".madou.club")){return ""}
			}
			raw=parsed.Path
		}else if parsed.IsAbs(){
			raw=parsed.EscapedPath()
			if source==source91Porna&&parsed.RawQuery!=""{raw+="?"+parsed.RawQuery}
		}
	}
	raw=strings.TrimLeft(raw,"/")
	switch source{
	case sourceSupJav:if strings.HasPrefix(raw,"video.php?slug="){return raw};return strings.TrimRight(raw,"/")
	case sourcePPP:return strings.Trim(strings.TrimPrefix(raw,"v/"),"/")
	case sourceMadou:return strings.TrimRight(raw,"/")
	case source91Porna:if parsed,err:=url.Parse(raw);err==nil&&strings.Contains(raw,"video_key="){return parsed.Query().Get("video_key")};return raw
	};return ""
}
func extraAdultValidID(source,id string)bool{
	switch canonicalProviderSource(source){
	case sourceSupJav:return regexp.MustCompile("^(video/[A-Za-z0-9._-]{1,180}|video\\.php\\?slug=[A-Za-z0-9._-]{1,180})$").MatchString(id)
	case sourcePPP:return regexp.MustCompile("^[a-z0-9]{1,100}$").MatchString(id)
	case sourceMadou:return regexp.MustCompile("^[\\p{L}\\p{N}][\\p{L}\\p{N}._-]{0,180}(\\.html)?$").MatchString(id)
	case source91Porna:return regexp.MustCompile("^[A-Za-z0-9_-]{1,128}$").MatchString(id)
	};return false
}
func extraAdultDetailURL(source,id string)string{
	if !extraAdultValidID(source,id){return ""};base:=extraAdultBaseURL(source)
	switch canonicalProviderSource(source){
	case sourceSupJav:if strings.HasPrefix(id,"video.php?")||strings.HasPrefix(id,"video/"){return base+"/"+id};return base+"/video/"+id
	case sourcePPP:return base+"/v/"+url.PathEscape(id)+"/"
	case sourceMadou:return base+"/"+url.PathEscape(id)
	case source91Porna:return base+"/comic/index/detail?video_key="+url.QueryEscape(id)
	};return ""
}
func extraAdultDetailURLs(source,id string)[]string {
	primary:=extraAdultDetailURL(source,id)
	if primary=="" { return nil }
	if canonicalProviderSource(source)!=source91Porna { return []string{primary} }
	legacy:=extraAdultBaseURL(source)+"/comic/index/avdetail?video_key="+url.QueryEscape(id)
	if legacy==primary { return []string{primary} }
	return []string{primary,legacy}
}
func extraAdultUserAgent(source string) string {
	if canonicalProviderSource(source)==source91Porna { return ninetyOneMobileUserAgent }
	return adultUserAgent
}
func extraAdultFetch(ctx context.Context,d *Downloader,source,address,referer string)(string,string,error){
	return extraAdultFetchWithUserAgent(ctx,d,source,address,referer,extraAdultUserAgent(source))
}

func extraAdultFetchWithUserAgent(ctx context.Context,d *Downloader,source,address,referer,agent string)(string,string,error){
	source = canonicalProviderSource(source)
	if strings.TrimSpace(agent)=="" { agent=adultUserAgent }
	if source == source91Porna || source == sourcePPP {
		ctx = context.WithValue(ctx, nativeRPZFallbackKey{}, true)
	}
	ctx=context.WithValue(ctx,providerTextUserAgentKey{},agent);rs:=&playbackResponseURLs{};ctx=context.WithValue(ctx,playbackResponseURLsKey{},rs)
	body,e:=d.fetchProviderText(ctx,address,firstNonEmpty(referer,extraAdultBaseURL(source)+"/"));if e!=nil{return "",address,e}
	if actual,ok:=rs.values.Load(address);ok{if v,valid:=actual.(string);valid&&v!=""{address=v}}
	return body,address,nil
}
func extraAdultSegments(body,source string)[]string{
	var re *regexp.Regexp
	switch source{
	case sourceSupJav:re=regexp.MustCompile("(?i)<div\\b[^>]*class=['\\x22]box-item['\\x22][^>]*>")
	case sourcePPP:re=regexp.MustCompile("(?i)<div\\b[^>]*class=['\\x22][^'\\x22]*item\\s+card-video[^'\\x22]*['\\x22][^>]*>")
	case sourceMadou:re=regexp.MustCompile("(?i)<article\\b[^>]*class=['\\x22][^'\\x22]*excerpt[^'\\x22]*['\\x22][^>]*>")
	case source91Porna:re=regexp.MustCompile("(?i)<div\\b[^>]*class=['\\x22]video-item['\\x22][^>]*>")
	default:return nil}
	starts:=re.FindAllStringIndex(body,-1);out:=make([]string,0,len(starts))
	for i,x:=range starts{end:=len(body);if i+1<len(starts){end=starts[i+1][0]}else if end>x[0]+6000{end=x[0]+6000};out=append(out,body[x[0]:end])};return out
}
func extraAdultHasNextPage(body, baseURL string, currentPage int, source string) bool {
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || currentPage < 1 {
		return false
	}
	doc, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		return false
	}
	pagePathPatterns := map[string]*regexp.Regexp{
		sourcePPP: regexp.MustCompile("(?i)/(?:new|categories/[^/]+)/([0-9]+)/?$"),
		sourceMadou: regexp.MustCompile("(?i)/(?:page|category/[^/]+/page)/([0-9]+)/?$"),
	}
	for _, anchor := range providerHTMLNodes(doc, func(n *htmlnet.Node) bool {
		return n.Type == htmlnet.ElementNode && n.Data == "a"
	}) {
		raw := strings.TrimSpace(providerHTMLAttr(anchor, "href"))
		if raw == "" || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
			continue
		}
		reference, parseErr := url.Parse(html.UnescapeString(raw))
		if parseErr != nil || reference.User != nil {
			continue
		}
		target := base.ResolveReference(reference)
		if target.Scheme != "https" || !strings.EqualFold(target.Hostname(), base.Hostname()) || target.User != nil {
			continue
		}

		nextPage := 0
		if value, convErr := strconv.Atoi(target.Query().Get("page")); convErr == nil {
			nextPage = value
		}
		if pattern := pagePathPatterns[source]; pattern != nil {
			if match := pattern.FindStringSubmatch(target.EscapedPath()); len(match) > 1 {
				if value, convErr := strconv.Atoi(match[1]); convErr == nil && value > nextPage {
					nextPage = value
				}
			}
		}
		if nextPage > 0 {
			if nextPage > currentPage {
				return true
			}
			continue
		}

		rel := strings.ToLower(providerHTMLAttr(anchor, "rel"))
		class := strings.ToLower(providerHTMLAttr(anchor, "class"))
		title := strings.ToLower(providerHTMLAttr(anchor, "title"))
		label := strings.ToLower(strings.TrimSpace(providerHTMLPlainText(providerHTMLText(anchor))))
		if strings.Contains(rel, "next") ||
			strings.Contains(class, "next") ||
			strings.Contains(title, "next") ||
			strings.Contains(label, "next") ||
			strings.Contains(label, "下一页") ||
			strings.Contains(label, "下一頁") ||
			strings.Contains(label, "下页") ||
			strings.Contains(label, "下頁") ||
			label == "»" || label == "›" || label == "→" {
			return true
		}
	}
	return false
}

// extraAdult91PornaDetailLink normalizes both relative and absolute
// links returned by the live catalogue. The old selector only matched
// href="/comic/index/detail?..."; absolute hrefs (and //host/... links) were
// silently dropped, leaving stale cached video_key values to be diagnosed.
func extraAdult91PornaDetailLink(raw, base string) (string, bool) {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" || strings.ContainsAny(raw, "\r\n\x00") {
		return "", false
	}
	baseURL, err := url.Parse(strings.TrimSpace(base))
	if err != nil || baseURL.Scheme != "https" ||
		(baseURL.Hostname() != "91porna.com" && baseURL.Hostname() != "www.91porna.com") {
		return "", false
	}
	reference, err := url.Parse(raw)
	if err != nil || reference.User != nil {
		return "", false
	}
	target := baseURL.ResolveReference(reference)
	if target.Scheme != "https" || target.User != nil ||
		(target.Hostname() != "91porna.com" && target.Hostname() != "www.91porna.com") {
		return "", false
	}
	if target.Path != "/comic/index/detail" && target.Path != "/comic/index/avdetail" {
		return "", false
	}
	id := strings.TrimSpace(target.Query().Get("video_key"))
	if !extraAdultValidID(source91Porna, id) {
		return "", false
	}
	return id, true
}

// extraAdult91PornaDetailPageError avoids treating a 200 challenge/login shell
// or a redirect to a different page as a valid detail result. It also makes a
// route fallback meaningful: a failed /detail candidate can be followed by
// /avdetail, rather than stopping on the first non-empty HTTP response.
func extraAdult91PornaDetailPageError(id, body, page string) error {
	parsed, err := url.Parse(strings.TrimSpace(page))
	if err != nil || parsed.Scheme != "https" ||
		(parsed.Hostname() != "91porna.com" && parsed.Hostname() != "www.91porna.com") ||
		(parsed.Path != "/comic/index/detail" && parsed.Path != "/comic/index/avdetail") ||
		parsed.Query().Get("video_key") != id {
		return errors.New("91Porna 詳情頁網址與作品 video_key 不相符")
	}
	lower := strings.ToLower(body)
	for _, marker := range []string{
		"just a moment", "cf-chl-", "challenge-platform",
		"checking your browser", "access denied", "video not found",
		"video has been removed", "該影片已刪除", "影片不存在",
		"視頻不存在", "视频不存在",
	} {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return fmt.Errorf("91Porna 詳情頁回傳驗證頁或作品不存在訊息：%s", marker)
		}
	}
	hasDetailMarker := strings.Contains(lower, "<h1") ||
		strings.Contains(lower, "<title") ||
		strings.Contains(lower, "og:title") ||
		strings.Contains(lower, "data-video") ||
		strings.Contains(lower, "embed_play.js") ||
		strings.Contains(lower, "/comic/index/embed")
	if !hasDetailMarker {
		return errors.New("91Porna 回應不是可辨識的作品詳情頁")
	}
	return nil
}


func extraAdultParseCard(source,chunk,base string)(Drama,bool){
	var id,title,cover,remark string
	switch source{
	case sourceSupJav:
		m:=regexp.MustCompile("(?is)href=['\\x22]([^'\\x22]*(?:video/|video\\.php\\?slug=)[^'\\x22]*)['\\x22]").FindStringSubmatch(chunk);if len(m)<2{return Drama{},false};id=extraAdultNormalizeID(source,m[1]);img:=regexp.MustCompile("(?is)<img\\b[^>]*>").FindString(chunk);if img!=""{cover=adultAttribute(img,"src");if cover==""{cover=adultAttribute(img,"data-src")};title=adultAttribute(img,"alt")};if m:=regexp.MustCompile("(?is)<div\\b[^>]*class=['\\x22]detail['\\x22][^>]*>(.*?)</div>").FindStringSubmatch(chunk);len(m)>1{title=adultClean(m[1])}
	case sourcePPP:
		m:=regexp.MustCompile("(?i)/v/([a-z0-9]{1,100})/").FindStringSubmatch(chunk);if len(m)<2{return Drama{},false};id=m[1];img:=regexp.MustCompile("(?is)<img\\b[^>]*>").FindString(chunk);if img!=""{cover=adultAttribute(img,"data-src");if cover==""{cover=adultAttribute(img,"src")};title=adultAttribute(img,"alt")};if m:=regexp.MustCompile("(?is)card-video__duration[^>]*>([^<]+)<").FindStringSubmatch(chunk);len(m)>1{remark=adultClean(m[1])}
	case sourceMadou:
		for _, anchor := range adultAnchorPattern.FindAllStringSubmatch(chunk, -1) {
			if len(anchor) < 3 { continue }
			href := adultAttribute("<a "+anchor[1]+">", "href")
			candidate := extraAdultNormalizeID(source, href)
			if extraAdultValidID(source, candidate) {
				id = candidate
				if title == "" { title = adultClean(anchor[2]) }
				break
			}
		}
		if id == "" { return Drama{}, false }
		img := adultImagePattern.FindString(chunk)
		if img != "" {
			for _, attribute := range []string{"data-original", "data-src", "data-lazy-src", "src"} {
				cover = adultAttribute(img, attribute)
				if cover != "" && !strings.HasPrefix(strings.ToLower(cover), "data:") { break }
				cover = ""
			}
		}
		if match := regexp.MustCompile("(?is)<h2\\b[^>]*>(.*?)</h2>").FindStringSubmatch(chunk); len(match) > 1 {
			title = adultClean(match[1])
		}
	case source91Porna:
		for _, anchor := range adultAnchorPattern.FindAllStringSubmatch(chunk, -1) {
			if len(anchor) < 3 { continue }
			href := adultAttribute("<a "+anchor[1]+">", "href")
			if candidate, ok := extraAdult91PornaDetailLink(href, base); ok {
				id = candidate
				break
			}
		}
		if id == "" { return Drama{}, false }
		img := regexp.MustCompile("(?is)<img\\b[^>]*>").FindString(chunk)
		if img != "" {
			cover = adultAttribute(img, "data-src")
			if cover == "" { cover = adultAttribute(img, "data-original") }
			if cover == "" { cover = adultAttribute(img, "src") }
			title = adultAttribute(img, "alt")
		}
		if m := regexp.MustCompile("(?i)([0-9]{1,3}:[0-9]{2})").FindStringSubmatch(chunk); len(m) > 1 {
			remark = m[1]
		}
	}
	if !extraAdultValidID(source,id){return Drama{},false};title=adultClean(title);if title==""{title=id};cover=adultSafeImageURL(base,cover)
	return Drama{ID:providerDramaID(source,id),Source:source,SourceID:id,Title:truncate(title,512),Name:truncate(title,512),Cover:cover,CoverURL:cover,ChannelName:extraAdultSourceName(source),CategoryName:extraAdultSourceName(source),Remark:truncate(remark,64),TotalEpisode:1,EpisodeCount:1},true
}
func (d *Downloader) fetchExtraAdultCatalog(ctx context.Context, source string, page int, category, query string) ([]Drama, bool, error) {
	source = canonicalProviderSource(source)
	if page < 1 || page > 100000 || !extraAdultValidCategory(source, category) {
		return nil, false, errors.New("進階片源目錄條件無效")
	}
	base := extraAdultBaseURL(source)
	var address string
	if strings.TrimSpace(query) != "" {
		path, _, err := extraAdultSearchPath(source, page, query)
		if err != nil { return nil, false, err }
		address = base + path
	} else {
		path, err := extraAdultCategoryPath(source, page, category)
		if err != nil { return nil, false, err }
		address = base + path
	}
	body, _, err := extraAdultFetch(ctx, d, source, address, base+"/")
	if err != nil {
		return nil, false, fmt.Errorf("%s 目錄讀取失敗：%w", extraAdultSourceName(source), err)
	}
	items := make([]Drama, 0, 40)
	seen := map[string]bool{}
	for _, block := range extraAdultSegments(body, source) {
		item, ok := extraAdultParseCard(source, block, base)
		if !ok || seen[item.ID] { continue }
		seen[item.ID] = true
		items = append(items, item)
		if len(items) >= 100 { break }
	}
	limit := 20
	if source == sourcePPP || source == sourceMadou { limit = 12 }
	if source == source91Porna { limit = 10 }
	more := extraAdultHasNextPage(body, base, page, source) || len(items) >= limit
	return items, more, nil
}
// extraAdultMobileScanRequired detects the current 91Porna desktop detail
// template, which exposes a phone-browser QR handoff instead of a web player.
func extraAdultMobileScanRequired(body string) bool {
	plain := adultClean(body)
	return strings.Contains(plain, "手機瀏覽器") &&
		(strings.Contains(plain, "掃碼觀看") || strings.Contains(plain, "扫码观看"))
}

func extraAdultTitle(body,fallback string)string{
	t:=adultClean(adultMetaValue(body,"og:title"));if t==""{t=adultClean(adultMetaValue(body,"twitter:title"))};if t==""{if m:=regexp.MustCompile("(?is)<h1\\b[^>]*>(.*?)</h1>").FindStringSubmatch(body);len(m)>1{t=adultClean(m[1])}};if t==""{if m:=regexp.MustCompile("(?is)<title\\b[^>]*>(.*?)</title>").FindStringSubmatch(body);len(m)>1{t=adultClean(m[1])}};if t==""{t=fallback};t=regexp.MustCompile("(?i)\\s*[-|]\\s*(?:SupJav|PPP\\.Porn|麻豆.*|91porna|91視頻).*$").ReplaceAllString(t,"");return strings.TrimSpace(t)
}
func (d *Downloader) fetchExtraAdultDetail(ctx context.Context,source,id string)(Drama,[]Chapter,error){
	source=canonicalProviderSource(source);if !extraAdultValidID(source,id){return Drama{},nil,errors.New("作品識別碼無效")};base:=extraAdultBaseURL(source)
	var body,final string
	var e error
	for _,address:=range extraAdultDetailURLs(source,id) {
		body,final,e=extraAdultFetch(ctx,d,source,address,base+"/")
		if e == nil && source == source91Porna {
			if validationErr := extraAdult91PornaDetailPageError(id, body, final); validationErr != nil {
				e = validationErr
			}
		}
		if e==nil { break }
		if ctx.Err()!=nil { return Drama{},nil,ctx.Err() }
	}
	if e!=nil{return Drama{},nil,fmt.Errorf("%s 作品詳情讀取失敗：%w",extraAdultSourceName(source),e)}
	title:=extraAdultTitle(body,id);desc:=adultClean(adultMetaValue(body,"description"))
	if source==source91Porna && extraAdultMobileScanRequired(body) {
		const note = "網站頁面顯示手機掃碼入口；D-TV 會另外解析作品嵌入播放器，若頁面沒有提供可用串流才會無法播放。"
		if desc!="" { desc += "\n\n" }
		desc += note
	}
	cover:="";for _,k:=range []string{"og:image","twitter:image"}{if v:=adultSafeImageURL(base,adultMetaValue(body,k));v!=""{cover=v;break}};if cover==""{images:=adultImageCandidates(body,base,source);if len(images)>0{cover=images[0]}}
	drama:=Drama{ID:providerDramaID(source,id),Source:source,SourceID:id,Title:truncate(title,512),Name:truncate(title,512),Desc:truncate(desc,12000),Intro:truncate(desc,12000),Cover:cover,CoverURL:cover,ChannelName:extraAdultSourceName(source),CategoryName:extraAdultSourceName(source),TotalEpisode:1,EpisodeCount:1}
	ch:=Chapter{ID:providerChapterID(source,id,final),Source:source,Title:"正片",PageURL:final,Referer:final,VideoURL:"adult-page://"+final,CurrentEpisode:json.RawMessage("1")};return drama,[]Chapter{ch},nil
}
func extraAdultFrames(page, body, source string) []string {
	pattern := regexp.MustCompile("(?is)<iframe\\b[^>]*\\bsrc\\s*=\\s*(?:['\\x22]([^'\\x22]+)['\\x22]|([^\\s>]+))")
	seen := map[string]bool{}
	var result []string
	for _, match := range pattern.FindAllStringSubmatch(body, 16) {
		if len(match) < 3 { continue }
		raw := firstNonEmpty(match[1], match[2])
		address := adultResolvePlaybackURL(page, strings.TrimRight(raw, "/>"))
		if address == "" || adultSafeURL(page, address, source) == "" || seen[address] { continue }
		seen[address] = true
		result = append(result, address)
	}
	return result
}

func extraAdultFrame(page, body string) string {
	// Keep this helper provider-neutral: Madou and SupJav have their own
	// stricter host validation immediately after extracting the iframe URL.
	pattern := regexp.MustCompile("(?is)<iframe\\b[^>]*\\bsrc\\s*=\\s*(?:['\\x22]([^'\\x22]+)['\\x22]|([^\\s>]+))")
	match := pattern.FindStringSubmatch(body)
	if len(match) < 3 {
		return ""
	}
	raw := firstNonEmpty(match[1], match[2])
	return adultResolvePlaybackURL(page, strings.TrimRight(raw, "/>"))
}

func extraAdultPPPIsPreviewMediaURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	lowerPath := strings.ToLower(parsed.Path)
	for _, segment := range strings.Split(lowerPath, "/") {
		switch strings.TrimSpace(segment) {
		case "preview", "previews", "trailer", "trailers", "teaser", "teasers", "sample", "samples", "thumb", "thumbnail":
			return true
		}
	}
	base := strings.ToLower(path.Base(lowerPath))
	for _, marker := range []string{"preview", "trailer", "teaser", "sample"} {
		if strings.Contains(base, marker) {
			return true
		}
	}
	query := parsed.Query()
	for key, values := range query {
		normalizedKey := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
		if normalizedKey == "preview" || normalizedKey == "is_preview" || normalizedKey == "teaser" {
			for _, value := range values {
				switch strings.ToLower(strings.TrimSpace(value)) {
				case "1", "true", "yes", "preview", "teaser":
					return true
				}
			}
		}
	}
	return false
}

type extraAdultPPPStreamRoute struct {
	address, referer string
	lowResolution    bool
}

func extraAdultPPPPreferFullStreams(routes []extraAdultPPPStreamRoute) {
	sort.SliceStable(routes, func(i, j int) bool {
		return !routes[i].lowResolution && routes[j].lowResolution
	})
}

// PPP occasionally exposes a short, low-resolution teaser before the actual
// player stream. Keep these URLs as a last-resort fallback, but never let a
// 180p/240p rendition outrank a normal-resolution stream.
func extraAdultPPPLowResolutionMediaURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	qualityPattern := regexp.MustCompile(`(?i)(?:^|[^0-9])(144|180|240)(?:p|[^0-9]|$)`)
	if qualityPattern.MatchString(parsed.Path) {
		return true
	}
	for key, values := range parsed.Query() {
		switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
		case "quality", "resolution", "definition", "height", "video_height":
			for _, value := range values {
				value = strings.ToLower(strings.TrimSpace(value))
				value = strings.TrimSuffix(value, "p")
				if height, parseErr := strconv.Atoi(value); parseErr == nil && height > 0 && height <= 240 {
					return true
				}
			}
		}
	}
	return false
}

func extraAdultPPPStreamURLs(body, page string) []string {
	body = strings.NewReplacer("\\/", "/", "\\u002F", "/", "\\u0026", "&").Replace(html.UnescapeString(body))
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?is)\b(?:var|let|const)\s+(?:stream|m3u8|videoUrl|video_url|url|file|hls|backup|backup_url)\s*=\s*['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?is)['"](?:m3u8|hls|file|url|videoUrl|video_url|backup|backup_url)['"]\s*:\s*['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?is)\b(?:src|file|url|backup|backup_url|stream|m3u8|hls)\s*:\s*['"]([^'"]+)['"]`),
	}
	candidates := make([]string, 0, 24)
	for _, pattern := range patterns {
		for _, match := range pattern.FindAllStringSubmatch(body, 64) {
			if len(match) > 1 { candidates = append(candidates, match[1]) }
		}
	}
	tagPattern := regexp.MustCompile(`(?is)<(?:video|source)\b[^>]*>`)
	for _, tag := range tagPattern.FindAllString(body, 64) {
		if raw := firstNonEmpty(adultAttribute(tag, "src"), adultAttribute(tag, "data-src")); raw != "" { candidates = append(candidates, raw) }
	}
	directPattern := regexp.MustCompile(`(?i)https?://[^"'<>[:space:]\\]+?\.(?:m3u8|mp4)(?:\?[^"'<>[:space:]\\]*)?`)
	candidates = append(candidates, directPattern.FindAllString(body, 128)...)
	seen := map[string]bool{}
	var result []string
	for _, raw := range candidates {
		media := adultResolvePlaybackURL(page, strings.TrimRight(strings.TrimSpace(raw), "),;]}"))
		parsed, err := url.Parse(media)
		if err != nil || !isProviderHTTPMediaURL(media) || parsed.User != nil { continue }
		mediaPath := strings.ToLower(parsed.Path)
		if !strings.Contains(mediaPath, "/stream/") || !strings.HasSuffix(mediaPath, ".m3u8") ||
			extraAdultPPPIsPreviewMediaURL(media) || seen[media] { continue }
		seen[media] = true
		result = append(result, media)
		if len(result) >= 16 { break }
	}
	return result
}
func extraAdultPPPStreamURL(body, page string) string {
	candidates := extraAdultPPPStreamURLs(body, page)
	if len(candidates) == 0 { return "" }
	return candidates[0]
}

func (d *Downloader) resolveExtraAdultPPPStreams(ctx context.Context, body, page string) ([]providerMedia, error) {
	var routes []extraAdultPPPStreamRoute
	seen := map[string]bool{}
	add := func(address, referer string) {
		address = strings.TrimSpace(address)
		parsed, err := url.Parse(address)
		if err != nil || !isProviderHTTPMediaURL(address) || parsed.User != nil ||
			!strings.Contains(strings.ToLower(parsed.Path), "/stream/") ||
			!strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") ||
			extraAdultPPPIsPreviewMediaURL(address) || seen[address] {
			return
		}
		seen[address] = true
		routes = append(routes, extraAdultPPPStreamRoute{
			address: address, referer: referer,
			lowResolution: extraAdultPPPLowResolutionMediaURL(address),
		})
	}
	preferredRouteCount := func() int {
		count := 0
		for _, candidate := range routes {
			if !candidate.lowResolution {
				count++
			}
		}
		return count
	}
	addPage := func(sourceBody, sourcePage string) {
		candidates := extraAdultPPPStreamURLs(sourceBody, sourcePage)
		if len(candidates) == 0 {
			if candidate := extraAdultHLS(sourceBody, sourcePage); candidate != "" {
				candidates = append(candidates, candidate)
			}
		}
		for _, address := range candidates {
			add(address, sourcePage)
		}
	}
	addPage(body, page)
	for _, frame := range extraAdultFrames(page, body, sourcePPP) {
		if preferredRouteCount() >= 12 {
			break
		}
		embed, embedURL, err := extraAdultFetch(ctx, d, sourcePPP, frame, page)
		if err != nil {
			continue
		}
		beforePreferred := preferredRouteCount()
		addPage(embed, embedURL)
		// A short 180p preview must not stop the parser from following the
		// actual player iframe (and its nested iframe) to the full stream.
		if preferredRouteCount() == beforePreferred {
			for _, nestedFrame := range extraAdultFrames(embedURL, embed, sourcePPP) {
				if preferredRouteCount() >= 12 {
					break
				}
				nested, nestedURL, nestedErr := extraAdultFetch(ctx, d, sourcePPP, nestedFrame, embedURL)
				if nestedErr != nil {
					continue
				}
				addPage(nested, nestedURL)
			}
		}
	}
	if len(routes) == 0 {
		return nil, errors.New("PPP 頁面沒有找到完整的 /stream/ HLS 播放清單（已略過預覽影片）")
	}
	// Keep real streams first and reserve 144p/180p/240p renditions for
	// fallback only when the page exposes no better stream.
	extraAdultPPPPreferFullStreams(routes)
	media := make([]providerMedia, 0, len(routes))
	for _, candidate := range routes {
		credentials := &providerMediaCredentials{referer: candidate.referer, userAgent: adultUserAgent}
		media = append(media, providerMedia{
			URL: candidate.address, Referer: candidate.referer,
			credentials: credentials, StandardHTTP: runtime.GOOS == "android",
		})
	}
	return media, nil
}

func (d *Downloader) resolveExtraAdultPPPStream(ctx context.Context, body, page string) (string, string, error) {
	routes, err := d.resolveExtraAdultPPPStreams(ctx, body, page)
	if err != nil { return "", page, err }
	return routes[0].URL, routes[0].Referer, nil
}


func extraAdult91PornaEmbedCandidates(id, page, body string) []string {
	base, err := url.Parse(strings.TrimSpace(page))
	if err != nil || base.Scheme != "https" ||
		(base.Hostname() != "91porna.com" && base.Hostname() != "www.91porna.com") ||
		!extraAdultValidID(source91Porna, id) {
		return nil
	}
	seen := map[string]bool{}
	var result []string
	add := func(raw string) {
		value := normalize91PornaEmbedURL(raw, base)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	// Current detail pages are keyed by video_key. Prefer the matching embed
	// route, then keep the older id parameter as a compatibility fallback.
	add(ninetyOneBaseURL + "/comic/index/embed?video_key=" + url.QueryEscape(id))
	add(ninetyOneBaseURL + "/comic/index/embed?id=" + url.QueryEscape(id))
	if document, parseErr := htmlnet.Parse(strings.NewReader(body)); parseErr == nil {
		for _, node := range providerHTMLNodes(document, func(node *htmlnet.Node) bool {
			return node.Type == htmlnet.ElementNode && node.Data == "iframe"
		}) {
			for _, attribute := range []string{"src", "data-src"} {
				if raw := strings.TrimSpace(providerHTMLAttr(node, attribute)); raw != "" {
					add(raw)
				}
			}
		}
	}
	_, embeddedURL := extraAdult91PornaPlayParams(body)
	if embeddedURL != "" {
		add(embeddedURL)
	}
	return result
}

func extraAdult91PornaImageFallback(body, page string) string {
	if img, _ := extraAdult91PornaPlayParams(body); strings.TrimSpace(img) != "" {
		return strings.TrimSpace(img)
	}
	candidates := []string{
		adultMetaValue(body, "og:image"),
		adultMetaValue(body, "twitter:image"),
	}
	if document, err := htmlnet.Parse(strings.NewReader(body)); err == nil {
		for _, node := range providerHTMLNodes(document, func(node *htmlnet.Node) bool {
			if node.Type != htmlnet.ElementNode {
				return false
			}
			if node.Data == "video" {
				return providerHTMLAttr(node, "poster") != ""
			}
			return node.Data == "img" &&
				(providerHTMLClass(node, "cover") || providerHTMLClass(node, "poster") ||
					providerHTMLClass(node, "thumb") || providerHTMLClass(node, "video-cover"))
		}) {
			attributes := []string{"poster", "data-original", "data-src", "data-lazy-src", "src"}
			for _, attribute := range attributes {
				if raw := strings.TrimSpace(providerHTMLAttr(node, attribute)); raw != "" {
					candidates = append(candidates, raw)
				}
			}
		}
	}
	for _, raw := range candidates {
		raw = strings.TrimSpace(html.UnescapeString(raw))
		if raw == "" || strings.ContainsAny(raw, "\r\n\x00") {
			continue
		}
		if parsed, err := url.Parse(raw); err == nil {
			if !parsed.IsAbs() {
				if base, baseErr := url.Parse(page); baseErr == nil {
					parsed = base.ResolveReference(parsed)
				}
			}
			if parsed.Hostname() == "" || (strings.EqualFold(parsed.Hostname(), "91porna.com") || strings.EqualFold(parsed.Hostname(), "www.91porna.com")) {
				value := strings.TrimLeft(parsed.EscapedPath(), "/")
				if value != "" && len(value) <= 2048 {
					return value
				}
			}
		}
		if len(raw) <= 2048 {
			return raw
		}
	}
	return ""
}

// extraAdult91PornaPlayScriptURL prefers the exact script src emitted by the
// embed page. Rebuilding only img/u used to drop additional signed arguments
// and could also call the wrong path when the site moved embed_play.js.
func extraAdult91PornaPlayScriptURL(body, page string) (string, error) {
	normalize := func(value string) string {
		return strings.NewReplacer(
			"\\/", "/",
			"\\u002F", "/",
			"\\u0026", "&",
			"\\u003D", "=",
			"&amp;", "&",
			"\\\"", "\"",
		).Replace(html.UnescapeString(value))
	}
	base, err := url.Parse(strings.TrimSpace(page))
	if err != nil || base.Scheme != "https" ||
		(base.Hostname() != "91porna.com" && base.Hostname() != "www.91porna.com") ||
		base.User != nil {
		return "", errors.New("91Porna 嵌入頁網址無效")
	}

	imgFallback, uFallback := extraAdult91PornaPlayParams(body)
	imgFallback = strings.TrimSpace(imgFallback)
	if decoded, decodeErr := url.QueryUnescape(imgFallback); decodeErr == nil && decoded != "" {
		imgFallback = decoded
	}
	if imgFallback == "" {
		imgFallback = extraAdult91PornaImageFallback(body, page)
	}
	uFallback = normalize91PornaEmbedURL(uFallback, base)

	candidates := make([]string, 0, 8)
	document, parseErr := htmlnet.Parse(strings.NewReader(body))
	if parseErr == nil {
		for _, node := range providerHTMLNodes(document, func(node *htmlnet.Node) bool {
			return node.Type == htmlnet.ElementNode && node.Data == "script"
		}) {
			for _, attribute := range []string{"src", "data-src"} {
				if value := strings.TrimSpace(providerHTMLAttr(node, attribute)); value != "" {
					candidates = append(candidates, normalize(value))
				}
			}
		}
	}
	normalizedBody := normalize(body)
	scriptPattern := regexp.MustCompile(`(?is)(?:https?:)?//[^"'<>[:space:]]*embed_play\.js(?:\?[^"'<>[:space:]]*)?|(?:\.\.?/|/)?[A-Za-z0-9_./-]*embed_play\.js(?:\?[^"'<>[:space:]]*)?`)
	for _, match := range scriptPattern.FindAllString(normalizedBody, 32) {
		candidates = append(candidates, strings.TrimRight(match, `);,]}`))
	}

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(strings.Trim(candidate, `"'`))
		if candidate == "" {
			continue
		}
		reference, parseErr := url.Parse(candidate)
		if parseErr != nil || reference.User != nil {
			continue
		}
		target := base.ResolveReference(reference)
		host := strings.ToLower(target.Hostname())
		if target.Scheme != "https" ||
			(host != "91porna.com" && host != "www.91porna.com") ||
			!strings.EqualFold(path.Base(target.Path), "embed_play.js") {
			continue
		}

		query, queryErr := url.ParseQuery(target.RawQuery)
		if queryErr != nil {
			continue
		}
		img := strings.TrimSpace(query.Get("img"))
		if img == "" {
			img = imgFallback
		}
		embedURL := normalize91PornaEmbedURL(query.Get("u"), base)
		if embedURL == "" {
			embedURL = uFallback
		}
		// Some current templates provide img in the script URL but omit u.
		// The validated current embed page is the intended u target.
		if embedURL == "" {
			embedURL = normalize91PornaEmbedURL(page, base)
		}
		if img == "" || strings.ContainsAny(img, "\r\n\x00") || embedURL == "" {
			continue
		}
		query.Set("img", img)
		query.Set("u", embedURL)
		if query.Get("t") == "" {
			query.Set("t", strconv.FormatInt(time.Now().Unix()/1800, 10))
		}
		target.RawQuery = query.Encode()
		target.Fragment = ""
		return target.String(), nil
	}

	// Compatibility fallback for older pages where the script URL is assembled
	// from inline img/u variables rather than exposed in a script src attribute.
	if imgFallback == "" || strings.ContainsAny(imgFallback, "\r\n\x00") || uFallback == "" {
		return "", errors.New("91Porna 嵌入頁缺少 embed_play.js 的 img/u 引數")
	}
	query := url.Values{}
	query.Set("img", imgFallback)
	query.Set("u", uFallback)
	query.Set("t", strconv.FormatInt(time.Now().Unix()/1800, 10))
	return "https://91porna.com/index/embed_play.js?" + query.Encode(), nil
}

func normalize91PornaEmbedURL(raw string, base *url.URL) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	raw = strings.NewReplacer("\\/", "/", "\\u002F", "/", "\\u0026", "&").Replace(raw)
	for attempt := 0; attempt < 3 && raw != ""; attempt++ {
		reference, err := url.Parse(raw)
		if err == nil {
			target := base.ResolveReference(reference)
			host := strings.ToLower(target.Hostname())
			if target.Scheme == "https" &&
				(host == "91porna.com" || host == "www.91porna.com") &&
				strings.HasPrefix(strings.ToLower(target.Path), "/comic/index/embed") &&
				target.User == nil {
				target.Fragment = ""
				return target.String()
			}
		}
		decoded, decodeErr := url.QueryUnescape(raw)
		if decodeErr != nil || decoded == raw {
			break
		}
		raw = decoded
	}
	return ""
}

// 91Porna playback: detail -> embed?id=... -> embed_play.js -> HLS/MP4.
func extraAdult91PornaPlayParams(body string) (string, string) {
	normalize := func(value string) string {
		return strings.NewReplacer(
			"\\/", "/",
			"\\u002F", "/",
			"\\u0026", "&",
			"\\\"", "\"",
			"\\'", "'",
			"&amp;", "&",
		).Replace(value)
	}
	unpacked := adultUnpackPacker(body)
	if unpacked == "" {
		unpacked = body
	}
	unpacked = normalize(unpacked)
	normalizedBody := normalize(body)

	var img, encodedURL string
	// Current pages may put both values in the embed_play.js script URL,
	// not in JavaScript img/u variable declarations.
	scriptURLPattern := regexp.MustCompile("(?is)embed_play\\.js\\?([^\"'<>[:space:]]+)")
	for _, source := range []string{unpacked, normalizedBody} {
		for _, match := range scriptURLPattern.FindAllStringSubmatch(source, 32) {
			query := strings.TrimRight(match[1], "});,]")
			values, err := url.ParseQuery(query)
			if err != nil {
				continue
			}
			if img == "" {
				img = strings.TrimSpace(values.Get("img"))
			}
			if encodedURL == "" {
				encodedURL = strings.TrimSpace(values.Get("u"))
			}
		}
		if img != "" && encodedURL != "" {
			break
		}
	}

	if img == "" {
		imgPatterns := []*regexp.Regexp{
			regexp.MustCompile("(?is)embed_play\\.js\\?[^\"'&[:space:]]*?img=([^\"'&[:space:]]+)"),
			regexp.MustCompile("(?is)\\bimg\\s*=\\s*['\"]([^'\"]+)['\"]"),
		}
		for _, source := range []string{unpacked, normalizedBody} {
			for _, pattern := range imgPatterns {
				if match := pattern.FindStringSubmatch(source); len(match) > 1 {
					img = strings.TrimSpace(match[1])
					break
				}
			}
			if img != "" {
				break
			}
		}
	}
	if encodedURL == "" {
		uPatterns := []*regexp.Regexp{
			regexp.MustCompile("(?is)encodeURIComponent\\s*\\(\\s*['\"]([^'\"]+)['\"]\\s*\\)"),
			regexp.MustCompile("(?is)\\b(?:var|let|const)\\s+(?:u|src|embedUrl|embed_url|embed)\\s*=\\s*['\"]([^'\"]+)['\"]"),
		}
		for _, source := range []string{unpacked, normalizedBody} {
			for _, pattern := range uPatterns {
				if match := pattern.FindStringSubmatch(source); len(match) > 1 {
					encodedURL = strings.TrimSpace(match[1])
					break
				}
			}
			if encodedURL != "" {
				break
			}
		}
	}

	// Some player templates encode the entire target URL twice.
	if encodedURL != "" &&
		!strings.HasPrefix(strings.ToLower(encodedURL), "https://") &&
		!strings.HasPrefix(strings.ToLower(encodedURL), "http://") {
		if decoded, err := url.QueryUnescape(encodedURL); err == nil &&
			(strings.HasPrefix(strings.ToLower(decoded), "https://") ||
				strings.HasPrefix(strings.ToLower(decoded), "http://")) {
			encodedURL = decoded
		}
	}
	return img, encodedURL
}

func extraAdult91PornaDirectMediaURL(body, page string) string {
	body = strings.NewReplacer("\\/", "/", "\\u002F", "/", "&amp;", "&").Replace(body)
	if match := adultM3U8Pattern.FindString(body); match != "" {
		media := adultResolvePlaybackURL(page, strings.TrimRight(match, "\\),;]"))
		if isProviderHTTPMediaURL(media) {
			return media
		}
	}
	mp4Pattern := regexp.MustCompile("(?i)https?://[^\\x22'<>\\s\\\\]+\\.mp4(?:\\?[^\\x22'<>\\s\\\\]*)?")
	if match := mp4Pattern.FindString(body); match != "" {
		media := adultResolvePlaybackURL(page, strings.TrimRight(match, "\\),;]"))
		if isProviderHTTPMediaURL(media) {
			return media
		}
	}
	return ""
}

func extraAdult91PornaDecryptAES(ciphertext []byte) []byte {
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil
	}
	key := []byte("f5d965df75336270")
	iv := []byte("97b60394abc2fbe1")
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > aes.BlockSize || padding > len(plain) {
		return nil
	}
	for _, value := range plain[len(plain)-padding:] {
		if int(value) != padding {
			return nil
		}
	}
	return plain[:len(plain)-padding]
}

func extraAdult91PornaMediaURL(body, page string) string {
	body = strings.NewReplacer("\\/", "/", "\\u002F", "/", "&amp;", "&").Replace(body)
	if unpacked := adultUnpackPacker(body); unpacked != "" {
		body += "\\n" + unpacked
	}
	if media := extraAdult91PornaDirectMediaURL(body, page); media != "" {
		return media
	}

	// Current player scripts can carry an AES-CBC encrypted URL as either
	// base64 or hex; decode only the well-known player payload format.
	base64Pattern := regexp.MustCompile("[A-Za-z0-9+/]{40,}={0,2}")
	for _, encoded := range base64Pattern.FindAllString(body, 128) {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(encoded, "="))
		}
		if err != nil {
			continue
		}
		if plain := extraAdult91PornaDecryptAES(decoded); len(plain) > 0 {
			if media := extraAdult91PornaDirectMediaURL(string(plain), page); media != "" {
				return media
			}
		}
	}

	hexPattern := regexp.MustCompile("(?i)[0-9a-f]{64,}")
	for _, encoded := range hexPattern.FindAllString(body, 128) {
		decoded, err := hex.DecodeString(encoded)
		if err != nil {
			continue
		}
		if plain := extraAdult91PornaDecryptAES(decoded); len(plain) > 0 {
			if media := extraAdult91PornaDirectMediaURL(string(plain), page); media != "" {
				return media
			}
		}
	}
	return ""
}

var extraAdult91PornaMediaIDPattern = regexp.MustCompile(`(?i)\bdata-video\s*=\s*['"]([0-9]{1,20})['"]`)

// The current upstream module resolves detail -> data-video media ID ->
// POST /api/videos/{id}/playback. Prefer that documented API path; the older
// embed_play.js flow remains a compatibility fallback below.
func extraAdult91PornaMediaID(body string) string {
	match := extraAdult91PornaMediaIDPattern.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func extraAdult91PornaPlaybackURLs(payload map[string]any, page string) []string {
	streams := nestedMap(payload, "data", "streams")
	seen := make(map[string]bool, 3)
	var result []string
	for _, field := range []string{"main", "hevc", "preview"} {
		raw, _ := streams[field].(string)
		address := adultResolvePlaybackURL(page, raw)
		parsed, err := url.Parse(address)
		if err != nil || !isProviderHTTPMediaURL(address) || parsed.User != nil {
			continue
		}
		path := strings.ToLower(parsed.Path)
		if !(strings.HasSuffix(path, ".m3u8") || strings.HasSuffix(path, ".mp4")) || seen[address] {
			continue
		}
		seen[address] = true
		result = append(result, address)
	}
	return result
}

func (d *Downloader) fetchExtraAdult91PornaPlayback(ctx context.Context, mediaID, page string) (providerMedia, error) {
	if !regexp.MustCompile("^[0-9]{1,20}$").MatchString(strings.TrimSpace(mediaID)) {
		return providerMedia{}, errors.New("91Porna 媒體 ID 無效")
	}
	parsedPage, err := url.Parse(strings.TrimSpace(page))
	if err != nil || parsedPage.Scheme != "https" || parsedPage.User != nil ||
		(parsedPage.Hostname() != "91porna.com" && parsedPage.Hostname() != "www.91porna.com") {
		return providerMedia{}, errors.New("91Porna 播放頁網址無效")
	}
	if d == nil || d.client == nil {
		return providerMedia{}, errors.New("91Porna 播放 API 用戶端尚未初始化")
	}
	requestCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	requestCtx = context.WithValue(requestCtx, nativeRPZFallbackKey{}, true)
	endpoint := ninetyOneBaseURL + "/api/videos/" + mediaID + "/playback"
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, strings.NewReader("{}"))
	if err != nil {
		return providerMedia{}, err
	}
	request.Header.Set("User-Agent", ninetyOneMobileUserAgent)
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	request.Header.Set("Referer", page)
	request.Header.Set("Content-Type", "application/json")
	response, err := d.client.Do(request)
	if err != nil {
		return providerMedia{}, fmt.Errorf("91Porna 播放 API 請求失敗：%w", err)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if readErr != nil {
		return providerMedia{}, readErr
	}
	if len(body) > 4<<20 {
		return providerMedia{}, errors.New("91Porna 播放 API 回應過大")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return providerMedia{}, fmt.Errorf("91Porna 播放 API 回應 HTTP %d", response.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return providerMedia{}, errors.New("91Porna 播放 API 回應不是有效 JSON")
	}
	addresses := extraAdult91PornaPlaybackURLs(payload, page)
	if len(addresses) == 0 {
		return providerMedia{}, errors.New("91Porna 播放 API 沒有提供 main、hevc 或 preview 串流")
	}
	credentials := &providerMediaCredentials{referer: page, userAgent: ninetyOneMobileUserAgent}
	media := providerMedia{
		URL: addresses[0], Referer: page, credentials: credentials,
		StandardHTTP: runtime.GOOS == "android",
	}
	for _, address := range addresses[1:] {
		media.Variants = append(media.Variants, providerMedia{
			URL: address, Referer: page, credentials: credentials,
			StandardHTTP: runtime.GOOS == "android",
		})
	}
	return media, nil
}

func (d *Downloader) resolveExtraAdult91PornaAPI(ctx context.Context, sourceID string, body, page string) (providerMedia, error) {
	tryPage := func(candidateBody, candidateURL string) (providerMedia, bool, error) {
		mediaID := extraAdult91PornaMediaID(candidateBody)
		if mediaID == "" {
			return providerMedia{}, false, nil
		}
		media, err := d.fetchExtraAdult91PornaPlayback(ctx, mediaID, candidateURL)
		if err != nil {
			return providerMedia{}, false, err
		}
		return media, true, nil
	}
	var lastErr error
	if media, ok, err := tryPage(body, page); ok {
		return media, nil
	} else if err != nil {
		lastErr = err
	}
	for _, detailPath := range []string{
		"/comic/index/detail?video_key=" + url.QueryEscape(sourceID),
		"/comic/index/avdetail?video_key=" + url.QueryEscape(sourceID),
	} {
		address := ninetyOneBaseURL + detailPath
		if address == page {
			continue
		}
		candidateBody, finalURL, fetchErr := extraAdultFetchWithUserAgent(
			ctx, d, source91Porna, address, ninetyOneBaseURL+"/", ninetyOneMobileUserAgent,
		)
		if fetchErr != nil {
			lastErr = fetchErr
			if ctx.Err() != nil {
				return providerMedia{}, ctx.Err()
			}
			continue
		}
		media, ok, resolveErr := tryPage(candidateBody, finalURL)
		if ok {
			return media, nil
		}
		if resolveErr != nil {
			lastErr = resolveErr
			if ctx.Err() != nil {
				return providerMedia{}, ctx.Err()
			}
		}
	}
	if lastErr != nil {
		return providerMedia{}, lastErr
	}
	return providerMedia{}, errors.New("91Porna 詳情頁沒有 data-video 媒體 ID")
}


func extraAdultHLS(body,page string)string{
	body=strings.ReplaceAll(body,"\\/","/")
	patterns:=[]*regexp.Regexp{regexp.MustCompile("(?i)setVideoHLS\\s*\\(\\s*['\\x22]([^'\\x22]+)['\\x22]"),regexp.MustCompile("(?i)\\bvar\\s+m3u8\\s*=\\s*['\\x22]([^'\\x22]+)['\\x22]"),regexp.MustCompile("(?i)['\\x22](?:m3u8|hls|file)['\\x22]\\s*:\\s*['\\x22]([^'\\x22]*\\.m3u8[^'\\x22]*)['\\x22]")}
	for _,p:=range patterns{m:=p.FindStringSubmatch(body);if len(m)>1{if v:=adultResolvePlaybackURL(page,m[1]);v!=""{return v}}};if m:=adultM3U8Pattern.FindString(body);m!=""{return adultResolvePlaybackURL(page,m)};return ""
}
func (d *Downloader) resolveExtraAdultMedia(ctx context.Context,task Task)(providerMedia,error){
	source,id,ok:=splitProviderDramaID(task.DramaID);prefix:=providerChapterID(source,id,"");if !ok||!extraAdultValidID(source,id)||!strings.HasPrefix(task.Chapter.ID,prefix){return providerMedia{},errors.New("進階片源分集資訊無效，請重新整理")}
	userAgent:=extraAdultUserAgent(source)
	base:=extraAdultBaseURL(source);page:=firstNonEmpty(task.Chapter.PageURL,strings.TrimPrefix(task.Chapter.ID,prefix));if page==""{page=extraAdultDetailURL(source,id)};body,final,e:=extraAdultFetchWithUserAgent(ctx,d,source,page,base+"/",userAgent);if e!=nil{return providerMedia{},fmt.Errorf("%s 播放頁讀取失敗：%w",extraAdultSourceName(source),e)}
	switch source{
	case sourcePPP:
		routes, resolveErr := d.resolveExtraAdultPPPStreams(ctx, body, final)
		if resolveErr != nil {
			return providerMedia{}, resolveErr
		}
		primary := routes[0]
		primary.Variants = append([]providerMedia(nil), routes[1:]...)
		return d.prepareWebProviderMediaWithFallback(ctx, primary, "PPP")
	case sourceMadou:
		frame:=extraAdultFrame(final,body);u,err:=url.Parse(frame);if err!=nil||u.Scheme!="https"||!strings.EqualFold(u.Hostname(),"dash.madou.club")||u.User!=nil{return providerMedia{},errors.New("madou 播放器分享網址無效")};share,surl,err:=extraAdultFetch(ctx,d,source,frame,final);if err!=nil{return providerMedia{},err};token:="";if m:=regexp.MustCompile("(?i)\\bvar\\s+token\\s*=\\s*['\\x22]([^'\\x22]*)['\\x22]").FindStringSubmatch(share);len(m)>1{token=m[1]};media:="";if m:=regexp.MustCompile("(?i)\\bvar\\s+m3u8\\s*=\\s*['\\x22]([^'\\x22]+)['\\x22]").FindStringSubmatch(share);len(m)>1{media=adultResolvePlaybackURL(surl,m[1])};if media==""{if m:=regexp.MustCompile("(?i)['\\x22](/videos/[^'\\x22]+\\.m3u8[^'\\x22]*)['\\x22]").FindStringSubmatch(share);len(m)>1{media=adultResolvePlaybackURL(madouDashBaseURL+"/",m[1])}};if media==""{return providerMedia{},errors.New("madou 分享頁沒有提供 HLS 播放網址")};if token!=""{u,_:=url.Parse(media);q:=u.Query();if q.Get("token")==""{q.Set("token",token);u.RawQuery=q.Encode();media=u.String()}};cred:=&providerMediaCredentials{referer:surl,userAgent:adultUserAgent};return d.prepareWebProviderMedia(ctx,providerMedia{URL:media,Referer:surl,credentials:cred,StandardHTTP:runtime.GOOS=="android"},"madou")
	case sourceSupJav:
		media:=extraAdultHLS(body,final);referer:=final;if media==""{frame:=extraAdultFrame(final,body);u,err:=url.Parse(frame);if err!=nil||u.Scheme!="https"||u.User!=nil{return providerMedia{},errors.New("SupJav 播放器網址無效")};embed,eu,err:=extraAdultFetch(ctx,d,source,frame,final);if err!=nil{return providerMedia{},err};referer=eu;media=extraAdultHLS(embed,eu);if media==""{if m:=regexp.MustCompile("(?is)window\\.PLAYER_CONFIG\\s*=\\s*(\\{[\\s\\S]*?\\})\\s*;").FindStringSubmatch(embed);len(m)>1{var cfg map[string]any;if json.Unmarshal([]byte(m[1]),&cfg)==nil{for _,k:=range []string{"m3u8","url","hls","file"}{if s,ok:=cfg[k].(string);ok{media=adultResolvePlaybackURL(eu,s);if media!=""{break}}}}}}};if media==""{return providerMedia{},errors.New("SupJav 播放器沒有提供 HLS 網址")};cred:=&providerMediaCredentials{referer:referer,userAgent:adultUserAgent};return d.prepareWebProviderMedia(ctx,providerMedia{URL:media,Referer:referer,credentials:cred,StandardHTTP:runtime.GOOS=="android"},"SupJav")
	case source91Porna:
		apiMedia, apiErr := d.resolveExtraAdult91PornaAPI(ctx, id, body, final)
		if apiErr == nil {
			return d.prepareWebProviderMediaWithFallback(ctx, apiMedia, "91Porna")
		}
		media := extraAdult91PornaMediaURL(body, final)
		referer := final
		scanOnly := extraAdultMobileScanRequired(body)
		if media == "" {
			embedURLs := extraAdult91PornaEmbedCandidates(id, final, body)
			var lastEmbedErr error
			for _, embedURL := range embedURLs {
				embed, embedFinal, fetchErr := extraAdultFetchWithUserAgent(ctx, d, source, embedURL, final, ninetyOneMobileUserAgent)
				if fetchErr != nil {
					lastEmbedErr = fetchErr
					continue
				}
				currentScanOnly := extraAdultMobileScanRequired(embed)
				media = extraAdult91PornaMediaURL(embed, embedFinal)
				if media == "" {
					jsURL, scriptErr := extraAdult91PornaPlayScriptURL(embed, embedFinal)
					if scriptErr != nil {
						lastEmbedErr = scriptErr
						scanOnly = scanOnly || currentScanOnly
						continue
					}
					jsBody, _, jsErr := extraAdultFetchWithUserAgent(ctx, d, source, jsURL, embedFinal, ninetyOneMobileUserAgent)
					if jsErr != nil {
						lastEmbedErr = jsErr
						scanOnly = scanOnly || currentScanOnly
						continue
					}
					media = extraAdult91PornaMediaURL(jsBody, embedFinal)
					currentScanOnly = currentScanOnly || extraAdultMobileScanRequired(jsBody)
				}
				if media != "" {
					referer = embedFinal
					scanOnly = scanOnly || currentScanOnly
					break
				}
				scanOnly = scanOnly || currentScanOnly
			}
			if media == "" && lastEmbedErr != nil && !scanOnly {
				return providerMedia{}, fmt.Errorf("91Porna 嵌入播放器解析失敗：%w", lastEmbedErr)
			}
		}
		if !isProviderHTTPMediaURL(media) {
			if scanOnly {
				return providerMedia{}, errors.New("91Porna 目前仍要求以手機瀏覽器開啟掃碼頁面；這次回應沒有提供 D-TV 可直接使用的 HLS／MP4 網址")
			}
			return providerMedia{}, fmt.Errorf("91Porna 官方播放 API 與嵌入備援均未取得有效串流（API：%v）", apiErr)
		}
		cred := &providerMediaCredentials{referer: referer, userAgent: ninetyOneMobileUserAgent}
		return d.prepareWebProviderMedia(ctx, providerMedia{URL: media, Referer: referer, credentials: cred, StandardHTTP: runtime.GOOS == "android"}, "91Porna")
	}
	return providerMedia{},errors.New("不支援的進階片源")
}
