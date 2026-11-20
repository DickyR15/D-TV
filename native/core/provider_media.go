package core

import (
	"errors"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type providerMedia struct {
	credentials *providerMediaCredentials
	URL         string
	Referer     string
	StandardHTTP bool
	Duration    time.Duration
	Playlist    string
	HLSKey      []byte
	CENCKey     []byte
	Quality     int
	Variants    []providerMedia
}

func (d *Downloader) providerBaseURL(source string) string {
	source = canonicalProviderSource(source)
	var configured, fallback string
	switch source {
	case sourceHuangguoAI:
		configured, fallback = d.cfg.HuangguoAIURL, huangguoAIBaseURL
	case sourceHuangguoVideo:
		configured, fallback = d.cfg.HuangguoVideoURL, huangguoVideoBaseURL
	case sourceHuangdou:
		configured, fallback = d.cfg.HuangdouURL, huangdouBaseURL
	case sourceHongguo:
		configured, fallback = d.cfg.HongguoURL, hongguoBaseURL
	case sourceHuangju:
		configured, fallback = d.cfg.HuangjuURL, huangjuBaseURL
	case sourceYeguo:
		configured, fallback = d.cfg.YeguoURL, yeguoBaseURL
	case sourceDSD:
		configured, fallback = d.cfg.DSDURL, dsdBaseURL
	case sourceGuipian:
		configured, fallback = d.cfg.GuipianURL, guipianSiteBaseURL
	case sourceMissAV:
		fallback = missAVBaseURL
	case sourceIkanBot:
		fallback = ikanbotBaseURL
	case sourceSupJav, sourcePPP, sourceMadou, source91Porna:
		fallback = extraAdultBaseURL(source)
	case sourceChaoliu:
		d.providerMu.Lock()
		configured = strings.TrimRight(d.providerHosts[sourceChaoliu], "/")
		d.providerMu.Unlock()
		fallback = chaoliuSites[0]
	case sourceDuoduo:
		d.providerMu.Lock()
		configured = strings.TrimRight(d.providerHosts[sourceDuoduo], "/")
		d.providerMu.Unlock()
		fallback = "https://323433ssdfd.top"
	case sourceHanxiaoquan:
		configured, fallback = d.cfg.HanxiaoquanURL, hanxiaoquanSiteBaseURL
	case sourceHeitao:
		configured, fallback = d.cfg.HeitaoURL, heitaoBaseURL
	default:
		fallback = "https://d2pypzndaqisk.cloudfront.net"
	}
	return strings.TrimRight(firstNonEmpty(configured, fallback), "/")
}

func providerSourceForURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "huangguoai.com" || isHuangguoAIMirrorHost(host):
		return sourceHuangguoAI
	case host == "huangguo.video":
		return sourceHuangguoVideo
	case isHuangdouHost(host):
		return sourceHuangdou
	case host == "hongguoduanju.com" || host == "www.hongguoduanju.com":
		return sourceHongguo
	case host == "huangju.net" || host == "www.huangju.net" || host == "api.huangju.net":
		return sourceHuangju
	case host == "ygdj7.com" || host == "www.ygdj7.com" || host == "ygdj15.com" || host == "www.ygdj15.com" ||
		host == "analyze.buxefaex.cc" || strings.HasSuffix(host, ".buxefaex.cc") ||
		strings.HasSuffix(host, ".fzchosdi.cc") || strings.HasSuffix(host, ".lwblvxvy.cc") || strings.HasSuffix(host, ".vgbyziuz.cc") ||
		host == "delta.ygrwdsgt.cc" || host == "yeguodj.com" || host == "www.yeguodj.com":
		return sourceYeguo
	case host == "dsd.com.se" || host == "www.dsd.com.se":
		return sourceDSD
	case host == "jennyhow.com" || host == "www.jennyhow.com":
		return sourceHanxiaoquan
	case host == "heitaodj.com" || host == "www.heitaodj.com":
		return sourceHeitao
	case host == "guipianwu.com" || host == "www.guipianwu.com":
		return sourceGuipian
	case host == "www1.ikanbot.com" || host == "www.ikanbot.com" || host == "ikanbot.com" || strings.HasSuffix(host, ".ikanbot.com") || host == "admin-api.aikanbot.com":
		return sourceIkanBot
	case host == "supjav.blog" || strings.HasSuffix(host, ".supjav.blog"):
		return sourceSupJav
	case host == "ppp.porn" || strings.HasSuffix(host, ".ppp.porn") || host == "cdnlab.live" || strings.HasSuffix(host, ".cdnlab.live"):
		return sourcePPP
	case host == "madou.club" || strings.HasSuffix(host, ".madou.club"):
		return sourceMadou
	case host == "91porna.com" || strings.HasSuffix(host, ".91porna.com"):
		return source91Porna
	case isChaoliuHost(host):
		return sourceChaoliu
	case host == "missav.ai" || host == "www.missav.ai" ||
		host == "missav.ws" || host == "www.missav.ws" ||
		host == "missav.live" || host == "www.missav.live" ||
		host == "missav123.com" || host == "www.missav123.com":
		return sourceMissAV
	default:
		return ""
	}
}

func isHuangguoAIMirrorHost(host string) bool {
	for _, domain := range huangguoAIMirrorDomains {
		if strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (d *Downloader) providerURLCandidates(raw string) []string {
	source := providerSourceForURL(raw)
	parsed, parseErr := url.Parse(raw)
	if parseErr == nil && source == sourceChaoliu {
		if !isChaoliuSiteHost(strings.ToLower(parsed.Hostname())) { return []string{raw} }
		candidates := make([]string, 0, len(chaoliuSites)+1)
		d.providerMu.Lock()
		preferred := d.providerHosts[sourceChaoliu]
		d.providerMu.Unlock()
		for _, root := range append([]string{preferred}, chaoliuSites...) {
			if root == "" { continue }
			candidate := rehostProviderURL(parsed, root)
			if candidate != "" && !containsString(candidates, candidate) { candidates = append(candidates, candidate) }
		}
		return d.orderProviderEndpoints(sourceChaoliu, "site", candidates)
	}
	if parseErr == nil && source == sourceHuangguoAI {
		candidates := make([]string, 0, len(huangguoAIBaseMirrors)+1)
		d.providerMu.Lock()
		preferred := d.providerHosts[sourceHuangguoAI]
		d.providerMu.Unlock()
		for _, root := range append([]string{preferred}, huangguoAIBaseMirrors...) {
			if root == "" { continue }
			candidate := rehostProviderURL(parsed, root)
			if candidate != "" && !containsString(candidates, candidate) { candidates = append(candidates, candidate) }
		}
		return d.orderProviderEndpoints(sourceHuangguoAI, "site", candidates)
	}
	if source == "" || source == sourceHuangju || source == sourceYeguo ||
		source == sourceIkanBot || source == sourceSupJav || source == sourcePPP ||
		source == sourceMadou || source == source91Porna {
		return []string{raw}
	}
	d.providerMu.Lock()
	preferred := d.providerHosts[source]
	d.providerMu.Unlock()
	parsed, _ = url.Parse(raw)
	var candidates []string
	seen := map[string]bool{}
	add := func(candidate string) {
		if candidate != "" && !seen[candidate] {
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}
	}
	add(rehostProviderURL(parsed, preferred))
	configured := d.providerBaseURL(source)
	add(rehostProviderURL(parsed, configured))
	// Candidate generation is intentionally non-recursive: this function is
	// already resolving a source URL, and re-entering it for the same URL would
	// recurse forever whenever the configured host is recognized as that source.
	return candidates
}

func (d *Downloader) resolveProviderMedia(ctx context.Context, task Task) (providerMedia, error) {
	chapter := task.Chapter
	chapter.Source = canonicalProviderSource(chapter.Source)
	if chapter.Source == "" {
		chapter.Source = sourceFromDramaID(task.DramaID)
	}
	if chapter.Source == sourceCloudFront {
		return d.resolveLegacyMedia(ctx, task)
	}
	if chapter.Source == sourceHuangju {
		return d.resolveHuangjuMedia(ctx, task)
	}
	if chapter.Source == sourceYeguo {
		return d.resolveYeguoMedia(ctx, task)
	}
	if chapter.Source == sourceDSD {
		return d.resolveDSDMedia(ctx, task)
	}
	if chapter.Source == sourceDuoduo {
		return d.resolveDuoduoMedia(ctx, task)
	}
	if chapter.Source == sourceHanxiaoquan {
		return d.resolveHanxiaoquanMedia(ctx, task)
	}
	if chapter.Source == sourceHeitao {
		return d.resolveHeitaoMedia(ctx, task)
	}
	if chapter.Source == sourceGuipian {
		return d.resolveGuipianMedia(ctx, task)
	}
	if chapter.Source == sourceMissAV {
		return d.resolveAdultMedia(ctx, task)
	}
	if chapter.Source == sourceIkanBot { return d.resolveIkanbotMedia(ctx, task) }
	if chapter.Source == sourceChaoliu { return d.resolveChaoliuMedia(ctx, task) }
	if chapter.Source == sourceSupJav || chapter.Source == sourcePPP || chapter.Source == sourceMadou || chapter.Source == source91Porna {
		return d.resolveAdultMedia(ctx, task)
	}
	if strings.HasPrefix(chapter.VideoURL, "hongguo-cenc://") {
		return d.resolveHongguoMedia(ctx, task)
	}
	if chapter.PageURL == "" && (chapter.Source == sourceHuangguoAI || chapter.Source == sourceHuangguoVideo) {
		if source, sourceID, valid := splitProviderDramaID(task.DramaID); valid {
			_, chapters, err := d.GetHuangguoChapters(ctx, source, sourceID)
			if err != nil {
				return providerMedia{}, fmt.Errorf("重新取得舊任務播放網址失敗：%w", err)
			}
			matched := false
			for _, fresh := range chapters {
				if fresh.ID == chapter.ID {
					chapter = fresh
					matched = true
					break
				}
			}
			if !matched {
				return providerMedia{}, fmt.Errorf("原章节已变化，请更新该合集后重新下载")
			}
		}
	}
	media := providerMedia{URL: chapter.VideoURL, Referer: firstNonEmpty(chapter.Referer, d.providerBaseURL(chapter.Source)+"/")}
	if chapter.Source == sourceHuangdou {
		_, sourceID, valid := splitProviderDramaID(task.DramaID)
		if valid {
			sequence, err := strconv.Atoi(chapter.EpisodeString(task.Index))
			if err != nil || sequence < 1 {
				return providerMedia{}, fmt.Errorf("黃豆集數無效")
			}
			client := newHuangdouAPIClient(d)
			media, err = d.resolveHuangdouPlayback(ctx, client, sourceID, sequence)
			if err != nil {
				return providerMedia{}, err
			}
			media.Referer = client.host + "/home"
		}
	}
	if chapter.Source == sourceHuangguoAI {
		_, sourceID, valid := splitProviderDramaID(task.DramaID)
		if !valid {
			return providerMedia{}, errors.New("黃果劇集資訊無效")
		}
		address, err := d.fetchHuangguoAIPlayURL(ctx, sourceID, chapterEpisodeNumber(chapter, task.Index))
		if err != nil {
			return providerMedia{}, err
		}
		media.URL = address
	}
	if chapter.PageURL != "" && chapter.Source == sourceHuangguoVideo {
		responses := &playbackResponseURLs{}
		pageContext := context.WithValue(ctx, playbackResponseURLsKey{}, responses)
		body, err := d.fetchProviderText(pageContext, chapter.PageURL, media.Referer)
		if err != nil {
			return providerMedia{}, err
		}
		pageURL := chapter.PageURL
		if actual, ok := responses.values.Load(chapter.PageURL); ok {
			pageURL = actual.(string)
		}
		media.URL = parseDataHLS(body, pageURL)
		media.Referer = pageURL
	}
	if !isProviderHTTPMediaURL(media.URL) {
		return providerMedia{}, fmt.Errorf("%s 未提供有效的播放網址，請重新整理分集或確認片源存取權限", chapter.Source)
	}
	parsed, _ := url.Parse(media.URL)
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		playlist, finalURL, err := d.fetchMediaPlaylist(ctx, media.URL, media.Referer)
		if err != nil {
			return providerMedia{}, fmt.Errorf("取得播放清單失敗：%w", err)
		}
		if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff")), "#EXTM3U") {
			return providerMedia{}, fmt.Errorf("片源未提供有效的 M3U8 播放清單，可能需要登入或連結已失效")
		}
		if duration := m3u8Duration(playlist); duration > 0 {
			media.Duration = duration
		}
		media.Playlist = playlist
		media.URL = finalURL
	}
	return media, nil
}
