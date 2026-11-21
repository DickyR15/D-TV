package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	openhongguo "github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"
)

const (
	hongguoSDKAPIOrigin      = "https://api5-normal-sinfonlinea.fqnovel.com"
	hongguoSDKRegisterOrigin = "https://log5-applog-lq.fqnovel.com"
)

type hongguoSDKClientSession struct {
	client *openhongguo.Client
	err    error
}

type hongguoSDKTransport struct {
	client *http.Client
}

func (t hongguoSDKTransport) Do(ctx context.Context, input *openhongguo.Request) (*openhongguo.Response, error) {
	if t.client == nil {
		return nil, errors.New("紅果 SDK HTTP client 尚未初始化")
	}
	if input == nil {
		return nil, errors.New("紅果 SDK request 不可為空")
	}
	method := strings.TrimSpace(input.Method)
	if method == "" {
		method = http.MethodGet
	}
	request, err := http.NewRequestWithContext(ctx, method, input.URL, bytes.NewReader(input.Body))
	if err != nil {
		return nil, err
	}
	for name, value := range input.Headers {
		request.Header.Set(name, value)
	}
	response, err := t.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	const maxResponseBytes = 8 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("紅果 SDK response 超過大小限制")
	}
	headers := make(map[string]string, len(response.Header))
	for name, values := range response.Header {
		if len(values) == 0 || strings.EqualFold(name, "Set-Cookie") || strings.EqualFold(name, "Cookie") {
			continue
		}
		headers[name] = values[0]
	}
	cookies := make(map[string]string)
	for _, cookie := range response.Cookies() {
		if cookie.Name != "" {
			cookies[cookie.Name] = cookie.Value
		}
	}
	return &openhongguo.Response{
		StatusCode: response.StatusCode,
		Headers:    headers,
		Body:       body,
		Cookies:    cookies,
	}, nil
}

type hongguoSDKMemoryProfileStore struct {
	mu      sync.Mutex
	profile *openhongguo.DeviceProfile
}

func cloneHongguoSDKProfile(profile *openhongguo.DeviceProfile) *openhongguo.DeviceProfile {
	if profile == nil {
		return nil
	}
	cloned := *profile
	if profile.Device != nil {
		device := *profile.Device
		cloned.Device = &device
	}
	return &cloned
}

func (s *hongguoSDKMemoryProfileStore) Load(ctx context.Context) (*openhongguo.DeviceProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.profile == nil {
		return nil, openhongguo.ErrProfileNotFound
	}
	return cloneHongguoSDKProfile(s.profile), nil
}

func (s *hongguoSDKMemoryProfileStore) Save(ctx context.Context, profile *openhongguo.DeviceProfile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if profile == nil {
		return errors.New("紅果 SDK device profile 不可為空")
	}
	s.mu.Lock()
	s.profile = cloneHongguoSDKProfile(profile)
	s.mu.Unlock()
	return nil
}

func (s *hongguoSDKMemoryProfileStore) Reset(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.profile = nil
	s.mu.Unlock()
	return nil
}

func (downloader *Downloader) hongguoSDKClient() (*openhongguo.Client, error) {
	downloader.hongguoSDKOnce.Do(func() {
		if downloader.client == nil {
			downloader.hongguoSDKSession.err = errors.New("紅果 SDK 無法使用：HTTP client 尚未初始化")
			return
		}
		profiles := &hongguoSDKMemoryProfileStore{}
		// Keep the SDK's captured 7.3.9.32 app identity and its matching
		// default device together; do not mix it with D-TV's 7.4.3.32 profile.
		device := model.DefaultDevice
		client, err := openhongguo.NewClient(openhongguo.Options{
			Transport:     hongguoSDKTransport{client: downloader.client},
			ProfileStore:  profiles,
			APIOrigin:     hongguoSDKAPIOrigin,
			RegisterOrigin: hongguoSDKRegisterOrigin,
			Device:        device,
			RetryAttempts: 1,
			DeviceResets:  openhongguo.Int(0),
		})
		downloader.hongguoSDKSession.client = client
		downloader.hongguoSDKSession.err = err
	})
	return downloader.hongguoSDKSession.client, downloader.hongguoSDKSession.err
}

func hongguoSDKStatusText(status model.SeriesStatus) string {
	switch status {
	case model.SeriesEnd:
		return "已完結"
	case model.SeriesUpdating:
		return "更新中"
	case model.SeriesUpdatedToday:
		return "今日更新"
	case model.SeriesUpdateStopped:
		return "已停止更新"
	default:
		return ""
	}
}

func hongguoSDKAlbumToNativeSeasons(album *model.Album) ([]nativeDramaSeason, error) {
	if album == nil {
		return nil, errors.New("紅果 SDK 未返回系列季別資料")
	}
	items := make([]nativeDramaSeason, 0, len(album.Seasons))
	seen := make(map[string]bool, len(album.Seasons))
	for _, season := range album.Seasons {
		seriesID := strings.TrimSpace(season.SeriesID)
		title := strings.TrimSpace(season.Title)
		if !hongguoNumericID.MatchString(seriesID) || seriesID == "0" || title == "" || seen[seriesID] {
			continue
		}
		seen[seriesID] = true

		row := map[string]any{
			"series_id":   seriesID,
			"title":       title,
			"series_cover": season.CoverURL,
			"episode_cnt": season.EpisodeCount,
		}
		drama := hongguoDramaFromAny(map[string]any{"video_data": row}, "短劇")
		drama.ID = "hongguo:" + seriesID
		drama.Source = sourceHongguo
		drama.SourceID = seriesID
		drama.Title = title
		if season.EpisodeCount > 0 {
			drama.Episodes = season.EpisodeCount
			drama.EpisodeCount = season.EpisodeCount
			drama.TotalEpisode = season.EpisodeCount
			drama.EpisodesVerified = true
		} else {
			drama.Episodes = 0
			drama.EpisodesVerified = false
		}

		number := season.Index
		if number <= 0 {
			number = len(items) + 1
		}
		label := fmt.Sprintf("第%d季", number)
		drama.SeriesSeasonNumber = number
		drama.SeriesSeasonUnit = "季"
		items = append(items, nativeDramaSeason{
			Drama:    nativeNormalize(drama),
			Number:   number,
			Label:    label,
			Subtitle: hongguoSDKStatusText(season.Status),
			Playable: season.EpisodeCount > 0,
			Upcoming: season.EpisodeCount <= 0,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Number != items[j].Number {
			return items[i].Number < items[j].Number
		}
		return items[i].Drama.ID < items[j].Drama.ID
	})
	if len(items) == 0 {
		return []nativeDramaSeason{}, nil
	}

	// One official series favorite is shared by all seasons. Choose the lowest
	// numbered official season ID as the stable group key.
	groupID := ""
	for _, item := range items {
		if item.Number > 0 {
			_, sourceID, valid := splitProviderDramaID(item.Drama.ID)
			if valid {
				groupID = sourceID
				break
			}
		}
	}
	if groupID == "" {
		_, groupID, _ = splitProviderDramaID(items[0].Drama.ID)
	}
	if groupID != "" {
		for i := range items {
			items[i].Drama.SeriesGroupID = groupID
		}
	}
	return items, nil
}

func (downloader *Downloader) fetchHongguoSDKSeasons(ctx context.Context, seriesID string) ([]nativeDramaSeason, error) {
	if !hongguoNumericID.MatchString(seriesID) || seriesID == "0" {
		return nil, errors.New("紅果系列 ID 無效")
	}
	client, err := downloader.hongguoSDKClient()
	if err != nil {
		return nil, fmt.Errorf("紅果系列季別 SDK 初始化失敗：%w", err)
	}
	album, err := client.Seasons(ctx, seriesID)
	if err != nil {
		return nil, fmt.Errorf("紅果系列季別 API 失敗：%w", err)
	}
	return hongguoSDKAlbumToNativeSeasons(album)
}
