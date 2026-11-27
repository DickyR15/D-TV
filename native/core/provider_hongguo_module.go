package core

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Versioned data-only contract for a separately maintained, authorized media
// provider. It does not load arbitrary scripts, sign requests, or extract keys.
const hongguoModuleContractVersion = 1

type hongguoModuleStream struct {
	URL     string
	Quality int
	Preview bool
}

type hongguoModuleResult struct {
	Version   int
	SeriesID  string
	VideoID   string
	Duration  time.Duration
	Protected bool
	Streams   []hongguoModuleStream
}

// An authorized provider implementation can be wired here without changing
// the catalog SDK or player. No production provider is registered yet.
type hongguoModuleBackend struct {
	load func(context.Context, hongguoPlaybackRequest) (hongguoModuleResult, error)
}

func (hongguoModuleBackend) name() string { return "independent_module_v1" }

func (b hongguoModuleBackend) resolve(ctx context.Context, request hongguoPlaybackRequest) (providerMedia, error) {
	if err := ctx.Err(); err != nil {
		return providerMedia{}, err
	}
	if b.load == nil {
		return providerMedia{}, errors.New("尚未設定授權播放模組")
	}
	result, err := b.load(ctx, request)
	if err != nil {
		return providerMedia{}, err
	}
	if err := ctx.Err(); err != nil {
		return providerMedia{}, err
	}
	return hongguoModuleMedia(request, result)
}

func hongguoModuleMedia(request hongguoPlaybackRequest, result hongguoModuleResult) (providerMedia, error) {
	if result.Version != hongguoModuleContractVersion {
		return providerMedia{}, errors.New("紅果播放模組版本不相容")
	}
	if hongguoExactID(request.seriesID) == "" || hongguoExactID(request.videoID) == "" ||
		result.SeriesID != request.seriesID || result.VideoID != request.videoID {
		return providerMedia{}, errors.New("紅果播放模組回傳的分集不符")
	}
	if result.Protected {
		return providerMedia{}, errors.New("此影片需要平台支援的授權播放環境")
	}
	if result.Duration < 0 || len(result.Streams) > 128 {
		return providerMedia{}, errors.New("紅果播放模組資料無效")
	}
	var variants []providerMedia
	for _, stream := range result.Streams {
		if stream.Preview || stream.Quality < 0 || stream.Quality > 16384 ||
			(stream.Quality > 0 && stream.Quality < 720) {
			continue
		}
		address := strings.TrimSpace(stream.URL)
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" ||
			u.User != nil || u.Fragment != "" || !isProviderHTTPMediaURL(address) {
			continue
		}
		variants = append(variants, providerMedia{
			URL: address, Quality: stream.Quality, Duration: result.Duration,
		})
	}
	sort.SliceStable(variants, func(i, j int) bool { return variants[i].Quality > variants[j].Quality })
	seen := make(map[string]bool, len(variants))
	distinct := variants[:0]
	for _, stream := range variants {
		if !seen[stream.URL] {
			seen[stream.URL] = true
			distinct = append(distinct, stream)
		}
	}
	if len(distinct) == 0 {
		return providerMedia{}, errors.New("紅果播放模組未提供符合條件的完整影片")
	}
	selected := distinct[0]
	selected.Variants = distinct
	return selected, nil
}
