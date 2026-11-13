package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type nativeDramaSeason struct {
	Drama       nativeDrama `json:"drama"`
	Number      int         `json:"number"`
	Label       string      `json:"label"`
	Subtitle    string      `json:"subtitle,omitempty"`
	Upcoming    bool        `json:"upcoming"`
	Playable    bool        `json:"playable"`
}

var hongguoOfficialSeasonLabel = regexp.MustCompile(`^第([1-9][0-9]*)季$`)

// This is the official same-series selector. It returns related series IDs,
// not title-based guesses; keep the complete list in the API's own order.
func hongguoAppSeasonQuery(seriesID string) url.Values {
	return url.Values{
		"scene":                 {"10"},
		"from":                  {"series_short_play_info"},
		"offset":                {"0"},
		"limit":                 {"0"},
		"total_chapter_num":     {"0"},
		"current_chapter_num":  {"0"},
		"post_id":               {"0"},
		"is_horizontal_screen":  {"false"},
		"bookstore_tab":         {"0"},
		"bookstore_tab_type":    {"0"},
		"book_id":               {seriesID},
		"filter_ids":            {seriesID},
	}
}

func (downloader *Downloader) hongguoAppSeasons(
	ctx context.Context,
	seriesID string,
) ([]nativeDramaSeason, error) {
	if !hongguoNumericID.MatchString(seriesID) || seriesID == "0" {
		return nil, errors.New("紅果系列 ID 無效")
	}
	result, err := downloader.hongguoAppRequest(
		ctx,
		http.MethodGet,
		"/reading/bookapi/plan/v",
		hongguoAppSeasonQuery(seriesID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("取得紅果系列季別失敗：%w", err)
	}
	return parseHongguoAppSeasons(result)
}

func parseHongguoAppSeasons(result map[string]any) ([]nativeDramaSeason, error) {
	if mapString(result, "code") != "0" {
		return nil, errors.New("紅果未確認系列季別請求成功")
	}
	blocks, ok := result["data"].([]any)
	if !ok {
		return nil, errors.New("紅果系列季別回應格式無效")
	}
	items := make([]nativeDramaSeason, 0, 8)
	seen := make(map[string]int)
seasonRows:
	for _, rawBlock := range blocks {
		block, ok := rawBlock.(map[string]any)
		if !ok {
			return nil, errors.New("紅果系列季別區塊格式無效")
		}
		if block["video_data"] == nil {
			continue
		}
		rows, ok := block["video_data"].([]any)
		if !ok {
			return nil, errors.New("紅果系列季別清單格式無效")
		}
		for _, rawRow := range rows {
			row, ok := rawRow.(map[string]any)
			if !ok {
				return nil, errors.New("紅果系列季別項目格式無效")
			}
			seriesID := firstNonEmpty(
				mapString(row, "series_id_str", "series_id"),
				mapString(nestedMap(row, "video_data"), "series_id_str", "series_id"),
			)
			if !hongguoNumericID.MatchString(seriesID) || seriesID == "0" {
				continue
			}
			drama := hongguoDramaFromAny(map[string]any{"video_data": row}, "短劇")
			if drama.ID == "" || drama.DisplayTitle() == "" {
				continue
			}
			episodeCount, _ := strconv.Atoi(mapString(row, "episode_cnt"))
			if episodeCount > 0 {
				drama.EpisodeCount = episodeCount
				drama.TotalEpisode = episodeCount
				drama.EpisodesVerified = true
			}
			item := nativeDramaSeason{
				Drama: nativeNormalize(drama),
				Subtitle: strings.Join(mapStringSliceFromObjects(row["sub_title_list"]), " · "),
			}

			for _, label := range mapStringSliceFromObjects(row["secondary_info_list"]) {
				if match := hongguoOfficialSeasonLabel.FindStringSubmatch(strings.TrimSpace(label)); match != nil {
					item.Number, _ = strconv.Atoi(match[1])
					item.Label = strings.TrimSpace(label)
					break
				}
			}
			if item.Number == 0 {
				item.Number, _ = strconv.Atoi(mapString(row, "season_index"))
			}
			if item.Label == "" && item.Number > 0 {
				item.Label = fmt.Sprintf("第%d季", item.Number)
			}
			if item.Number > 0 {
				item.Drama.SeriesSeasonNumber = item.Number
				if strings.HasSuffix(item.Label, "部") {
					item.Drama.SeriesSeasonUnit = "部"
				} else {
					item.Drama.SeriesSeasonUnit = "季"
				}
			}

			subscribeItem, hasSubscribeItem := row["subscribe_item"].(map[string]any)
			vid := firstNonEmpty(mapString(row, "vid"), mapString(row, "start_vid"))
			item.Upcoming = hasSubscribeItem || vid == "" || vid == "0" || episodeCount <= 0
			item.Playable = !item.Upcoming && !mapBool(row, "disable_play")
			if item.Upcoming {
				item.Drama.Episodes = 0
				item.Drama.EpisodesVerified = false
			}

			if previous, found := seen[seriesID]; found {
				if items[previous].Number != item.Number {
					return nil, errors.New("紅果系列季別包含重複且衝突的系列 ID")
				}
				continue
			}
			seen[seriesID] = len(items)
			items = append(items, item)
			if len(items) >= 100 {
				break seasonRows
			}
			_ = subscribeItem // Reservation IDs are intentionally not written here.
		}
	}

	// The lowest numbered season is the stable canonical identity for the
	// season group. Every sibling query returns the same official season list,
	// so selecting a different season does not create a second favorite group.
	groupID := ""
	groupNumber := int(^uint(0) >> 1)
	for _, item := range items {
		_, sourceID, valid := splitProviderDramaID(item.Drama.ID)
		if !valid || sourceID == "" {
			continue
		}
		if item.Number > 0 && (item.Number < groupNumber ||
			item.Number == groupNumber && (groupID == "" || sourceID < groupID)) {
			groupID = sourceID
			groupNumber = item.Number
		}
	}
	if groupID == "" {
		for _, item := range items {
			_, sourceID, valid := splitProviderDramaID(item.Drama.ID)
			if valid && sourceID != "" && (groupID == "" || sourceID < groupID) {
				groupID = sourceID
			}
		}
	}
	if groupID != "" {
		for index := range items {
			items[index].Drama.SeriesGroupID = groupID
		}
	}
	return items, nil
}

func mapStringSliceFromObjects(value any) []string {
	rows, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if text := strings.TrimSpace(mapString(row, "content")); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func mapBool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func (engine *nativeEngine) nativeSeasons(
	ctx context.Context,
	drama nativeDrama,
) (map[string]any, error) {
	source, sourceID, valid := splitProviderDramaID(drama.ID)
	if !valid || source != sourceHongguo {
		return map[string]any{"items": []nativeDramaSeason{}}, nil
	}
	items, err := engine.downloader.hongguoAppSeasons(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": items}, nil
}
