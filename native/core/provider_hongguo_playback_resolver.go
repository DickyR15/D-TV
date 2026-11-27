package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	openhongguo "github.com/beilunyang/openhongguo/sdk"
)

// hongguoPlaybackAccessStatus reports observed failures, not an inferred
// entitlement. An empty response alone does not prove login is required.
func hongguoPlaybackAccessStatus(err error) (string, string) {
	var apiErr *openhongguo.APIError
	switch {
	case err == nil:
		return "available", "播放介接正常"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "interrupted", "播放請求已取消或逾時"
	case errors.Is(err, openhongguo.ErrChallengeRequired):
		return "verification_required", "平台要求額外驗證；請使用平台提供的驗證方式"
	case errors.Is(err, openhongguo.ErrSignatureRejected):
		return "signature_rejected", "平台拒絕播放請求驗證；目前沒有已確認的授權介接"
	case errors.Is(err, openhongguo.ErrRateLimited):
		return "rate_limited", "平台限制請求頻率"
	// The SDK maps bare HTTP 403 to ErrAuthenticationRequired, but a
	// forbidden response without an explicit auth code does not establish
	// that signing in will restore access.
	case errors.As(err, &apiErr) && apiErr != nil && apiErr.HTTPStatus == http.StatusForbidden &&
		apiErr.Code != "auth_required" && apiErr.Code != "401" && apiErr.Code != "403":
		return "access_rejected", "播放或裝置註冊遭拒絕；實際授權要求尚未確認"
	case errors.Is(err, openhongguo.ErrAuthenticationRequired):
		return "authentication_required", "平台要求登入或授權；目前未建立可用的正式授權工作階段"
	case errors.Is(err, errHongguoSDKPlaybackCooling):
		return "playback_cooldown", "先前播放空回應仍在冷卻；本次未發送播放請求"
	case errors.Is(err, errHongguoSDKRegistrationCooling):
		return "registration_cooldown", "裝置註冊仍在冷卻；本次未發送播放請求"
	case errors.Is(err, openhongguo.ErrAnonymousLimited):
		return "anonymous_empty", "本次匿名播放未取得有效資料；實際授權要求尚未確認"
	case errors.Is(err, openhongguo.ErrTransport):
		return "transport_failed", "網路傳輸失敗；請查看紅果 SDK 連線診斷的主機與失敗階段"
	case hongguoSDKPlaybackRegistrationRejected(err):
		return "access_rejected", "播放或裝置註冊遭拒絕"
	default:
		return "request_failed", "播放請求失敗；請參考下列診斷"
	}
}

// A skipped resolver is not a successful request or an unexplained nil error.
func hongguoAttemptDiagnostic(err error) string {
	if err == nil {
		return "未執行"
	}
	return fmt.Sprint(publicError(err))
}

type hongguoPlaybackOriginKey struct{}

func hongguoPlaybackRequestOrigin(ctx context.Context) string {
	if _, ok := ctx.Value(sourceTraceKey{}).(*sourceResponseTrace); ok {
		return "source_health_check"
	}
	if background, _ := ctx.Value(backgroundCatalogKey{}).(bool); background {
		return "background_catalog"
	}
	if action, _ := ctx.Value(hongguoPlaybackOriginKey{}).(string); action != "" {
		switch action {
		case "resolve", "preload", "prepareHandoff":
			return action
		}
	}
	return "playback_or_other"
}

func (d *Downloader) resolveHongguoMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, seriesID, ok := splitProviderDramaID(task.DramaID)
	if !ok || source != sourceHongguo {
		return providerMedia{}, fmt.Errorf("紅果章節缺少劇集 ID，請重新整理片庫後再試")
	}
	if hongguoExactID(seriesID) == "" {
		return providerMedia{}, fmt.Errorf("紅果劇集 ID 無效，請重新整理片庫")
	}
	if task.Chapter.VIP && task.Chapter.VideoURL == "" {
		return providerMedia{}, errors.New("紅果片源標示此分集目前不可播放")
	}
	videoID := hongguoChapterVideoID(seriesID, task.Chapter)
	if videoID == "" {
		d.recordDiagnostic(diagnosticEvent{
			Level: "warn", Event: "hongguo.invalid_chapter_identity", Source: sourceHongguo,
			Message: "影片 ID 無效；未發送播放請求",
		})
		return providerMedia{}, fmt.Errorf("紅果影片 ID 無效，請重新取得章節")
	}

	// The OpenHongguo SDK owns guest registration, V5 signing, and the
	// single-model fallback. Do not first register a second independent
	// Current Player device against the retired log.snssdk.com aliases.
	caller := hongguoPlaybackRequestOrigin(ctx)
	d.recordDiagnostic(diagnosticEvent{
		Level: "info", Event: "hongguo.playback_request_origin", Source: sourceHongguo,
		Message: "caller=" + caller,
	})
	sdkMedia, sdkErr := d.resolveHongguoSDKMedia(ctx, videoID)
	if sdkErr == nil {
		return sdkMedia, nil
	}
	if err := ctx.Err(); err != nil {
		return providerMedia{}, err
	}

	type attempt struct {
		name string
		run  func() (providerMedia, error)
	}
	quick := attempt{"quick", func() (providerMedia, error) { return d.resolveHongguoQuickAppMedia(ctx, seriesID, videoID) }}
	native := attempt{"native", func() (providerMedia, error) { return d.resolveHongguoAppMedia(ctx, videoID) }}
	multi := attempt{"multi", func() (providerMedia, error) { return d.resolveHongguoMultiModelMedia(ctx, videoID) }}
	web := attempt{"web", func() (providerMedia, error) { return d.resolveHongguoWebMedia(ctx, seriesID, videoID) }}

	profile := d.providerParserProfile(sourceHongguo, "hongguo_v5_first")
	status, explanation := hongguoPlaybackAccessStatus(sdkErr)
	d.recordDiagnostic(diagnosticEvent{
		Level: "warn", Event: "hongguo.playback_access_status", Source: sourceHongguo,
		Message: "status=" + status + "；" + explanation,
	})
	// A request already suppressed by cooldown is not a fresh upstream
	// failure. Do not repeatedly probe the quick/web routes during this
	// interval, or present their unrelated 404s as a new playback failure.
	if errors.Is(sdkErr, errHongguoSDKPlaybackCooling) || errors.Is(sdkErr, errHongguoSDKRegistrationCooling) {
		return providerMedia{}, fmt.Errorf("紅果暫時無法播放：%s；%w", explanation, sdkErr)
	}
	// An explicit authentication, verification, signature or rate-limit
	// response is not a transport failure. Do not try another access route.
	switch status {
	case "authentication_required", "verification_required", "signature_rejected", "rate_limited":
		return providerMedia{}, fmt.Errorf("紅果暫時無法播放：%s；%w", explanation, sdkErr)
	}
	var attempts []attempt
	if hongguoSDKPlaybackRegistrationRejected(sdkErr) {
		// Once the new guest-registration path has been rejected, avoid
		// cascading into legacy App routes that try the same retired hosts.
		attempts = []attempt{quick, web}
	} else {
		switch profile {
		case "hongguo_legacy_first":
			attempts = []attempt{native, multi, quick, web}
		case "hongguo_web_first":
			attempts = []attempt{quick, web, native, multi}
		default:
			attempts = []attempt{quick, native, multi, web}
		}
	}

	errs := map[string]error{}
	for _, candidate := range attempts {
		media, err := candidate.run()
		if err == nil {
			return media, nil
		}
		errs[candidate.name] = err
		if ctx.Err() != nil {
			return providerMedia{}, ctx.Err()
		}
	}

	nativeErr, multiModelErr := errs["native"], errs["multi"]
	if errors.Is(sdkErr, errHongguoH266Only) ||
		errors.Is(nativeErr, errHongguoH266Only) ||
		errors.Is(multiModelErr, errHongguoH266Only) {
		helperMedia, helperErr := d.resolveHongguoHelperMedia(ctx, seriesID, videoID)
		if helperErr == nil {
			return helperMedia, nil
		}
		multiModelErr = fmt.Errorf("%w；輔助裝置 H.265 備援失敗：%v", errHongguoH266Only, publicError(helperErr))
	}
	quickWebErr, pageErr := errs["quick"], errs["web"]
	return providerMedia{}, fmt.Errorf(
		"紅果取流失敗：%s；新版匿名 SDK V5：%v；解析設定 %s；快捷網頁：%v；舊版 App：%v；多畫質：%v；官網網頁：%w",
		explanation,
		publicError(sdkErr),
		profile,
		hongguoAttemptDiagnostic(quickWebErr),
		hongguoAttemptDiagnostic(nativeErr),
		hongguoAttemptDiagnostic(multiModelErr),
		pageErr,
	)
}
