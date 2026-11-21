package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	openhongguo "github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"
)

const (
	hongguoSDKAPIOrigin      = "https://api5-normal-sinfonlinea.fqnovel.com"
	hongguoSDKRegisterOrigin = "https://log5-applog-lq.fqnovel.com"
)

func hongguoHTTPRegistrationHosts() []string {
	hosts := append([]string(nil), hongguoDeviceRegisterHosts()...)
	host := strings.TrimPrefix(hongguoSDKRegisterOrigin, "https://")
	for _, existing := range hosts {
		if strings.EqualFold(existing, host) {
			return hosts
		}
	}
	return append(hosts, host)
}

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

const hongguoSDKProfileFileName = "hongguo-sdk-profile.json"

// hongguoSDKFileProfileStore keeps only the anonymous guest profile in D-TV's
// private data directory. It never stores account credentials or writes profile
// contents to diagnostics.
type hongguoSDKFileProfileStore struct {
	mu   sync.Mutex
	path string
}

func newHongguoSDKProfileStore(directory string) openhongguo.ProfileStore {
	if strings.TrimSpace(directory) == "" {
		return &hongguoSDKMemoryProfileStore{}
	}
	return &hongguoSDKFileProfileStore{
		path: filepath.Join(directory, hongguoSDKProfileFileName),
	}
}

func (s *hongguoSDKFileProfileStore) Load(ctx context.Context) (*openhongguo.DeviceProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, openhongguo.ErrProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("讀取紅果匿名設備檔案失敗：%w", err)
	}
	var profile openhongguo.DeviceProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("紅果匿名設備檔案格式無效：%w", err)
	}
	return cloneHongguoSDKProfile(&profile), nil
}

func (s *hongguoSDKFileProfileStore) Save(ctx context.Context, profile *openhongguo.DeviceProfile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if profile == nil {
		return errors.New("紅果 SDK device profile 不可為空")
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("序列化紅果匿名設備檔案失敗：%w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("建立紅果匿名設備目錄失敗：%w", err)
	}
	file, err := os.CreateTemp(directory, ".hongguo-sdk-profile-*")
	if err != nil {
		return fmt.Errorf("建立紅果匿名設備暫存檔失敗：%w", err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return fmt.Errorf("設定紅果匿名設備檔案權限失敗：%w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("寫入紅果匿名設備檔案失敗：%w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("同步紅果匿名設備檔案失敗：%w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("關閉紅果匿名設備暫存檔失敗：%w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		// Windows may not replace an existing destination with Rename.
		if removeErr := os.Remove(s.path); removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
			if retryErr := os.Rename(tempPath, s.path); retryErr == nil {
				return nil
			} else {
				return fmt.Errorf("替換紅果匿名設備檔案失敗：%w", retryErr)
			}
		}
		return fmt.Errorf("保存紅果匿名設備檔案失敗：%w", err)
	}
	return nil
}

func (s *hongguoSDKFileProfileStore) Reset(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("清除紅果匿名設備檔案失敗：%w", err)
	}
	return nil
}

func (downloader *Downloader) hongguoSDKClient() (*openhongguo.Client, error) {
	downloader.hongguoSDKOnce.Do(func() {
		if downloader.client == nil {
			downloader.hongguoSDKSession.err = errors.New("紅果 SDK 無法使用：HTTP client 尚未初始化")
			return
		}
		profiles := newHongguoSDKProfileStore(downloader.cfg.dataDir)
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

func hongguoSDKPlaybackRegistrationRejected(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, openhongguo.ErrAuthenticationRequired) ||
		errors.Is(err, openhongguo.ErrDeviceRejected) ||
		errors.Is(err, openhongguo.ErrRateLimited) {
		return true
	}
	var apiErr *openhongguo.APIError
	return errors.As(err, &apiErr) &&
		(apiErr.HTTPStatus == http.StatusUnauthorized ||
			apiErr.HTTPStatus == http.StatusForbidden ||
			apiErr.HTTPStatus == http.StatusTooManyRequests ||
			apiErr.HTTPStatus == 444)
}

func hongguoSDKCodecRank(codec string) int {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc", "avc1":
		return 3
	case "hevc", "h265", "hvc1", "hev1", "bytevc1":
		return 2
	case "", "mp4", "mpeg":
		return 1
	default:
		return 0
	}
}

func hongguoSDKPlaybackToNativeMedia(playback *model.Playback) (providerMedia, error) {
	if playback == nil || len(playback.Streams) == 0 {
		return providerMedia{}, errors.New("紅果 SDK 沒有回傳可用的播放串流")
	}
	protected := playback.Protected
	hasH266 := false
	for _, stream := range playback.Streams {
		protected = protected || stream.Protected
		if strings.Contains(strings.ToLower(stream.Codec), "bytevc2") {
			hasH266 = true
		}
	}
	var cencKey []byte
	if protected {
		contentKey, err := openhongguo.ContentKey(playback)
		if err != nil {
			return providerMedia{}, fmt.Errorf("紅果 SDK CENC 金鑰解析失敗：%w", err)
		}
		cencKey, err = hex.DecodeString(contentKey.Hex)
		if err != nil || len(cencKey) != 16 {
			return providerMedia{}, errors.New("紅果 SDK CENC 金鑰格式無效")
		}
	}
	duration := time.Duration(max(playback.DurationSeconds, 0)) * time.Second
	type scored struct {
		media providerMedia
		score int
	}
	var choices []scored
	seen := map[string]bool{}
	for _, stream := range playback.Streams {
		codecRank := hongguoSDKCodecRank(stream.Codec)
		if codecRank == 0 || strings.Contains(strings.ToLower(stream.Codec), "bytevc2") {
			continue
		}
		definition := firstNonEmpty(stream.Definition, stream.Quality)
		definitionHeight, _ := strconv.Atoi(hongguoQualityNumber.FindString(definition))
		quality := hongguoMediaShortEdgeQuality(stream.Width, stream.Height, definitionHeight)
		addresses := []string{stream.URL, stream.BackupURL}
		for _, rawAddress := range addresses {
			address := strings.TrimSpace(rawAddress)
			if address == "" || seen[address] || !isProviderHTTPMediaURL(address) {
				continue
			}
			seen[address] = true
			media := providerMedia{
				URL: address, Referer: "https://novelquickapp.com/",
				Duration: duration, Quality: quality,
				CENCKey: append([]byte(nil), cencKey...),
			}
			score := quality*100 + codecRank
			choices = append(choices, scored{media: media, score: score})
		}
	}
	if len(choices) == 0 {
		if hasH266 {
			return providerMedia{}, errHongguoH266Only
		}
		return providerMedia{}, errors.New("紅果 SDK 回傳的播放網址或編碼不受支援")
	}
	sort.SliceStable(choices, func(i, j int) bool { return choices[i].score > choices[j].score })
	selected := choices[0].media
	for _, choice := range choices {
		selected.Variants = append(selected.Variants, choice.media)
	}
	return selected, nil
}

func (downloader *Downloader) resolveHongguoSDKMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) || videoID == "0" {
		return providerMedia{}, errors.New("紅果 SDK 播放集數 ID 無效")
	}
	client, err := downloader.hongguoSDKClient()
	if err != nil {
		return providerMedia{}, fmt.Errorf("紅果播放 SDK 初始化失敗：%w", err)
	}
	playback, err := client.ResolvePlayback(ctx, videoID, openhongguo.PlaybackOptions{})
	if err != nil && !errors.Is(err, openhongguo.ErrDRMProtected) {
		return providerMedia{}, fmt.Errorf("紅果 SDK V5 播放請求失敗：%w", err)
	}
	media, mapErr := hongguoSDKPlaybackToNativeMedia(playback)
	if mapErr != nil {
		return providerMedia{}, mapErr
	}
	downloader.recordDiagnostic(diagnosticEvent{
		Level: "info", Event: "hongguo.sdk_playback_success",
		Source: sourceHongguo, Message: fmt.Sprintf("streams=%d protected=%t", len(media.Variants), playback.Protected),
	})
	return media, nil
}
