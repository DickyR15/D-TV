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
	if !validChaoliuCategory("") || !validChaoliuCategory("chinese") || validChaoliuCategory("../admin") { t.Fatal("chaoliu 分類驗證錯誤") }
	path,err:=chaoliuCategoryPath(3,"uncensored");if err!=nil||path!="/zh/uncensored_list/all/3.html"{t.Fatalf("分類路徑=%q err=%v",path,err)}
	search,err:=chaoliuSearchPath(2,"女優/測試");if err!=nil||!strings.Contains(search,"/2.html")||strings.Contains(search,"女優/測試"){t.Fatalf("搜尋路徑=%q err=%v",search,err)}
}
func TestChaoliuListAndSafeIdentity(t *testing.T) {
	body:=`<div class="post"><h3><a href="/zh/chinese_content/19/sample-title.html">測試作品</a></h3><img data-original="/images/test.jpg"></div>`
	items:=parseChaoliuList(body,"https://mjv011.com/zh/chinese_list/all/1.html")
	if len(items)!=1||items[0].Source!=sourceChaoliu||items[0].SourceID!="chinese_content/19/sample-title"||items[0].Title!="測試作品"||items[0].Cover!="https://mjv011.com/images/test.jpg"||items[0].ChannelName!="chaoliu"||items[0].CategoryName!="chaoliu"{t.Fatalf("chaoliu 卡片解析失敗：%+v",items)}
	if !validChaoliuID("chinese_content/19/sample-title")||validChaoliuID("../other/content")||validChaoliuID("https://evil.example/x"){t.Fatal("chaoliu 識別碼驗證錯誤")}
}
func TestChaoliuListAcceptsReorderedAttributesAndArticleCards(t *testing.T) {
	body := `<!doctype html><html><body>
		<article data-layout="new" class="entry post type-post">
			<h2><a href="/zh/uncensored_content/22/layout-updated.html">新版卡片標題</a></h2>
			<img alt="新版卡片" src="/placeholder.jpg" data-src="/images/updated-poster.jpg">
		</article>
	</body></html>`
	items := parseChaoliuList(body, "https://mjv011.com/zh/uncensored_list/all/1.html")
	if len(items) != 1 {
		t.Fatalf("updated chaoliu card markup parsed %d items, want 1", len(items))
	}
	if items[0].SourceID != "uncensored_content/22/layout-updated" ||
		items[0].Title != "新版卡片標題" ||
		items[0].Cover != "https://mjv011.com/images/updated-poster.jpg" {
		t.Fatalf("updated chaoliu card parsed incorrectly: %#v", items[0])
	}
}

func TestChaoliuPosterCandidatesIncludeLazySrcsetAndJSONLDImages(t *testing.T) {
	fragment := `<img src="/loading.gif" data-original="/poster/original.webp"
		data-src="/poster/lazy.jpg"
		data-srcset="/poster/small.jpg 320w, /poster/large.jpg 1200w"
		style="background-image:url('/poster/background.jpg')">`
	got := chaoliuImageCandidates(fragment, "https://mjv011.com/zh/detail.html")
	for _, want := range []string{
		"https://mjv011.com/poster/original.webp",
		"https://mjv011.com/poster/lazy.jpg",
		"https://mjv011.com/poster/small.jpg",
		"https://mjv011.com/poster/large.jpg",
		"https://mjv011.com/poster/background.jpg",
	} {
		if !containsString(got, want) {
			t.Fatalf("chaoliu poster candidate %q missing from %#v", want, got)
		}
	}

	body := `<script type="application/ld+json">{
		"@type":"VideoObject","name":"JSON-LD 作品",
		"image":["/poster/main.webp","/poster/backup.jpg"],
		"thumbnailUrl":"/poster/thumb.jpg"
	}</script><meta content="/poster/og.jpg" property="og:image">
		<img data-src="/poster/html.jpg">`
	covers := chaoliuDetailCoverCandidates(body, "https://mjv011.com/zh/detail.html")
	want := []string{
		"https://mjv011.com/poster/main.webp",
		"https://mjv011.com/poster/backup.jpg",
		"https://mjv011.com/poster/thumb.jpg",
		"https://mjv011.com/poster/og.jpg",
		"https://mjv011.com/poster/html.jpg",
	}
	for _, address := range want {
		if !containsString(covers, address) {
			t.Fatalf("chaoliu detail poster %q missing from %#v", address, covers)
		}
	}
}

func TestChaoliuQualityParserKeepsUnknownAndRejectsKnownLowRes(t *testing.T) {
	const token = "AbcdefghijklmnopqrstUVWX"
	body := "mvarr['q360p'] = ['" + token + "'];" +
		"mvarr['q720p'] = ['" + token + "'];" +
		"mvarr['q1080p'] = ['" + token + "'];" +
		"mvarr['opaque'] = ['" + token + "'];" +
		"mvarr['auto'] = {numresolution=480, token:'" + token + "'};"

	qualities := parseChaoliuQualities(body)
	got := map[string]int{}
	for _, quality := range qualities {
		got[quality.Key] = quality.Resolution
	}
	want := map[string]int{
		"q360p": 360, "q720p": 720, "q1080p": 1080,
		"opaque": 0, "auto": 480,
	}
	if len(got) != len(want) {
		t.Fatalf("parsed chaoliu quality labels = %#v, want %#v", got, want)
	}
	for key, resolution := range want {
		if got[key] != resolution {
			t.Fatalf("chaoliu quality %q = %d, want %d (%#v)", key, got[key], resolution, got)
		}
	}
	if chaoliuQualityAllowed(got["q360p"]) || chaoliuQualityAllowed(got["auto"]) {
		t.Fatal("known 360P/480P routes must be rejected")
	}
	if !chaoliuQualityAllowed(got["q720p"]) || !chaoliuQualityAllowed(got["q1080p"]) ||
		!chaoliuQualityAllowed(got["opaque"]) {
		t.Fatal("720P+, and genuinely unknown quality labels, should remain eligible")
	}
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
	if err == nil || strings.Contains(err.Error(), "unsupported provider source") || !strings.Contains(err.Error(), "chaoliu 作品識別碼無效") {
		t.Fatalf("chaoliu 健康檢查未進入專屬分集解析：%v", err)
	}
}

func TestChaoliuValidatorAcceptsValidDetailHTMLWithoutLegacyMarkers(t *testing.T) {
	candidate := "https://mjv011.com/zh/chinese_content/19/new-layout-title.html"
	body := []byte(`<!doctype html><html><head><title>Example detail</title><meta property="og:title" content="Example"></head><body><h1>Example</h1><video></video></body></html>`)
	if err := chaoliuValidator(candidate, 200, body); err != nil {
		t.Fatalf("valid detail HTML without legacy markers was rejected: %v", err)
	}
}

func TestChaoliuValidatorRejectsChallengeHTMLOnDetailRoute(t *testing.T) {
	candidate := "https://mjv011.com/zh/chinese_content/19/sample.html"
	body := []byte(`<!doctype html><html><head><title>Just a moment...</title></head><body><div>Checking your browser</div></body></html>`)
	if err := chaoliuValidator(candidate, 200, body); err == nil {
		t.Fatal("Cloudflare challenge HTML must not pass as a valid detail page")
	}
}

func TestChaoliuValidatorDoesNotAcceptGenericHTMLFromUntrustedHost(t *testing.T) {
	body := []byte(`<!doctype html><html><head><title>Example</title></head><body><h1>Example</h1></body></html>`)
	if err := chaoliuValidator("https://evil.example/zh/chinese_content/19/sample.html", 200, body); err == nil {
		t.Fatal("generic HTML from an untrusted host was accepted")
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
