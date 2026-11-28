package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const (
	hongguoModuleScriptLimit = 256 << 10
	hongguoModuleResultLimit = 256 << 10
	hongguoModuleExecutionLimit = 2 * time.Second
)

// Compile only reviewed, bundled code. This runtime deliberately has no host
// network, filesystem, process, signing, account, or decryption bindings.
// It is not a general sandbox for untrusted or downloaded JavaScript.
func newHongguoJSModuleBackend(script string) (hongguoModuleBackend, error) {
	if len(script) == 0 || len(script) > hongguoModuleScriptLimit {
		return hongguoModuleBackend{}, errors.New("紅果 JS 模組大小無效")
	}
	program, err := goja.Compile("reviewed-playback-module.js", script, true)
	if err != nil {
		return hongguoModuleBackend{}, errors.New("紅果 JS 模組編譯失敗")
	}
	session := &hongguoJSModuleSession{program: program, gate: make(chan struct{}, 1)}
	return hongguoModuleBackend{load: session.load}, nil
}

// Session ownership is local to one reviewed adapter instance. Callers must
// create a new backend when replacing a module or changing user profiles.
// No account credentials, device identity or playback keys are persisted.
type hongguoJSModuleSession struct {
	program *goja.Program
	gate chan struct{}
	vm *goja.Runtime
	resolve goja.Callable
	lastUsed time.Time
	calls int
}

const (
	hongguoJSModuleIdleLimit = 2 * time.Minute
	hongguoJSModuleMaxCalls = 64
)

func (s *hongguoJSModuleSession) reset() {
	s.vm = nil
	s.resolve = nil
	s.lastUsed = time.Time{}
	s.calls = 0
}

func (s *hongguoJSModuleSession) load(parent context.Context, request hongguoPlaybackRequest) (result hongguoModuleResult, err error) {
	ctx, cancel := context.WithTimeout(parent, hongguoModuleExecutionLimit)
	defer cancel()
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return hongguoModuleResult{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return hongguoModuleResult{}, err
	}
	if s.vm != nil && (time.Since(s.lastUsed) >= hongguoJSModuleIdleLimit || s.calls >= hongguoJSModuleMaxCalls) {
		s.reset()
	}
	fresh := s.vm == nil
	if fresh {
		s.vm = goja.New()
		s.vm.SetMaxCallStackSize(256)
	}
	vm := s.vm
	interruptDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(interruptDone)
		vm.Interrupt(ctx.Err())
	})
	defer func() {
		// Wait for any started interrupt before releasing the session gate.
		// Never let a late cancellation interrupt a later request.
		if !stop() {
			<-interruptDone
		}
		if ctx.Err() != nil {
			result, err = hongguoModuleResult{}, ctx.Err()
		}
		if err != nil {
			s.reset()
		} else {
			s.calls++
			s.lastUsed = time.Now()
		}
	}()
	if fresh {
		if _, err := vm.RunProgram(s.program); err != nil {
			return hongguoModuleResult{}, errors.New("紅果 JS 模組初始化失敗")
		}
		resolve, ok := goja.AssertFunction(vm.Get("resolveMedia"))
		if !ok {
			return hongguoModuleResult{}, errors.New("紅果 JS 模組缺少 resolveMedia")
		}
		s.resolve = resolve
	}
	value, err := s.resolve(goja.Undefined(), vm.ToValue(request.seriesID), vm.ToValue(request.videoID))
	if err != nil {
		return hongguoModuleResult{}, errors.New("紅果 JS 模組執行失敗")
	}
	raw, ok := value.(goja.String)
	if !ok || raw.Length() > hongguoModuleResultLimit {
		return hongguoModuleResult{}, errors.New("紅果 JS 模組回傳格式無效")
	}
	result, err = decodeHongguoJSPlaybackResult(raw.String(), request, time.Now())
	if err != nil {
		return hongguoModuleResult{}, err
	}
	// Discard state after an invalid response, including an incorrect ID,
	// a preview-only result or a protected-media response.
	if _, err := hongguoModuleMedia(request, result); err != nil {
		return hongguoModuleResult{}, err
	}
	return result, nil
}

func decodeHongguoJSModuleResult(raw string) (hongguoModuleResult, error) {
	if len(raw) > hongguoModuleResultLimit {
		return hongguoModuleResult{}, errors.New("紅果 JS 模組資料過大")
	}
	var wire struct {
		Version int `json:"version"`
		SeriesID string `json:"seriesId"`
		VideoID string `json:"videoId"`
		DurationSeconds int64 `json:"durationSeconds"`
		Protected bool `json:"protected"`
		Streams []struct {
			URL string `json:"url"`
			Quality int `json:"quality"`
			Preview bool `json:"preview"`
		} `json:"streams"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil || dec.Decode(new(any)) != io.EOF ||
		wire.DurationSeconds < 0 || wire.DurationSeconds > 86400 || len(wire.Streams) > 128 {
		return hongguoModuleResult{}, errors.New("紅果 JS 模組回傳資料無效")
	}
	result := hongguoModuleResult{
		Version: wire.Version, SeriesID: wire.SeriesID, VideoID: wire.VideoID,
		Duration: time.Duration(wire.DurationSeconds) * time.Second, Protected: wire.Protected,
	}
	for _, stream := range wire.Streams {
		result.Streams = append(result.Streams, hongguoModuleStream{
			URL: stream.URL, Quality: stream.Quality, Preview: stream.Preview,
		})
	}
	return result, nil
}


// Reviewed modules may return either the versioned D-TV contract or the
// THL-style play result. This is a data-format adapter only: no network,
// device registration, signing, credentials or protected media are added.
func decodeHongguoJSPlaybackResult(raw string, request hongguoPlaybackRequest, now time.Time) (hongguoModuleResult, error) {
	if len(raw) > hongguoModuleResultLimit {
		return hongguoModuleResult{}, errors.New("紅果 JS 模組資料過大")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return hongguoModuleResult{}, errors.New("紅果 JS 模組回傳資料無效")
	}
	if _, ok := envelope["version"]; ok {
		return decodeHongguoJSModuleResult(raw)
	}
	var wire struct {
		Parse *int `json:"parse"`
		URL string `json:"url"`
		Header map[string]string `json:"header"`
		DecryptionKey *string `json:"decryptionKey"`
		MediaBytes int64 `json:"mediaBytes"`
		Quality int `json:"quality"`
		Qualities []int `json:"qualities"`
		ExpiresAt int64 `json:"expiresAt"`
		EpisodeID string `json:"episodeId"`
		DanmakuID string `json:"danmakuId"`
		Error string `json:"error"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil || dec.Decode(new(any)) != io.EOF ||
		wire.Parse == nil || *wire.Parse != 0 || wire.Error != "" ||
		wire.EpisodeID != request.videoID || wire.DanmakuID != "hongguo:"+request.seriesID+":"+request.videoID {
		return hongguoModuleResult{}, errors.New("紅果播放結果無效或分集不符")
	}
	if wire.DecryptionKey == nil || *wire.DecryptionKey != "" {
		return hongguoModuleResult{}, errors.New("紅果播放結果未確認為未受保護媒體")
	}
	// Never forward headers from a module to the player. If a URL requires
	// special credentials, use the platform's supported playback environment.
	for name := range wire.Header {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "user-agent", "referer", "accept":
		default:
			return hongguoModuleResult{}, errors.New("紅果播放結果要求不受支援的請求標頭")
		}
	}
	if wire.ExpiresAt <= now.Add(5*time.Second).UnixMilli() ||
		wire.Quality < 720 || wire.Quality > 16384 {
		return hongguoModuleResult{}, errors.New("紅果播放網址已過期、即將到期或畫質不符")
	}
	result := hongguoModuleResult{
		Version: hongguoModuleContractVersion, SeriesID: request.seriesID, VideoID: request.videoID,
		Streams: []hongguoModuleStream{{URL: wire.URL, Quality: wire.Quality}},
	}
	if _, err := hongguoModuleMedia(request, result); err != nil {
		return hongguoModuleResult{}, err
	}
	return result, nil
}
