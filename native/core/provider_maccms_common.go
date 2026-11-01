package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var maccmsEpisodeNumber = regexp.MustCompile(`(?i)(?:第\s*0*(\d+)\s*(?:集|話|期)|(?:更新至|共|全)\s*0*(\d+)\s*(?:集|話|期)|(?:episode|ep)\s*#?\s*0*(\d+))`)

type maccmsBackend struct {
	host    string
	apiBase string
	home    string
}

type maccmsAPIResponse struct {
	Code      int              `json:"code"`
	Message   string           `json:"msg"`
	Page      json.RawMessage  `json:"page"`
	PageCount json.RawMessage  `json:"pagecount"`
	Total     json.RawMessage  `json:"total"`
	List      []map[string]any `json:"list"`
}

type maccmsEpisode struct {
	Title string
	URL   string
	Index int
}

type maccmsPlayGroup struct {
	Name     string
	Episodes []maccmsEpisode
}

func maccmsRawInteger(raw json.RawMessage) int {
	var number int
	if json.Unmarshal(raw, &number) == nil {
		return number
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		number, _ = strconv.Atoi(strings.TrimSpace(text))
	}
	return number
}

func parseMaccmsAPI(body string) (maccmsAPIResponse, error) {
	var payload maccmsAPIResponse
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return payload, fmt.Errorf("MacCMS API 回應格式錯誤：%w", err)
	}
	if payload.Code != 1 {
		return payload, fmt.Errorf("MacCMS API 回應失敗：%s", firstNonEmpty(payload.Message, "未知錯誤"))
	}
	if payload.List == nil {
		return payload, errors.New("MacCMS API 未回傳影片清單")
	}
	return payload, nil
}

func maccmsAPIAddress(backend maccmsBackend, values url.Values) string {
	base, err := url.Parse(backend.apiBase)
	if err != nil {
		return ""
	}
	base.RawQuery = values.Encode()
	return base.String()
}

func (d *Downloader) fetchMaccmsAPIWithBackend(ctx context.Context, backend maccmsBackend, values url.Values) (maccmsAPIResponse, error) {
	address := maccmsAPIAddress(backend, values)
	if address == "" {
		return maccmsAPIResponse{}, errors.New("MacCMS API 位址無效")
	}
	body, err := d.fetchProviderText(ctx, address, backend.home)
	if err != nil {
		return maccmsAPIResponse{}, fmt.Errorf("%s 上游連線失敗：%w", backend.host, err)
	}
	payload, err := parseMaccmsAPI(body)
	if err != nil {
		return maccmsAPIResponse{}, fmt.Errorf("%s：%w", backend.host, err)
	}
	return payload, nil
}

func maccmsRecordString(record map[string]any, key string) string {
	return strings.TrimSpace(nativeText(record[key]))
}

func maccmsDramaFromRecord(config maccmsSourceConfig, record map[string]any, category string) Drama {
	upstreamID := maccmsRecordString(record, "vod_id")
	if !webProviderNumericID.MatchString(upstreamID) {
		return Drama{}
	}
	title := maccmsRecordString(record, "vod_name")
	if title == "" {
		return Drama{}
	}
	cover := maccmsRecordString(record, "vod_pic")
	if strings.HasPrefix(cover, "//") {
		cover = "https:" + cover
	}
	if !isProviderHTTPMediaURL(cover) {
		cover = ""
	}
	description := maccmsRecordString(record, "vod_blurb")
	if description == "" {
		description = maccmsRecordString(record, "vod_content")
	}
	categoryName := firstNonEmpty(maccmsRecordString(record, "type_name"), category, "影視")
	remark := maccmsRecordString(record, "vod_remarks")
	year := maccmsRecordString(record, "vod_year")
	episodeCount, _ := strconv.Atoi(maccmsRecordString(record, "vod_total"))
	var tags []string
	for _, tag := range strings.Split(maccmsRecordString(record, "vod_class"), ",") {
		if tag = strings.TrimSpace(tag); tag != "" && len([]rune(tag)) <= 32 && len(tags) < 16 {
			tags = append(tags, tag)
		}
	}
	return Drama{
		ID: providerDramaID(config.id, upstreamID), Source: config.id, SourceID: upstreamID,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Desc: truncate(htmlstd.UnescapeString(description), 12000),
		Intro: truncate(htmlstd.UnescapeString(description), 12000),
		Cover: cover, CoverURL: cover, ChannelName: config.name, CategoryName: categoryName,
		Remark: truncate(remark, 64), OnlineDate: year,
		TotalEpisode: episodeCount, EpisodeCount: episodeCount, Tags: tags,
	}
}

func splitMaccmsGroupNames(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	separator := strings.Repeat("$", 3)
	if strings.Contains(raw, separator) {
		parts := strings.Split(raw, separator)
		names := make([]string, 0, len(parts))
		for _, part := range parts {
			if name := strings.TrimSpace(part); name != "" {
				names = append(names, name)
			}
		}
		return names
	}
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		names := make([]string, 0, len(parts))
		for _, part := range parts {
			if name := strings.TrimSpace(part); name != "" {
				names = append(names, name)
			}
		}
		return names
	}
	return []string{raw}
}

func splitMaccmsPlayURLGroups(raw string, expected int) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	separator := strings.Repeat("$", 3)
	parts := strings.Split(raw, separator)
	if expected > 1 && len(parts) == 1 {
		twoDollarSeparator := strings.Repeat("$", 2)
		if strings.Contains(raw, twoDollarSeparator) {
			parts = strings.Split(raw, twoDollarSeparator)
		} else if strings.Contains(raw, ",") {
			parts = strings.Split(raw, ",")
		}
	}
	return parts
}

func maccmsPlayGroupPriority(name string) int {
	name = strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(name, "m3u8") {
		return 0
	}
	if strings.Contains(name, "mp4") {
		return 1
	}
	if strings.Contains(name, "yun") || strings.Contains(name, "cloud") {
		return 3
	}
	return 2
}

func parseMaccmsPlayGroups(record map[string]any) []maccmsPlayGroup {
	froms := splitMaccmsGroupNames(maccmsRecordString(record, "vod_play_from"))
	playURLs := splitMaccmsPlayURLGroups(maccmsRecordString(record, "vod_play_url"), len(froms))
	count := min(len(froms), len(playURLs))
	groups := make([]maccmsPlayGroup, 0, count)
	for groupIndex := 0; groupIndex < count; groupIndex++ {
		name := strings.TrimSpace(froms[groupIndex])
		var episodes []maccmsEpisode
		for episodeIndex, rawEpisode := range strings.Split(playURLs[groupIndex], "#") {
			parts := strings.SplitN(strings.TrimSpace(rawEpisode), "$", 2)
			if len(parts) != 2 {
				continue
			}
			title, rawURL := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			if title == "" || !isProviderHTTPMediaURL(rawURL) {
				continue
			}
			episodes = append(episodes, maccmsEpisode{Title: truncate(title, 128), URL: rawURL, Index: episodeIndex + 1})
		}
		if len(episodes) > 0 {
			groups = append(groups, maccmsPlayGroup{Name: name, Episodes: episodes})
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return maccmsPlayGroupPriority(groups[i].Name) < maccmsPlayGroupPriority(groups[j].Name)
	})
	return groups
}

func maccmsEpisodeOrder(title string, fallback int) int {
	match := maccmsEpisodeNumber.FindStringSubmatch(title)
	if len(match) == 0 {
		return fallback
	}
	for index := 1; index < len(match); index++ {
		if match[index] != "" {
			if value, err := strconv.Atoi(match[index]); err == nil && value > 0 {
				return value
			}
		}
	}
	return fallback
}
