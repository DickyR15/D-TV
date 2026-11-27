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
	return hongguoModuleBackend{load: func(ctx context.Context, request hongguoPlaybackRequest) (hongguoModuleResult, error) {
		ctx, cancel := context.WithTimeout(ctx, hongguoModuleExecutionLimit)
		defer cancel()
		if err := ctx.Err(); err != nil {
			return hongguoModuleResult{}, err
		}
		vm := goja.New()
		vm.SetMaxCallStackSize(256)
		stop := context.AfterFunc(ctx, func() { vm.Interrupt(ctx.Err()) })
		defer stop()
		if _, err := vm.RunProgram(program); err != nil {
			if ctx.Err() != nil {
				return hongguoModuleResult{}, ctx.Err()
			}
			return hongguoModuleResult{}, errors.New("紅果 JS 模組初始化失敗")
		}
		resolve, ok := goja.AssertFunction(vm.Get("resolveMedia"))
		if !ok {
			return hongguoModuleResult{}, errors.New("紅果 JS 模組缺少 resolveMedia")
		}
		value, err := resolve(goja.Undefined(), vm.ToValue(request.seriesID), vm.ToValue(request.videoID))
		if ctx.Err() != nil {
			return hongguoModuleResult{}, ctx.Err()
		}
		if err != nil {
			return hongguoModuleResult{}, errors.New("紅果 JS 模組執行失敗")
		}
		// Only JSON strings cross the boundary. Never export an arbitrary
		// JS object graph into Go, and do not log URLs or script exceptions.
		raw, ok := value.(goja.String)
		if !ok || raw.Length() > hongguoModuleResultLimit {
			return hongguoModuleResult{}, errors.New("紅果 JS 模組回傳格式無效")
		}
		return decodeHongguoJSModuleResult(raw.String())
	}}, nil
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
