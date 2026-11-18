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
	"net/url"
	"regexp"
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
)

var extraAdultCategories = map[string][]nativeCategory{
	sourceSupJav: {{ID:"0",Name:"最新"},{ID:"uncensored",Name:"無碼"},{ID:"uncensored-leaked",Name:"無碼破解"},{ID:"censored",Name:"有碼"},{ID:"chinese",Name:"中文字幕"},{ID:"amateur",Name:"素人"}},
	sourcePPP: {{ID:"new",Name:"最新上架"},{ID:"c-china",Name:"中國"},{ID:"c-japan",Name:"日本"},{ID:"c-taiwan",Name:"台灣"},{ID:"c-korea",Name:"韓國"},{ID:"c-hongkong",Name:"香港"},{ID:"c-se-asia",Name:"東南亞"},{ID:"c-amateur",Name:"素人自拍"},{ID:"c-cosplay",Name:"角色扮演"},{ID:"c-streamer",Name:"直播主"},{ID:"c-first-person-pov",Name:"第一人稱"},{ID:"c-china-av",Name:"中國成人影片"},{ID:"c-japan-producer",Name:"日本片商"}},
	sourceMadou: {{ID:"0",Name:"全部"},{ID:"麻豆传媒",Name:"麻豆傳媒"},{ID:"麻豆番外篇",Name:"麻豆番外篇"},{ID:"麻豆花絮",Name:"麻豆花絮"},{ID:"hongkongdoll",Name:"HongKongDoll"},{ID:"psychoporntw",Name:"PsychopornTW"},{ID:"91制片厂",Name:"91製片廠"},{ID:"果冻传媒",Name:"果凍傳媒"},{ID:"蜜桃影像",Name:"蜜桃影像"},{ID:"天美传媒",Name:"天美傳媒"},{ID:"皇家华人",Name:"皇家華人"},{ID:"兔子先生",Name:"兔子先生"},{ID:"星空无限传媒",Name:"星空無限傳媒"},{ID:"爱豆",Name:"愛豆"},{ID:"麻豆导演系列",Name:"麻豆導演系列"},{ID:"大象传媒",Name:"大象傳媒"},{ID:"猫爪影像",Name:"貓爪影像"},{ID:"精东影业",Name:"精東影業"},{ID:"杏吧",Name:"杏吧"},{ID:"乐播传媒",Name:"樂播傳媒"},{ID:"草莓",Name:"草莓"},{ID:"抖阴",Name:"抖陰"},{ID:"sa国际传媒",Name:"SA國際傳媒"},{ID:"起点传媒-性视界传媒",Name:"起點傳媒／性視界傳媒"},{ID:"大鸟十八",Name:"大鳥十八"},{ID:"小鹏奇啪行",Name:"小鵬奇啪行"},{ID:"女优淫娃培训营",Name:"女優培訓營"},{ID:"淫欲游戏王",Name:"淫慾遊戲王"},{ID:"女神羞羞研究所",Name:"女神羞羞研究所"},{ID:"突袭女优家",Name:"突襲女優家"},{ID:"情趣K歌房",Name:"情趣 K 歌房"},{ID:"kiss糖果屋",Name:"KISS 糖果屋"}},
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
	case sourceMadou: return "麻豆社"
	case source91Porna: return "91porna"
	default: return "進階片源"
	}
}
func extraAdultValidCategory(source, category string) bool {
	if category == "" { return true }
	for _, item := range extraAdultCategories[canonicalProviderSource(source)] { if item.ID == category { return true } }
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
func extraAdultFetch(ctx context.Context,d *Downloader,source,address,referer string)(string,string,error){
	ctx=context.WithValue(ctx,providerTextUserAgentKey{},adultUserAgent);rs:=&playbackResponseURLs{};ctx=context.WithValue(ctx,playbackResponseURLsKey{},rs)
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
		m:=regexp.MustCompile("(?i)href=['\\x22]/comic/index/(?:avdetail|detail)\\?video_key=([A-Za-z0-9_-]+)['\\x22]").FindStringSubmatch(chunk);if len(m)<2{return Drama{},false};id=m[1];img:=regexp.MustCompile("(?is)<img\\b[^>]*>").FindString(chunk);if img!=""{cover=adultAttribute(img,"data-src");if cover==""{cover=adultAttribute(img,"src")};title=adultAttribute(img,"alt")};if m:=regexp.MustCompile("(?i)([0-9]{1,3}:[0-9]{2})").FindStringSubmatch(chunk);len(m)>1{remark=m[1]}
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
	source=canonicalProviderSource(source);if !extraAdultValidID(source,id){return Drama{},nil,errors.New("作品識別碼無效")};base:=extraAdultBaseURL(source);body,final,e:=extraAdultFetch(ctx,d,source,extraAdultDetailURL(source,id),base+"/");if e!=nil{return Drama{},nil,fmt.Errorf("%s 作品詳情讀取失敗：%w",extraAdultSourceName(source),e)}
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
func extraAdultFrame(page, body string) string {
	// Some current pages emit iframe src without quotes.
	pattern := regexp.MustCompile("(?is)<iframe\\b[^>]*\\bsrc\\s*=\\s*(?:['\\x22]([^'\\x22]+)['\\x22]|([^\\s>]+))")
	match := pattern.FindStringSubmatch(body)
	if len(match) < 3 {
		return ""
	}
	raw := firstNonEmpty(match[1], match[2])
	return adultResolvePlaybackURL(page, strings.TrimRight(raw, "/>"))
}

// PPP pages publish the actual HLS URL in a JavaScript var stream assignment.
func extraAdultPPPStreamURL(body, page string) string {
	body = html.UnescapeString(strings.ReplaceAll(body, "\\/", "/"))
	patterns := []*regexp.Regexp{
		regexp.MustCompile("(?is)\\bvar\\s+stream\\s*=\\s*'([^']+)'"),
		regexp.MustCompile("(?is)\\bvar\\s+stream\\s*=\\s*\\x22([^\\x22]+)\\x22"),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(body); len(match) > 1 {
			media := adultResolvePlaybackURL(page, match[1])
			if isProviderHTTPMediaURL(media) && strings.Contains(strings.ToLower(media), ".m3u8") {
				return media
			}
		}
	}
	if match := adultM3U8Pattern.FindString(body); match != "" {
		media := adultResolvePlaybackURL(page, match)
		if isProviderHTTPMediaURL(media) {
			return media
		}
	}
	return ""
}

// 91Porna playback: detail -> embed?id=... -> embed_play.js -> HLS/MP4.
func extraAdult91PornaPlayParams(body string) (string, string) {
	unpacked := adultUnpackPacker(body)
	if unpacked == "" {
		unpacked = body
	}
	unpacked = strings.NewReplacer("\\/", "/", "\\u002F", "/", "&amp;", "&").Replace(unpacked)
	img := ""
	for _, source := range []string{unpacked, body} {
		if match := regexp.MustCompile("(?is)embed_play\\.js\\?img=([^\\x22'&\\s]+)").FindStringSubmatch(source); len(match) > 1 {
			img = strings.TrimSpace(match[1])
			break
		}
	}
	encodedURL := ""
	for _, source := range []string{unpacked, body} {
		if match := regexp.MustCompile("(?is)encodeURIComponent\\(\\s*['\\x22]([^'\\x22]+)['\\x22]\\s*\\)").FindStringSubmatch(source); len(match) > 1 {
			encodedURL = strings.TrimSpace(match[1])
			break
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

func extraAdultHLS(body,page string)string{
	body=strings.ReplaceAll(body,"\\/","/")
	patterns:=[]*regexp.Regexp{regexp.MustCompile("(?i)setVideoHLS\\s*\\(\\s*['\\x22]([^'\\x22]+)['\\x22]"),regexp.MustCompile("(?i)\\bvar\\s+m3u8\\s*=\\s*['\\x22]([^'\\x22]+)['\\x22]"),regexp.MustCompile("(?i)['\\x22](?:m3u8|hls|file)['\\x22]\\s*:\\s*['\\x22]([^'\\x22]*\\.m3u8[^'\\x22]*)['\\x22]")}
	for _,p:=range patterns{m:=p.FindStringSubmatch(body);if len(m)>1{if v:=adultResolvePlaybackURL(page,m[1]);v!=""{return v}}};if m:=adultM3U8Pattern.FindString(body);m!=""{return adultResolvePlaybackURL(page,m)};return ""
}
func (d *Downloader) resolveExtraAdultMedia(ctx context.Context,task Task)(providerMedia,error){
	source,id,ok:=splitProviderDramaID(task.DramaID);prefix:=providerChapterID(source,id,"");if !ok||!extraAdultValidID(source,id)||!strings.HasPrefix(task.Chapter.ID,prefix){return providerMedia{},errors.New("進階片源分集資訊無效，請重新整理")}
	base:=extraAdultBaseURL(source);page:=firstNonEmpty(task.Chapter.PageURL,strings.TrimPrefix(task.Chapter.ID,prefix));if page==""{page=extraAdultDetailURL(source,id)};body,final,e:=extraAdultFetch(ctx,d,source,page,base+"/");if e!=nil{return providerMedia{},fmt.Errorf("%s 播放頁讀取失敗：%w",extraAdultSourceName(source),e)}
	switch source{
	case sourcePPP:
		media := extraAdultPPPStreamURL(body, final)
		if !isProviderHTTPMediaURL(media) {
			return providerMedia{}, errors.New("PPP 頁面沒有找到有效的 HLS 播放網址")
		}
		cred := &providerMediaCredentials{referer: final, userAgent: adultUserAgent}
		return d.prepareWebProviderMedia(ctx, providerMedia{URL: media, Referer: final, credentials: cred, StandardHTTP: runtime.GOOS == "android"}, "PPP")
	case sourceMadou:
		frame:=extraAdultFrame(final,body);u,err:=url.Parse(frame);if err!=nil||u.Scheme!="https"||!strings.EqualFold(u.Hostname(),"dash.madou.club")||u.User!=nil{return providerMedia{},errors.New("麻豆播放器分享網址無效")};share,surl,err:=extraAdultFetch(ctx,d,source,frame,final);if err!=nil{return providerMedia{},err};token:="";if m:=regexp.MustCompile("(?i)\\bvar\\s+token\\s*=\\s*['\\x22]([^'\\x22]*)['\\x22]").FindStringSubmatch(share);len(m)>1{token=m[1]};media:="";if m:=regexp.MustCompile("(?i)\\bvar\\s+m3u8\\s*=\\s*['\\x22]([^'\\x22]+)['\\x22]").FindStringSubmatch(share);len(m)>1{media=adultResolvePlaybackURL(surl,m[1])};if media==""{if m:=regexp.MustCompile("(?i)['\\x22](/videos/[^'\\x22]+\\.m3u8[^'\\x22]*)['\\x22]").FindStringSubmatch(share);len(m)>1{media=adultResolvePlaybackURL(madouDashBaseURL+"/",m[1])}};if media==""{return providerMedia{},errors.New("麻豆分享頁沒有提供 HLS 播放網址")};if token!=""{u,_:=url.Parse(media);q:=u.Query();if q.Get("token")==""{q.Set("token",token);u.RawQuery=q.Encode();media=u.String()}};cred:=&providerMediaCredentials{referer:surl,userAgent:adultUserAgent};return d.prepareWebProviderMedia(ctx,providerMedia{URL:media,Referer:surl,credentials:cred,StandardHTTP:runtime.GOOS=="android"},"麻豆社")
	case sourceSupJav:
		media:=extraAdultHLS(body,final);referer:=final;if media==""{frame:=extraAdultFrame(final,body);u,err:=url.Parse(frame);if err!=nil||u.Scheme!="https"||u.User!=nil{return providerMedia{},errors.New("SupJav 播放器網址無效")};embed,eu,err:=extraAdultFetch(ctx,d,source,frame,final);if err!=nil{return providerMedia{},err};referer=eu;media=extraAdultHLS(embed,eu);if media==""{if m:=regexp.MustCompile("(?is)window\\.PLAYER_CONFIG\\s*=\\s*(\\{[\\s\\S]*?\\})\\s*;").FindStringSubmatch(embed);len(m)>1{var cfg map[string]any;if json.Unmarshal([]byte(m[1]),&cfg)==nil{for _,k:=range []string{"m3u8","url","hls","file"}{if s,ok:=cfg[k].(string);ok{media=adultResolvePlaybackURL(eu,s);if media!=""{break}}}}}}};if media==""{return providerMedia{},errors.New("SupJav 播放器沒有提供 HLS 網址")};cred:=&providerMediaCredentials{referer:referer,userAgent:adultUserAgent};return d.prepareWebProviderMedia(ctx,providerMedia{URL:media,Referer:referer,credentials:cred,StandardHTTP:runtime.GOOS=="android"},"SupJav")
	case source91Porna:
		media := extraAdult91PornaMediaURL(body, final)
		referer := final
		if media == "" {
			embedURL := ninetyOneBaseURL + "/comic/index/embed?id=" + url.QueryEscape(id)
			embed, embedFinal, fetchErr := extraAdultFetch(ctx, d, source, embedURL, final)
			if fetchErr != nil {
				return providerMedia{}, fmt.Errorf("91Porna 播放嵌入頁讀取失敗：%w", fetchErr)
			}
			media = extraAdult91PornaMediaURL(embed, embedFinal)
			if media == "" {
				img, encodedURL := extraAdult91PornaPlayParams(embed)
				if img == "" || encodedURL == "" {
					return providerMedia{}, errors.New("91Porna 嵌入頁缺少 embed_play.js 參數")
				}
				imgValue, decodeErr := url.QueryUnescape(img)
				if decodeErr != nil {
					imgValue = img
				}
				jsURL := ninetyOneBaseURL + "/index/embed_play.js?img=" +
					url.QueryEscape(imgValue) + "&u=" + url.QueryEscape(encodedURL) +
					"&t=" + strconv.FormatInt(time.Now().Unix()/1800, 10)
				jsBody, _, jsErr := extraAdultFetch(ctx, d, source, jsURL, embedFinal)
				if jsErr != nil {
					return providerMedia{}, fmt.Errorf("91Porna 播放腳本讀取失敗：%w", jsErr)
				}
				media = extraAdult91PornaMediaURL(jsBody, embedFinal)
			}
			referer = embedFinal
		}
		if !isProviderHTTPMediaURL(media) {
			return providerMedia{}, errors.New("91Porna 播放腳本沒有回傳有效的 HLS／MP4 網址")
		}
		cred := &providerMediaCredentials{referer: referer, userAgent: adultUserAgent}
		return d.prepareWebProviderMedia(ctx, providerMedia{URL: media, Referer: referer, credentials: cred, StandardHTTP: runtime.GOOS == "android"}, "91Porna")
	}
	return providerMedia{},errors.New("不支援的進階片源")
}
