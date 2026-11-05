package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const huangguoAISearchPageSize = 20

func (d *Downloader) fetchProviderRequest(ctx context.Context, rawURL, referer string, payload []byte) (string, error) {
	retries := d.cfg.Retries
	if retries <= 0 {
		retries = 3
	}
	if referer == "" {
		referer = rawURL
	}
	candidates := d.providerURLCandidates(rawURL)
	var lastErr error
	tried := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		attempts := retries
		if len(candidates) > 1 {
			attempts = 1
		}
		for attempt := 1; attempt <= attempts; attempt++ {
			tried++
			if attempt > 1 {
				select {
				case <-time.After(time.Duration(attempt) * time.Second):
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}
			timeout := providerTimeout
			if len(candidates) > 1 {
				timeout = 5 * time.Second
			}
			method, requestBody := http.MethodGet, io.Reader(nil)
			if payload != nil {
				method, requestBody = http.MethodPost, bytes.NewReader(payload)
			}
			req, err := http.NewRequestWithContext(ctx, method, candidate, requestBody)
			if err != nil {
				return "", err
			}
			if payload != nil {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json")
			}
			req.Header.Set("User-Agent", userAgent)
			if agent, ok := ctx.Value(providerTextUserAgentKey{}).(string); ok && agent != "" {
				req.Header.Set("User-Agent", agent)
			}
			if providerSourceForURL(rawURL) != "" {
				req.Header.Set("Referer", providerRefererForURL(candidate, referer))
			} else {
				req.Header.Set("Referer", referer)
			}
			req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
			if noCache, _ := ctx.Value(providerTextNoCacheKey{}).(bool); noCache {
				req.Header.Set("Cache-Control", "no-cache")
			}
			resp, err := d.doCatalogRequestWithTimeout(req, timeout)
			if err != nil {
				lastErr = err
				var backoff *requestBackoff
				if errors.As(err, &backoff) {
					break
				}
				continue
			}
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
			closeErr := resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				continue
			}
			if closeErr != nil {
				lastErr = closeErr
				continue
			}
			if len(body) > providerMaxBodyBytes {
				lastErr = fmt.Errorf("response exceeds %d bytes", providerMaxBodyBytes)
				continue
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 || catalogResponseBlockReason(resp, body) != "" {
				lastErr = d.catalogResponseError(req, resp, body)
				if catalogResponseBlockReason(resp, body) != "" || resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 {
					break
				}
				continue
			}
			if source := providerSourceForURL(rawURL); source != "" {
				effectiveURL := req.URL
				if resp.Request != nil {
					effectiveURL = resp.Request.URL
				}
				d.providerMu.Lock()
				d.providerHosts[source] = effectiveURL.Scheme + "://" + effectiveURL.Host
				d.providerMu.Unlock()
			}
			rememberPlaybackResponseURL(ctx, rawURL, resp)
			return string(body), nil
		}
	}
	if len(candidates) > 1 && lastErr != nil {
		return "", fmt.Errorf("黃果來源請求失敗：已嘗試 %d 個網域，最後錯誤：%w", tried, lastErr)
	}
	return "", lastErr
}

func (d *Downloader) huangguoAIAPI(ctx context.Context, path string, out any) error {
	envelope, err := json.Marshal(map[string]string{"method": http.MethodGet, "path": path, "body": "", "content_type": ""})
	if err != nil {
		return err
	}
	base := d.providerBaseURL(sourceHuangguoAI)
	body, err := d.fetchProviderRequest(ctx, base+"/api/edge/request", base+"/", envelope)
	if err != nil {
		return err
	}
	var reply struct {
		Status int             `json:"status"`
		Msg    string          `json:"msg"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		return fmt.Errorf("黃果 API 回傳格式無效：%w", err)
	}
	if reply.Status != 1 {
		return fmt.Errorf("黃果 API 回傳失敗：%s", firstNonEmpty(reply.Msg, "未知錯誤"))
	}
	if err := json.Unmarshal(reply.Data, out); err != nil {
		return fmt.Errorf("黃果 API 資料格式已變更：%w", err)
	}
	return nil
}

func (d *Downloader) fetchHuangguoAIDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.TrimPrefix(strings.Trim(strings.TrimSpace(sourceID), "/"), "detail/")
	if !rankingSourceID.MatchString(sourceID) {
		return Drama{}, nil, errors.New("無效的黃果劇集 ID")
	}
	var detail map[string]any
	if err := d.huangguoAIAPI(ctx, "/api/videos/"+url.PathEscape(sourceID), &detail); err != nil {
		return Drama{}, nil, err
	}
	base := d.providerBaseURL(sourceHuangguoAI)
	drama, ok := dramaFromAIMap(detail, base+"/", "")
	if !ok || drama.SourceID != sourceID {
		return Drama{}, nil, errors.New("黃果詳情與要求的劇集不符")
	}
	rows, _ := detail["episodes"].([]any)
	var chapters []Chapter
	for _, value := range rows {
		row, _ := value.(map[string]any)
		episode, err := strconv.Atoi(mapString(row, "ep_num"))
		if err != nil || episode < 1 {
			continue
		}
		chapters = append(chapters, huangguoAIChapter(base, sourceID, episode, mapString(row, "title")))
	}
	if len(rows) == 0 {
		chapters = append(chapters, huangguoAIChapter(base, sourceID, 1, drama.DisplayTitle()))
	}
	sortProviderChapters(chapters)
	return drama, uniqueChapters(chapters), nil
}

func huangguoAIChapter(base, sourceID string, episode int, title string) Chapter {
	key, pageURL := "auto-1", base+"/video/"+sourceID+"/"
	if episode > 1 {
		key = "ep-"+strconv.Itoa(episode)
		pageURL += key+"/"
	}
	return Chapter{ID: providerChapterID(sourceHuangguoAI, sourceID, key), Source: sourceHuangguoAI,
		Title: firstNonEmpty(title, fmt.Sprintf("第%d集", episode)), PageURL: pageURL, CurrentEpisode: rawEpisode(episode)}
}

func chapterEpisodeNumber(chapter Chapter, fallback int) int {
	if episode, err := strconv.Atoi(chapter.EpisodeString(fallback)); err == nil && episode > 0 {
		return episode
	}
	if fallback > 0 {
		return fallback
	}
	return 1
}

func (d *Downloader) fetchHuangguoAIPlayURL(ctx context.Context, sourceID string, episode int) (string, error) {
	if !rankingSourceID.MatchString(sourceID) || episode < 1 {
		return "", errors.New("黃果集數無效")
	}
	var play struct {
		VideoURL string `json:"video_url"`
	}
	path := fmt.Sprintf("/api/videos/%s/play?ep=%d&page_texts=0", url.PathEscape(sourceID), episode)
	if err := d.huangguoAIAPI(ctx, path, &play); err != nil {
		return "", err
	}
	return normalizeProviderMediaURL(d.providerBaseURL(sourceHuangguoAI)+"/", play.VideoURL), nil
}

func (d *Downloader) searchHuangguoAI(ctx context.Context, page int, query string) ([]Drama, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 256 {
		return nil, false, errors.New("黃果搜尋關鍵字無效")
	}
	var result struct {
		Items      json.RawMessage `json:"items"`
		Pagination struct {
			Pages int `json:"pages"`
		} `json:"pagination"`
	}
	path := "/api/search?"+url.Values{"q": {query}, "page": {strconv.Itoa(page)}, "size": {strconv.Itoa(huangguoAISearchPageSize)}}.Encode()
	if err := d.huangguoAIAPI(ctx, path, &result); err != nil {
		return nil, false, err
	}
	items := parseHuangguoAIJSONCards(result.Items, d.providerBaseURL(sourceHuangguoAI)+"/", "")
	return items, page < result.Pagination.Pages, nil
}

func parseHuangguoAIRankingRows(rows []map[string]any, pageURL string) (rankingPage, error) {
	failure := errors.New("黃果未回傳有效榜單")
	if len(rows) == 0 || len(rows) > 100 {
		return rankingPage{}, failure
	}
	result := rankingPage{Items: make([]rankingItem, 0, len(rows)), TotalPages: 1}
	seen := map[string]bool{}
	for index, row := range rows {
		row["id"] = row["video_id"]
		drama, ok := dramaFromAIMap(row, pageURL, mapString(row, "cate_name"))
		if !ok || !rankingSourceID.MatchString(drama.SourceID) || seen[drama.SourceID] {
			return rankingPage{}, failure
		}
		seen[drama.SourceID] = true
		rank, err := strconv.Atoi(mapString(row, "rank"))
		if err != nil || rank < 1 {
			rank = index + 1
		}
		result.Items = append(result.Items, rankingItem{Rank: rank, Drama: drama, Metric: huangguoRankingMetric(row)})
	}
	return result, nil
}

func huangguoRankingMetric(row map[string]any) string {
	label := mapString(row, "metric_label")
	value, err := strconv.ParseFloat(mapString(row, "metric_value"), 64)
	if label == "" || err != nil {
		return ""
	}
	if value >= 10000 {
		return label+" "+strconv.FormatFloat(value/10000, 'f', 1, 64)+"萬"
	}
	return label+" "+strconv.FormatFloat(value, 'f', -1, 64)
}
