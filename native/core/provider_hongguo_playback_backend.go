package core

import "context"

// hongguoPlaybackRequest carries stable episode identity across the playback
// boundary. The backend owns only source resolution and its session lifecycle;
// rendering, route selection and user data remain in the existing D TV core.
type hongguoPlaybackRequest struct {
	seriesID string
	videoID string
}

type hongguoPlaybackBackend interface {
	name() string
	resolve(context.Context, hongguoPlaybackRequest) (providerMedia, error)
}

// An optional recovery capability belongs to a backend, not to the
// common resolver. Independent providers implement only resolve.
type hongguoPlaybackRecovery interface {
	recover(context.Context, hongguoPlaybackRequest, error, string) (providerMedia, error)
}

// Keep the existing SDK as the only compiled-in implementation until a
// supported playback integration is available. No dynamic code or signer
// configuration is accepted from remote updates.
type hongguoSDKPlaybackBackend struct {
	downloader *Downloader
}

func (hongguoSDKPlaybackBackend) name() string { return "openhongguo" }

func (b hongguoSDKPlaybackBackend) resolve(ctx context.Context, request hongguoPlaybackRequest) (providerMedia, error) {
	return b.downloader.resolveHongguoSDKMedia(ctx, request.videoID)
}

func (b hongguoSDKPlaybackBackend) recover(ctx context.Context, request hongguoPlaybackRequest, err error, explanation string) (providerMedia, error) {
	return b.downloader.recoverHongguoSDKMedia(ctx, request, err, explanation)
}

func hongguoPlaybackStopsFallback(status string) bool {
	switch status {
	case "interrupted", "authentication_required", "verification_required",
		"signature_rejected", "rate_limited", "access_rejected", "protected_media",
		"anonymous_empty", "playback_cooldown", "registration_cooldown":
		return true
	default:
		return false
	}
}
