package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"time"

	htmlnet "golang.org/x/net/html"
)

var (
	adultSupJavURLPlayPattern = regexp.MustCompile(`(?is)\burlPlay\s*[:=]\s*['"]([^'"]+)['"]`)
	adultSupJavStreamtapePattern = regexp.MustCompile(`(?is)getElementById\(\s*['"]robotlink['"]\s*\)\.innerHTML\s*=\s*['"]([^'"]*)['"]\s*\+\s*(?:['"]{2}\s*\+\s*)?\(\s*['"]([^'"]*)['"]\s*\)((?:\.substring\(\s*\d+\s*\))+)`)
	adultSupJavSubstringPattern = regexp.MustCompile(`substring\(\s*(\d+)\s*\)`)
	adultSupJavVOERedirectPattern = regexp.MustCompile(`(?is)window\.location\.href\s*=\s*['"]([^'"]+)['"]`)
	adultSupJavSafeFileCode = regexp.MustCompile(`^[A-Za-z0-9]+$`)
)

func adultSupJavNormalizeServerName(raw string) string {
	name := strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(raw)), ""))
	switch name {
	case "FAST", "FPLAYER", "FC2STREAM":
		return "FST"
	case "STREAMTAPE", "STPLAYER":
		return "ST"
	case "FAV", "FAVJAV":
		return "TV"
	default:
		return name
	}
}

// adultSupJavServerLinks parses current button markup and tolerates harmless
// theme changes around the anchor. It does not execute remote scripts.
func adultSupJavServerLinks(body string) map[string]string {
	result := make(map[string]string)
	document, err := htmlnet.Parse(strings.NewReader(body))
	if err != nil {
		return result
	}
	adultHTMLWalk(document, func(node *htmlnet.Node) {
		if node.Type != htmlnet.ElementNode || node.Data != "a" {
			return
		}
		link := strings.TrimSpace(providerHTMLAttr(node, "data-link"))
		if link == "" {
			return
		}
		name := adultSupJavNormalizeServerName(providerHTMLText(node))
		if name == "" {
			return
		}
		if _, exists := result[name]; !exists {
			result[name] = link
		}
	})
	return result
}

// This rotates among the primary gateway and its published mirrors. It does
// not attempt to solve or bypass Cloudflare challenges.
func adultSupJavGatewayCandidates(token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	reversed := adultReverse(token)
	hosts := []string{"lk1.supremejav.com", "lk2.supremejav.com", "supremejav.com"}
	result := make([]string, 0, len(hosts)*2)
	for _, host := range hosts {
		result = append(result, "https://"+host+"/supjav.php?c="+url.QueryEscape(reversed))
	}
	for _, host := range hosts {
		result = append(result, "https://"+host+"/supjav.php?l="+url.QueryEscape(token))
	}
	return result
}

func (d *Downloader) fetchAdultSupJavEmbed(ctx context.Context, token, referer string) (string, string, error) {
	var lastErr error
	for _, address := range adultSupJavGatewayCandidates(token) {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
		requestContext = context.WithValue(requestContext, providerTextUserAgentKey{}, adultUserAgent)
		requestContext = context.WithValue(requestContext, providerTextNoCacheKey{}, true)
		responses := &playbackResponseURLs{}
		requestContext = context.WithValue(requestContext, playbackResponseURLsKey{}, responses)
		body, err := d.fetchProviderText(requestContext, address, referer)
		cancel()
		if err == nil && strings.TrimSpace(body) != "" {
			finalURL := address
			if actual, ok := responses.values.Load(address); ok {
				if resolved, valid := actual.(string); valid && resolved != "" {
					finalURL = resolved
				}
			}
			return body, finalURL, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("閘道回傳空白頁面")
		}
	}
	if lastErr == nil {
		lastErr = errors.New("沒有可用的 SupJav 播放閘道")
	}
	return "", "", lastErr
}

func adultSupJavTVHLS(body, embedURL string) string {
	for _, match := range adultSupJavURLPlayPattern.FindAllStringSubmatch(strings.ReplaceAll(body, "\\/", "/"), -1) {
		if len(match) > 1 {
			if address := adultResolvePlaybackURL(embedURL, match[1]); address != "" {
				return address
			}
		}
	}
	return adultFindM3U8(body, embedURL)
}

func adultSupJavStreamtapeDirectURL(body, embedURL string) string {
	match := adultSupJavStreamtapePattern.FindStringSubmatch(body)
	if len(match) != 4 {
		return ""
	}
	suffix := match[2]
	for _, offset := range adultSupJavSubstringPattern.FindAllStringSubmatch(match[3], -1) {
		if len(offset) != 2 {
			continue
		}
		index := 0
		for _, character := range offset[1] {
			index = index*10 + int(character-'0')
			if index > len(suffix) {
				index = len(suffix)
				break
			}
		}
		if index > len(suffix) {
			index = len(suffix)
		}
		suffix = suffix[index:]
	}
	link := strings.TrimLeft(match[1]+suffix, "/")
	if link == "" || !strings.Contains(strings.ToLower(link), "get_video") {
		return ""
	}
	address := link
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	resolved := adultResolvePlaybackURL(embedURL, address)
	if !isProviderHTTPMediaURL(resolved) {
		return ""
	}
	return resolved
}

func adultSupJavVOERedirect(body, currentURL string) string {
	for _, match := range adultSupJavVOERedirectPattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		address := adultResolvePlaybackURL(currentURL, match[1])
		parsed, err := url.Parse(address)
		if err != nil || !isProviderHTTPMediaURL(address) ||
			!(parsed.Hostname() == "voe.sx" || parsed.Hostname() == "www.voe.sx") ||
			!regexp.MustCompile(`^/e/[A-Za-z0-9]+/?$`).MatchString(parsed.Path) {
			continue
		}
		return address
	}
	return ""
}

func adultSupJavVASRequest(embedURL, body string) (string, string, bool) {
	if !strings.Contains(strings.ToLower(body), "/api/stream") {
		return "", "", false
	}
	parsed, err := url.Parse(embedURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return "", "", false
	}
	for _, part := range parts {
		if part == "." || part == ".." {
			return "", "", false
		}
	}
	filecode := parts[len(parts)-1]
	if !adultSupJavSafeFileCode.MatchString(filecode) {
		return "", "", false
	}
	query := parsed.Query()
	query.Del("filecode")
	endpoint := parsed.Scheme + "://" + parsed.Host + "/api/stream"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	return endpoint, filecode, true
}

func (d *Downloader) fetchAdultSupJavVAS(ctx context.Context, embedURL, body string) (string, error) {
	endpoint, filecode, ok := adultSupJavVASRequest(embedURL, body)
	if !ok {
		return "", errors.New("VAS 播放頁缺少有效的 API 或 filecode")
	}
	payload, err := json.Marshal(map[string]any{"filecode": filecode, "device": "web", "codecs": []string{}})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	parsed, _ := url.Parse(embedURL)
	origin := parsed.Scheme + "://" + parsed.Host
	request.Header.Set("User-Agent", adultUserAgent)
	request.Header.Set("Referer", embedURL)
	request.Header.Set("Origin", origin)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := d.doCatalogRequestWithTimeout(request, 12*time.Second)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("VAS API HTTP %d", response.StatusCode)
	}
	var result struct {
		StreamingURL    string `json:"streaming_url"`
		StreamingURLAlt string `json:"streamingUrl"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("VAS API JSON 無法解析：%w", err)
	}
	raw := result.StreamingURL
	if raw == "" {
		raw = result.StreamingURLAlt
	}
	address := adultResolvePlaybackURL(embedURL, raw)
	if address == "" || !isProviderHTTPMediaURL(address) {
		return "", errors.New("VAS API 未回傳有效的 HTTPS 播放網址")
	}
	return address, nil
}

func (d *Downloader) resolveAdultSupJavRoutes(ctx context.Context, pageURL, pageHTML string) ([]providerMedia, []string) {
	servers := adultSupJavServerLinks(pageHTML)
	// TV is the site's normal player choice. All other providers are fallbacks.
	order := []string{"TV", "FST", "EVS", "VAS", "LUC", "VOE", "ST"}
	var routes []providerMedia
	var routeErrors []string
	seen := make(map[string]bool)
	for _, name := range order {
		token, found := servers[name]
		if !found || strings.TrimSpace(token) == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			routeErrors = append(routeErrors, name+": "+err.Error())
			break
		}
		embedHTML, embedURL, err := d.fetchAdultSupJavEmbed(ctx, token, pageURL)
		if err != nil {
			routeErrors = append(routeErrors, name+": "+truncate(err.Error(), 120))
			continue
		}
		if name == "VOE" {
			for hop := 0; hop < 2; hop++ {
				redirect := adultSupJavVOERedirect(embedHTML, embedURL)
				if redirect == "" {
					break
				}
				redirectHTML, finalURL, redirectErr := d.fetchAdultSupJavEmbedURL(ctx, redirect, embedURL)
				if redirectErr != nil {
					routeErrors = append(routeErrors, name+": "+truncate(redirectErr.Error(), 120))
					break
				}
				embedHTML, embedURL = redirectHTML, finalURL
			}
		}
		mediaURL := ""
		referer := embedURL
		switch name {
		case "TV":
			mediaURL = adultSupJavTVHLS(embedHTML, embedURL)
			referer = supJavBaseURL + "/"
		case "FST":
			mediaURL = adultSupJavFSTHLS(embedHTML, embedURL)
		case "VOE":
			mediaURL = adultSupJavVOEHLS(embedHTML, embedURL)
		case "VAS":
			mediaURL, err = d.fetchAdultSupJavVAS(ctx, embedURL, embedHTML)
		case "ST":
			mediaURL = adultSupJavStreamtapeDirectURL(embedHTML, embedURL)
		default: // EVS and LUC use the public packed-player HLS format.
			mediaURL = adultFindM3U8(embedHTML, embedURL)
		}
		if err != nil {
			routeErrors = append(routeErrors, name+": "+truncate(err.Error(), 120))
			continue
		}
		if mediaURL == "" || !isProviderHTTPMediaURL(mediaURL) {
			routeErrors = append(routeErrors, name+": 播放頁未找到有效的影片網址")
			continue
		}
		if seen[mediaURL] {
			continue
		}
		credentials := &providerMediaCredentials{referer: referer, userAgent: adultUserAgent}
		media := providerMedia{
			URL: mediaURL, Referer: referer, credentials: credentials,
			StandardHTTP: runtime.GOOS == "android",
		}
		// HLS may use a .txt or extensionless URL. Validate the actual
		// manifest instead of requiring a .m3u8 filename suffix.
		if name != "ST" {
			playlist, finalURL, playlistErr := d.fetchMediaPlaylist(providerMediaContext(ctx, credentials), mediaURL, referer)
			if playlistErr != nil {
				routeErrors = append(routeErrors, name+": "+truncate(playlistErr.Error(), 120))
				continue
			}
			media.URL = finalURL
			media.Playlist = playlist
			media.Duration = m3u8Duration(playlist)
		}
		seen[media.URL] = true
		routes = append(routes, media)
	}
	return routes, routeErrors
}

func (d *Downloader) fetchAdultSupJavEmbedURL(ctx context.Context, address, referer string) (string, string, error) {
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	requestContext = context.WithValue(requestContext, providerTextUserAgentKey{}, adultUserAgent)
	requestContext = context.WithValue(requestContext, providerTextNoCacheKey{}, true)
	responses := &playbackResponseURLs{}
	requestContext = context.WithValue(requestContext, playbackResponseURLsKey{}, responses)
	body, err := d.fetchProviderText(requestContext, address, referer)
	if err != nil {
		return "", "", err
	}
	finalURL := address
	if actual, ok := responses.values.Load(address); ok {
		if resolved, valid := actual.(string); valid && resolved != "" {
			finalURL = resolved
		}
	}
	return body, finalURL, nil
}
