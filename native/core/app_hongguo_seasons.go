package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type nativeDramaSeason struct {
	Drama    nativeDrama `json:"drama"`
	Number   int         `json:"number"`
	Label    string      `json:"label"`
	Subtitle string      `json:"subtitle,omitempty"`
	Upcoming bool        `json:"upcoming"`
	Playable bool        `json:"playable"`
}

// hongguoAppSeasons uses the current anonymous Redfruit SDK protocol for
// registration, signed plan-v1 season lookup, and its official multi-season
// fallbacks. Do not restore the retired hand-built App request here.
func (downloader *Downloader) hongguoAppSeasons(
	ctx context.Context,
	seriesID string,
) ([]nativeDramaSeason, error) {
	return downloader.fetchHongguoSDKSeasons(ctx, seriesID)
}

func hongguoRegistrationWasRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP 444")
}

func hongguoShouldUseCurrentSeasonFallback(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return hongguoRegistrationWasRateLimited(err) ||
		strings.Contains(message, "http 404") ||
		strings.Contains(message, "anonymous response was empty") ||
		strings.Contains(message, "device rejected") ||
		strings.Contains(message, "101116")
}

// When the official season relationship is temporarily unavailable, keep
// the already-known current season visible. Never infer or fabricate siblings.
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
		Drama:    drama,
		Number:   drama.SeriesSeasonNumber,
		Label:    label,
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
					"items":   hongguoCurrentSeasonFallback(drama),
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
