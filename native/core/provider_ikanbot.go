package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	htmlnet "golang.org/x/net/html"
)

const (
	ikanbotBaseURL = "https://www.ikanbot.com"
	ikanbotUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	ikanbotPageSize = 36
	ikanbotMaxLines = 40
)

var ikanbotCategories = []nativeCategory{
	{ID:"movie-熱門",Name:"熱門電影"},
	{ID:"tv-熱門",Name:"熱門影集"},
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
func ikanbotImage(node *htmlnet.Node,pageURL string)string{
	if node==nil{return ""}
	for _,attribute:=range []string{"data-original","data-src","data-lazy-src","data-lazyload","data-image","src"}{
		raw:=strings.TrimSpace(providerHTMLAttr(node,attribute))
		if raw==""||strings.HasPrefix(strings.ToLower(raw),"data:"){continue}
		resolved:=resolveProviderURL(pageURL,raw)
		if isProviderHTTPMediaURL(resolved){return resolved}
	}
	srcset:=strings.TrimSpace(providerHTMLAttr(node,"srcset"))
	if srcset!=""{
		for _,candidate:=range strings.Split(srcset,","){
			fields:=strings.Fields(strings.TrimSpace(candidate))
			if len(fields)==0{continue}
			resolved:=resolveProviderURL(pageURL,fields[0])
			if isProviderHTTPMediaURL(resolved){return resolved}
		}
	}
	return ""
}
func ikanbotFindCard(root *htmlnet.Node)[]*htmlnet.Node{
	return providerHTMLNodes(root,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&providerHTMLClass(n,"item")})
}
func ikanbotCardIDFromHref(raw string)string{
	u,err:=url.Parse(strings.TrimSpace(raw))
	if err!=nil{return ""}
	if u.IsAbs(){
		host:=strings.ToLower(u.Hostname())
		if host!="ikanbot.com"&&host!="www.ikanbot.com"{return ""}
	}
	m:=regexp.MustCompile("^/play/([0-9]{1,12})(?:/|$)").FindStringSubmatch(u.Path)
	if len(m)<2||!ikanbotNumericID(m[1]){return ""}
	return m[1]
}

func ikanbotFirstImage(root *htmlnet.Node)*htmlnet.Node{
	if root==nil{return nil}
	for _,node:=range providerHTMLNodes(root,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="img"}){
		if ikanbotImage(node,"https://www.ikanbot.com/")!=""{return node}
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
		cover:=ikanbotImage(image,pageURL)
		seen[id]=true
		out=append(out,Drama{ID:providerDramaID(sourceIkanBot,id),Source:sourceIkanBot,SourceID:id,Title:truncate(title,512),Name:truncate(title,512),Cover:cover,CoverURL:cover,ChannelName:"愛看機器人",CategoryName:ikanbotCategoryKind(category)})
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
		out=append(out,Drama{ID:providerDramaID(sourceIkanBot,m[1]),Source:sourceIkanBot,SourceID:m[1],Title:truncate(title,512),Name:truncate(title,512),Cover:ikanbotImage(image,pageURL),CoverURL:ikanbotImage(image,pageURL),ChannelName:"愛看機器人",CategoryName:"影集"})
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
func ikanbotEpisodeOrder(name string,fallback int)int{
	m:=regexp.MustCompile("([0-9]{1,4})").FindStringSubmatch(name);if len(m)>1{if n,e:=strconv.Atoi(m[1]);e==nil&&n>0&&n<100000{return n}};return fallback
}
func ikanbotParseLines(payload map[string]any)[]ikanbotLine{
	data,_:=payload["data"].(map[string]any);rows,_:=data["list"].([]any);lines:=[]ikanbotLine{}
	for _,row:=range rows{
		obj,_:=row.(map[string]any);raw,_:=obj["resData"].(string);if raw==""{continue}
		var entries []struct{Flag string "json:\"flag\"";URL string "json:\"url\""}
		if json.Unmarshal([]byte(raw),&entries)!=nil{continue}
		for _,entry:=range entries{
			if strings.TrimSpace(entry.Flag)==""||entry.URL==""{continue}
			line:=ikanbotLine{Flag:strings.TrimSpace(entry.Flag)}
			for index,piece:=range strings.Split(entry.URL,"#"){
				piece=strings.TrimSpace(piece);if piece==""{continue};parts:=strings.SplitN(piece,"$",2);if len(parts)<2{continue}
				name:=strings.TrimSpace(parts[0]);media:=strings.TrimSpace(parts[1]);if extra:=strings.Index(media,"$");extra>=0{media=media[:extra]}
				u,e:=url.Parse(media);if e!=nil||u.User!=nil||!isProviderHTTPMediaURL(media){continue}
				line.Episodes=append(line.Episodes,ikanbotEpisode{Name:name,URL:media,Order:ikanbotEpisodeOrder(name,index+1)})
			}
			if len(line.Episodes)>0{lines=append(lines,line)}
			if len(lines)>=ikanbotMaxLines{return lines}
		}
	}
	sort.SliceStable(lines,func(i,j int)bool{return len(lines[i].Episodes)>len(lines[j].Episodes)})
	return lines
}
func ikanbotPlaybackAPIURL(pageURL, currentID, mtype, signedToken string) (string, error) {
	page, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil || page.Scheme != "https" || page.User != nil {
		return "", errors.New("愛看機器人播放頁網址無效")
	}
	host := strings.ToLower(page.Hostname())
	switch host {
	case "ikanbot.com", "www.ikanbot.com", "v.aikanbot.com", "www1.aikanbot.com":
	default:
		return "", errors.New("愛看機器人播放頁網域不受信任")
	}
	if !ikanbotNumericID(currentID) || strings.TrimSpace(signedToken) == "" {
		return "", errors.New("愛看機器人播放參數無效")
	}
	if mtype == "" {
		mtype = "1"
	}
	query := url.Values{}
	query.Set("videoId", currentID)
	query.Set("mtype", mtype)
	query.Set("token", signedToken)
	return page.Scheme + "://" + page.Host + "/api/getResN?" + query.Encode(), nil
}

func (d *Downloader) fetchIkanbotLines(ctx context.Context, sourceID, currentID, token, mtype, pageURL string) ([]ikanbotLine, error) {
	signed := ikanbotSignToken(currentID, token)
	if signed == "" {
		return nil, errors.New("愛看機器人播放令牌無效，請重新整理詳情")
	}
	address, err := ikanbotPlaybackAPIURL(pageURL, currentID, mtype, signed)
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
	if state, ok := payload["state"].(float64); ok && state != 1 {
		return nil, fmt.Errorf("愛看機器人線路取得失敗：%v", payload["message"])
	}
	lines := ikanbotParseLines(payload)
	if len(lines) == 0 {
		return nil, errors.New("愛看機器人目前沒有可播放線路")
	}
	return lines, nil
}
func (d *Downloader) fetchIkanbotDetail(ctx context.Context,sourceID string)(Drama,[]Chapter,error){
	if !ikanbotNumericID(sourceID){return Drama{},nil,errors.New("愛看機器人作品識別碼無效")}
	pageURL:=ikanbotBaseURL+"/play/"+sourceID
	doc,body,final,err:=fetchIkanbotPage(ctx,d,pageURL,ikanbotBaseURL+"/");if err!=nil{return Drama{},nil,fmt.Errorf("愛看機器人作品詳情讀取失敗：%w",err)}
	title:="";for _,n:=range providerHTMLNodes(doc,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="h1"&&providerHTMLAttr(n,"id")=="video_title"}){title=providerHTMLText(n);break}
	if title==""{title=providerHTMLPlainText(adultMetaValue(body,"og:title"))};if title==""{return Drama{},nil,errors.New("愛看機器人詳情缺少標題")}
	var cover string;for _,n:=range providerHTMLNodes(doc,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="img"}){if providerHTMLClass(n,"cover"){cover=ikanbotImage(n,final);break}};if cover==""{for _,n:=range providerHTMLNodes(doc,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="img"}){cover=ikanbotImage(n,final);if cover!=""{break}}}
	currentID:=ikanbotInput(doc,"current_id");if currentID==""{currentID=sourceID};token:=ikanbotInput(doc,"e_token");mtype:=ikanbotInput(doc,"mtype");if token==""{return Drama{},nil,errors.New("愛看機器人缺少播放驗證資訊")}
	lines,err:=d.fetchIkanbotLines(ctx,sourceID,currentID,token,mtype,final);if err!=nil{return Drama{},nil,err}
	meta:=[]string{};for _,n:=range providerHTMLNodes(doc,func(n *htmlnet.Node)bool{return n.Type==htmlnet.ElementNode&&n.Data=="h3"&&providerHTMLClass(n,"meta")}){v:=providerHTMLText(n);if v!=""{meta=append(meta,v)}}
	tags:=[]string{};year:="";for _,v:=range meta{if year==""&&regexp.MustCompile("^(19|20)[0-9]{2}$").MatchString(v){year=v};if strings.Contains(v,"/"){tags=append(tags,truncate(v,64))}}
	primary:=lines[0];orders:=map[int]bool{};chapters:=[]Chapter{}
	for index,ep:=range primary.Episodes{
		order:=ep.Order;if order<=0{order=index+1};if orders[order]{continue};orders[order]=true
		name:=strings.TrimSpace(ep.Name);if name==""{name=fmt.Sprintf("第 %d 集",order)}
		var alternatives []string;seen:=map[string]bool{ep.URL:true}
		for _,line:=range lines{for _,other:=range line.Episodes{if other.Order==order&&!seen[other.URL]{seen[other.URL]=true;alternatives=append(alternatives,fmt.Sprintf("%d:%s",ikanbotQuality(line.Flag),other.URL))}}}
		chapters=append(chapters,Chapter{ID:providerChapterID(sourceIkanBot,sourceID,strconv.Itoa(order)),Source:sourceIkanBot,Title:truncate(name,128),VideoURL:ep.URL,PlaybackVariants:alternatives,PlayLine:primary.Flag,PageURL:final,Referer:final,CurrentEpisode:json.RawMessage(strconv.Itoa(order))})
	}
	sort.SliceStable(chapters,func(i,j int)bool{return chapters[i].EpisodeString(i+1)<chapters[j].EpisodeString(j+1)})
	if len(chapters)==0{return Drama{},nil,errors.New("愛看機器人目前沒有可播放分集")}
	drama:=Drama{ID:providerDramaID(sourceIkanBot,sourceID),Source:sourceIkanBot,SourceID:sourceID,Title:truncate(providerHTMLPlainText(title),512),Name:truncate(providerHTMLPlainText(title),512),Desc:truncate(strings.Join(meta," · "),12000),Intro:truncate(strings.Join(meta," · "),12000),Cover:cover,CoverURL:cover,OnlineDate:year,Tags:tags,TotalEpisode:len(chapters),EpisodeCount:len(chapters),ChannelName:"愛看機器人",CategoryName:"影集"}
	return drama,chapters,nil
}
func (d *Downloader) resolveIkanbotMedia(ctx context.Context,task Task)(providerMedia,error){
	source,id,ok:=splitProviderDramaID(task.DramaID);prefix:=providerChapterID(sourceIkanBot,id,"");if !ok||source!=sourceIkanBot||!ikanbotNumericID(id)||!strings.HasPrefix(task.Chapter.ID,prefix){return providerMedia{},errors.New("愛看機器人播放分集資訊無效，請重新整理")}
	mediaURL:=strings.TrimSpace(task.Chapter.VideoURL);if !isProviderHTTPMediaURL(mediaURL){return providerMedia{},errors.New("愛看機器人播放網址無效")}
	ref:=firstNonEmpty(task.Chapter.Referer,task.Chapter.PageURL,ikanbotBaseURL+"/");cred:=&providerMediaCredentials{referer:ref,userAgent:ikanbotUserAgent}
	media:=providerMedia{URL:mediaURL,Referer:ref,credentials:cred,StandardHTTP:runtime.GOOS=="android"}
	for _,item:=range task.Chapter.PlaybackVariants{qualityText,address,found:=strings.Cut(item,":");if !found||!isProviderHTTPMediaURL(address){continue};quality,_:=strconv.Atoi(qualityText);media.Variants=append(media.Variants,providerMedia{URL:address,Referer:ref,credentials:cred,Quality:quality})}
	if strings.HasSuffix(strings.ToLower(mediaURL),".m3u8"){return d.prepareWebProviderMedia(ctx,media,"愛看機器人")}
	return media,nil
}
