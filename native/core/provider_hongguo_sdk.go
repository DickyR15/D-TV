package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	openhongguo "github.com/beilunyang/openhongguo/sdk"
	"github.com/beilunyang/openhongguo/sdk/model"
)

const (
	hongguoSDKAPIOrigin       = "https://api5-normal-sinfonlinea.fqnovel.com"
	hongguoSDKRegisterOrigin  = "https://log5-applog-lq.fqnovel.com"
	hongguoSDKRegisterPause   = 10 * time.Minute
	hongguoSDKPlaybackPause   = 30 * time.Second
)

var (
	errHongguoSDKRegistrationCooling = errors.New("紅果 SDK 裝置註冊冷卻中")
	errHongguoSDKPlaybackCooling     = errors.New("紅果 SDK 播放空回應冷卻中")
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
	client                *openhongguo.Client
	err                   error
	mu                    sync.Mutex
	registerPausedUntil   time.Time
	registerPauseStatus   int
	playbackPausedUntil   time.Time
	playbackGate          chan struct{}
}

// Serialize SDK playback requests for this shared session. A queued request
// must observe a preceding request's rejection/cooldown before contacting
// the provider. Waiting is context-aware so cancelled preloads do not block
// the foreground player.
func (session *hongguoSDKClientSession) acquirePlayback(ctx context.Context) (func(), error) {
	session.mu.Lock()
	if session.playbackGate == nil {
		session.playbackGate = make(chan struct{}, 1)
	}
	gate := session.playbackGate
	session.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case gate <- struct{}{}:
	}
	if err := ctx.Err(); err != nil {
		<-gate
		return nil, err
	}
	return func() { <-gate }, nil
}

func hongguoSDKBuildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range info.Deps {
			if dependency.Path == "github.com/beilunyang/openhongguo" {
				if dependency.Replace != nil {
					return dependency.Replace.Path + "@" + dependency.Replace.Version
				}
				return dependency.Version
			}
		}
	}
	return "unavailable"
}

func (session *hongguoSDKClientSession) recordRegistrationStatus(status int) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		session.registerPausedUntil = time.Time{}
		session.registerPauseStatus = 0
		return
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusTooManyRequests, 444:
		session.registerPausedUntil = time.Now().Add(hongguoSDKRegisterPause)
		session.registerPauseStatus = status
	}
}

func (session *hongguoSDKClientSession) registrationCooldownError() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.registerPausedUntil.IsZero() || !time.Now().Before(session.registerPausedUntil) {
		session.registerPausedUntil = time.Time{}
		session.registerPauseStatus = 0
		return nil
	}
	return fmt.Errorf("%w：HTTP %d，暫停重試 10 分鐘",
		errHongguoSDKRegistrationCooling, session.registerPauseStatus)
}

func (downloader *Downloader) hongguoSDKRegistrationCooldownError() error {
	return downloader.hongguoSDKSession.registrationCooldownError()
}

func (session *hongguoSDKClientSession) recordPlaybackResult(err error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if errors.Is(err, openhongguo.ErrAnonymousLimited) {
		session.playbackPausedUntil = time.Now().Add(hongguoSDKPlaybackPause)
		return
	}
	// A cancellation or unrelated failure must not clear an active
	// provider rejection. Expiry is handled by playbackCooldownError.
	if err == nil {
		session.playbackPausedUntil = time.Time{}
	}
}

func (session *hongguoSDKClientSession) playbackCooldownError() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	remaining := time.Until(session.playbackPausedUntil)
	if session.playbackPausedUntil.IsZero() || remaining <= 0 {
		session.playbackPausedUntil = time.Time{}
		return nil
	}
	seconds := int((remaining + time.Second - 1) / time.Second)
	return fmt.Errorf("%w：先前請求收到空回應，本次未發送播放請求，請於 %d 秒後重試",
		errHongguoSDKPlaybackCooling, seconds)
}

// The pinned SDK accepts absent Content-Encoding or gzip, but HTTP can
// explicitly return identity. Normalize only equivalent representation
// metadata; never change the signed request or the response payload.
func hongguoSDKResponseEncoding(value string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "", "identity":
		return ""
	case "gzip":
		return "gzip"
	default:
		return value
	}
}

// Describe response shape without exposing media URLs, identifiers,
// credentials, or response body content.
func hongguoSDKPlaybackResponseShape(body []byte, encoding string) string {
	if len(body) == 0 {
		return "wire_empty"
	}
	if hongguoSDKResponseEncoding(encoding) != "" {
		return "encoded_body"
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return "whitespace_only"
	}
	if bytes.Equal(body, []byte("null")) {
		return "json_null"
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return "non_object_or_invalid_json"
	}
	if len(object) == 0 {
		return "empty_json_object"
	}
	if data, ok := object["data"]; ok {
		data = bytes.TrimSpace(data)
		if bytes.Equal(data, []byte("null")) || bytes.Equal(data, []byte("{}")) || bytes.Equal(data, []byte("[]")) {
			return "empty_data"
		}
		return "data_present"
	}
	return "data_not_present"
}

func (downloader *Downloader) recordHongguoSDKPlaybackResponse(
	method, host, path string, status, responseBytes int, contentType, contentEncoding, responseShape string,
) {
	responseType := strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	level := "info"
	result := "received"
	if status == http.StatusOK && responseBytes == 0 {
		level, result = "warn", "empty_response"
	} else if status >= 400 {
		level, result = "warn", "http_error"
	}
	downloader.recordDiagnostic(diagnosticEvent{
		Level: level, Event: "hongguo.sdk_playback_http_response",
		Source: sourceHongguo, Host: host, HTTPStatus: status, ResponseType: responseType,
		Message: fmt.Sprintf(
			"method=%s path=%s response_bytes=%d content_encoding=%s result=%s response_shape=%s",
			method, path, responseBytes, strings.TrimSpace(contentEncoding), result, responseShape,
		),
	})
}

// Classify actual transport failures without recording request URLs,
// credentials, headers, response bodies, or raw error strings.
func hongguoSDKTransportFailureKind(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var invalidCert x509.CertificateInvalidError
	var recordErr tls.RecordHeaderError
	var netErr net.Error
	var opErr *net.OpError
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.As(err, &dnsErr):
		return "dns"
	case errors.As(err, &certErr), errors.As(err, &unknownAuthority), errors.As(err, &hostnameErr), errors.As(err, &invalidCert):
		return "tls_certificate"
	case errors.As(err, &recordErr):
		return "tls_protocol"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	case errors.As(err, &opErr):
		return "connection"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection_closed"
	default:
		return "transport"
	}
}

func (downloader *Downloader) recordHongguoSDKTransportFailure(host, stage, kind string, elapsed time.Duration) {
	downloader.recordDiagnostic(diagnosticEvent{
		Level: "warn", Event: "hongguo.sdk_transport_failure",
		Source: sourceHongguo, Host: host,
		Message: fmt.Sprintf("stage=%s kind=%s elapsed_ms=%d", stage, kind, elapsed.Milliseconds()),
	})
}

type hongguoSDKTransport struct {
	client               *http.Client
	onRegistrationStatus func(int)
	onTransportFailure   func(host, stage, kind string, elapsed time.Duration)
	onPlaybackResponse   func(method, host, path string, status, responseBytes int, contentType, contentEncoding, responseShape string)
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
	// The SDK expects raw response bytes and handles Content-Encoding itself.
	// Prevent net/http from adding gzip and transparently decoding the body
	// when the SDK did not specify an encoding. Preserve explicit SDK headers.
	if request.Header.Get("Accept-Encoding") == "" {
		request.Header.Set("Accept-Encoding", "identity")
	}
	started := time.Now()
	reportFailure := func(stage string, err error) {
		if t.onTransportFailure != nil {
			t.onTransportFailure(request.URL.Hostname(), stage, hongguoSDKTransportFailureKind(err), time.Since(started))
		}
	}
	response, err := t.client.Do(request)
	if err != nil {
		reportFailure("request", err)
		return nil, err
	}
	defer response.Body.Close()
	if input.URL != "" && t.onRegistrationStatus != nil &&
		request.URL.Hostname() == "log5-applog-lq.fqnovel.com" &&
		request.URL.Path == hongguoDeviceRegisterPath {
		t.onRegistrationStatus(response.StatusCode)
	}

	const maxResponseBytes = 8 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		reportFailure("response_body", err)
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
		if strings.EqualFold(name, "Content-Encoding") {
			if encoding := hongguoSDKResponseEncoding(values[0]); encoding != "" {
				headers[name] = encoding
			}
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
	if t.onPlaybackResponse != nil &&
		request.URL.Hostname() == "api5-normal-sinfonlinea.fqnovel.com" &&
		strings.HasPrefix(request.URL.Path, "/novel/player/") {
		t.onPlaybackResponse(
			method, request.URL.Hostname(), request.URL.Path,
			response.StatusCode, len(body),
			response.Header.Get("Content-Type"), response.Header.Get("Content-Encoding"),
			hongguoSDKPlaybackResponseShape(body, response.Header.Get("Content-Encoding")),
		)
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
		// Keep the SDK's default configuration separate from the legacy
		// D-TV profile. Report the linked module, not the legacy app label.
		device := model.DefaultDevice
		downloader.recordDiagnostic(diagnosticEvent{
			Level: "info", Event: "hongguo.sdk_build_version", Source: sourceHongguo,
			Message: "module_version=" + hongguoSDKBuildVersion(),
		})
		client, err := openhongguo.NewClient(openhongguo.Options{
			Transport: hongguoSDKTransport{
				client: downloader.client,
				onRegistrationStatus: downloader.hongguoSDKSession.recordRegistrationStatus,
				onPlaybackResponse: downloader.recordHongguoSDKPlaybackResponse,
				onTransportFailure: downloader.recordHongguoSDKTransportFailure,
			},
			ProfileStore:  profiles,
			APIOrigin:     hongguoSDKAPIOrigin,
			RegisterOrigin: hongguoSDKRegisterOrigin,
			Device:        device,
			RetryAttempts: 1,
			// Repeated empty playback responses persisted across fresh devices.
			// Keep a single guest identity instead of rotating it on rejection.
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

// Convert the SDK's typed list without rounding provider identifiers or
// substituting another season's episode. Preserve unavailable episodes as
// non-playable entries, including their original episode numbers.
func hongguoSDKEpisodesToChapters(seriesID string, episodes []model.Episode) ([]Chapter, error) {
	if hongguoExactID(seriesID) == "" || len(episodes) == 0 {
		return nil, errors.New("紅果 SDK 未提供有效分集")
	}
	chapters := make([]Chapter, 0, len(episodes))
	numbers := make(map[int]bool, len(episodes))
	for _, episode := range episodes {
		id := hongguoExactID(episode.ID)
		if id == "" || episode.SeriesID != seriesID || episode.Number < 1 || numbers[episode.Number] {
			return nil, errors.New("紅果 SDK 分集識別資料無效或重複")
		}
		numbers[episode.Number] = true
		title := firstNonEmpty(episode.Title, fmt.Sprintf("第%d集", episode.Number))
		chapter := Chapter{
			ID: providerChapterID(sourceHongguo, seriesID, id),
			Source: sourceHongguo,
			Title: title,
			CurrentEpisode: rawEpisode(episode.Number),
			VIP: !episode.Playable,
		}
		if episode.Playable {
			chapter.VideoURL = "hongguo-cenc://" + id
		}
		if episode.Vertical {
			vertical := true
			chapter.Vertical = &vertical
		}
		chapters = append(chapters, chapter)
	}
	sort.Slice(chapters, func(i, j int) bool {
		return episodeNumberForHongguoSDK(chapters[i]) < episodeNumberForHongguoSDK(chapters[j])
	})
	if !hongguoChaptersValid(seriesID, chapters) {
		return nil, errors.New("紅果 SDK 分集影片 ID 無效或重複")
	}
	return chapters, nil
}

func episodeNumberForHongguoSDK(chapter Chapter) int {
	number, _ := strconv.Atoi(chapter.EpisodeString(0))
	return number
}

func (downloader *Downloader) fetchHongguoSDKEpisodes(ctx context.Context, seriesID string) ([]Chapter, error) {
	client, err := downloader.hongguoSDKClient()
	if err != nil {
		return nil, err
	}
	if err := downloader.hongguoSDKRegistrationCooldownError(); err != nil {
		return nil, err
	}
	page, err := client.ListEpisodes(ctx, seriesID, openhongguo.EpisodeOptions{})
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, errors.New("紅果 SDK 未回傳分集")
	}
	chapters, err := hongguoSDKEpisodesToChapters(seriesID, page.Items)
	if err == nil {
		downloader.recordDiagnostic(diagnosticEvent{
			Level: "info", Event: "hongguo.sdk_episode_list", Source: sourceHongguo,
			Message: fmt.Sprintf("episodes=%d", len(chapters)),
		})
	}
	return chapters, err
}

func (downloader *Downloader) fetchHongguoSDKSeasons(ctx context.Context, seriesID string) ([]nativeDramaSeason, error) {
	if !hongguoNumericID.MatchString(seriesID) || seriesID == "0" {
		return nil, errors.New("紅果系列 ID 無效")
	}
	client, err := downloader.hongguoSDKClient()
	if err != nil {
		return nil, fmt.Errorf("紅果系列季別 SDK 初始化失敗：%w", err)
	}
	if err := downloader.hongguoSDKRegistrationCooldownError(); err != nil {
		return nil, err
	}
	album, err := client.Seasons(ctx, seriesID)
	if err != nil {
		return nil, fmt.Errorf("紅果系列季別 API 失敗：%w", err)
	}
	return hongguoSDKAlbumToNativeSeasons(album)
}

// hongguoSDKPlaybackRegistrationRejected also covers anonymous empty responses:
// after both SDK playback endpoints return empty data, the legacy App paths
 // only repeat the retired registration flow and cannot provide a new stream.
func hongguoSDKPlaybackRegistrationRejected(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, openhongguo.ErrAuthenticationRequired) ||
		errors.Is(err, openhongguo.ErrDeviceRejected) ||
		errors.Is(err, openhongguo.ErrChallengeRequired) ||
		errors.Is(err, openhongguo.ErrSignatureRejected) ||
		errors.Is(err, openhongguo.ErrRateLimited) ||
		errors.Is(err, openhongguo.ErrAnonymousLimited) {
		return true
	}
	var apiErr *openhongguo.APIError
	return errors.Is(err, errHongguoSDKRegistrationCooling) ||
		errors.Is(err, errHongguoSDKPlaybackCooling) ||
		(errors.As(err, &apiErr) &&
			(apiErr.HTTPStatus == http.StatusUnauthorized ||
				apiErr.HTTPStatus == http.StatusForbidden ||
				apiErr.HTTPStatus == http.StatusNotFound ||
				apiErr.HTTPStatus == http.StatusTooManyRequests ||
				apiErr.HTTPStatus == 444))
}

// Both response parsers accept standard codec aliases, including RFC 6381
// profile-qualified values. Unsupported VVC must not reach the player.
func hongguoCodecIsH266(codec string) bool {
	codec = strings.ToLower(strings.TrimSpace(codec))
	return strings.Contains(codec, "bytevc2") || codec == "h266" ||
		codec == "h.266" || codec == "vvc" || codec == "vvc1" || codec == "vvi1" ||
		strings.HasPrefix(codec, "vvc1.") || strings.HasPrefix(codec, "vvi1.")
}

func hongguoSDKCodecRank(codec string) int {
	codec = strings.ToLower(strings.TrimSpace(codec))
	if hongguoCodecIsH266(codec) {
		return 0
	}
	switch codec {
	case "h264", "h.264", "avc", "avc1", "avc3":
		return 3
	case "hevc", "h265", "h.265", "hvc1", "hev1", "bytevc1":
		return 2
	case "", "mp4", "mpeg":
		return 1
	}
	if strings.HasPrefix(codec, "avc1.") || strings.HasPrefix(codec, "avc3.") {
		return 3
	}
	if strings.HasPrefix(codec, "hvc1.") || strings.HasPrefix(codec, "hev1.") {
		return 2
	}
	return 0
}

func hongguoSDKPlaybackToNativeMedia(playback *model.Playback) (providerMedia, error) {
	if playback == nil || len(playback.Streams) == 0 {
		return providerMedia{}, errors.New("紅果 SDK 沒有回傳可用的播放串流")
	}
	protected := playback.Protected
	hasH266 := false
	for _, stream := range playback.Streams {
		protected = protected || stream.Protected
		if hongguoCodecIsH266(stream.Codec) {
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
	for _, stream := range playback.Streams {
		codecRank := hongguoSDKCodecRank(stream.Codec)
		if codecRank == 0 {
			continue
		}
		definition := firstNonEmpty(stream.Definition, stream.Quality)
		definitionHeight, _ := strconv.Atoi(hongguoQualityNumber.FindString(definition))
		quality := hongguoMediaShortEdgeQuality(stream.Width, stream.Height, definitionHeight)
		addresses := []string{stream.URL, stream.BackupURL}
		for _, rawAddress := range addresses {
			address := strings.TrimSpace(rawAddress)
			if address == "" || !isProviderHTTPMediaURL(address) {
				continue
			}
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
	// Deduplicate after sorting: a URL repeated in multiple model entries
	// must retain its best supported quality rather than the first entry.
	selected := choices[0].media
	seen := make(map[string]bool)
	for _, choice := range choices {
		if seen[choice.media.URL] {
			continue
		}
		seen[choice.media.URL] = true
		selected.Variants = append(selected.Variants, choice.media)
	}
	return selected, nil
}

func (downloader *Downloader) resolveHongguoSDKMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) || videoID == "0" {
		return providerMedia{}, errors.New("紅果 SDK 播放集數 ID 無效")
	}
	release, err := downloader.hongguoSDKSession.acquirePlayback(ctx)
	if err != nil {
		return providerMedia{}, err
	}
	defer release()
	if err := downloader.hongguoSDKRegistrationCooldownError(); err != nil {
		return providerMedia{}, err
	}
	if err := downloader.hongguoSDKSession.playbackCooldownError(); err != nil {
		return providerMedia{}, err
	}
	client, err := downloader.hongguoSDKClient()
	if err != nil {
		return providerMedia{}, fmt.Errorf("紅果播放 SDK 初始化失敗：%w", err)
	}
	downloader.recordDiagnostic(diagnosticEvent{
		Level: "info", Event: "hongguo.sdk_playback_request_start", Source: sourceHongguo,
		Message: "caller=" + hongguoPlaybackRequestOrigin(ctx),
	})
	playback, err := client.ResolvePlayback(ctx, videoID, openhongguo.PlaybackOptions{})
	downloader.hongguoSDKSession.recordPlaybackResult(err)
	if errors.Is(err, openhongguo.ErrAnonymousLimited) {
		downloader.recordDiagnostic(diagnosticEvent{
			Level: "warn", Event: "hongguo.sdk_playback_soft_reject",
			Source: sourceHongguo,
			Message: "anonymous playback returned empty data; suppressing retries for 30 seconds; caller=" + hongguoPlaybackRequestOrigin(ctx),
		})
	}
	if err != nil {
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
