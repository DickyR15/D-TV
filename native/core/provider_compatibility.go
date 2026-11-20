package core

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	providerCompatibilitySchemaVersion = 1
	providerCompatibilityMaxBytes      = 256 << 10
	providerCompatibilityRefreshEvery  = 6 * time.Hour
	providerCompatibilityStaleGrace    = 14 * 24 * time.Hour
	providerCompatibilityCacheName     = "provider-compatibility-cache.json"
)

var providerCompatibilityManifestURLs = []string{
	"https://raw.githubusercontent.com/DickyR15/D-TV/main/config/provider-compatibility.json",
	"https://raw.githubusercontent.com/DickyR15/D-TV/release/v1.0.8-candidate/config/provider-compatibility.json",
}

var providerCompatibilityHostLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type providerCompatibilityManifest struct {
	SchemaVersion int                                  `json:"schemaVersion"`
	ManifestVersion int                                `json:"manifestVersion"`
	GeneratedAt   time.Time                            `json:"generatedAt"`
	ExpiresAt     time.Time                            `json:"expiresAt"`
	Providers     map[string]providerCompatibilityEntry `json:"providers"`
}

type providerCompatibilityEntry struct {
	BaseURL          string            `json:"baseUrl,omitempty"`
	APIBaseURL       string            `json:"apiBaseUrl,omitempty"`
	PlaybackBaseURL  string            `json:"playbackBaseUrl,omitempty"`
	Endpoints        map[string]string `json:"endpoints,omitempty"`
	AllowedHosts     []string          `json:"allowedHosts,omitempty"`
	FallbackBaseURLs []string          `json:"fallbackBaseUrls,omitempty"`
	Paths            map[string]string `json:"paths,omitempty"`
	ParserProfile    string            `json:"parserProfile,omitempty"`
}

type providerCompatibilityCache struct {
	DownloadedAt time.Time       `json:"downloadedAt"`
	Manifest     json.RawMessage `json:"manifest"`
	Signature    string          `json:"signature"`
}

func knownCompatibilityProvider(source string) bool {
	switch canonicalProviderSource(source) {
	case sourceHongguo, sourceHuangguoAI, sourceHuangguoVideo, sourceHuangdou,
		sourceHuangju, sourceYeguo, sourceDSD, sourceGuipian, sourceMissAV,
		sourceIkanBot, sourceSupJav, sourcePPP, sourceMadou, sourceDuoduo,
		sourceHanxiaoquan, sourceHeitao:
		return true
	default:
		return false
	}
}

func compatibilityPathNames(source string) map[string]bool {
	switch canonicalProviderSource(source) {
	case sourceHongguo:
		return map[string]bool{"videoDetail": true, "videoModel": true, "playback": true, "searchTab": true}
	case sourceIkanBot:
		return map[string]bool{"catalog": true, "catalogPage": true, "search": true, "detail": true, "getRes": true}
	case sourcePPP:
		return map[string]bool{"catalog": true, "search": true, "detail": true, "embed": true}
	case sourceMadou, sourceSupJav, sourceMissAV:
		return map[string]bool{"catalog": true, "search": true, "detail": true, "play": true}
	case sourceHuangju:
		return map[string]bool{"catalog": true, "detail": true, "guest": true, "play": true}
	case sourceYeguo:
		return map[string]bool{"catalog": true, "detail": true, "search": true, "play": true}
	case sourceHuangguoAI:
		return map[string]bool{"ping": true, "catalog": true, "detail": true, "play": true}
	case sourceDuoduo:
		return map[string]bool{"catalog": true, "detail": true, "play": true}
	default:
		return map[string]bool{}
	}
}

func compatibilityEndpointNames(source string) map[string]bool {
	switch canonicalProviderSource(source) {
	case sourceHongguo:
		return map[string]bool{"deviceRegister": true}
	default:
		return map[string]bool{}
	}
}

func compatibilityParserProfiles(source string) map[string]bool {
	switch canonicalProviderSource(source) {
	case sourceHongguo:
		return map[string]bool{"hongguo_v5_first": true, "hongguo_legacy_first": true, "hongguo_web_first": true}
	case sourcePPP:
		return map[string]bool{"ppp_regex_v1": true, "ppp_dom_v1": true}
	case sourceIkanBot:
		return map[string]bool{"ikanbot_dom_v1": true}
	case sourceMadou:
		return map[string]bool{"madou_html_v1": true}
	case sourceSupJav:
		return map[string]bool{"supjav_html_v1": true}
	case sourceMissAV:
		return map[string]bool{"missav_html_v1": true}
	default:
		return map[string]bool{}
	}
}

func validCompatibilityHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || strings.Contains(host, "*") || net.ParseIP(host) != nil ||
		host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.ContainsAny(host, " /\\:@?#") {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !providerCompatibilityHostLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func compatibilityHostAllowed(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, candidate := range allowed {
		if strings.EqualFold(host, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func validateCompatibilityURL(raw string, allowed []string) error {
	address, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || address.Scheme != "https" || address.Hostname() == "" ||
		address.User != nil || address.RawQuery != "" || address.Fragment != "" ||
		address.Port() != "" && address.Port() != "443" ||
		!compatibilityHostAllowed(address.Hostname(), allowed) {
		return errors.New("遠端相容性設定包含不允許的 HTTPS 網址")
	}
	return nil
}

func validCompatibilityPathTemplate(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 || !strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\r\n\x00\\") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	path := strings.ReplaceAll(parsed.Path, "{page}", "1")
	path = strings.ReplaceAll(path, "{category}", "sample")
	path = strings.ReplaceAll(path, "{query}", "sample")
	path = strings.ReplaceAll(path, "{id}", "sample")
	path = strings.ReplaceAll(path, "{episode}", "1")
	path = strings.ReplaceAll(path, "{line}", "1")
	path = strings.ReplaceAll(path, "{sort}", "hot")
	path = strings.ReplaceAll(path, "{slug}", "sample")
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." || segment == "." {
			return false
		}
	}
	for _, key := range []string{"{page}", "{category}", "{query}", "{id}", "{episode}", "{line}", "{sort}", "{slug}"} {
		value = strings.ReplaceAll(value, key, "")
	}
	return !strings.ContainsAny(value, "{}")
}

func validateProviderCompatibilityManifest(manifest providerCompatibilityManifest, now time.Time, allowExpired bool) error {
	if manifest.SchemaVersion != providerCompatibilitySchemaVersion ||
		manifest.ManifestVersion < 1 || manifest.ManifestVersion > 1_000_000 ||
		manifest.GeneratedAt.IsZero() || manifest.ExpiresAt.IsZero() ||
		manifest.GeneratedAt.After(now.Add(24*time.Hour)) ||
		!manifest.ExpiresAt.After(manifest.GeneratedAt) ||
		manifest.ExpiresAt.Sub(manifest.GeneratedAt) > 90*24*time.Hour ||
		len(manifest.Providers) > 32 {
		return errors.New("遠端相容性設定版本或有效期限無效")
	}
	if !allowExpired && !manifest.ExpiresAt.After(now) {
		return errors.New("遠端相容性設定已逾期")
	}
	if allowExpired && now.After(manifest.ExpiresAt.Add(providerCompatibilityStaleGrace)) {
		return errors.New("本機相容性設定已超過離線保留期限")
	}
	for source, entry := range manifest.Providers {
		if !knownCompatibilityProvider(source) || canonicalProviderSource(source) != source {
			return fmt.Errorf("遠端相容性設定包含未知片源：%s", source)
		}
		if len(entry.AllowedHosts) > 32 || len(entry.FallbackBaseURLs) > 8 || len(entry.Paths) > 16 || len(entry.Endpoints) > 8 {
			return errors.New("遠端相容性設定項目數量超過限制")
		}
		hostSet := map[string]bool{}
		for _, host := range entry.AllowedHosts {
			host = strings.ToLower(strings.TrimSpace(host))
			if !validCompatibilityHost(host) || hostSet[host] {
				return errors.New("遠端相容性設定包含無效或重複的網域白名單")
			}
			hostSet[host] = true
		}
		if entry.BaseURL != "" {
			if err := validateCompatibilityURL(entry.BaseURL, entry.AllowedHosts); err != nil {
				return fmt.Errorf("%s：%w", source, err)
			}
		}
		if entry.APIBaseURL != "" {
			if err := validateCompatibilityURL(entry.APIBaseURL, entry.AllowedHosts); err != nil {
				return fmt.Errorf("%s API：%w", source, err)
			}
		}
		if entry.PlaybackBaseURL != "" {
			if err := validateCompatibilityURL(entry.PlaybackBaseURL, entry.AllowedHosts); err != nil {
				return fmt.Errorf("%s 播放 API：%w", source, err)
			}
		}
		for _, raw := range entry.FallbackBaseURLs {
			if err := validateCompatibilityURL(raw, entry.AllowedHosts); err != nil {
				return fmt.Errorf("%s 備用網址：%w", source, err)
			}
		}
		for name, raw := range entry.Endpoints {
			if !compatibilityEndpointNames(source)[name] {
				return fmt.Errorf("%s 包含不支援的端點名稱：%s", source, name)
			}
			if err := validateCompatibilityURL(raw, entry.AllowedHosts); err != nil {
				return fmt.Errorf("%s 端點 %s：%w", source, name, err)
			}
		}
		allowedPaths := compatibilityPathNames(source)
		for name, template := range entry.Paths {
			if !allowedPaths[name] || !validCompatibilityPathTemplate(template) {
				return fmt.Errorf("%s 包含不支援的 API 路徑或格式：%s", source, name)
			}
		}
		if entry.ParserProfile != "" && !compatibilityParserProfiles(source)[entry.ParserProfile] {
			return fmt.Errorf("%s 包含尚未內建的解析器設定：%s", source, entry.ParserProfile)
		}
	}
	return nil
}

const providerCompatibilityPublicKeyHex = "2dc718db121ac8f87fe0eff584e8c9904c9a3aba091d1168af4ce0e3582394cf"

func verifyProviderCompatibilitySignatureWithKey(body []byte, encodedSignature string, publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("App 內建相容性公開金鑰無效")
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedSignature))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("遠端相容性簽章格式無效")
	}
	if !ed25519.Verify(publicKey, body, signature) {
		return errors.New("遠端相容性簽章驗證失敗，已拒絕套用")
	}
	return nil
}

func verifyProviderCompatibilitySignature(body []byte, encodedSignature string) error {
	publicKeyBytes, err := hex.DecodeString(providerCompatibilityPublicKeyHex)
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
		return errors.New("App 內建相容性公開金鑰無效")
	}
	return verifyProviderCompatibilitySignatureWithKey(body, encodedSignature, ed25519.PublicKey(publicKeyBytes))
}

func decodeProviderCompatibilityManifest(body []byte, now time.Time) (providerCompatibilityManifest, error) {
	var manifest providerCompatibilityManifest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return manifest, errors.New("遠端相容性設定包含多餘資料")
	}
	if err := validateProviderCompatibilityManifest(manifest, now, false); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func (d *Downloader) loadProviderCompatibilityCache() {
	if d.cfg.dataDir == "" {
		return
	}
	address := filepath.Join(d.cfg.dataDir, providerCompatibilityCacheName)
	info, err := os.Stat(address)
	if err != nil || info.Size() <= 0 || info.Size() > providerCompatibilityMaxBytes+(16<<10) {
		return
	}
	body, err := os.ReadFile(address)
	if err != nil {
		return
	}
	var cache providerCompatibilityCache
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cache); err != nil || cache.DownloadedAt.IsZero() ||
		len(cache.Manifest) == 0 || len(cache.Manifest) > providerCompatibilityMaxBytes ||
		verifyProviderCompatibilitySignature(cache.Manifest, cache.Signature) != nil {
		return
	}
	manifest, err := decodeProviderCompatibilityManifestForCache(cache.Manifest)
	if err != nil || validateProviderCompatibilityManifest(manifest, time.Now(), true) != nil {
		return
	}
	d.compatibilityMu.Lock()
	copy := manifest
	d.compatibilityManifest = &copy
	d.compatibilityLastFetch = cache.DownloadedAt
	d.compatibilityOrigin = "cache"
	d.compatibilityMu.Unlock()
}

func decodeProviderCompatibilityManifestForCache(body []byte) (providerCompatibilityManifest, error) {
	var manifest providerCompatibilityManifest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return manifest, errors.New("本機相容性快取包含多餘資料")
	}
	return manifest, nil
}

func (d *Downloader) startProviderCompatibilityRefresh() {
	d.compatibilityRefreshOnce.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			_ = d.refreshProviderCompatibility(ctx)
		}()
	})
}

func fetchProviderCompatibilityResource(ctx context.Context, client *http.Client, resourceURL string) ([]byte, error) {
	parsed, err := url.Parse(resourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "raw.githubusercontent.com" || parsed.User != nil {
		return nil, errors.New("相容性設定資源網址無效")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, resourceURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, text/plain")
	request.Header.Set("User-Agent", "D-TV-Compatibility/1")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL == nil ||
		!strings.EqualFold(response.Request.URL.Scheme, "https") ||
		!strings.EqualFold(response.Request.URL.Hostname(), "raw.githubusercontent.com") {
		return nil, errors.New("相容性設定重新導向至不受信任的網域")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("相容性設定服務回應 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, providerCompatibilityMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body) > providerCompatibilityMaxBytes {
		return nil, errors.New("遠端相容性設定大小無效")
	}
	return body, nil
}

func (d *Downloader) refreshProviderCompatibility(ctx context.Context) error {
	d.compatibilityRefreshMu.Lock()
	defer d.compatibilityRefreshMu.Unlock()

	d.compatibilityStartupVerified.Store(false)
	now := time.Now()
	d.compatibilityMu.RLock()
	lastFetch := d.compatibilityLastFetch
	currentVersion := 0
	if d.compatibilityManifest != nil {
		currentVersion = d.compatibilityManifest.ManifestVersion
	}
	d.compatibilityMu.RUnlock()
	// App startup always fetches and verifies the current GitHub manifest.
	// Cached values are available only as diagnostics/recovery data; they do
	// not satisfy the startup gate for a new app session.
	_ = lastFetch

	client := d.client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	var lastErr error
	for _, manifestURL := range providerCompatibilityManifestURLs {
		manifestContext, manifestCancel := context.WithTimeout(ctx, 8*time.Second)
		body, fetchErr := fetchProviderCompatibilityResource(manifestContext, client, manifestURL)
		manifestCancel()
		if fetchErr != nil {
			lastErr = fetchErr
			continue
		}
		signatureContext, signatureCancel := context.WithTimeout(ctx, 8*time.Second)
		signatureBody, signatureErr := fetchProviderCompatibilityResource(signatureContext, client, manifestURL+".sig")
		signatureCancel()
		if signatureErr != nil {
			lastErr = signatureErr
			continue
		}
		signature := strings.TrimSpace(string(signatureBody))
		if err := verifyProviderCompatibilitySignature(body, signature); err != nil {
			lastErr = err
			continue
		}
		manifest, decodeErr := decodeProviderCompatibilityManifest(body, time.Now())
		if decodeErr != nil {
			lastErr = decodeErr
			continue
		}
		if manifest.ManifestVersion < currentVersion {
			lastErr = errors.New("遠端相容性設定版本倒退，已拒絕套用")
			continue
		}
		if manifest.ManifestVersion == currentVersion {
			d.compatibilityMu.Lock()
			d.compatibilityLastFetch = time.Now()
			d.compatibilityLastError = ""
			d.compatibilityMu.Unlock()
			d.compatibilityStartupVerified.Store(true)
			return nil
		}
		cache := providerCompatibilityCache{
			DownloadedAt: time.Now(),
			Manifest: append(json.RawMessage(nil), body...),
			Signature: signature,
		}
		encoded, marshalErr := json.Marshal(cache)
		if marshalErr != nil {
			return marshalErr
		}
		if d.cfg.dataDir != "" {
			if err := os.MkdirAll(d.cfg.dataDir, 0700); err != nil {
				return err
			}
			if err := writeNativeCacheFile(filepath.Join(d.cfg.dataDir, providerCompatibilityCacheName), encoded); err != nil {
				lastErr = err
				continue
			}
		}
		d.compatibilityMu.Lock()
		copy := manifest
		d.compatibilityManifest = &copy
		d.compatibilityLastFetch = time.Now()
		d.compatibilityLastError = ""
		d.compatibilityOrigin = "remote"
		d.compatibilityMu.Unlock()
		d.compatibilityStartupVerified.Store(true)
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("目前沒有可用的遠端相容性設定")
	}
	d.compatibilityMu.Lock()
	d.compatibilityLastError = lastErr.Error()
	d.compatibilityMu.Unlock()
	return lastErr
}

func (d *Downloader) compatibilityEntry(source string) providerCompatibilityEntry {
	source = canonicalProviderSource(source)
	d.compatibilityMu.RLock()
	defer d.compatibilityMu.RUnlock()
	if d.compatibilityManifest == nil {
		return providerCompatibilityEntry{}
	}
	return d.compatibilityManifest.Providers[source]
}

func (d *Downloader) providerCompatibilityBaseURL(source, fallback string) string {
	entry := d.compatibilityEntry(source)
	if entry.BaseURL == "" {
		return strings.TrimRight(fallback, "/")
	}
	return strings.TrimRight(entry.BaseURL, "/")
}

func (d *Downloader) providerCompatibilityAPIBaseURL(source, fallback string) string {
	entry := d.compatibilityEntry(source)
	if entry.APIBaseURL == "" {
		return strings.TrimRight(fallback, "/")
	}
	return strings.TrimRight(entry.APIBaseURL, "/")
}

func (d *Downloader) providerCompatibilityPlaybackBaseURL(source, fallback string) string {
	entry := d.compatibilityEntry(source)
	if entry.PlaybackBaseURL == "" {
		return strings.TrimRight(fallback, "/")
	}
	return strings.TrimRight(entry.PlaybackBaseURL, "/")
}

func (d *Downloader) providerCompatibilityEndpointURL(source, name, fallback string) string {
	entry := d.compatibilityEntry(source)
	if address := entry.Endpoints[name]; address != "" {
		return address
	}
	return fallback
}

func (d *Downloader) providerCompatibilityPath(source, name string, values map[string]string, fallback string) string {
	template := d.compatibilityEntry(source).Paths[name]
	if template == "" {
		return fallback
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	// Replace longer placeholder names first to avoid overlapping tokens.
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		value := values[key]
		switch key {
		case "query":
			value = url.QueryEscape(value)
		case "page", "episode", "line":
			if _, err := fmt.Sscan(value, new(int)); err != nil {
				return fallback
			}
		default:
			value = url.PathEscape(value)
		}
		template = strings.ReplaceAll(template, "{"+key+"}", value)
	}
	if strings.ContainsAny(template, "{}") || !validResolvedCompatibilityPath(template) {
		return fallback
	}
	return template
}

func validResolvedCompatibilityPath(value string) bool {
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") ||
		strings.ContainsAny(value, "\r\n\x00\\") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == ".." || decoded == "." {
			return false
		}
	}
	return true
}

func (d *Downloader) providerParserProfile(source, fallback string) string {
	entry := d.compatibilityEntry(source)
	if entry.ParserProfile == "" {
		return fallback
	}
	return entry.ParserProfile
}

func (d *Downloader) providerCompatibilityAllowsHost(source, host string) bool {
	entry := d.compatibilityEntry(source)
	return compatibilityHostAllowed(host, entry.AllowedHosts)
}

func (d *Downloader) providerCompatibilityStatus() map[string]any {
	d.compatibilityMu.RLock()
	defer d.compatibilityMu.RUnlock()
	result := map[string]any{
		"version": 0,
		"origin": "builtin",
		"lastFetch": d.compatibilityLastFetch,
		"lastError": d.compatibilityLastError,
	}
	if d.compatibilityManifest != nil {
		result["version"] = d.compatibilityManifest.ManifestVersion
		result["generatedAt"] = d.compatibilityManifest.GeneratedAt
		result["expiresAt"] = d.compatibilityManifest.ExpiresAt
		result["origin"] = d.compatibilityOrigin
	}
	return result
}

