package core

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	htmlparser "golang.org/x/net/html"
)

type providerTextValidatorKey struct{}
type providerTextValidator func(candidate string, status int, body []byte) error

var chaoliuSites = []string{
	"https://mjv011.com","https://mjv001.com","https://mjv012.com","https://mjv009.com","https://mjv002.com",
	"https://mjv013.com","https://mjv010.com","https://mjv003.com","https://mjv004.com","https://mjv005.com",
	"https://mjv006.com","https://mjv007.com","https://mjv008.com","https://mjv014.com","https://mjv015.com",
}
var chaoliuMediaDomains = []string{"mavrtracktor.com","eemmhh01.com","imgstream1.com","ddaa4.com","streamfastpro.com"}
var chaoliuCategories = []nativeCategory{
	{ID:"chinese",Name:"中文字幕"},{ID:"censored",Name:"有碼 AV"},{ID:"uncensored",Name:"無碼 AV"},
	{ID:"reducing-mosaic",Name:"無碼破解"},{ID:"amateurjav",Name:"素人 AV"},{ID:"dt",Name:"國產自拍"},
	{ID:"animation",Name:"H 動畫"},{ID:"CensoredAnimation",Name:"H 有碼動畫"},
	{ID:"UncensoredAnimation",Name:"H 無碼動畫"},{ID:"tdAnimation",Name:"H 3D 動畫"},
}
var (
	chaoliuCardHref=regexp.MustCompile("(?is)href=['\\x22](?:https?:)?//[^'\\x22]*?/zh/([A-Za-z0-9-]+)_content/([0-9]+)/([^'\\x22/?#]+?)\\.html|href=['\\x22]/?zh/([A-Za-z0-9-]+)_content/([0-9]+)/([^'\\x22/?#]+?)\\.html")
	chaoliuIDPattern=regexp.MustCompile("^[A-Za-z0-9-]+_content/[0-9]+/[A-Za-z0-9-]+$")
	chaoliuMvarrArray=regexp.MustCompile("(?is)mvarr\\[['\\x22]?([\\w.]+)['\\x22]?\\]\\s*=\\s*\\[([\\s\\S]*?)\\];")
	chaoliuMvarrObject=regexp.MustCompile("(?is)mvarr\\[['\\x22]?([\\w.]+)['\\x22]?\\]\\s*=\\s*\\{([\\s\\S]*?)\\};")
	chaoliuEncryptedString=regexp.MustCompile("['\\x22]([0-9A-Za-z+/=]{16,})['\\x22]")
	chaoliuSourcePattern=regexp.MustCompile("(?i)src\\s*:\\s*['\\x22]([^'\\x22]+?\\.(?:m3u8|mp4)[^'\\x22]*)['\\x22](?:\\s*,\\s*type\\s*:\\s*['\\x22][^'\\x22]*['\\x22])?(?:\\s*,\\s*size\\s*:\\s*(\\d+))?")
	chaoliuDirectPattern=regexp.MustCompile("(?i)https?://[^'\\x22<>\\s]+?\\.(?:m3u8|mp4)(?:\\?[^'\\x22<>\\s]*)?")
)

type chaoliuQuality struct { Key string; Encrypted string; Resolution int }
type chaoliuMediaSource struct { URL string; Size int64 }
type chaoliuConstants struct { Key,IV string; Base,XOR int }

func isChaoliuSiteHost(host string) bool {
	host=strings.ToLower(strings.TrimSpace(host))
	for _,root:=range chaoliuSites {parsed,_:=url.Parse(root);domain:=parsed.Hostname();if host==domain||strings.HasSuffix(host,"."+domain){return true}}
	return false
}
func isChaoliuHost(host string) bool {
	if isChaoliuSiteHost(host){return true}
	host=strings.ToLower(strings.TrimSpace(host))
	for _,domain:=range chaoliuMediaDomains {if host==domain||strings.HasSuffix(host,"."+domain){return true}}
	return false
}
func validChaoliuCategory(category string) bool {
	if category=="" {return true};for _,item:=range chaoliuCategories {if category==item.ID{return true}};return false
}
func validChaoliuID(id string) bool {
	id=strings.Trim(strings.TrimSpace(id),"/");return len(id)<=240&&chaoliuIDPattern.MatchString(id)
}
func chaoliuCategoryPath(page int,category string)(string,error) {
	if page<1||page>100000||!validChaoliuCategory(category){return "",errors.New("chaoliu 分類或頁碼無效")}
	if category=="" {category="chinese"};return fmt.Sprintf("/zh/%s_list/all/%d.html",category,page),nil
}
func chaoliuSearchPath(page int,query string)(string,error) {
	query=strings.TrimSpace(query);if page<1||page>100000||query==""||len([]rune(query))>128{return "",errors.New("chaoliu 搜尋條件無效")}
	query=strings.NewReplacer("/"," ","\\"," ","?"," ","#"," ","%"," ").Replace(query)
	return "/zh/fc_search/all/"+url.PathEscape(query)+"/"+strconv.Itoa(page)+".html",nil
}
func chaoliuPlayerEndpoint(candidate string) bool {
	parsed, err := url.Parse(candidate)
	return err == nil && strings.EqualFold(strings.TrimRight(parsed.Path, "/"), "/js/player/play.php")
}

func chaoliuValidator(candidate string,status int,body []byte) error {
	if status<200||status>=300{return fmt.Errorf("chaoliu HTTP %d",status)}
	text:=string(body)
	// play.php is a media API, not an article/catalogue HTML page. Its JSON
	// response can legitimately omit all site-shell markers. Validate it by
	// the actual HLS/MP4 payload instead of retrying the same API on all 15
	// mirrors because it is not an HTML document.
	if chaoliuPlayerEndpoint(candidate) {
		if len(parseChaoliuSources(text,candidate)) > 0 { return nil }
		return errors.New("chaoliu 播放解析端點未回傳有效的 HLS／MP4 媒體網址")
	}
	if len(text) < 100 {
		return errors.New("chaoliu 回應內容過短")
	}
	if chaoliuLooksOffsite(text) {
		return errors.New("chaoliu 回應被導向廣告頁")
	}
	if chaoliuIsAgeGate(text) {
		return nil
	}
	if !chaoliuLooksLikeSite(text) && !chaoliuLooksLikeDetailHTML(candidate, text) {
		return errors.New("chaoliu 回應不是有效的站點頁面")
	}
	return nil
}
func chaoliuLooksLikeSite(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range []string{
		"_content/", "_list/", "fc_search", "mvarr",
		"application/ld+json", "play.php", "18av",
		"chinese_list", "jfun_show", "og:title",
		"og:image", "video_title", "numresolution=",
		"__play", "playerconfig",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// chaoliuLooksLikeDetailHTML accepts a real HTML document for an explicitly
// allow-listed detail route even when the site removes its historical marker
// strings. It rejects Cloudflare/access-denied shells so one valid 200 detail
// response is not needlessly replayed against all 15 mirrors.
func chaoliuLooksLikeDetailHTML(candidate, body string) bool {
	parsed, err := url.Parse(candidate)
	if err != nil || !isChaoliuSiteHost(parsed.Hostname()) {
		return false
	}
	path := strings.ToLower(parsed.Path)
	if !strings.Contains(path, "_content/") && !strings.Contains(path, "jfun_show") {
		return false
	}
	lower := strings.ToLower(body)
	for _, blocked := range []string{
		"just a moment", "cf-chl-", "challenge-platform",
		"attention required", "access denied", "checking your browser",
		"security check", "error 403", "error 404", "error 410",
	} {
		if strings.Contains(lower, blocked) {
			return false
		}
	}
	if !strings.Contains(lower, "<html") && !strings.Contains(lower, "<!doctype html") {
		return false
	}
	for _, marker := range []string{"<title", "<h1", "<meta", "<video", "application/ld+json"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
func chaoliuIsAgeGate(body string) bool {
	if strings.Contains(body,"scale_content"){return true}
	return len(body)<8000&&(strings.Contains(body,"IamOverEighteenYearsOld")||strings.Contains(body,"Eighteen_declaration"))
}
func chaoliuLooksOffsite(body string) bool {
	if body==""||len(body)<400||chaoliuIsAgeGate(body)||chaoliuLooksLikeSite(body){return false}
	lower:=strings.ToLower(body)
	for _,marker:=range []string{"mavrtracktor","popads","popcash","exoclick","juicyads","propellerads","adsterra","clickadu"} {if strings.Contains(lower,marker){return true}}
	return strings.Contains(lower,"window.location")&&len(body)<3000
}
func (d *Downloader) fetchChaoliuText(ctx context.Context,rawURL,referer string)(string,string,error) {
	parsed,err:=url.Parse(rawURL);if err!=nil||parsed.Scheme!="https"||!isChaoliuSiteHost(parsed.Hostname())||parsed.User!=nil{return "","",errors.New("chaoliu 網址不在允許的站點清單")}
	ctx=context.WithValue(ctx,providerTextCookieKey{},"YES_Eighteen=IamOverEighteenYearsOld")
	ctx=context.WithValue(ctx,providerTextUserAgentKey{},"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36")
	ctx=context.WithValue(ctx,providerTextValidatorKey{},providerTextValidator(chaoliuValidator))
	body,err:=d.fetchProviderText(ctx,rawURL,firstNonEmpty(referer,chaoliuSites[0]+"/"));if err!=nil{return "","",fmt.Errorf("chaoliu 站點請求失敗：%w",err)}
	finalURL:=d.providerBaseURL(sourceChaoliu)+parsed.RequestURI()
	if chaoliuIsAgeGate(body) {
		gatePaths := []string{}
		redirects := regexp.MustCompile("(?i)window\\.location\\s*=\\s*['\\x22]([^'\\x22]+)['\\x22]")
		for _, match := range redirects.FindAllStringSubmatch(body, 8) {
			if len(match) < 2 || !strings.Contains(strings.ToLower(match[1]), "iamover18yearsold") { continue }
			path := match[1]
			if target, parseErr := url.Parse(path); parseErr == nil && target.IsAbs() {
				if !strings.EqualFold(target.Hostname(), parsed.Hostname()) { continue }
				path = target.RequestURI()
			}
			if strings.HasPrefix(path, "/") && !containsString(gatePaths, path) { gatePaths = append(gatePaths, path) }
		}
		for _, path := range []string{
			"/zh/chinese_IamOverEighteenYearsOld/19/index.html",
			"/zh/chinese_IamOverEighteenYearsOld/19/",
			"/zh/censored_IamOverEighteenYearsOld/19/index.html",
		} {
			if !containsString(gatePaths, path) { gatePaths = append(gatePaths, path) }
		}
		passed := false
		for index, path := range gatePaths {
			if index >= 4 { break }
			gate := *parsed
			gate.Path, gate.RawQuery, gate.Fragment = path, "", ""
			gateBody, gateErr := d.fetchProviderText(ctx, gate.String(), parsed.Scheme+"://"+parsed.Host+"/")
			if gateErr == nil && !chaoliuIsAgeGate(gateBody) && !chaoliuLooksOffsite(gateBody) {
				passed = true
				break
			}
		}
		if !passed { return "", "", errors.New("chaoliu 年齡確認頁無法通過") }
		finalURL = d.providerBaseURL(sourceChaoliu) + parsed.RequestURI()
		body, err = d.fetchProviderText(ctx, rawURL, finalURL)
		if err != nil { return "", "", err }
		finalURL = d.providerBaseURL(sourceChaoliu) + parsed.RequestURI()
		if chaoliuIsAgeGate(body) || chaoliuLooksOffsite(body) {
			return "", "", errors.New("chaoliu 年齡確認後仍未取得有效頁面")
		}
	}
	return body,finalURL,nil
}
func chaoliuCardNode(node *htmlparser.Node) bool {
	if node == nil || node.Type != htmlparser.ElementNode ||
		(node.Data != "div" && node.Data != "article" && node.Data != "li") {
		return false
	}
	for _, attribute := range node.Attr {
		if !strings.EqualFold(attribute.Key, "class") {
			continue
		}
		for _, className := range strings.Fields(strings.ToLower(attribute.Val)) {
			if className == "post" || className == "post-item" ||
				className == "video-item" || className == "item-video" ||
				strings.HasPrefix(className, "post-") {
				return true
			}
		}
	}
	return false
}

var (
	chaoliuImageTag = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	chaoliuVideoTag = regexp.MustCompile(`(?is)<video\b[^>]*>`)
)

func chaoliuImageCandidates(fragment, pageURL string) []string {
	var addresses []string
	add := func(raw string) {
		address := resolveProviderURL(pageURL, html.UnescapeString(strings.TrimSpace(raw)))
		parsed, err := url.Parse(address)
		if err == nil && parsed.User == nil && validNativeCoverURL(parsed) {
			addresses = append(addresses, address)
		}
	}
	for _, tag := range chaoliuImageTag.FindAllString(fragment, -1) {
		for _, attribute := range []string{"data-original", "data-lazy-src", "data-src", "data-url", "src"} {
			if raw := extractAttr(tag, attribute); raw != "" {
				add(raw)
			}
		}
		for _, attribute := range []string{"data-srcset", "data-lazy-srcset", "srcset"} {
			if raw := extractAttr(tag, attribute); raw != "" {
				for _, entry := range adultImageSrcsetCandidates(raw) {
					add(entry)
				}
			}
		}
		if style := extractAttr(tag, "style"); style != "" {
			for _, match := range adultCSSImagePattern.FindAllStringSubmatch(style, -1) {
				if len(match) > 1 {
					add(match[1])
				}
			}
		}
	}
	for _, tag := range chaoliuVideoTag.FindAllString(fragment, -1) {
		add(firstNonEmpty(extractAttr(tag, "poster"), extractAttr(tag, "data-poster")))
	}
	return nativeUniqueCoverAddresses(addresses...)
}

func chaoliuDetailCoverCandidates(body, pageURL string) []string {
	var addresses []string
	metadata := chaoliuJSONLD(body)
	for _, key := range []string{"image", "thumbnailUrl", "thumbnail"} {
		if value, exists := metadata[key]; exists {
			addresses = append(addresses, providerCoverAddresses(value, pageURL)...)
		}
	}
	for _, key := range []string{"og:image", "twitter:image"} {
		if raw := extractMetaContent(body, key); raw != "" {
			addresses = append(addresses, resolveProviderURL(pageURL, html.UnescapeString(raw)))
		}
	}
	addresses = append(addresses, chaoliuImageCandidates(body, pageURL)...)
	return nativeUniqueCoverAddresses(addresses...)
}

func parseChaoliuList(body, pageURL string) []Drama {
	document, err := htmlparser.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	cards := make([]*htmlparser.Node, 0, 24)
	var collect func(*htmlparser.Node)
	collect = func(node *htmlparser.Node) {
		if chaoliuCardNode(node) {
			cards = append(cards, node)
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(document)

	out := make([]Drama, 0, len(cards))
	seen := map[string]bool{}
	for _, card := range cards {
		var fragment bytes.Buffer
		if err := htmlparser.Render(&fragment, card); err != nil {
			continue
		}
		chunk := fragment.String()
		match := chaoliuCardHref.FindStringSubmatch(chunk)
		if len(match) < 7 {
			continue
		}
		kind, number, slug := match[1], match[2], match[3]
		if kind == "" {
			kind, number, slug = match[4], match[5], match[6]
		}
		sourceID := kind + "_content/" + number + "/" + slug
		if !validChaoliuID(sourceID) || seen[sourceID] {
			continue
		}
		seen[sourceID] = true

		title := ""
		if heading := regexp.MustCompile("(?is)<h[1-3][^>]*>([\\s\\S]{1,500}?)</h[1-3]>").FindStringSubmatch(chunk); len(heading) > 1 {
			title = cleanText(heading[1])
		}
		if title == "" {
			title = strings.ReplaceAll(slug, "-", " ")
		}

		coverCandidates := chaoliuImageCandidates(chunk, pageURL)
		cover := ""
		if len(coverCandidates) > 0 {
			cover = coverCandidates[0]
		}
		out = append(out, Drama{
			ID: providerDramaID(sourceChaoliu, sourceID), Source: sourceChaoliu, SourceID: sourceID,
			Title: truncate(title, 512), Name: truncate(title, 512),
			Cover: cover, CoverURL: cover, CoverCandidates: append([]string(nil), coverCandidates[1:]...),
			ChannelName: "chaoliu", CategoryName: "chaoliu",
			TotalEpisode: 1, EpisodeCount: 1,
		})
	}
	return out
}
func chaoliuHasNextPage(body string,page int) bool {
	if page<1{return false};re:=regexp.MustCompile("(?i)_list/all/([0-9]+)\\.html");maxPage:=1
	for _,match:=range re.FindAllStringSubmatch(body,-1){if len(match)>1{if n,e:=strconv.Atoi(match[1]);e==nil&&n>maxPage{maxPage=n}}};return maxPage>page
}
func (d *Downloader) fetchChaoliuCatalogPage(ctx context.Context,page int,category,query string)([]Drama,bool,error) {
	var path string;var err error
	if strings.TrimSpace(query)!=""{path,err=chaoliuSearchPath(page,query)}else{path,err=chaoliuCategoryPath(page,category)}
	if err!=nil{return nil,false,err}
	base:=d.providerBaseURL(sourceChaoliu);body,final,err:=d.fetchChaoliuText(ctx,strings.TrimRight(base,"/")+path,base+"/")
	if err!=nil{return nil,false,err}
	items:=parseChaoliuList(body,final)
	// A successful HTTP response is not proof that the catalogue parser still
	// matches the page. On the first catalogue page, fail closed when no cards
	// were parsed so a site redesign or access-denied shell cannot replace a
	// previously populated cache with an empty catalogue. Empty search results
	// remain valid and are returned as an empty list.
	if len(items)==0 && page==1 && strings.TrimSpace(query)=="" {
		return nil,false,errors.New("chaoliu 目錄頁已連線，但未解析到作品卡片；已保留既有快取")
	}
	return items,chaoliuHasNextPage(body,page),nil
}
func chaoliuJSONLD(body string) map[string]any {
	pattern:=regexp.MustCompile("(?is)<script[^>]*type=['\\x22]application/ld\\+json['\\x22][^>]*>([\\s\\S]*?)</script>")
	for _,match:=range pattern.FindAllStringSubmatch(body,-1){if len(match)<2{continue};var value any;if json.Unmarshal([]byte(html.UnescapeString(strings.TrimSpace(match[1]))),&value)!=nil{continue}
		var walk func(any)map[string]any
		walk=func(v any)map[string]any{switch item:=v.(type){case map[string]any:if _,ok:=item["name"];ok{return item};if graph,ok:=item["@graph"];ok{if found:=walk(graph);found!=nil{return found}};return nil;case []any:for _,child:=range item{if found:=walk(child);found!=nil{return found}}};return nil}
		if found:=walk(value);found!=nil{return found}
	};return map[string]any{}
}
func chaoliuMetaField(body,label string) string {
	re:=regexp.MustCompile("(?is)"+regexp.QuoteMeta(label)+"[\\s\\S]{0,80}?<[^>]+>([^<]{1,100})</a>|"+regexp.QuoteMeta(label)+"[\\s\\S]{0,40}?[:：]\\s*([^<\\n]{1,100})")
	m:=re.FindStringSubmatch(body);if len(m)>2{return cleanText(firstNonEmpty(m[1],m[2]))};return ""
}
func chaoliuParseConstants(body string) chaoliuConstants {
	result:=chaoliuConstants{Key:"0123456789abcdef",IV:"0123456789abcdef",Base:11,XOR:18}
	find:=func(pattern string)string{m:=regexp.MustCompile(pattern).FindStringSubmatch(body);if len(m)>1{return m[1]};return ""}
	if v:=find("(?i)(?:argdeqweqweqwe|hdddedd252)\\s*=\\s*['\\x22]([0-9a-f]{16})['\\x22]");v!=""{result.Key=v}
	if v:=find("(?i)(?:hdddedg252|argdeqweqweqww)\\s*=\\s*['\\x22]([0-9a-f]{16})['\\x22]");v!=""{result.IV=v}
	if v:=find("(?i)hcdeedg252\\s*=\\s*(\\d+)");v!=""{if n,e:=strconv.Atoi(v);e==nil{result.Base=n}}
	if v:=find("(?i)hadeedg252\\s*=\\s*(\\d+)");v!=""{if n,e:=strconv.Atoi(v);e==nil{result.XOR=n}}
	if result.Base>25{result.Base%=25};if result.Base<2{result.Base=11};return result
}
var (
	chaoliuQualityMarker = regexp.MustCompile("(?i)(?:^|[^0-9])(2160|1440|1080|720|576|540|480|360|240|180|144)(?:p\\b|[^0-9]|$)")
	chaoliuNumResolution = regexp.MustCompile("(?i)numresolution\\s*=\\s*(\\d+)")
)

func chaoliuQualityResolution(key, payload string) int {
	if match := chaoliuNumResolution.FindStringSubmatch(payload); len(match) > 1 {
		if value, err := strconv.Atoi(match[1]); err == nil && value > 0 {
			return value
		}
	}
	if match := chaoliuQualityMarker.FindStringSubmatch(key); len(match) > 1 {
		if value, err := strconv.Atoi(match[1]); err == nil && value > 0 {
			return value
		}
	}
	// Unknown labels stay unknown; do not silently promote them to 720P.
	return 0
}

func chaoliuQualityAllowed(resolution int) bool {
	return resolution == 0 || resolution >= nativeMinimumPlaybackQuality
}

func parseChaoliuQualities(body string) []chaoliuQuality {
	out := []chaoliuQuality{}
	seen := map[string]bool{}
	for _, match := range chaoliuMvarrArray.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 || strings.TrimSpace(match[2]) == "" || seen[match[1]] {
			continue
		}
		encoded := ""
		for _, value := range chaoliuEncryptedString.FindAllStringSubmatch(match[2], -1) {
			if len(value) > 1 && len(value[1]) >= 20 &&
				regexp.MustCompile("^[0-9A-Za-z]+$").MatchString(value[1]) &&
				len(value[1]) > len(encoded) {
				encoded = value[1]
			}
		}
		if encoded == "" {
			continue
		}
		seen[match[1]] = true
		out = append(out, chaoliuQuality{
			Key: match[1], Encrypted: encoded,
			Resolution: chaoliuQualityResolution(match[1], match[2]),
		})
	}
	for _, match := range chaoliuMvarrObject.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 || seen[match[1]] {
			continue
		}
		encoded := ""
		for _, value := range chaoliuEncryptedString.FindAllStringSubmatch(match[2], -1) {
			if len(value) > 1 && len(value[1]) >= 20 &&
				regexp.MustCompile("^[0-9A-Za-z]+$").MatchString(value[1]) {
				encoded = value[1]
				break
			}
		}
		if encoded == "" {
			continue
		}
		seen[match[1]] = true
		out = append(out, chaoliuQuality{
			Key: match[1], Encrypted: encoded,
			Resolution: chaoliuQualityResolution(match[1], match[2]),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Resolution > out[j].Resolution
	})
	return out
}
func chaoliuDecrypto(encoded string,base,xor int) string {
	if base<2||base>36{return ""};parts:=strings.Split(encoded,string(rune(base+97)));var out strings.Builder
	for _,part:=range parts{value,e:=strconv.ParseInt(strings.TrimSpace(part),base,64);if e!=nil{return ""};ch:=byte((value^int64(xor))&0xff);if ch<32||ch>126{return ""};out.WriteByte(ch)};return out.String()
}
func chaoliuDecodePlayID(encoded string,constants chaoliuConstants)(string,error) {
	candidates:=[]string{};valid:=regexp.MustCompile("^[A-Za-z0-9+/]+={0,2}$")
	primary:=chaoliuDecrypto(encoded,constants.Base,constants.XOR);if primary!=""&&valid.MatchString(primary){candidates=append(candidates,primary)}
	for base:=2;base<=25&&len(candidates)<12;base++{for xor:=0;xor<=32&&len(candidates)<12;xor++{value:=chaoliuDecrypto(encoded,base,xor);if len(value)>=16&&valid.MatchString(value)&&!containsString(candidates,value){candidates=append(candidates,value)}}}
	block,e:=aes.NewCipher([]byte(constants.Key));if e!=nil{return "",e};if len(constants.IV)!=block.BlockSize(){return "",errors.New("chaoliu AES IV 長度錯誤")}
	for _,candidate:=range candidates{raw,e:=base64.StdEncoding.DecodeString(candidate);if e!=nil||len(raw)==0||len(raw)%block.BlockSize()!=0{continue};plain:=make([]byte,len(raw));cipher.NewCBCDecrypter(block,[]byte(constants.IV)).CryptBlocks(plain,raw);plain,e=pkcs7Unpad(plain,block.BlockSize());if e!=nil{continue};id:=regexp.MustCompile("[^A-Za-z0-9._-]").ReplaceAllString(string(plain),"");if len(id)>=12{return id,nil}}
	return "",errors.New("chaoliu 播放地址解密失敗")
}
func parseChaoliuSources(body,pageURL string) []chaoliuMediaSource {
	body=html.UnescapeString(strings.NewReplacer("\\/", "/", "\\u002F", "/", "\\u0026", "&").Replace(body))
	out:=[]chaoliuMediaSource{};seen:=map[string]bool{}
	add:=func(raw string,size int64){address:=resolveProviderURL(pageURL,strings.TrimSpace(raw));parsed,e:=url.Parse(address);if e!=nil||!isProviderHTTPMediaURL(address)||parsed.User!=nil{return};p:=strings.ToLower(parsed.Path);if !(strings.HasSuffix(p,".m3u8")||strings.HasSuffix(p,".mp4"))||seen[address]{return};seen[address]=true;out=append(out,chaoliuMediaSource{address,size})}
	for _,m:=range chaoliuSourcePattern.FindAllStringSubmatch(body,-1){if len(m)>1{var size int64;if len(m)>2{size,_=strconv.ParseInt(m[2],10,64)};add(m[1],size)}}
	for _,raw:=range chaoliuDirectPattern.FindAllString(body,-1){add(raw,0)}
	sort.SliceStable(out,func(i,j int)bool{return out[i].Size>out[j].Size});return out
}
func chaoliuDetailTitle(body,baseURL,sourceID string)(string,string,string) {
	ld:=chaoliuJSONLD(body);title,_:=ld["name"].(string);desc,_:=ld["description"].(string)
	if title==""{if m:=regexp.MustCompile("(?is)<h1[^>]*>([\\s\\S]{1,300}?)</h1>").FindStringSubmatch(body);len(m)>1{title=cleanText(m[1])}}
	if title==""{title=sourceID}
	if desc==""{desc=extractMetaContent(body,"description")}
	covers:=chaoliuDetailCoverCandidates(body,baseURL);cover:=""
	if len(covers)>0{cover=covers[0]}
	return truncate(cleanText(title),512),cover,truncate(cleanText(desc),12000)
}
func extractMetaContent(body,key string) string {
	for _,tag:=range regexp.MustCompile("(?is)<meta\\b[^>]*>").FindAllString(body,-1){name:=firstNonEmpty(extractAttr(tag,"property"),extractAttr(tag,"name"));if strings.EqualFold(name,key){return extractAttr(tag,"content")}}
	return ""
}
func (d *Downloader) fetchChaoliuDetail(ctx context.Context,sourceID string)(Drama,[]Chapter,error) {
	if !validChaoliuID(sourceID){return Drama{},nil,errors.New("chaoliu 作品識別碼無效")}
	base:=d.providerBaseURL(sourceChaoliu);body,final,err:=d.fetchChaoliuText(ctx,strings.TrimRight(base,"/")+"/zh/"+sourceID+".html",base+"/")
	if err!=nil{return Drama{},nil,fmt.Errorf("chaoliu 作品詳情讀取失敗：%w",err)}
	title,cover,desc:=chaoliuDetailTitle(body,final,sourceID)
	if v:=chaoliuMetaField(body,"番號");v!=""{desc=strings.TrimSpace(desc+"\n番號："+v)}
	if v:=chaoliuMetaField(body,"發行商");v!=""&&v!="未知"{desc=strings.TrimSpace(desc+"\n發行商："+v)}
	if v:=chaoliuMetaField(body,"製作商");v!=""&&v!="未知"{desc=strings.TrimSpace(desc+"\n製作商："+v)}
	if len(parseChaoliuQualities(body))==0{desc=strings.TrimSpace(desc+"\n站方未提供播放密文，目前無法直接播放。")}
	coverCandidates:=chaoliuDetailCoverCandidates(body,final)
	drama:=Drama{ID:providerDramaID(sourceChaoliu,sourceID),Source:sourceChaoliu,SourceID:sourceID,Title:title,Name:title,Desc:desc,Intro:desc,Cover:cover,CoverURL:cover,CoverCandidates:append([]string(nil),coverCandidates[1:]...),ChannelName:"chaoliu",CategoryName:"chaoliu",TotalEpisode:1,EpisodeCount:1}
	chapter:=Chapter{ID:providerChapterID(sourceChaoliu,sourceID,final),Source:sourceChaoliu,Title:"正片",PageURL:final,Referer:final,VideoURL:"chaoliu-page://"+final,CurrentEpisode:json.RawMessage("1")}
	return drama,[]Chapter{chapter},nil
}
func (d *Downloader) resolveChaoliuMedia(ctx context.Context,task Task)(providerMedia,error) {
	source,sourceID,ok:=splitProviderDramaID(task.DramaID)
	if !ok||source!=sourceChaoliu||!validChaoliuID(sourceID)||!strings.HasPrefix(task.Chapter.ID,providerChapterID(sourceChaoliu,sourceID,"")){return providerMedia{},errors.New("chaoliu 分集資訊無效，請重新整理作品詳情")}
	pageURL:=strings.TrimSpace(task.Chapter.PageURL);if pageURL==""{pageURL=strings.TrimPrefix(task.Chapter.VideoURL,"chaoliu-page://")}
	parsed,e:=url.Parse(pageURL);if e!=nil||parsed.Scheme!="https"||!isChaoliuSiteHost(parsed.Hostname()){return providerMedia{},errors.New("chaoliu 播放頁網址無效")}
	body,final,e:=d.fetchChaoliuText(ctx,pageURL,firstNonEmpty(task.Chapter.Referer,parsed.Scheme+"://"+parsed.Host+"/"));if e!=nil{return providerMedia{},fmt.Errorf("chaoliu 播放頁讀取失敗：%w",e)}
	qualities:=parseChaoliuQualities(body);if len(qualities)==0{return providerMedia{},errors.New("chaoliu 作品沒有可用的播放密文")}
	constants:=chaoliuParseConstants(body);var lastErr error;skippedLowQuality:=false
	for _,quality:=range qualities {
		if !chaoliuQualityAllowed(quality.Resolution) { skippedLowQuality=true; continue }
		playID,decodeErr:=chaoliuDecodePlayID(quality.Encrypted,constants);if decodeErr!=nil{lastErr=decodeErr;continue}
		baseParsed,_:=url.Parse(final);playPath:=fmt.Sprintf("/js/player/play.php?numresolution=%d&id=%s",max(quality.Resolution,nativeMinimumPlaybackQuality),url.QueryEscape(playID))
		playerURL:=baseParsed.Scheme+"://"+baseParsed.Host+playPath
		playerBody,playerFinal,fetchErr:=d.fetchChaoliuText(ctx,playerURL,final);if fetchErr!=nil{lastErr=fetchErr;continue}
		sources:=parseChaoliuSources(playerBody,playerFinal);if len(sources)==0{lastErr=errors.New("chaoliu 播放頁沒有提供有效的 HLS 或 MP4 網址");continue}
		orderedURLs:=make([]string,0,len(sources))
		sourceByURL:=make(map[string]chaoliuMediaSource,len(sources))
		for _,candidate:=range sources{orderedURLs=append(orderedURLs,candidate.URL);sourceByURL[candidate.URL]=candidate}
		orderedURLs=d.orderProviderEndpoints(sourceChaoliu,"media",orderedURLs)
		var prepared providerMedia
		preparedOK:=false
		for index,mediaURL:=range orderedURLs {
			started:=time.Now()
			candidate,prepareErr:=d.prepareWebProviderMedia(ctx,providerMedia{URL:mediaURL,Referer:baseParsed.Scheme+"://"+baseParsed.Host+"/"},"chaoliu")
			candidate.Quality=quality.Resolution
			d.recordProviderEndpoint(sourceChaoliu,"media",mediaURL,time.Since(started),prepareErr)
			if prepareErr!=nil {lastErr=prepareErr;if ctx.Err()!=nil{return providerMedia{},ctx.Err()};continue}
			for _,fallbackURL:=range orderedURLs[index+1:] {
				fallback:=sourceByURL[fallbackURL]
				candidate.Variants=append(candidate.Variants,providerMedia{URL:fallback.URL,Referer:baseParsed.Scheme+"://"+baseParsed.Host+"/",Quality:quality.Resolution})
			}
			prepared=candidate
			preparedOK=true
			break
		}
		if preparedOK{return prepared,nil}
	}
	if lastErr==nil { if skippedLowQuality { lastErr=errors.New("chaoliu 目前只提供低於 720P 的播放線路") } else { lastErr=errors.New("chaoliu 所有畫質線路皆無法解析") } };return providerMedia{},lastErr
}
