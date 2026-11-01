package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type maccmsSourceConfig struct {
	id      string
	name    string
	host    string
	apiBase string
	home    string
}

var maccmsSources = []maccmsSourceConfig{
	{id: sourceBaofeng, name: "暴風", host: "bfzyapi.com", apiBase: "https://bfzyapi.com/api.php/provide/vod/", home: "https://bfzyapi.com/"},
	{id: sourceJisu, name: "極速", host: "jszyapi.com", apiBase: "https://jszyapi.com/api.php/provide/vod/", home: "https://jszyapi.com/"},
	{id: source360, name: "360", host: "360zy.com", apiBase: "https://360zy.com/api.php/provide/vod/", home: "https://360zy.com/"},
	{id: sourceHuya, name: "虎牙", host: "huyazy.net", apiBase: "https://huyazy.net/api.php/provide/vod/", home: "https://huyazy.net/"},
}

func maccmsSourceByID(source string) (maccmsSourceConfig, bool) {
	source = canonicalProviderSource(source)
	for _, item := range maccmsSources {
		if item.id == source {
			return item, true
		}
	}
	return maccmsSourceConfig{}, false
}

func maccmsSourceByHost(host string) (maccmsSourceConfig, bool) {
	host = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
	for _, item := range maccmsSources {
		if item.host == host {
			return item, true
		}
	}
	return maccmsSourceConfig{}, false
}

func validMaccmsCategory(category string) bool {
	if category == "" {
		return true
	}
	id, err := strconv.Atoi(category)
	return err == nil && id > 0 && id <= 1000000
}

func (d *Downloader) fetchMaccmsAPI(ctx context.Context, source string, values url.Values) (kanmovieAPIResponse, error) {
	config, ok := maccmsSourceByID(source)
	if !ok {
		return kanmovieAPIResponse{}, errors.New("不支援的 MacCMS 片源")
	}
	backend := kanmovieBackend{host: config.host, apiBase: config.apiBase, home: config.home}
	return d.fetchKanmMovieAPI(ctx, backend, values)
}

func maccmsDramaFromRecord(config maccmsSourceConfig, record map[string]any, category string) Drama {
	backend := kanmovieBackend{host: config.host, apiBase: config.apiBase, home: config.home}
	drama := kanmovieDramaFromRecord(backend, record, category)
	if drama.ID == "" {
		return Drama{}
	}
	drama.Source = config.id
	drama.SourceID = kanmovieRecordString(record, "vod_id")
	drama.ID = providerDramaID(config.id, drama.SourceID)
	drama.ChannelName = config.name
	return drama
}

func (d *Downloader) fetchMaccmsCatalogPage(ctx context.Context, source string, page int, category, query string) ([]Drama, bool, error) {
	config, ok := maccmsSourceByID(source)
	if !ok || page < 1 || page > 100000 || len(query) > 256 || !validMaccmsCategory(category) {
		return nil, false, errors.New("片源目錄查詢參數無效")
	}
	values := url.Values{"ac": {"list"}, "pg": {strconv.Itoa(page)}, "at": {"json"}}
	if category != "" {
		values.Set("t", category)
	}
	if query != "" {
		values.Set("wd", query)
	}
	payload, err := d.fetchMaccmsAPI(ctx, source, values)
	if err != nil {
		return nil, false, err
	}
	items := make([]Drama, 0, len(payload.List))
	for _, row := range payload.List {
		if drama := maccmsDramaFromRecord(config, row, ""); drama.ID != "" {
			items = append(items, drama)
		}
	}
	return items, kanmovieRawInteger(payload.PageCount) > page, nil
}

func (d *Downloader) fetchMaccmsCategories(ctx context.Context, source string) ([]nativeCategory, error) {
	config, ok := maccmsSourceByID(source)
	if !ok {
		return nil, errors.New("不支援的 MacCMS 片源")
	}
	raw, err := d.fetchProviderText(ctx, config.apiBase+"?ac=list&at=json", config.home)
	if err != nil {
		return nil, err
	}
	var document struct {
		Code  int `json:"code"`
		Class []struct {
			ID   json.RawMessage `json:"type_id"`
			Name string          `json:"type_name"`
		} `json:"class"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return nil, fmt.Errorf("片源分類資料格式錯誤：%w", err)
	}
	if document.Code != 1 {
		return nil, errors.New("片源未回傳有效分類")
	}
	categories := []nativeCategory{{Name: "全部"}}
	seen := map[string]bool{}
	for _, row := range document.Class {
		id := strings.TrimSpace(string(row.ID))
		name := strings.TrimSpace(row.Name)
		if id == "" || name == "" || !validMaccmsCategory(id) || seen[id] {
			continue
		}
		seen[id] = true
		categories = append(categories, nativeCategory{ID: id, Name: truncate(name, 48)})
	}
	if len(categories) == 1 {
		return nil, errors.New("片源暫未提供分類")
	}
	return categories, nil
}

func (d *Downloader) fetchMaccmsDetailRecord(ctx context.Context, source, sourceID string) (maccmsSourceConfig, map[string]any, error) {
	config, ok := maccmsSourceByID(source)
	if !ok || !webProviderNumericID.MatchString(sourceID) {
		return maccmsSourceConfig{}, nil, errors.New("片源影片代碼無效")
	}
	values := url.Values{"ac": {"detail"}, "ids": {sourceID}, "at": {"json"}}
	payload, firstErr := d.fetchMaccmsAPI(ctx, source, values)
	if firstErr == nil && len(payload.List) > 0 {
		return config, payload.List[0], nil
	}
	values.Set("ac", "videolist")
	payload, secondErr := d.fetchMaccmsAPI(ctx, source, values)
	if secondErr == nil && len(payload.List) > 0 {
		return config, payload.List[0], nil
	}
	if firstErr != nil || secondErr != nil {
		return maccmsSourceConfig{}, nil, errors.Join(firstErr, secondErr)
	}
	return maccmsSourceConfig{}, nil, errors.New("片源目前沒有回傳這部影片")
}

func (d *Downloader) fetchMaccmsDetail(ctx context.Context, source, sourceID string) (Drama, []Chapter, error) {
	config, record, err := d.fetchMaccmsDetailRecord(ctx, source, sourceID)
	if err != nil {
		return Drama{}, nil, err
	}
	drama := maccmsDramaFromRecord(config, record, "")
	if drama.ID == "" {
		return Drama{}, nil, errors.New("片源詳情缺少影片名稱或 ID")
	}
	groups := parseKanmMoviePlayGroups(record)
	if len(groups) == 0 {
		return Drama{}, nil, errors.New("這部影片目前沒有可用的分集或播放線路")
	}
	best := groups[0]
	for _, group := range groups[1:] {
		if len(group.Episodes) > len(best.Episodes) {
			best = group
		}
	}
	chapters := make([]Chapter, 0, len(best.Episodes))
	for _, episode := range best.Episodes {
		key := strconv.Itoa(episode.Index)
		chapters = append(chapters, Chapter{
			ID: providerChapterID(source, sourceID, key), Source: source,
			Title: episode.Title, CurrentEpisode: rawEpisode(kanmovieEpisodeOrder(episode.Title, episode.Index)),
			VideoURL: fmt.Sprintf("maccms-play://%s/%s", source, key),
			Referer: config.home,
		})
	}
	drama.TotalEpisode, drama.EpisodeCount = len(chapters), len(chapters)
	drama.EpisodesVerified = len(chapters) > 0
	return drama, chapters, nil
}

func (d *Downloader) resolveMaccmsMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	config, supported := maccmsSourceByID(source)
	prefix := providerChapterID(source, sourceID, "")
	if !valid || !supported || task.Chapter.Source != source || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("播放分集資訊無效，請重新整理詳情")
	}
	key := strings.TrimPrefix(task.Chapter.ID, prefix)
	episodeIndex, err := strconv.Atoi(key)
	if err != nil || episodeIndex < 1 || episodeIndex > 100000 {
		return providerMedia{}, errors.New("播放集數無效")
	}
	_, record, err := d.fetchMaccmsDetailRecord(ctx, source, sourceID)
	if err != nil {
		return providerMedia{}, err
	}
	groups := parseKanmMoviePlayGroups(record)
	var options []providerMedia
	seen := map[string]bool{}
	for _, group := range groups {
		var selected *kanmovieEpisode
		for index := range group.Episodes {
			episode := &group.Episodes[index]
			if task.Chapter.Title != "" && strings.EqualFold(strings.TrimSpace(episode.Title), strings.TrimSpace(task.Chapter.Title)) {
				selected = episode
				break
			}
		}
		if selected == nil {
			for index := range group.Episodes {
				if group.Episodes[index].Index == episodeIndex {
					selected = &group.Episodes[index]
					break
				}
			}
		}
		if selected == nil || !isProviderHTTPMediaURL(selected.URL) || seen[selected.URL] {
			continue
		}
		seen[selected.URL] = true
		options = append(options, providerMedia{URL: selected.URL, Referer: config.home})
	}
	if len(options) == 0 {
		return providerMedia{}, errors.New("片源目前找不到這一集的有效播放網址")
	}
	primary := options[0]
	if len(options) > 1 {
		primary.Variants = append([]providerMedia(nil), options[1:]...)
	}
	return primary, nil
}
