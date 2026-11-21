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

const (
	hongguoAppSeasonPlanPath = "/reading/bookapi/plan/v1/"
	hongguoShowTypeSeriesSeasons = "547"
)

type hongguoSeasonPlanRequestKey struct{}

var hongguoOfficialSeasonLabel = regexp.MustCompile(`^第([1-9][0-9]*)季$`)

// The official detail-page selector is a plan feed. Keep its exact
// discriminator and session fields; only show_type=547 contains season rows.
func hongguoAppSeasonQuery(seriesID string) url.Values {
	return url.Values{
		"from": {"single_col_same_ip_series"},
		"scene": {"10"},
		"book_id": {seriesID},
		"session_uuid": {uuidLike()},
		"user_is_login": {"0"},
		"filter_ids": {seriesID},
		"cdid": {uuidLike()},
		"openudid": {randomHex(8)},
		"dragon_device_type": {"phone"},
		"pv_player": {"74332"},
		"compliance_status": {"0"},
		"need_personal_recommend": {"1"},
		"player_so_load": {"1"},
		"is_android_pad_screen": {"0"},
	}
}

func hongguoAppNextSeasonQuery(seriesID string) url.Values {
	query := hongguoAppSeasonQuery(seriesID)
	query.Set("from", "series_short_play_info")
	query.Set("session_uuid", uuidLike())
	return query
}

func hongguoNeedsSeasonFallback(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	if strings.Contains(message, "HTTP 444") || strings.Contains(message, "HTTP 403") || strings.Contains(message, "HTTP 429") {
		return false
	}
	return strings.Contains(message, "101116") ||
		strings.Contains(message, "介面未提供有效資料") ||
		strings.Contains(message, "介面未返回有效資料") ||
		strings.Contains(message, "接口未返回有效資料")
}

func hongguoShouldUseCurrentSeasonFallback(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return hongguoRegistrationWasRateLimited(err) ||
		strings.Contains(message, "101116") ||
		strings.Contains(message, "介面未提供有效資料") ||
		strings.Contains(message, "介面未返回有效資料") ||
		strings.Contains(message, "接口未返回有效資料")
}
func (downloader *Downloader) hongguoAppSeasons(
	ctx context.Context,
	seriesID string,
) ([]nativeDramaSeason, error) {
	return downloader.fetchHongguoSDKSeasons(ctx, seriesID)
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
		if showType := mapString(block, "show_type"); showType != "" && showType != hongguoShowTypeSeriesSeasons {
			continue
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

func hongguoRegistrationWasRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), "紅果設備註冊 HTTP 444")
}

// When the official same-series endpoint is temporarily blocked at device
// registration (HTTP 444), keep the already-loaded current work available.
// This is a one-item fallback only; never infer or fabricate sibling seasons.
func hongguoCurrentSeasonFallback(drama nativeDrama) []nativeDramaSeason {
	label := "目前作品"
	if drama.SeriesSeasonNumber > 0 {
		unit := drama.SeriesSeasonUnit
		if unit != "部" {
			unit = "季"
		}
		label = fmt.Sprintf("第%d%s", drama.SeriesSeasonNumber, unit)
	}
	return []nativeDramaSeason{{
		Drama: drama,
		Number: drama.SeriesSeasonNumber,
		Label: label,
		Subtitle: "紅果季別資料暫不可用，先保留目前作品",
		Playable: drama.Episodes > 0,
		Upcoming: false,
	}}
}

func (engine *nativeEngine) nativeSeasons(
	ctx context.Context,
	drama nativeDrama,
) (map[string]any, error) {
	source, sourceID, valid := splitProviderDramaID(drama.ID)
	if !valid {
		return map[string]any{"items": []nativeDramaSeason{}}, nil
	}
	switch source {
	case sourceHongguo:
		items, err := engine.downloader.hongguoAppSeasons(ctx, sourceID)
		if err != nil {
			if hongguoShouldUseCurrentSeasonFallback(err) {
				return map[string]any{
					"items": hongguoCurrentSeasonFallback(drama),
					"warning": err.Error(),
				}, nil
			}
			return nil, err
		}
		return map[string]any{"items": items}, nil
	case sourceDuoduo:
		items, err := engine.downloader.fetchDuoduoSeriesSeasons(ctx, drama)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	default:
		return map[string]any{"items": []nativeDramaSeason{}}, nil
	}
}
