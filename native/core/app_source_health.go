package core

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type nativeHealthStep struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	Message    string `json:"message"`
	Host       string `json:"host,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	ElapsedMS  int64  `json:"elapsedMs"`
	CFRay      string `json:"cfRay,omitempty"`
}

type nativeSourceHealth struct {
	CheckedAt time.Time          `json:"checkedAt"`
	State     string             `json:"state"`
	Sample    string             `json:"sample"`
	Steps     []nativeHealthStep `json:"steps"`
}

type sourceResponseTrace struct {
	mu   sync.Mutex
	last nativeHealthStep
}

type sourceTraceKey struct{}

type sourceHealthWarning struct { message string }

func (warning sourceHealthWarning) Error() string { return warning.message }

func observeSourceResponse(ctx context.Context, response *http.Response) {
	trace, _ := ctx.Value(sourceTraceKey{}).(*sourceResponseTrace)
	if trace == nil || response == nil {
		return
	}
	step := nativeHealthStep{HTTPStatus: response.StatusCode, CFRay: truncate(response.Header.Get("Cf-Ray"), 128)}
	if response.Request != nil && response.Request.URL != nil {
		step.Host = response.Request.URL.Hostname()
	}
	trace.mu.Lock()
	trace.last = step
	trace.mu.Unlock()
}

// sourceHealthSampleCandidates prevents one stale/deleted catalogue entry
// from marking an otherwise healthy source as unavailable. Explicitly selected
// works (for a detail-page diagnostic) are never silently replaced.
func sourceHealthSampleCandidates(selected nativeDrama, items []nativeDrama) []nativeDrama {
	if strings.TrimSpace(selected.ID) != "" {
		return []nativeDrama{selected}
	}
	candidates := make([]nativeDrama, 0, 5)
	seen := make(map[string]bool, len(items))
	appendGroup := func(vip bool) {
		for _, item := range items {
			isVIP := item.VIP != nil && *item.VIP
			if isVIP != vip || strings.TrimSpace(item.ID) == "" || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			candidates = append(candidates, item)
			if len(candidates) >= 5 {
				return
			}
		}
	}
	appendGroup(false)
	if len(candidates) < 5 {
		appendGroup(true)
	}
	return candidates
}

func (engine *nativeEngine) checkSource(ctx context.Context, source string, selected nativeDrama, playback bool) error {
	report := nativeSourceHealth{CheckedAt: time.Now(), State: "checking", Steps: []nativeHealthStep{}}
	trace := &sourceResponseTrace{}
	ctx = context.WithValue(ctx, sourceTraceKey{}, trace)
	publish := func() {
		copy := report
		copy.Steps = append([]nativeHealthStep{}, report.Steps...)
		engine.changeSourceRecord(source, func(record *nativeSourceRecord) { record.Health = &copy })
	}
	var failure error
	var warning bool
	defer func() {
		report.CheckedAt = time.Now()
		if failure != nil {
			report.State = "failed"
		} else if warning {
			report.State = "warning"
		} else if playback {
			report.State = "ok"
		} else {
			report.State = "catalogOnly"
		}
		publish()
	}()
	run := func(name string, action func() (string, error)) bool {
		engine.changeSourceRecord(source, func(record *nativeSourceRecord) {
			record.Stage, record.Completed = name, len(report.Steps)
			record.Total = 1
			if playback {
				record.Total = 5
			}
		})
		trace.mu.Lock()
		trace.last = nativeHealthStep{}
		trace.mu.Unlock()
		start := time.Now()
		message, err := action()
		trace.mu.Lock()
		step := trace.last
		trace.mu.Unlock()
		step.Name, step.State, step.Message, step.ElapsedMS = name, "ok", message, time.Since(start).Milliseconds()
		isWarning := false
		if err != nil {
			var healthWarning sourceHealthWarning
			if errors.As(err, &healthWarning) {
				warning = true
				isWarning = true
				step.State = "warning"
				step.Message = publicError(err).Error()
			} else {
				failure = err
				step.State = "failed"
				if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
					step.Message = "單次網路請求逾時"
				} else {
					step.Message = publicError(err).Error()
				}
			}
			var backoff *requestBackoff
			if errors.As(err, &backoff) {
				step.HTTPStatus, step.Host = backoff.status, backoff.host
			}
		}
		report.Steps = append(report.Steps, step)
		publish()
		return err == nil || isWarning
	}
	var page nativeCatalogResult
	if !run("入口与目录", func() (string, error) {
		var err error
		page, err = engine.nativeCatalog(ctx, nativeInput{Source: source, Page: 1, Force: true})
		catalogErr := err
		if catalogErr == nil && page.Warning != "" {
			catalogErr = errors.New(page.Warning)
		}
		if catalogErr == nil && len(page.Items) == 0 {
			catalogErr = errors.New("入口可達，但沒有解析到有效劇集")
		}
		if catalogErr != nil && source == sourceMadou {
			items := append([]nativeDrama(nil), page.Items...)
			if len(items) == 0 && selected.ID != "" {
				items = append(items, selected)
			}
			if len(items) == 0 {
				engine.mu.Lock()
				items = append(items, engine.catalogs[source]...)
				engine.mu.Unlock()
			}
			if len(items) > 0 {
				page.Items = items
				message := "madou 即時目錄檢查未通過"
				if detail := publicError(catalogErr).Error(); detail != "" {
					message += "：" + detail
				}
				message += "；已改用目前載入的作品資料繼續驗證實際播放"
				return fmt.Sprintf("即時目錄有警告，使用已載入資料 %d 部", len(items)), sourceHealthWarning{message: message}
			}
		}
		return fmt.Sprintf("已解析 %d 部劇", len(page.Items)), catalogErr
	}) || !playback {
		return failure
	}
	drama := selected
	var chapter Chapter
	if !run("分集目录", func() (string, error) {
		candidates := sourceHealthSampleCandidates(selected, page.Items)
		if len(candidates) == 0 {
			return "", errors.New("目錄中沒有可供檢測的作品")
		}
		var failures []error
		for _, candidate := range candidates {
			_, id, valid := splitProviderDramaID(candidate.ID)
			if !valid || id == "" {
				failures = append(failures, fmt.Errorf("%s：作品識別碼無效", firstNonEmpty(candidate.Title, candidate.ID)))
				continue
			}
			_, chapters, err := engine.downloader.GetHuangguoChapters(ctx, source, id)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s：%w", firstNonEmpty(candidate.Title, candidate.ID), err))
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				continue
			}
			var playable *Chapter
			for index := range chapters {
				if !chapters[index].VIP {
					playable = &chapters[index]
					break
				}
			}
			if playable == nil {
				failures = append(failures, fmt.Errorf("%s：沒有可用的免費分集", firstNonEmpty(candidate.Title, candidate.ID)))
				continue
			}
			drama = candidate
			chapter = *playable
			report.Sample = drama.Title
			skipped := len(failures)
			message := fmt.Sprintf("%d 集，檢測第 %s 集", len(chapters), chapter.EpisodeString(1))
			if skipped > 0 {
				message += fmt.Sprintf("（略過 %d 部無法取得詳情或分集的作品）", skipped)
			}
			return message, nil
		}
		if len(failures) == 0 {
			return "", errors.New("沒有可用的免費分集")
		}
		return "", fmt.Errorf("前 %d 部候選作品皆無法取得可播放分集：%w", len(candidates), errors.Join(failures...))
	}) {
		return failure
	}
	var media providerMedia
	var keyURL, segmentURL string
	if !run("播放網址與播放清單", func() (string, error) {
		var err error
		media, err = engine.downloader.resolveProviderMedia(ctx, Task{DramaID: drama.ID, Chapter: chapter, Index: 1})
		if err != nil {
			return "", err
		}
		media, keyURL, segmentURL, err = engine.downloader.sourceProbePlayableRoute(ctx, media)
		if err != nil {
			return "", err
		}
		return "已驗證至少一條可讀取的播放線路", nil
	}) {
		return failure
	}
	if !run("播放密钥", func() (string, error) {
		if len(media.CENCKey) == 16 || len(media.HLSKey) == 16 {
			return "密钥长度有效", nil
		}
		if keyURL == "" {
			return "目前媒體不需要額外金鑰", nil
		}
		if strings.HasPrefix(keyURL, "data:") {
			_, encoded, found := strings.Cut(keyURL, ",")
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if !found || err != nil || len(decoded) != 16 {
				return "", errors.New("內嵌金鑰格式無效")
			}
			return "内嵌密钥有效", nil
		}
		body, err := engine.downloader.sourceProbeBytesWithMedia(ctx, media, keyURL, true)
		if err == nil && len(body) != 16 {
			err = errors.New("回傳的播放金鑰不是 16 位元組")
		}
		return "密钥可达且长度有效", err
	}) {
		return failure
	}
	run("媒體連線", func() (string, error) {
		body, err := engine.downloader.sourceProbeBytesWithMedia(ctx, media, segmentURL, false)
		if err == nil && len(body) == 0 {
			err = errors.New("媒體伺服器回傳空內容")
		}
		return "已讀取媒體開頭；實際播放效果請在播放器中確認", err
	})
	engine.changeSourceRecord(source, func(record *nativeSourceRecord) { record.Completed = len(report.Steps) })
	return failure
}

func (d *Downloader) sourceProbeResources(ctx context.Context, media providerMedia) (string, string, error) {
	ctx = providerMediaContext(ctx, media.credentials)
	if media.Playlist == "" {
		parsed, parseErr := url.Parse(media.URL)
		if parseErr != nil || !strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
			return "", media.URL, nil
		}
		playlist, finalURL, fetchErr := d.fetchProviderMediaPlaylist(ctx, media, media.URL)
		if fetchErr != nil {
			return "", "", fetchErr
		}
		media.Playlist, media.URL = playlist, finalURL
	}
	body, address := media.Playlist, media.URL
	for depth := 0; depth < 6; depth++ {
		lines, _, master := nativeHLSSelect(strings.Split(body, "\n"), 480)
		base, err := url.Parse(address)
		if err != nil {
			return "", "", err
		}
		resolve := func(raw string) string {
			ref, err := url.Parse(strings.TrimSpace(raw))
			if err != nil {
				return ""
			}
			return base.ResolveReference(ref).String()
		}
		key, segment := "", ""
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#EXT-X-KEY:") {
				attributes := nativeHLSAttributes(line)
				if attributes["METHOD"] == "AES-128" {
					key = resolve(attributes["URI"])
				} else if attributes["METHOD"] != "NONE" {
					return "", "", errors.New("目前的金鑰格式不支援健康檢查")
				}
			}
			if line != "" && !strings.HasPrefix(line, "#") {
				segment = resolve(line)
				break
			}
		}
		if !isProviderHTTPMediaURL(segment) {
			return "", "", errors.New("播放清單缺少有效的媒體網址")
		}
		if !master {
			return key, segment, nil
		}
		body, address, err = d.fetchMediaPlaylist(ctx, segment, media.Referer)
		if err != nil {
			return "", "", err
		}
	}
	return "", "", errors.New("播放清單巢狀層級過多")
}

// sourceProbePlayableRoute checks the actual media path for each same-episode
// candidate and returns the first route whose playlist and first segment can
// be read. A CDN failure on the preferred URL must not mark a source unhealthy
// when its retained fallback route is still playable.
func (d *Downloader) sourceProbePlayableRoute(ctx context.Context, media providerMedia) (providerMedia, string, string, error) {
	candidates := append([]providerMedia{media}, media.Variants...)
	seen := map[string]bool{}
	var failures []error
	for _, candidate := range candidates {
		if !isProviderHTTPMediaURL(candidate.URL) {
			continue
		}
		identity := nativePlaybackMediaIdentity(candidate)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		candidate.Variants = nil
		keyURL, segmentURL, err := d.sourceProbeResources(ctx, candidate)
		if err != nil {
			failures = append(failures, fmt.Errorf("播放線路：%w", err))
			continue
		}
		if _, err := d.sourceProbeBytesWithMedia(ctx, candidate, segmentURL, false); err != nil {
			failures = append(failures, fmt.Errorf("媒體片段：%w", err))
			continue
		}
		return candidate, keyURL, segmentURL, nil
	}
	if len(failures) == 0 {
		return providerMedia{}, "", "", errors.New("沒有可供檢測的播放路線")
	}
	return providerMedia{}, "", "", errors.Join(failures...)
}

func (d *Downloader) sourceProbeBytes(ctx context.Context, address, referer string, key bool) ([]byte, error) {
	return d.sourceProbeBytesWithClient(ctx, address, referer, key, d.client)
}

func (d *Downloader) sourceProbeBytesWithMedia(ctx context.Context, media providerMedia, address string, key bool) ([]byte, error) {
	ctx = providerMediaContext(ctx, media.credentials)
	body, err := d.sourceProbeBytesWithClient(ctx, address, media.Referer, key, d.client)
	if err == nil || media.StandardHTTP || d.streamClient == nil || d.streamClient == d.client {
		return body, err
	}
	// Probe with the same specialised transport that the native player uses
	// when the general media client cannot reach a CDN edge.
	return d.sourceProbeBytesWithClient(ctx, address, media.Referer, key, d.streamClient)
}

func (d *Downloader) sourceProbeBytesWithClient(ctx context.Context, address, referer string, key bool, client *http.Client) ([]byte, error) {
	if !isProviderHTTPMediaURL(address) {
		return nil, errors.New("媒體網址無效")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Referer", referer)
	limit := int64(1024)
	if key {
		limit = 17
	} else {
		request.Header.Set("Range", "bytes=0-1023")
	}
	response, err := d.doMediaRequestWithClient(request, client)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return nil, d.catalogResponseError(request, response, body)
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/json") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		if catalogResponseBlockReason(response, body) != "" {
			return nil, d.catalogResponseError(request, response, body)
		}
		return nil, errors.New("媒體介面回傳網頁或錯誤資訊")
	}
	return io.ReadAll(io.LimitReader(response.Body, limit))
}
