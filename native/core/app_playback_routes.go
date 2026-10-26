package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
)

type nativePlaybackChoice struct {
	danmakuSeries    string
	danmakuVideo     string
	media            []providerMedia
	index            int
	qualities        []int
	streamSession    string
	created          time.Time
	task             Task
	requestedQuality int
	earlyRefreshUsed bool
}

// After three consecutive failed routes, resolve fresh signed URLs once but
// retain every not-yet-tried route from the previous list as a last resort.
const nativeEarlyPlaybackRefreshAfterFailedRoutes = 3

func nativePlaybackChoices(media providerMedia, quality int) nativePlaybackChoice {
	choice := nativePlaybackChoice{qualities: []int{}}
	seen := map[string]bool{}
	qualities := map[int]bool{}
	for _, option := range append([]providerMedia{media}, media.Variants...) {
		if !isProviderHTTPMediaURL(option.URL) {
			continue
		}
		identity := nativePlaybackMediaIdentity(option)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		option.Variants = nil
		choice.media = append(choice.media, option)
		if option.Quality > 0 {
			qualities[option.Quality] = true
		}
	}
	sort.SliceStable(choice.media, func(i, j int) bool { return choice.media[i].Quality > choice.media[j].Quality })
	for value := range qualities {
		choice.qualities = append(choice.qualities, value)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(choice.qualities)))
	if quality > 0 && qualities[quality] {
		filtered := make([]providerMedia, 0, len(choice.media))
		for _, option := range choice.media {
			if option.Quality == quality {
				filtered = append(filtered, option)
			}
		}
		choice.media = filtered
	}
	return choice
}

func (engine *nativeEngine) nativeOpenPlayback(ctx context.Context, choice nativePlaybackChoice) (nativePlan, error) {
	if err := ctx.Err(); err != nil {
		return nativePlan{}, err
	}
	if choice.index < 0 || choice.index >= len(choice.media) {
		return nativePlan{}, errors.New("该集没有其他可用的播放线路")
	}
	media := choice.media[choice.index]
	var err error
	if !isProviderHTTPMediaURL(media.URL) {
		return nativePlan{}, errors.New("站源未返回有效的播放地址")
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nativePlan{}, errors.New("无法初始化播放会话")
	}
	plan := nativePlan{
		DanmakuID: choice.danmakuVideo,
		URL:       media.URL, Headers: map[string]string{"User-Agent": userAgent, "Referer": media.Referer},
		Key: hex.EncodeToString(media.CENCKey), Quality: media.Quality, Qualities: choice.qualities,
		RouteIndex: choice.index, RouteCount: len(choice.media), Session: hex.EncodeToString(tokenBytes),
	}
	if media.credentials != nil && !media.credentials.expires.IsZero() {
		if !time.Now().Before(media.credentials.expires) {
			return nativePlan{}, errors.New("播放凭证已过期，请重新解析播放")
		}
		plan.ExpiresAt = media.credentials.expires.UnixMilli()
	}
	choice.streamSession = ""
	{
		engine.mu.Lock()
		if engine.stream == nil {
			engine.stream, err = newNativeStreamServer(engine.downloader)
		}
		stream := engine.stream
		engine.mu.Unlock()
		if err != nil {
			return nativePlan{}, err
		}
		plan.URL, choice.streamSession = stream.nativeOpen(media)
	}
	choice.created = time.Now()
	engine.mu.Lock()
	if engine.playbacks == nil {
		engine.playbacks = map[string]nativePlaybackChoice{}
	}
	var expired []string
	for token, old := range engine.playbacks {
		if time.Since(old.created) > 12*time.Hour {
			expired = append(expired, old.streamSession)
			delete(engine.playbacks, token)
		}
	}
	if len(engine.playbacks) >= 8 {
		oldest := ""
		for token, old := range engine.playbacks {
			if oldest == "" || old.created.Before(engine.playbacks[oldest].created) {
				oldest = token
			}
		}
		expired = append(expired, engine.playbacks[oldest].streamSession)
		delete(engine.playbacks, oldest)
	}
	engine.playbacks[plan.Session] = choice
	stream := engine.stream
	engine.mu.Unlock()
	if stream != nil {
		for _, token := range expired {
			stream.nativeRelease(token)
		}
	}
	if err := ctx.Err(); err != nil {
		engine.nativeReleasePlayback(plan.Session)
		return nativePlan{}, err
	}
	return plan, nil
}

// nextNativePlaybackRouteIndex prefers an uncooled candidate after the
// current failed route. If every remaining host is cooling down, preserve the
// original fallback order so recovery still has a chance to succeed.
func nextNativePlaybackRouteIndex(
	media []providerMedia,
	currentIndex int,
	isCooling func(string) bool,
) int {
	next := currentIndex + 1
	if next >= len(media) {
		return len(media)
	}
	for index := next; index < len(media); index++ {
		if isCooling == nil || !isCooling(media[index].URL) {
			return index
		}
	}
	return next
}

func (engine *nativeEngine) nativeNextPlayback(ctx context.Context, session string) (nativePlan, error) {
	engine.mu.Lock()
	choice, exists := engine.playbacks[session]
	engine.mu.Unlock()
	if !exists {
		return nativePlan{}, errors.New("播放线路已失效，请重新解析播放")
	}
	// A repeated open/demux failure is often caused by an expired signed URL
	// set, not by the TV decoder. Refresh after a few failures rather than making
	// the viewer wait through ten serial opens. Untried prior routes are kept.
	if !choice.earlyRefreshUsed &&
		choice.task.DramaID != "" &&
		choice.index+1 >= nativeEarlyPlaybackRefreshAfterFailedRoutes {
		return engine.nativeRefreshPlaybackAfterFailures(ctx, choice)
	}
	choice.index = nextNativePlaybackRouteIndex(
		choice.media,
		choice.index,
		func(address string) bool {
			return engine.downloader != nil &&
				engine.downloader.nativeStreamHostCoolingDown(address)
		},
	)
	return engine.nativeOpenPlayback(ctx, choice)
}

func (engine *nativeEngine) nativeRefreshPlaybackAfterFailures(
	ctx context.Context,
	previous nativePlaybackChoice,
) (nativePlan, error) {
	if engine.downloader == nil || previous.task.DramaID == "" {
		previous.earlyRefreshUsed = true
		previous.index = nextNativePlaybackRouteIndex(previous.media, previous.index, func(address string) bool {
			return engine.downloader != nil && engine.downloader.nativeStreamHostCoolingDown(address)
		})
		return engine.nativeOpenPlayback(ctx, previous)
	}
	attemptedEnd := previous.index + 1
	if attemptedEnd > len(previous.media) {
		attemptedEnd = len(previous.media)
	}
	if attemptedEnd < 0 {
		attemptedEnd = 0
	}
	attempted := previous.media[:attemptedEnd]
	remaining := append([]providerMedia(nil), previous.media[attemptedEnd:]...)

	freshMedia, err := engine.downloader.resolveProviderMedia(ctx, previous.task)
	if ctx.Err() != nil {
		return nativePlan{}, ctx.Err()
	}

	freshChoice := nativePlaybackChoice{
		task:             previous.task,
		requestedQuality: previous.requestedQuality,
		earlyRefreshUsed: true,
		danmakuSeries:    previous.danmakuSeries,
		danmakuVideo:     previous.danmakuVideo,
		qualities:        append([]int(nil), previous.qualities...),
	}
	freshCount := 0
	refreshState := "failed"
	refreshError := ""
	if err == nil {
		freshChoice = nativePlaybackChoices(freshMedia, previous.requestedQuality)
		freshChoice.task = previous.task
		freshChoice.requestedQuality = previous.requestedQuality
		freshChoice.earlyRefreshUsed = true
		freshChoice.danmakuSeries = previous.danmakuSeries
		freshChoice.danmakuVideo = previous.danmakuVideo
		freshChoice.media = nativeExcludeAttemptedPlaybackRoutes(freshChoice.media, attempted)
		freshChoice.qualities = nativeMergePlaybackQualities(freshChoice.qualities, previous.qualities)
		freshCount = len(freshChoice.media)
		if len(freshChoice.media) > 0 {
			freshChoice = engine.rankNativePlaybackRoutes(ctx, freshChoice, previous.task)
		}
		refreshState = "success"
	} else {
		refreshError = nativeStreamError(err)
	}

	// If the resolver did not return any new, untried URLs, probe the unused
	// old candidates instead of looping over media that has already failed.
	if len(freshChoice.media) == 0 && len(remaining) > 0 {
		fallbackChoice := nativePlaybackChoice{
			media:            remaining,
			qualities:        append([]int(nil), previous.qualities...),
			task:             previous.task,
			requestedQuality: previous.requestedQuality,
			earlyRefreshUsed: true,
			danmakuSeries:    previous.danmakuSeries,
			danmakuVideo:     previous.danmakuVideo,
		}
		freshChoice = engine.rankNativePlaybackRoutes(ctx, fallbackChoice, previous.task)
	}
	freshChoice = nativeMergeRefreshedPlaybackChoice(previous, freshChoice, remaining)
	if len(freshChoice.media) == 0 {
		return nativePlan{}, errors.New("该集没有其他可用的播放线路")
	}
	engine.downloader.recordDiagnostic(diagnosticEvent{
		Level:      "info",
		Event:      "playback.route_refresh",
		Source:     sourceFromDramaID(previous.task.DramaID),
		DramaID:    previous.task.DramaID,
		DramaTitle: previous.task.DramaTitle,
		Episode:    previous.task.Index,
		Message: fmt.Sprintf(
			"state=%s failed_routes=%d fresh_routes=%d preserved_routes=%d error=%s",
			refreshState, attemptedEnd, freshCount, len(remaining), refreshError,
		),
	})
	return engine.nativeOpenPlayback(ctx, freshChoice)
}

func nativePlaybackMediaIdentity(option providerMedia) string {
	return option.URL + "\x00" + option.Referer + "\x00" +
		hex.EncodeToString(option.CENCKey) + "\x00" + hex.EncodeToString(option.HLSKey)
}

func nativeExcludeAttemptedPlaybackRoutes(candidates, attempted []providerMedia) []providerMedia {
	used := make(map[string]bool, len(attempted))
	for _, option := range attempted {
		used[nativePlaybackMediaIdentity(option)] = true
	}
	seen := make(map[string]bool, len(candidates))
	result := make([]providerMedia, 0, len(candidates))
	for _, option := range candidates {
		if !isProviderHTTPMediaURL(option.URL) {
			continue
		}
		identity := nativePlaybackMediaIdentity(option)
		if used[identity] || seen[identity] {
			continue
		}
		seen[identity] = true
		option.Variants = nil
		result = append(result, option)
	}
	return result
}

func nativeMergePlaybackRoutes(primary, fallback []providerMedia) []providerMedia {
	seen := make(map[string]bool, len(primary)+len(fallback))
	result := make([]providerMedia, 0, len(primary)+len(fallback))
	for _, group := range [][]providerMedia{primary, fallback} {
		for _, option := range group {
			if !isProviderHTTPMediaURL(option.URL) {
				continue
			}
			identity := nativePlaybackMediaIdentity(option)
			if seen[identity] {
				continue
			}
			seen[identity] = true
			option.Variants = nil
			result = append(result, option)
		}
	}
	return result
}

func nativeMergePlaybackQualities(primary, fallback []int) []int {
	seen := make(map[int]bool, len(primary)+len(fallback))
	var result []int
	for _, values := range [][]int{primary, fallback} {
		for _, quality := range values {
			if quality <= 0 || seen[quality] {
				continue
			}
			seen[quality] = true
			result = append(result, quality)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(result)))
	return result
}

func nativeMergeRefreshedPlaybackChoice(
	previous nativePlaybackChoice,
	fresh nativePlaybackChoice,
	remaining []providerMedia,
) nativePlaybackChoice {
	fresh.media = nativeMergePlaybackRoutes(fresh.media, remaining)
	fresh.qualities = nativeMergePlaybackQualities(fresh.qualities, previous.qualities)
	fresh.task = previous.task
	fresh.requestedQuality = previous.requestedQuality
	fresh.earlyRefreshUsed = true
	fresh.danmakuSeries = previous.danmakuSeries
	fresh.danmakuVideo = previous.danmakuVideo
	fresh.index = 0
	fresh.streamSession = ""
	fresh.created = time.Time{}
	return fresh
}

func (engine *nativeEngine) nativeReleasePlayback(session string) {
	engine.mu.Lock()
	choice, exists := engine.playbacks[session]
	delete(engine.playbacks, session)
	stream := engine.stream
	engine.mu.Unlock()
	if exists && stream != nil && choice.streamSession != "" {
		stream.nativeRelease(choice.streamSession)
	}
}
