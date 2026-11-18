package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
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
	sourceMadou: {{ID:"0",Name:"全部"},{ID:"2",Name:"麻豆傳媒"},{ID:"51",Name:"麻豆番外篇"},{ID:"168",Name:"麻豆花絮"},{ID:"436",Name:"HongKongDoll"},{ID:"342",Name:"PsychopornTW"},{ID:"354",Name:"91製片廠"},{ID:"85",Name:"果凍傳媒"},{ID:"86",Name:"蜜桃影像"},{ID:"254",Name:"天美傳媒"},{ID:"87",Name:"皇家華人"},{ID:"99",Name:"兔子先生"},{ID:"283",Name:"星空無限傳媒"},{ID:"359",Name:"愛豆"},{ID:"155",Name:"麻豆導演系列"}},
	source91Porna: {{ID:"new_update",Name:"最近更新"},{ID:"now_hot",Name:"目前最熱門"},{ID:"now_month_hot",Name:"本月最熱門"},{ID:"original",Name:"91 原創"},{ID:"hd",Name:"高畫質"},{ID:"ten_minutes",Name:"10 分鐘以上"},{ID:"twenty_minutes",Name:"20 分鐘以上"},{ID:"now_month_collect",Name:"本月收藏"},{ID:"now_month_comment",Name:"本月討論"},{ID:"max_collect",Name:"收藏最多"}},
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
	case sourcePPP: return "PPP"
	case sourceMadou: return "麻豆傳媒"
	case source91Porna: return "91Porn"
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
		if category=="" {category="0"};return fmt.Sprintf("/page/%d/?cat=%s",page,url.QueryEscape(category)),nil
	case source91Porna:
		if category==""||category=="new"{category="new_update"};return "/comic/index/video?category="+url.QueryEscape(category)+"&page="+strconv.Itoa(page),nil
	}
	return "",errors.New("不支援的進階片源")
}
func extraAdultSearchPath(source string,page int,query string)(string,bool,error){
	query=strings.TrimSpace(query);if query==""||len([]rune(query))>256||page<1||page>100000{return "",false,errors.New("搜尋條件無效")};q:=url.QueryEscape(query)
	switch canonicalProviderSource(source){
	case sourceSupJav:return "/search?keyword="+q,false,nil
	case sourcePPP:p:="/search/?q="+q;if page>1{p+="&page="+strconv.Itoa(page)};return p,true,nil
	case sourceMadou:if page==1{return "/?s="+q,false,nil};return fmt.Sprintf("/page/%d/?s=%s",page,q),false,nil
	case source91Porna:return "/comic/index/search?keyword="+q,false,nil
	};return "",false,errors.New("不支援的進階片源搜尋")
}
func extraAdultNormalizeID(source,raw string)string{
	source=canonicalProviderSource(source);raw=strings.TrimSpace(html.UnescapeString(raw))
	if u,e:=url.Parse(raw);e==nil&&u.IsAbs(){raw=u.EscapedPath();if source==source91Porna&&u.RawQuery!=""{raw+="?"+u.RawQuery}}
	raw=strings.TrimLeft(raw,"/")
	switch source{
	case sourceSupJav:if strings.HasPrefix(raw,"video.php?slug="){return raw};return strings.TrimRight(raw,"/")
	case sourcePPP:return strings.Trim(strings.TrimPrefix(raw,"v/"),"/")
	case sourceMadou:return strings.TrimRight(raw,"/")
	case source91Porna:if u,e:=url.Parse(raw);e==nil&&strings.Contains(raw,"video_key="){return u.Query().Get("video_key")};return raw
	};return ""
}
func extraAdultValidID(source,id string)bool{
	switch canonicalProviderSource(source){
	case sourceSupJav:return regexp.MustCompile("^(video/[A-Za-z0-9._-]{1,180}|video\\.php\\?slug=[A-Za-z0-9._-]{1,180})$").MatchString(id)
	case sourcePPP:return regexp.MustCompile("^[a-z0-9]{1,100}$").MatchString(id)
	case sourceMadou:return regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9._-]{0,180}(\\.html)?$").MatchString(id)
	case source91Porna:return regexp.MustCompile("^[A-Za-z0-9_-]{1,128}$").MatchString(id)
	};return false
}
func extraAdultDetailURL(source,id string)string{
	if !extraAdultValidID(source,id){return ""};base:=extraAdultBaseURL(source)
	switch canonicalProviderSource(source){
	case sourceSupJav:if strings.HasPrefix(id,"video.php?")||strings.HasPrefix(id,"video/"){return base+"/"+id};return base+"/video/"+id
	case sourcePPP:return base+"/v/"+url.PathEscape(id)+"/"
	case sourceMadou:return base+"/"+url.PathEscape(id)
	case source91Porna:return base+"/comic/index/avdetail?video_key="+url.QueryEscape(id)
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
func extraAdultParseCard(source,chunk,base string)(Drama,bool){
	var id,title,cover,remark string
	switch source{
	case sourceSupJav:
		m:=regexp.MustCompile("(?is)href=['\\x22]([^'\\x22]*(?:video/|video\\.php\\?slug=)[^'\\x22]*)['\\x22]").FindStringSubmatch(chunk);if len(m)<2{return Drama{},false};id=extraAdultNormalizeID(source,m[1]);img:=regexp.MustCompile("(?is)<img\\b[^>]*>").FindString(chunk);if img!=""{cover=adultAttribute(img,"src");if cover==""{cover=adultAttribute(img,"data-src")};title=adultAttribute(img,"alt")};if m:=regexp.MustCompile("(?is)<div\\b[^>]*class=['\\x22]detail['\\x22][^>]*>(.*?)</div>").FindStringSubmatch(chunk);len(m)>1{title=adultClean(m[1])}
	case sourcePPP:
		m:=regexp.MustCompile("(?i)/v/([a-z0-9]{1,100})/").FindStringSubmatch(chunk);if len(m)<2{return Drama{},false};id=m[1];img:=regexp.MustCompile("(?is)<img\\b[^>]*>").FindString(chunk);if img!=""{cover=adultAttribute(img,"data-src");if cover==""{cover=adultAttribute(img,"src")};title=adultAttribute(img,"alt")};if m:=regexp.MustCompile("(?is)card-video__duration[^>]*>([^<]+)<").FindStringSubmatch(chunk);len(m)>1{remark=adultClean(m[1])}
	case sourceMadou:
		m:=regexp.MustCompile("(?is)<a\\b[^>]*class=['\\x22][^'\\x22]*thumbnail[^'\\x22]*['\\x22][^>]*href=['\\x22]([^'\\x22]+)['\\x22]").FindStringSubmatch(chunk);if len(m)<2{m=regexp.MustCompile("(?is)<h2\\b[^>]*>\\s*<a\\b[^>]*href=['\\x22]([^'\\x22]+)['\\x22]").FindStringSubmatch(chunk)};if len(m)<2{return Drama{},false};id=extraAdultNormalizeID(source,m[1]);img:=regexp.MustCompile("(?is)<img\\b[^>]*>").FindString(chunk);if img!=""{cover=adultAttribute(img,"data-src");if cover==""{cover=adultAttribute(img,"src")}};if m:=regexp.MustCompile("(?is)<h2\\b[^>]*>(.*?)</h2>").FindStringSubmatch(chunk);len(m)>1{title=adultClean(m[1])}
	case source91Porna:
		m:=regexp.MustCompile("(?i)href=['\\x22]/comic/index/(?:avdetail|detail)\\?video_key=([A-Za-z0-9_-]+)['\\x22]").FindStringSubmatch(chunk);if len(m)<2{return Drama{},false};id=m[1];img:=regexp.MustCompile("(?is)<img\\b[^>]*>").FindString(chunk);if img!=""{cover=adultAttribute(img,"data-src");if cover==""{cover=adultAttribute(img,"src")};title=adultAttribute(img,"alt")};if m:=regexp.MustCompile("(?i)([0-9]{1,3}:[0-9]{2})").FindStringSubmatch(chunk);len(m)>1{remark=m[1]}
	}
	if !extraAdultValidID(source,id){return Drama{},false};title=adultClean(title);if title==""{title=id};cover=adultSafeImageURL(base,cover)
	return Drama{ID:providerDramaID(source,id),Source:source,SourceID:id,Title:truncate(title,512),Name:truncate(title,512),Cover:cover,CoverURL:cover,ChannelName:extraAdultSourceName(source),CategoryName:extraAdultSourceName(source),Remark:truncate(remark,64),TotalEpisode:1,EpisodeCount:1},true
}
func (d *Downloader) fetchExtraAdultCatalog(ctx context.Context,source string,page int,category,query string)([]Drama,bool,error){
	source=canonicalProviderSource(source);if page<1||page>100000||!extraAdultValidCategory(source,category){return nil,false,errors.New("進階片源目錄條件無效")};base:=extraAdultBaseURL(source);var address string;var paged bool;var e error
	if strings.TrimSpace(query)!=""{p,h,err:=extraAdultSearchPath(source,page,query);if err!=nil{return nil,false,err};address=base+p;paged=h}else{p,err:=extraAdultCategoryPath(source,page,category);if err!=nil{return nil,false,err};address=base+p}
	body,_,e:=extraAdultFetch(ctx,d,source,address,base+"/");if e!=nil{return nil,false,fmt.Errorf("%s 目錄讀取失敗：%w",extraAdultSourceName(source),e)}
	items:=[]Drama{};seen:=map[string]bool{};for _,block:=range extraAdultSegments(body,source){item,ok:=extraAdultParseCard(source,block,base);if !ok||seen[item.ID]{continue};seen[item.ID]=true;items=append(items,item);if len(items)>=100{break}}
	limit:=20;if source==sourcePPP||source==sourceMadou{limit=12};if source==source91Porna{limit=10};return items,paged&&len(items)>=limit,nil
}
func extraAdultTitle(body,fallback string)string{
	t:=adultClean(adultMetaValue(body,"og:title"));if t==""{t=adultClean(adultMetaValue(body,"twitter:title"))};if t==""{if m:=regexp.MustCompile("(?is)<h1\\b[^>]*>(.*?)</h1>").FindStringSubmatch(body);len(m)>1{t=adultClean(m[1])}};if t==""{if m:=regexp.MustCompile("(?is)<title\\b[^>]*>(.*?)</title>").FindStringSubmatch(body);len(m)>1{t=adultClean(m[1])}};if t==""{t=fallback};t=regexp.MustCompile("(?i)\\s*[-|]\\s*(?:SupJav|PPP\\.Porn|麻豆.*|91porna|91視頻).*$").ReplaceAllString(t,"");return strings.TrimSpace(t)
}
func (d *Downloader) fetchExtraAdultDetail(ctx context.Context,source,id string)(Drama,[]Chapter,error){
	source=canonicalProviderSource(source);if !extraAdultValidID(source,id){return Drama{},nil,errors.New("作品識別碼無效")};base:=extraAdultBaseURL(source);body,final,e:=extraAdultFetch(ctx,d,source,extraAdultDetailURL(source,id),base+"/");if e!=nil{return Drama{},nil,fmt.Errorf("%s 作品詳情讀取失敗：%w",extraAdultSourceName(source),e)}
	title:=extraAdultTitle(body,id);desc:=adultClean(adultMetaValue(body,"description"));cover:="";for _,k:=range []string{"og:image","twitter:image"}{if v:=adultSafeImageURL(base,adultMetaValue(body,k));v!=""{cover=v;break}};if cover==""{images:=adultImageCandidates(body,base,source);if len(images)>0{cover=images[0]}}
	drama:=Drama{ID:providerDramaID(source,id),Source:source,SourceID:id,Title:truncate(title,512),Name:truncate(title,512),Desc:truncate(desc,12000),Intro:truncate(desc,12000),Cover:cover,CoverURL:cover,ChannelName:extraAdultSourceName(source),CategoryName:extraAdultSourceName(source),TotalEpisode:1,EpisodeCount:1}
	ch:=Chapter{ID:providerChapterID(source,id,final),Source:source,Title:"正片",PageURL:final,Referer:final,VideoURL:"adult-page://"+final,CurrentEpisode:json.RawMessage("1")};return drama,[]Chapter{ch},nil
}
func extraAdultFrame(page,body string)string{m:=regexp.MustCompile("(?is)<iframe\\b[^>]*src\\s*=\\s*['\\x22]([^'\\x22]+)['\\x22]").FindStringSubmatch(body);if len(m)<2{return ""};return adultResolvePlaybackURL(page,m[1])}
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
		m:=regexp.MustCompile("(?i)https://[^'\\x22<>\\s\\]+/stream/[^'\\x22<>\\s\\]+\\.m3u8(?:\\?[^'\\x22<>\\s\\]*)?").FindString(body);m=strings.TrimRight(m,".,;:)]}");if !isProviderHTTPMediaURL(m){return providerMedia{},errors.New("PPP 頁面沒有找到 HLS 播放網址")};cred:=&providerMediaCredentials{referer:final,userAgent:adultUserAgent};return d.prepareWebProviderMedia(ctx,providerMedia{URL:m,Referer:final,credentials:cred,StandardHTTP:runtime.GOOS=="android"},"PPP")
	case sourceMadou:
		frame:=extraAdultFrame(final,body);u,err:=url.Parse(frame);if err!=nil||u.Scheme!="https"||!strings.EqualFold(u.Hostname(),"dash.madou.club")||u.User!=nil{return providerMedia{},errors.New("麻豆播放器分享網址無效")};share,surl,err:=extraAdultFetch(ctx,d,source,frame,final);if err!=nil{return providerMedia{},err};token:="";if m:=regexp.MustCompile("(?i)\\bvar\\s+token\\s*=\\s*['\\x22]([^'\\x22]*)['\\x22]").FindStringSubmatch(share);len(m)>1{token=m[1]};media:="";if m:=regexp.MustCompile("(?i)\\bvar\\s+m3u8\\s*=\\s*['\\x22]([^'\\x22]+)['\\x22]").FindStringSubmatch(share);len(m)>1{media=adultResolvePlaybackURL(surl,m[1])};if media==""{if m:=regexp.MustCompile("(?i)['\\x22](/videos/[^'\\x22]+\\.m3u8[^'\\x22]*)['\\x22]").FindStringSubmatch(share);len(m)>1{media=adultResolvePlaybackURL(madouDashBaseURL+"/",m[1])}};if media==""{return providerMedia{},errors.New("麻豆分享頁沒有提供 HLS 播放網址")};if token!=""{u,_:=url.Parse(media);q:=u.Query();if q.Get("token")==""{q.Set("token",token);u.RawQuery=q.Encode();media=u.String()}};cred:=&providerMediaCredentials{referer:surl,userAgent:adultUserAgent};return d.prepareWebProviderMedia(ctx,providerMedia{URL:media,Referer:surl,credentials:cred,StandardHTTP:runtime.GOOS=="android"},"麻豆傳媒")
	case sourceSupJav:
		media:=extraAdultHLS(body,final);referer:=final;if media==""{frame:=extraAdultFrame(final,body);u,err:=url.Parse(frame);if err!=nil||u.Scheme!="https"||u.User!=nil{return providerMedia{},errors.New("SupJav 播放器網址無效")};embed,eu,err:=extraAdultFetch(ctx,d,source,frame,final);if err!=nil{return providerMedia{},err};referer=eu;media=extraAdultHLS(embed,eu);if media==""{if m:=regexp.MustCompile("(?is)window\\.PLAYER_CONFIG\\s*=\\s*(\\{[\\s\\S]*?\\})\\s*;").FindStringSubmatch(embed);len(m)>1{var cfg map[string]any;if json.Unmarshal([]byte(m[1]),&cfg)==nil{for _,k:=range []string{"m3u8","url","hls","file"}{if s,ok:=cfg[k].(string);ok{media=adultResolvePlaybackURL(eu,s);if media!=""{break}}}}}}};if media==""{return providerMedia{},errors.New("SupJav 播放器沒有提供 HLS 網址")};cred:=&providerMediaCredentials{referer:referer,userAgent:adultUserAgent};return d.prepareWebProviderMedia(ctx,providerMedia{URL:media,Referer:referer,credentials:cred,StandardHTTP:runtime.GOOS=="android"},"SupJav")
	case source91Porna:
		m:=regexp.MustCompile("(?i)data-video=['\\x22]([0-9]+)['\\x22]").FindStringSubmatch(body);if len(m)<2{return providerMedia{},errors.New("91Porn 詳情頁缺少播放識別碼")};api:=ninetyOneBaseURL+"/api/videos/"+m[1]+"/playback";req,err:=http.NewRequestWithContext(ctx,http.MethodPost,api,strings.NewReader("{}"));if err!=nil{return providerMedia{},err};req.Header.Set("User-Agent",adultUserAgent);req.Header.Set("Accept","application/json, text/plain, */*");req.Header.Set("Content-Type","application/json");req.Header.Set("Referer",final);req.Header.Set("Origin",ninetyOneBaseURL);resp,err:=d.doCatalogRequestWithTimeout(req,providerTimeout);if err!=nil{return providerMedia{},err};defer resp.Body.Close();data,err:=io.ReadAll(io.LimitReader(resp.Body,4<<20));if err!=nil{return providerMedia{},err};if resp.StatusCode<200||resp.StatusCode>=300{return providerMedia{},fmt.Errorf("91Porn 播放 API 回應 HTTP %d",resp.StatusCode)};var payload struct{Data struct{Streams map[string]string "json:\"streams\""} "json:\"data\""};if json.Unmarshal(data,&payload)!=nil{return providerMedia{},errors.New("91Porn 播放 API 回應格式無效")};media:=firstNonEmpty(payload.Data.Streams["main"],payload.Data.Streams["hevc"],payload.Data.Streams["preview"]);if !isProviderHTTPMediaURL(media){return providerMedia{},errors.New("91Porn 播放 API 沒有回傳有效網址")};cred:=&providerMediaCredentials{referer:final,userAgent:adultUserAgent};return d.prepareWebProviderMedia(ctx,providerMedia{URL:media,Referer:final,credentials:cred,StandardHTTP:runtime.GOOS=="android"},"91Porn")
	}
	return providerMedia{},errors.New("不支援的進階片源")
}
