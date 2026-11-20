package core

import (
	"context"
	"encoding/json"
	"html"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	htmlnet "golang.org/x/net/html"
)

const (
	ikanbotBaseURL = "https://admin-api.aikanbot.com"
	ikanbotUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	ikanbotPageSize = 36
	ikanbotMaxLines = 40
)

var ikanbotCategories = []nativeCategory{
	{ID:"movie-热门",Name:"熱門電影"},
	{ID:"tv-热门",Name:"熱門影集"},
	{ID:"tv-国产剧",Name:"中國劇"},
	{ID:"tv-日剧",Name:"日劇"},
	{ID:"tv-韩剧",Name:"韓劇"},
	{ID:"tv-美剧",Name:"美劇"},
	{ID:"tv-英剧",Name:"英劇"},
	{ID:"tv-港剧",Name:"港劇"},
	{ID:"tv-日本动画",Name:"日本動畫"},
	{ID:"tv-综艺",Name:"綜藝"},
	{ID:"tv-纪录片",Name:"紀錄片"},
}

type ikanbotLine struct {
	Flag string
	Episodes []ikanbotEpisode
}
type ikanbotEpisode struct {
	Name string
	URL string
	Order int
}

func validIkanbotCategory(category string) bool {
	if category=="" { return true }
	for _, item:=range ikanbotCategories { if item.ID==category { return true } }
	return false
}
func fetchIkanbotPage(ctx context.Context,d *Downloader,address,referer string)(*htmlnet.Node,string,string,error){
	ctx=context.WithValue(ctx,providerTextUserAgentKey{},ikanbotUserAgent)
	responses:=&playbackResponseURLs{}
	ctx=context.WithValue(ctx,playbackResponseURLsKey{},responses)
	body,err:=d.fetchProviderText(ctx,address,firstNonEmpty(referer,ikanbotBaseURL+"/"))
	if err!=nil{return nil,"",address,err}
	if actual,ok:=responses.values.Load(address);ok{if value,valid:=actual.(string);valid&&value!=""{address=value}}
	if len(body)>4<<20{return nil,"",address,errors.New("愛看機器人頁面過大")}
	doc,err:=htmlnet.Parse(strings.NewReader(body))
	return doc,body,address,err
}
func ikanbotNumericID(value string)bool{return regexp.MustCompile("^[0-9]{1,12}$").MatchString(strings.TrimSpace(value))&&strings.TrimSpace(value)!="0"}
func ikanbotCategoryPath(page int,category string)(string,error){
	if !validIkanbotCategory(category)||page<1||page>100000{return "",errors.New("愛看機器人分類或頁碼無效")}
	if category==""{category=ikanbotCategories[0].ID}
	if page==1{return "/hot/index-"+category+".html",nil}
	return fmt.Sprintf("/hot/index-%s-p-%d.html",category,page),nil
}
func ikanbotImageCandidates(node *htmlnet.Node, pageURL string) []string {
	if node == nil {
		return nil
	}
	seen := map[string]bool{}
	var result []string
	add := func(raw string) {
		raw = strings.TrimSpace(html.UnescapeString(raw))
		if raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:") ||
			strings.HasPrefix(strings.ToLower(raw), "blob:") {
			return
		}
		address := resolveProviderURL(pageURL, raw)
		if !isProviderHTTPMediaURL(address) || seen[address] {
			return
		}
		seen[address] = true
		result = append(result, address)
	}
	for _, attribute := range []string{
		"data-original", "data-original-src", "data-src", "data-lazy-src",
		"data-lazyload", "data-image", "data-url", "data-bg", "src",
	} {
		add(providerHTMLAttr(node, attribute))
	}
	for _, attribute := range []string{"data-srcset", "data-lazy-srcset", "srcset"} {
		for _, candidate := range strings.Split(providerHTMLAttr(node, attribute), ",") {
			fields := strings.Fields(strings.TrimSpace(candidate))
			if len(fields) > 0 {
				add(fields[0])
			}
		}
	}
	style := providerHTMLAttr(node, "style")
	backgroundPattern := regexp.MustCompile("(?i)url\\(\\s*['\\x22]?([^'\\x22)]+)")
	for _, match := range backgroundPattern.FindAllStringSubmatch(style, 8) {
		if len(match) > 1 {
			add(match[1])
		}
	}
	return result
}

func ikanbotImage(node *htmlnet.Node, pageURL string) string {
	candidates := ikanbotImageCandidates(node, pageURL)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func ikanbotFindCard(root *htmlnet.Node)[]*htmlnet.Node{
	return providerHTMLNodes(root,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&providerHTMLClass(n,"item")})
}
func ikanbotCardIDFromHref(raw string)string{
	u,err:=url.Parse(strings.TrimSpace(raw))
	if err!=nil{return ""}
	if u.IsAbs(){
		host:=strings.ToLower(u.Hostname())
		if host!="ikanbot.com"&&host!="www.ikanbot.com"&&host!="v.ikanbot.com"&&host!="v.aikanbot.com"&&host!="admin-api.aikanbot.com"{return ""}
	}
	m:=regexp.MustCompile("^/play/([0-9]{1,12})(?:/|$)").FindStringSubmatch(u.Path)
	if len(m)<2||!ikanbotNumericID(m[1]){return ""}
	return m[1]
}

func ikanbotFirstImage(root *htmlnet.Node)*htmlnet.Node{
	if root==nil{return nil}
	for _,node:=range providerHTMLNodes(root,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="img"}){
		if ikanbotImage(node,ikanbotBaseURL+"/")!=""{return node}
	}
	return nil
}

func ikanbotParseCards(root *htmlnet.Node,pageURL,category string)[]Drama{
	out:=[]Drama{};seen:=map[string]bool{}
	anchors:=providerHTMLNodes(root,func(n *htmlnet.Node)bool{
		return n.Type==htmlnet.ElementNode&&n.Data=="a"&&ikanbotCardIDFromHref(providerHTMLAttr(n,"href"))!=""
	})
	for _,anchor:=range anchors{
		id:=ikanbotCardIDFromHref(providerHTMLAttr(anchor,"href"))
		if id==""||seen[id]{continue}
		var image *htmlnet.Node
		// Current iKanBot templates put the poster inside the title link. Keep
		// checking nearby card ancestors for older layouts where it is a sibling.
		for current,depth:=anchor,0;current!=nil&&depth<4&&image==nil;current,depth=current.Parent,depth+1{
			image=ikanbotFirstImage(current)
		}
		title:=providerHTMLPlainText(providerHTMLAttr(anchor,"title"))
		if title==""&&image!=nil{title=providerHTMLPlainText(providerHTMLAttr(image,"alt"))}
		if title==""{title=providerHTMLPlainText(providerHTMLText(anchor))}
		if title==""{
			for current,depth:=anchor.Parent,0;current!=nil&&depth<3;current,depth=current.Parent,depth+1{
				for _,node:=range providerHTMLNodes(current,func(n *htmlnet.Node)bool{
					return n.Type==htmlnet.ElementNode&&(n.Data=="h3"||n.Data=="h4"||n.Data=="p")
				}){
					title=providerHTMLPlainText(providerHTMLText(node))
					if title!=""{break}
				}
				if title!=""{break}
			}
		}
		if title==""{continue}
		coverCandidates := ikanbotImageCandidates(image, pageURL)
		cover := ""
		if len(coverCandidates) > 0 {
			cover = coverCandidates[0]
		}
		seen[id]=true
		out=append(out,Drama{ID:providerDramaID(sourceIkanBot,id),Source:sourceIkanBot,SourceID:id,Title:truncate(title,512),Name:truncate(title,512),Cover:cover,CoverURL:cover,CoverCandidates:append([]string(nil),coverCandidates[1:]...),ChannelName:"愛看機器人",CategoryName:ikanbotCategoryKind(category)})
		if len(out)>=500{break}
	}
	return out
}
func ikanbotCategoryKind(category string)string{if strings.HasPrefix(category,"movie-"){return "電影"};return "影集"}
func ikanbotParseSearch(root *htmlnet.Node,pageURL string)[]Drama{
	if items:=ikanbotParseCards(root,pageURL,"");len(items)>0{return items}
	out:=[]Drama{};seen:=map[string]bool{}
	blocks:=providerHTMLNodes(root,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&providerHTMLClass(n,"media")})
	for _,block:=range blocks{
		var anchor,image *htmlnet.Node
		for _,node:=range providerHTMLNodes(block,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="a"&&providerHTMLClass(n,"title-text")}){if regexp.MustCompile("^/play/[0-9]{1,12}").MatchString(providerHTMLAttr(node,"href")){anchor=node;break}}
		if anchor==nil{continue}
		m:=regexp.MustCompile("^/play/([0-9]{1,12})").FindStringSubmatch(providerHTMLAttr(anchor,"href"));if len(m)<2||seen[m[1]]{continue}
		title:=providerHTMLText(anchor)
		for _,node:=range providerHTMLNodes(block,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="img"}){image=node;break}
		if title==""&&image!=nil{title=providerHTMLAttr(image,"alt")}
		title=providerHTMLPlainText(title);if title==""{continue};seen[m[1]]=true
		coverCandidates := ikanbotImageCandidates(image, pageURL)
		cover := ""
		if len(coverCandidates) > 0 { cover = coverCandidates[0] }
		out=append(out,Drama{ID:providerDramaID(sourceIkanBot,m[1]),Source:sourceIkanBot,SourceID:m[1],Title:truncate(title,512),Name:truncate(title,512),Cover:cover,CoverURL:cover,CoverCandidates:append([]string(nil),coverCandidates[1:]...),ChannelName:"愛看機器人",CategoryName:"影集"})
		if len(out)>=100{break}
	}
	return out
}
func (d *Downloader) fetchIkanbotCatalogPage(ctx context.Context,page int,category,query string)([]Drama,bool,error){
	var path string;var err error
	if strings.TrimSpace(query)!=""{
		if len([]rune(query))>256{return nil,false,errors.New("愛看機器人搜尋關鍵字過長")}
		path="/search?q="+url.QueryEscape(strings.TrimSpace(query));if page>1{return []Drama{},false,nil}
	}else{path,err=ikanbotCategoryPath(page,category);if err!=nil{return nil,false,err}}
	doc,body,final,err:=fetchIkanbotPage(ctx,d,ikanbotBaseURL+path,ikanbotBaseURL+"/");if err!=nil{return nil,false,fmt.Errorf("愛看機器人目錄讀取失敗：%w",err)}
	if strings.TrimSpace(query)!=""{return ikanbotParseSearch(doc,final),false,nil}
	items:=ikanbotParseCards(doc,final,category)
	maxPage:=page
	re:=regexp.MustCompile("-p-([0-9]+)\\.html")
	for _,m:=range re.FindAllStringSubmatch(body,-1){if len(m)>1{if n,e:=strconv.Atoi(m[1]);e==nil&&n>maxPage&&n<100000{maxPage=n}}}
	more:=maxPage>page||len(items)>=ikanbotPageSize
	return items,more,nil
}
func ikanbotInput(root *htmlnet.Node,id string)string{
	for _,n:=range providerHTMLNodes(root,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="input"&&providerHTMLAttr(n,"id")==id}){return strings.TrimSpace(providerHTMLAttr(n,"value"))}
	return ""
}
func ikanbotSignToken(currentID,token string)string{
	if len(currentID)<4||len(token)<16{return ""}
	tail:=currentID[len(currentID)-4:];rest:=token;var out strings.Builder
	for _,digit:=range tail{if digit<'0'||digit>'9'{return ""};start:=int(digit-'0')%3+1;if start+8>len(rest){return ""};out.WriteString(rest[start:start+8]);rest=rest[start+8:]}
	return out.String()
}
func ikanbotQuality(flag string)int{
	m:=regexp.MustCompile("(2160|1440|1080|720|480)").FindStringSubmatch(strings.ToLower(flag));if len(m)>1{n,_:=strconv.Atoi(m[1]);return n};return 0
}
func ikanbotQualityOnlyLabel(name string) bool {
	value := strings.ToLower(strings.TrimSpace(name))
	value = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(value)
	if regexp.MustCompile("^(?:(?:標清|标清|高清|超清|超高清|藍光|蓝光|sd|hd|fhd|uhd))?(?:2160|1440|1080|720|480)(?:p|集|話|话|期)?(?:(?:標清|标清|高清|超清|超高清|藍光|蓝光))?$").MatchString(value) {
		return true
	}
	switch value {
	case "標清", "标清", "高清", "超清", "超高清", "藍光", "蓝光", "sd", "hd", "fhd", "uhd", "4k":
		return true
	default:
		return false
	}
}

func ikanbotEpisodeOrder(name string, fallback int) int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile("(?i)第\\s*0*([0-9]{1,4})\\s*[集話话期]"),
		regexp.MustCompile("(?i)\\b(?:ep?|episode)\\s*0*([0-9]{1,4})\\b"),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(name); len(match) > 1 {
			if order, err := strconv.Atoi(match[1]); err == nil && order > 0 && order < 100000 {
				return order
			}
		}
	}
	if ikanbotQualityOnlyLabel(name) {
		return fallback
	}
	if match := regexp.MustCompile("([0-9]{1,4})").FindStringSubmatch(name); len(match) > 1 {
		if order, err := strconv.Atoi(match[1]); err == nil && order > 0 && order < 100000 {
			return order
		}
	}
	return fallback
}
type ikanbotResEntry struct {
	Flag string `json:"flag"`
	URL  string `json:"url"`
}

func ikanbotParseResData(raw string) []ikanbotResEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var entries []ikanbotResEntry
	if json.Unmarshal([]byte(raw), &entries) == nil && len(entries) > 0 {
		return entries
	}
	var single ikanbotResEntry
	if json.Unmarshal([]byte(raw), &single) == nil &&
		strings.TrimSpace(single.Flag) != "" && strings.TrimSpace(single.URL) != "" {
		return []ikanbotResEntry{single}
	}

	// Some responses contain a JSON string which is escaped one extra time.
	normalized := strings.NewReplacer(
		`\\\"`, `\"`,
		`\"`, `"`,
		`\/`, `/`,
		`&amp;`, `&`,
	).Replace(raw)
	if normalized != raw {
		if json.Unmarshal([]byte(normalized), &entries) == nil && len(entries) > 0 {
			return entries
		}
		if json.Unmarshal([]byte(normalized), &single) == nil &&
			strings.TrimSpace(single.Flag) != "" && strings.TrimSpace(single.URL) != "" {
			return []ikanbotResEntry{single}
		}
		raw = normalized
	}

	// Fallback to the field-pair structure used by the original IkanBot module.
	pattern := regexp.MustCompile(`(?is)"flag"\s*:\s*"((?:\\.|[^"\\])*)"\s*,\s*"url"\s*:\s*"((?:\\.|[^"\\])*)"`)
	matches := pattern.FindAllStringSubmatch(raw, ikanbotMaxLines)
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		flag, flagErr := strconv.Unquote("\"" + match[1] + "\"")
		if flagErr != nil {
			flag = match[1]
		}
		urlValue, urlErr := strconv.Unquote("\"" + match[2] + "\"")
		if urlErr != nil {
			urlValue = match[2]
		}
		entries = append(entries, ikanbotResEntry{Flag: flag, URL: urlValue})
	}
	return entries
}

func ikanbotParseLines(payload map[string]any) []ikanbotLine {
	data, _ := payload["data"].(map[string]any)
	rows, _ := data["list"].([]any)
	lines := make([]ikanbotLine, 0)
	for _, row := range rows {
		obj, _ := row.(map[string]any)
		raw := ""
		switch value := obj["resData"].(type) {
		case string:
			raw = value
		case []any, map[string]any:
			if encoded, err := json.Marshal(value); err == nil {
				raw = string(encoded)
			}
		}
		for _, entry := range ikanbotParseResData(raw) {
			flag := strings.TrimSpace(entry.Flag)
			if flag == "" || strings.TrimSpace(entry.URL) == "" {
				continue
			}
			line := ikanbotLine{Flag: flag}
			for index, piece := range strings.Split(entry.URL, "#") {
				piece = strings.TrimSpace(piece)
				if piece == "" {
					continue
				}
				parts := strings.SplitN(piece, "$", 2)
				name := ""
				media := piece
				if len(parts) == 2 {
					name = strings.TrimSpace(parts[0])
					media = strings.TrimSpace(parts[1])
				}
				if extra := strings.Index(media, "$"); extra >= 0 {
					media = media[:extra]
				}
				media = strings.TrimSpace(strings.ReplaceAll(media, `\/`, "/"))
				u, err := url.Parse(media)
				if err != nil || u.User != nil || !isProviderHTTPMediaURL(media) {
					continue
				}
				if name == "" {
					name = fmt.Sprintf("第 %d 集", index+1)
				}
				line.Episodes = append(line.Episodes, ikanbotEpisode{
					Name: name,
					URL: media,
					Order: ikanbotEpisodeOrder(name, index+1),
				})
			}
			if len(line.Episodes) > 0 {
				lines = append(lines, line)
			}
			if len(lines) >= ikanbotMaxLines {
				break
			}
		}
		if len(lines) >= ikanbotMaxLines {
			break
		}
	}
	sort.SliceStable(lines, func(i, j int) bool {
		leftQuality, rightQuality := ikanbotQuality(lines[i].Flag), ikanbotQuality(lines[j].Flag)
		if leftQuality != rightQuality {
			return leftQuality > rightQuality
		}
		return len(lines[i].Episodes) > len(lines[j].Episodes)
	})
	return lines
}

func ikanbotPlaybackAPIURL(pageURL, videoID, mtype, signedToken string) (string, error) {
	page, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil || page.Scheme != "https" || page.User != nil {
		return "", errors.New("愛看機器人播放頁網址無效")
	}
	host := strings.ToLower(page.Hostname())
	switch host {
	case "ikanbot.com", "www.ikanbot.com", "v.ikanbot.com", "v.aikanbot.com", "admin-api.aikanbot.com":
	default:
		return "", errors.New("愛看機器人播放頁網域不受信任")
	}
	if !ikanbotNumericID(videoID) || strings.TrimSpace(signedToken) == "" {
		return "", errors.New("愛看機器人播放參數無效")
	}
	if mtype == "" {
		mtype = "1"
	}
	query := url.Values{}
	query.Set("videoId", videoID)
	query.Set("mtype", mtype)
	query.Set("token", signedToken)
	return page.Scheme + "://" + page.Host + "/api/getResN?" + query.Encode(), nil
}

func (d *Downloader) fetchIkanbotLines(ctx context.Context, sourceID, currentID, token, mtype, pageURL string) ([]ikanbotLine, error) {
	signed := ikanbotSignToken(currentID, token)
	if signed == "" {
		return nil, errors.New("愛看機器人播放令牌無效，請重新整理詳情")
	}
	address, err := ikanbotPlaybackAPIURL(pageURL, sourceID, mtype, signed)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ikanbotUserAgent)
	req.Header.Set("Referer", pageURL)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "*/*")
	resp, err := d.doCatalogRequestWithTimeout(req, providerTimeout)
	if err != nil {
		return nil, fmt.Errorf("愛看機器人播放線路讀取失敗：%w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("愛看機器人播放線路讀取失敗：%w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("愛看機器人播放線路讀取失敗：HTTP %d", resp.StatusCode)
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return nil, errors.New("愛看機器人線路回應格式無效")
	}
	if stateValue, exists := payload["state"]; exists {
		state := strings.TrimSpace(fmt.Sprint(stateValue))
		if state != "" && state != "1" && state != "1.0" {
			return nil, fmt.Errorf("愛看機器人線路取得失敗：%v", payload["message"])
		}
	}
	lines := ikanbotParseLines(payload)
	if len(lines) == 0 {
		return nil, errors.New("愛看機器人目前沒有可播放線路")
	}
	return lines, nil
}
// ikanbotMovieLanguageLabel maps audio/dubbing markers to a stable display label.
// It checks both the episode name and the source flag because iKanBot places
// the language in either field depending on the upstream provider.
func ikanbotMovieLanguageLabel(name, lineFlag string) string {
	value := strings.ToLower(strings.TrimSpace(name + " " + lineFlag))
	value = strings.NewReplacer("繁體", "繁体", "台灣", "台湾").Replace(value)
	contains := func(values ...string) bool {
		for _, candidate := range values {
			if strings.Contains(value, candidate) {
				return true
			}
		}
		return false
	}
	switch {
	case contains("雙語", "双语", "dual audio", "bilingual"):
		return "雙語版"
	case contains("粵語", "粤语", "廣東話", "广东话", "粵配", "粤配", "cantonese"):
		return "粵語版"
	case contains("台語", "台语", "閩南語", "闽南语", "taiwanese"):
		return "台語版"
	case contains("國語", "国语", "普通話", "普通话", "國配", "国配", "mandarin"):
		return "國語版"
	case contains("日語", "日语", "日文", "日配", "japanese"):
		return "日語版"
	case contains("英語", "英语", "英文", "英配", "english"):
		return "英語版"
	case contains("韓語", "韩语", "韓文", "韩文", "韓配", "korean"):
		return "韓語版"
	case contains("原聲", "原声", "原音", "原版", "original audio", "original version"):
		return "原聲版"
	default:
		return ""
	}
}

// Movie listings are not episodes. Each distinct non-language source is one
// selectable movie entry, while audio-language variants share one entry and
// retain alternate URLs as playback fallbacks.
func ikanbotMovieSourceFlagBase(flag string) string {
	value := strings.ToLower(strings.TrimSpace(flag))
	value = regexp.MustCompile(`(?i)(2160|1440|1080|720|480)\s*p?`).ReplaceAllString(value, "")
	value = strings.NewReplacer(
		"超高清", "", "超清", "", "高清", "", "標清", "", "标清", "",
		"藍光", "", "蓝光", "", "uhd", "", "fhd", "", "hd", "", "sd", "",
		"4k", "", " ", "", "-", "", "_", "", "．", "", ".", "",
	).Replace(value)
	return strings.TrimSpace(value)
}

// Movie listings are not episodes. Keep one selectable entry per explicit
// audio language; treat every unlabelled CDN/source row as an automatic fallback.
func (d *Downloader) fetchIkanbotDetail(ctx context.Context,sourceID string)(Drama,[]Chapter,error){
	if !ikanbotNumericID(sourceID){return Drama{},nil,errors.New("愛看機器人作品識別碼無效")}
	pageURL:=ikanbotBaseURL+"/play/"+sourceID
	doc,body,final,err:=fetchIkanbotPage(ctx,d,pageURL,ikanbotBaseURL+"/");if err!=nil{return Drama{},nil,fmt.Errorf("愛看機器人作品詳情讀取失敗：%w",err)}
	title:="";for _,n:=range providerHTMLNodes(doc,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="h1"&&providerHTMLAttr(n,"id")=="video_title"}){title=providerHTMLText(n);break}
	if title==""{title=providerHTMLPlainText(adultMetaValue(body,"og:title"))};if title==""{return Drama{},nil,errors.New("愛看機器人詳情缺少標題")}
	var coverCandidates []string
	seenCover := map[string]bool{}
	addCover := func(address string) {
		address = strings.TrimSpace(address)
		if address != "" && isProviderHTTPMediaURL(address) && !seenCover[address] {
			seenCover[address] = true
			coverCandidates = append(coverCandidates, address)
		}
	}
	for _, node := range providerHTMLNodes(doc, func(n *htmlnet.Node) bool {
		return n.Type == htmlnet.ElementNode && n.Data == "img" && providerHTMLClass(n, "cover")
	}) {
		for _, address := range ikanbotImageCandidates(node, final) { addCover(address) }
	}
	for _, key := range []string{"og:image", "twitter:image"} {
		if raw := adultMetaValue(body, key); raw != "" {
			addCover(resolveProviderURL(final, raw))
		}
	}
	for _, node := range providerHTMLNodes(doc, func(n *htmlnet.Node) bool {
		return n.Type == htmlnet.ElementNode && n.Data == "img"
	}) {
		for _, address := range ikanbotImageCandidates(node, final) { addCover(address) }
	}
	cover := ""
	var coverFallbacks []string
	if len(coverCandidates) > 0 {
		cover = coverCandidates[0]
		if len(coverCandidates) > 1 {
			coverFallbacks = append([]string(nil), coverCandidates[1:]...)
		}
	}
	currentID:=ikanbotInput(doc,"current_id");if currentID==""{currentID=sourceID};token:=ikanbotInput(doc,"e_token");mtype:=ikanbotInput(doc,"mtype");if token==""{return Drama{},nil,errors.New("愛看機器人缺少播放驗證資訊")}
	lines,err:=d.fetchIkanbotLines(ctx,sourceID,currentID,token,mtype,final);if err!=nil{return Drama{},nil,err}
	meta:=[]string{};for _,n:=range providerHTMLNodes(doc,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="h3"&&providerHTMLClass(n,"meta")}){v:=providerHTMLText(n);if v!=""{meta=append(meta,v)}}
	tags:=[]string{};year:="";for _,v:=range meta{if year==""&&regexp.MustCompile("^(19|20)[0-9]{2}$").MatchString(v){year=v};if strings.Contains(v,"/"){tags=append(tags,truncate(v,64))}}
	isMovie := ikanbotIsMovieMType(mtype)
	chapters := ikanbotBuildChapters(sourceID, final, lines, isMovie)
	if len(chapters)==0{return Drama{},nil,errors.New("愛看機器人目前沒有可播放分集")}
	categoryName := "影集"
	if isMovie { categoryName = "電影" }
	drama:=Drama{ID:providerDramaID(sourceIkanBot,sourceID),Source:sourceIkanBot,SourceID:sourceID,Title:truncate(providerHTMLPlainText(title),512),Name:truncate(providerHTMLPlainText(title),512),Desc:truncate(strings.Join(meta," · "),12000),Intro:truncate(strings.Join(meta," · "),12000),Cover:cover,CoverURL:cover,CoverCandidates:coverFallbacks,OnlineDate:year,Tags:tags,TotalEpisode:ikanbotDisplayEpisodeCount(isMovie,len(chapters)),EpisodeCount:ikanbotDisplayEpisodeCount(isMovie,len(chapters)),ChannelName:"愛看機器人",CategoryName:categoryName}
	return drama,chapters,nil
}
func (d *Downloader) resolveIkanbotMedia(ctx context.Context,task Task)(providerMedia,error){
	source,id,ok:=splitProviderDramaID(task.DramaID);prefix:=providerChapterID(sourceIkanBot,id,"");if !ok||source!=sourceIkanBot||!ikanbotNumericID(id)||!strings.HasPrefix(task.Chapter.ID,prefix){return providerMedia{},errors.New("愛看機器人播放分集資訊無效，請重新整理")}
	mediaURL:=strings.TrimSpace(task.Chapter.VideoURL);if !isProviderHTTPMediaURL(mediaURL){return providerMedia{},errors.New("愛看機器人播放網址無效")}
	ref:=firstNonEmpty(task.Chapter.Referer,task.Chapter.PageURL,ikanbotBaseURL+"/");cred:=&providerMediaCredentials{referer:ref,userAgent:ikanbotUserAgent}
	media:=providerMedia{URL:mediaURL,Referer:ref,credentials:cred,StandardHTTP:runtime.GOOS=="android",Quality:ikanbotQuality(task.Chapter.PlayLine)}
	for _,item:=range task.Chapter.PlaybackVariants{qualityText,address,found:=strings.Cut(item,":");if !found||!isProviderHTTPMediaURL(address){continue};quality,_:=strconv.Atoi(qualityText);media.Variants=append(media.Variants,providerMedia{URL:address,Referer:ref,credentials:cred,Quality:quality})}
	if ikanbotIsHLSURL(mediaURL) {
		prepared, err := d.prepareWebProviderMediaWithFallback(ctx, media, "愛看機器人")
		if err != nil {
			return providerMedia{}, err
		}
		return d.ikanbotPreferHighestHLS(ctx, prepared), nil
	}
	return media,nil
}

func ikanbotBuildMovieChapters(sourceID, pageURL string, lines []ikanbotLine) []Chapter {
	type movieSelection struct {
		episode  ikanbotEpisode
		lineFlag string
		label    string
		variants []string
		seenURLs map[string]bool
	}

	// The upstream movie API commonly returns dozens of CDN/source rows.
	// They are transport fallbacks, not dozens of user-selectable episodes.
	// Sort known qualities first, while keeping upstream order for unnamed
	// routes (which is the only useful preference signal when quality is absent).
	orderedLines := append([]ikanbotLine(nil), lines...)
	sort.SliceStable(orderedLines, func(i, j int) bool {
		left, right := ikanbotQuality(orderedLines[i].Flag), ikanbotQuality(orderedLines[j].Flag)
		return left > right
	})

	selections := make([]movieSelection, 0, len(orderedLines))
	selectionIndexes := make(map[string]int)
	for _, line := range orderedLines {
		for _, episode := range line.Episodes {
			if !isProviderHTTPMediaURL(episode.URL) {
				continue
			}
			language := ikanbotMovieLanguageLabel(episode.Name, line.Flag)
			groupKey := "movie:default"
			label := "電影版"
			if language != "" {
				groupKey = "audio:" + language
				label = language
			}

			if index, found := selectionIndexes[groupKey]; found {
				entry := &selections[index]
				if entry.seenURLs[episode.URL] {
					continue
				}
				entry.seenURLs[episode.URL] = true
				entry.variants = append(entry.variants, fmt.Sprintf("%d:%s", ikanbotQuality(line.Flag), episode.URL))
				continue
			}

			selectionIndexes[groupKey] = len(selections)
			selections = append(selections, movieSelection{
				episode: episode,
				lineFlag: line.Flag,
				label: label,
				seenURLs: map[string]bool{episode.URL: true},
			})
		}
	}

	chapters := make([]Chapter, 0, len(selections))
	for index, entry := range selections {
		order := index + 1
		chapters = append(chapters, Chapter{
			ID: providerChapterID(sourceIkanBot, sourceID, strconv.Itoa(order)),
			Source: sourceIkanBot,
			Title: truncate(entry.label, 128),
			VideoURL: entry.episode.URL,
			PlaybackVariants: entry.variants,
			PlayLine: entry.lineFlag,
			PageURL: pageURL,
			Referer: pageURL,
			CurrentEpisode: json.RawMessage(strconv.Itoa(order)),
		})
	}
	return chapters
}