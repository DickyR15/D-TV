package core

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestChaoliuCategoriesAndPaths(t *testing.T) {
	if !validChaoliuCategory("") || !validChaoliuCategory("chinese") || validChaoliuCategory("../admin") { t.Fatal("潮流 AV 分類驗證錯誤") }
	path,err:=chaoliuCategoryPath(3,"uncensored");if err!=nil||path!="/zh/uncensored_list/all/3.html"{t.Fatalf("分類路徑=%q err=%v",path,err)}
	search,err:=chaoliuSearchPath(2,"女優/測試");if err!=nil||!strings.Contains(search,"/2.html")||strings.Contains(search,"女優/測試"){t.Fatalf("搜尋路徑=%q err=%v",search,err)}
}
func TestChaoliuListAndSafeIdentity(t *testing.T) {
	body:=`<div class="post"><h3><a href="/zh/chinese_content/19/sample-title.html">測試作品</a></h3><img data-original="/images/test.jpg"></div>`
	items:=parseChaoliuList(body,"https://mjv011.com/zh/chinese_list/all/1.html")
	if len(items)!=1||items[0].Source!=sourceChaoliu||items[0].SourceID!="chinese_content/19/sample-title"||items[0].Title!="測試作品"||items[0].Cover!="https://mjv011.com/images/test.jpg"{t.Fatalf("潮流 AV 卡片解析失敗：%+v",items)}
	if !validChaoliuID("chinese_content/19/sample-title")||validChaoliuID("../other/content")||validChaoliuID("https://evil.example/x"){t.Fatal("潮流 AV 識別碼驗證錯誤")}
}
func TestChaoliuHostRoleSeparation(t *testing.T) {
	for _,host:=range []string{"mjv011.com","www.mjv001.com","cdn.mjv015.com"}{if !isChaoliuSiteHost(host){t.Fatalf("主站網域未辨識：%s",host)}}
	for _,host:=range []string{"mavrtracktor.com","imgstream1.com","video.streamfastpro.com"}{if !isChaoliuHost(host)||isChaoliuSiteHost(host){t.Fatalf("播放器/CDN 網域角色錯誤：%s",host)}}
	if isChaoliuHost("evil.example"){t.Fatal("不應接受非模組白名單網域")}
}
func TestChaoliuSiteCandidatesStayWithinOriginalModuleHosts(t *testing.T) {
	d:=&Downloader{cfg:defaultConfig()};candidates:=d.providerURLCandidates("https://mjv011.com/zh/chinese_list/all/1.html")
	if len(candidates)!=len(chaoliuSites){t.Fatalf("候選站數=%d，預期=%d：%v",len(candidates),len(chaoliuSites),candidates)}
	for _,candidate:=range candidates{u,e:=url.Parse(candidate);if e!=nil||!isChaoliuSiteHost(u.Hostname())||u.Path!="/zh/chinese_list/all/1.html"{t.Fatalf("候選網址不符合主站白名單：%s",candidate)}}
}
func TestProviderEndpointRankingPrefersKnownFastHealthyHost(t *testing.T) {
	d:=&Downloader{cfg:defaultConfig()};slow:="https://slow.example/api";fast:="https://fast.example/api"
	d.recordProviderEndpoint(sourceDuoduo,"api",slow,900*time.Millisecond,nil);d.recordProviderEndpoint(sourceDuoduo,"api",fast,80*time.Millisecond,nil)
	got:=d.orderProviderEndpoints(sourceDuoduo,"api",[]string{slow,fast});if len(got)!=2||got[0]!=fast{t.Fatalf("未優先選擇較快且健康的 API：%v",got)}
	d.recordProviderEndpoint(sourceDuoduo,"api",fast,100*time.Millisecond,errors.New("timeout"));d.recordProviderEndpoint(sourceDuoduo,"api",fast,100*time.Millisecond,errors.New("timeout"))
	got=d.orderProviderEndpoints(sourceDuoduo,"api",[]string{fast,slow});if got[0]!=slow{t.Fatalf("冷卻中的端點不應優先：%v",got)}
}

func TestGetHuangguoChaptersDispatchesGuipianAndChaoliu(t *testing.T) {
	d := &Downloader{cfg: defaultConfig()}
	_, _, err := d.GetHuangguoChapters(context.Background(), sourceGuipian, "invalid-id")
	if err == nil || strings.Contains(err.Error(), "unsupported provider source") || !strings.Contains(err.Error(), "鬼片网视频 ID 无效") {
		t.Fatalf("鬼片健康檢查未進入專屬分集解析：%v", err)
	}
	_, _, err = d.GetHuangguoChapters(context.Background(), sourceChaoliu, "invalid-id")
	if err == nil || strings.Contains(err.Error(), "unsupported provider source") || !strings.Contains(err.Error(), "潮流 AV 作品識別碼無效") {
		t.Fatalf("潮流 AV 健康檢查未進入專屬分集解析：%v", err)
	}
}

func TestChaoliuValidatorAcceptsPlayerEndpointMediaPayload(t *testing.T) {
	candidate := "https://mjv011.com/js/player/play.php?numresolution=1080&id=sample"
	body := []byte(`{"url":"https:\/\/mavrtracktor.com\/playlist\/index.m3u8?token=x"}`)
	if err := chaoliuValidator(candidate, 200, body); err != nil {
		t.Fatalf("valid play.php media response was rejected: %v", err)
	}
	if got := parseChaoliuSources(string(body), candidate); len(got) != 1 ||
		!strings.Contains(got[0].URL, "mavrtracktor.com/playlist/index.m3u8") {
		t.Fatalf("play.php media URLs were not parsed: %#v", got)
	}
	if err := chaoliuValidator("https://mjv011.com/zh/chinese_content/19/sample.html", 200, body); err == nil {
		t.Fatal("a player JSON payload must not pass as a catalogue/detail HTML page")
	}
}
