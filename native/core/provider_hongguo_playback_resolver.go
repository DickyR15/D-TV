package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	case errors.Is(err, openhongguo.ErrAnonymousLimited), errors.Is(err, errHongguoSDKPlaybackCooling):
		return "anonymous_empty", "匿名播放未取得有效資料；實際授權要求尚未確認"
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

func (d *Downloader) resolveHongguoMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, seriesID, ok := splitProviderDramaID(task.DramaID)
	if !ok || source != sourceHongguo {
		return providerMedia{}, fmt.Errorf("紅果章節缺少劇集 ID，請重新整理片庫後再試")
	}
	videoID := strings.TrimPrefix(task.Chapter.VideoURL, "hongguo-cenc://")
	if !hongguoNumericID.MatchString(videoID) || !hongguoNumericID.MatchString(seriesID) {
		return providerMedia{}, fmt.Errorf("紅果章節 ID 無效，請重新取得章節")
	}

	// The OpenHongguo SDK owns guest registration, V5 signing, and the
	// single-model fallback. Do not first register a second independent
	// Current Player device against the retired log.snssdk.com aliases.
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
