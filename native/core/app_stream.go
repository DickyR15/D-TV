package core

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type nativeStreamAsset struct {
	kind        string
	address     string
	data        []byte
	contentType string
	total       int64
	etag        string
	modified    string
}

type nativeStreamSession struct {
	credentials  *providerMediaCredentials
	mu           sync.Mutex
	assets       map[string]nativeStreamAsset
	referer      string
	key          []byte
	ctx          context.Context
	cancel       context.CancelFunc
	lastUsed     time.Time
	diagnosticID string
	created      time.Time
}

type nativeStreamServer struct {
	mu         sync.Mutex
	downloader *Downloader
	address    string
	sessions   map[string]*nativeStreamSession
	server     *http.Server
}

const (
	// Race all IPv4 CDN addresses in parallel. Captured traces show one IPv4
	// address can stall for two seconds while a later address connects quickly.
	// Preserve an IPv6 fallback within the same overall dial deadline.
	nativeStreamDialTimeout           = 5 * time.Second
	nativeStreamIPv4DialBudget        = 2 * time.Second
	// Working TLS handshakes in the latest trace completed in under 350 ms;
	// dead CDN handshakes repeatedly consumed the full 2.5-second budget.
	nativeStreamTLSHandshakeTimeout   = 1500 * time.Millisecond
	nativeStreamResponseHeaderTimeout = 5 * time.Second
	nativeStreamReadIdleTimeout       = 12 * time.Second
	nativeStreamResumeAttempts        = 2
	nativeStreamMaxIdleConnections    = 16
	nativeStreamMaxIdlePerHost         = 4
	nativeStreamIdleConnTimeout        = 45 * time.Second
)

// nativeDialResolvedIPs races a set of already-resolved addresses. This avoids
// serially spending seconds on an unreachable CDN IP when another address for
// the same hostname is responsive.
func nativeDialResolvedIPs(
	ctx context.Context,
	network string,
	port string,
	ips []net.IPAddr,
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	if len(ips) == 0 {
		return nil, errors.New("no addresses available")
	}
	type dialResult struct {
		conn net.Conn
		err  error
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan dialResult)
	for _, entry := range ips {
		address := net.JoinHostPort(entry.String(), port)
		go func(address string) {
			conn, err := dial(attemptCtx, network, address)
			select {
			case results <- dialResult{conn: conn, err: err}:
			case <-attemptCtx.Done():
				if conn != nil {
					_ = conn.Close()
				}
			}
		}(address)
	}
	var lastErr error
	for completed := 0; completed < len(ips); {
		select {
		case result := <-results:
			completed++
			if result.err == nil {
				cancel()
				return result.conn, nil
			}
			lastErr = result.err
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("dial deadline reached: %v: %w", lastErr, ctx.Err())
			}
			return nil, ctx.Err()
		}
	}
	if lastErr == nil {
		lastErr = errors.New("all address attempts failed")
	}
	return nil, lastErr
}

// nativeStreamDialContext prefers IPv4 on the observed TV network, but races
// all A-record addresses in parallel. If no IPv4 address connects within its
// short budget, IPv6 remains available within the original overall deadline.
func nativeStreamDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			return dialer.DialContext(ctx, network, address)
		}

		dialCtx, cancel := context.WithTimeout(ctx, nativeStreamDialTimeout)
		defer cancel()
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return dialer.DialContext(dialCtx, network, address)
		}
		if ip := net.ParseIP(host); ip != nil {
			if ip.To4() != nil {
				return dialer.DialContext(dialCtx, "tcp4", net.JoinHostPort(ip.To4().String(), port))
			}
			return dialer.DialContext(dialCtx, "tcp6", address)
		}

		resolved, lookupErr := net.DefaultResolver.LookupIPAddr(dialCtx, host)
		if lookupErr != nil {
			if dialCtx.Err() != nil {
				return nil, fmt.Errorf("resolve media host: %w", lookupErr)
			}
			return dialer.DialContext(dialCtx, network, address)
		}
		var ipv4, ipv6 []net.IPAddr
		for _, entry := range resolved {
			if entry.IP.To4() != nil {
				ipv4 = append(ipv4, net.IPAddr{IP: entry.IP.To4()})
			} else if entry.IP.To16() != nil {
				ipv6 = append(ipv6, entry)
			}
		}

		var ipv4Err error
		if len(ipv4) > 0 {
			ipv4Ctx, cancelIPv4 := context.WithTimeout(dialCtx, nativeStreamIPv4DialBudget)
			conn, err := nativeDialResolvedIPs(ipv4Ctx, "tcp4", port, ipv4, dialer.DialContext)
			cancelIPv4()
			if err == nil {
				return conn, nil
			}
			ipv4Err = err
			if dialCtx.Err() != nil {
				return nil, fmt.Errorf("IPv4 dial failed: %v; overall dial deadline reached: %w", ipv4Err, dialCtx.Err())
			}
		}

		if len(ipv6) > 0 {
			conn, ipv6Err := nativeDialResolvedIPs(dialCtx, "tcp6", port, ipv6, dialer.DialContext)
			if ipv6Err == nil {
				return conn, nil
			}
			if ipv4Err != nil {
				return nil, fmt.Errorf("IPv4 dial failed: %v; IPv6 fallback failed: %w", ipv4Err, ipv6Err)
			}
			return nil, fmt.Errorf("IPv6 dial failed: %w", ipv6Err)
		}
		if ipv4Err != nil {
			return nil, fmt.Errorf("IPv4 dial failed: %w", ipv4Err)
		}
		return nil, errors.New("media host has no usable IP addresses")
	}
}

func newNativeStreamTransport(base *http.Transport) *http.Transport {
	transport := base.Clone()
	// MPV can issue several Range requests while seeking or filling its demuxer.
	// Reusing the same CDN connection avoids paying TCP/TLS setup for every read.
	transport.DisableKeepAlives = false
	// Pin the TLS ALPN protocol as well as disabling the Go HTTP/2 handler.
	// Some playback CDNs still sent HTTP/2 SETTINGS frames when the transport
	// expected an HTTP/1.x response, making every media route fail to open.
	tlsConfig := &tls.Config{}
	if transport.TLSClientConfig != nil {
		tlsConfig = transport.TLSClientConfig.Clone()
	}
	tlsConfig.NextProtos = []string{"http/1.1"}
	transport.TLSClientConfig = tlsConfig
	// Android 9 x86 emulators running this ARMv7 core through Houdini have
	// crashed in Go's net/http HTTP/2 read loop during repeated MP4 Range
	// requests. Keep media streaming on HTTP/1.1 while retaining Keep-Alive.
	// An explicit empty TLSNextProto map also prevents automatic HTTP/2 setup
	// on clones created by the CDN transport.
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	transport.MaxIdleConns = nativeStreamMaxIdleConnections
	transport.MaxIdleConnsPerHost = nativeStreamMaxIdlePerHost
	transport.IdleConnTimeout = nativeStreamIdleConnTimeout
	dialer := &net.Dialer{Timeout: nativeStreamDialTimeout}
	transport.DialContext = nativeStreamDialContext(dialer)
	transport.TLSHandshakeTimeout = nativeStreamTLSHandshakeTimeout
	transport.ResponseHeaderTimeout = nativeStreamResponseHeaderTimeout
	return transport
}

func (stream *nativeStreamServer) nativeRequest(request *http.Request) (*http.Response, error) {
	sourceClient := stream.downloader.streamClient
	if sourceClient == nil {
		sourceClient = stream.downloader.client
	}
	client := *sourceClient
	client.Timeout = 0
	info, _ := request.Context().Value(nativeStreamDiagnosticKey{}).(nativeStreamDiagnostic)
	if info.started.IsZero() {
		info.started = time.Now()
	}
	info.method = request.Method
	stream.nativeLog(info, "upstream_start", request.URL.String(), "", 0, "")
	requestCtx, cancel := context.WithCancel(request.Context())
	request = request.WithContext(httptrace.WithClientTrace(requestCtx, stream.nativeTrace(info, request.URL.String())))
	response, err := stream.downloader.doMediaRequestWithClient(request, &client)
	if err != nil {
		cancel()
		reason := nativeStreamError(err)
		stream.downloader.rememberNativeStreamHostFailure(request.URL.String(), reason)
		stream.nativeLog(info, "upstream_error", request.URL.String(), "error="+reason+" "+nativeStreamErrorDetail(err), 0, "")
		return response, err
	}
	address := request.URL.String()
	if response.Request != nil && response.Request.URL != nil {
		address = response.Request.URL.String()
	}
	stream.nativeLog(info, "upstream_headers", address, "", response.StatusCode, response.Header.Get("Content-Type"))
	// Do not clear a host cooldown from status headers alone: several CDNs
	// return HTTP 206 and then stop sending the media body. A complete body
	// transfer (or a fully read/re-written playlist) proves that the host recovered.
	diagnosticBody := &nativeDiagnosticBody{ReadCloser: response.Body, stream: stream, info: info, address: address}
	response.Body = &nativeStreamIdleBody{
		ReadCloser: diagnosticBody,
		timeout:    nativeStreamReadIdleTimeout,
		cancel:     cancel,
		onTimeout: func() {
			stream.downloader.rememberNativeStreamHostFailure(address, "timeout")
			stream.nativeLog(info, "upstream_idle_timeout", address,
				fmt.Sprintf("idle_timeout_ms=%d", nativeStreamReadIdleTimeout.Milliseconds()),
				0, response.Header.Get("Content-Type"))
		},
	}
	return response, nil
}

var nativePlaylistURI = regexp.MustCompile(`URI="([^"]+)"`)

func newNativeStreamServer(d *Downloader) (*nativeStreamServer, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("无法初始化本机播放器")
	}
	stream := &nativeStreamServer{downloader: d, address: "http://" + listener.Addr().String(), sessions: map[string]*nativeStreamSession{}}
	server := &http.Server{Handler: http.HandlerFunc(stream.nativeServe), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	stream.server = server
	go func() { _ = server.Serve(listener) }()
	return stream, nil
}

func (stream *nativeStreamServer) nativeOpen(media providerMedia) (string, string) {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		panic(err)
	}
	token := hex.EncodeToString(tokenBytes)
	ctx, cancel := context.WithCancel(providerMediaContext(context.Background(), media.credentials))
	session := &nativeStreamSession{assets: map[string]nativeStreamAsset{}, referer: media.Referer, key: media.HLSKey, ctx: ctx, cancel: cancel, lastUsed: time.Now(), credentials: media.credentials, diagnosticID: nativeStreamID(token), created: time.Now()}
	stream.mu.Lock()
	for id, old := range stream.sessions {
		if time.Since(old.lastUsed) > 10*time.Minute {
			stream.nativeCancel(old, "idle_expired")
			delete(stream.sessions, id)
		}
	}
	if len(stream.sessions) >= 8 {
		oldest := ""
		for id, old := range stream.sessions {
			if oldest == "" || old.lastUsed.Before(stream.sessions[oldest].lastUsed) {
				oldest = id
			}
		}
		stream.nativeCancel(stream.sessions[oldest], "capacity_eviction")
		delete(stream.sessions, oldest)
	}
	stream.sessions[token] = session
	stream.mu.Unlock()
	entry := nativeStreamAsset{kind: "media", address: media.URL, contentType: "video/mp4"}
	parsedURL, parseErr := url.Parse(media.URL)
	isHLS := media.Playlist != "" || (parseErr == nil && strings.HasSuffix(strings.ToLower(parsedURL.Path), ".m3u8"))
	if isHLS {
		entry.contentType = "application/vnd.apple.mpegurl"
	}
	if media.Playlist != "" {
		entry.data = []byte(media.Playlist)
		entry.contentType = "application/vnd.apple.mpegurl"
	}
	if isHLS || strings.Contains(entry.contentType, "mpegurl") {
		entry.kind = "entry_playlist"
	}
	stream.nativeLog(nativeStreamDiagnostic{session: session.diagnosticID, asset: entry.kind, started: session.created}, "session_open", media.URL, "", 0, "")
	return stream.nativeAsset(token, session, entry), token
}

func (stream *nativeStreamServer) nativeCancel(session *nativeStreamSession, reason string) {
	stream.nativeLog(nativeStreamDiagnostic{session: session.diagnosticID, started: session.created}, "session_cancel", "", "reason="+reason, 0, "")
	session.cancel()
}

func (stream *nativeStreamServer) nativeRelease(token string) {
	stream.mu.Lock()
	_, exists := stream.sessions[token]
	if session := stream.sessions[token]; session != nil {
		stream.nativeCancel(session, "release")
		delete(stream.sessions, token)
	}
	stream.mu.Unlock()
	if exists && stream.downloader != nil && stream.downloader.streamClient != nil {
		// Active requests are canceled with the session context; discard only
		// idle pooled sockets so a later episode cannot inherit stale idle state.
		stream.downloader.streamClient.CloseIdleConnections()
	}
}

func (stream *nativeStreamServer) nativeAsset(token string, session *nativeStreamSession, asset nativeStreamAsset) string {
	digest := sha256.Sum256([]byte(asset.address + "\x00" + asset.contentType))
	extension := ".ts"
	if parsed, err := url.Parse(asset.address); err == nil {
		switch candidate := strings.ToLower(path.Ext(parsed.Path)); candidate {
		case ".m3u8", ".ts", ".m4s", ".mp4", ".aac", ".m4a", ".mp3", ".vtt", ".webvtt", ".key":
			extension = candidate
		}
	}
	switch asset.contentType {
	case "application/vnd.apple.mpegurl":
		extension = ".m3u8"
	case "application/octet-stream":
		extension = ".key"
	case "video/mp4":
		extension = ".mp4"
	}
	id := hex.EncodeToString(digest[:12]) + extension
	session.mu.Lock()
	if previous, found := session.assets[id]; !found || len(previous.data) == 0 || len(asset.data) > 0 {
		session.assets[id] = asset
	}
	session.mu.Unlock()
	return stream.address + "/" + token + "/" + id
}

func (stream *nativeStreamServer) nativeRewrite(token string, session *nativeStreamSession, body, base string) (string, error) {
	var output []string
	nextPlaylist := false
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		text := strings.TrimSpace(line)
		rewrite := func(reference string, key bool, contentType string) string {
			if strings.HasPrefix(reference, "data:") {
				return reference
			}
			parsed, err := url.Parse(base)
			if err != nil {
				return ""
			}
			relative, err := url.Parse(reference)
			if err != nil {
				return ""
			}
			address := parsed.ResolveReference(relative).String()
			if !isProviderHTTPMediaURL(address) {
				return ""
			}
			kind := "segment"
			switch contentType {
			case "application/vnd.apple.mpegurl":
				kind = "child_playlist"
			case "application/octet-stream":
				kind = "key"
			case "video/mp4":
				kind = "map"
			}
			asset := nativeStreamAsset{kind: kind, address: address, contentType: contentType}
			if key && len(session.key) == 16 {
				asset.data = append([]byte{}, session.key...)
				asset.contentType = "application/octet-stream"
			}
			return stream.nativeAsset(token, session, asset)
		}
		if text != "" && !strings.HasPrefix(text, "#") {
			contentType := ""
			if nextPlaylist {
				contentType = "application/vnd.apple.mpegurl"
			}
			line = rewrite(text, false, contentType)
			nextPlaylist = false
			if line == "" {
				return "", errors.New("播放列表中的媒体地址无效")
			}
		} else if strings.Contains(text, "URI=") {
			invalid := false
			line = nativePlaylistURI.ReplaceAllStringFunc(line, func(match string) string {
				reference := nativePlaylistURI.FindStringSubmatch(match)[1]
				key := strings.HasPrefix(text, "#EXT-X-KEY:") || strings.HasPrefix(text, "#EXT-X-SESSION-KEY:")
				contentType := ""
				switch {
				case key:
					contentType = "application/octet-stream"
				case strings.HasPrefix(text, "#EXT-X-MAP:"):
					contentType = "video/mp4"
				case strings.HasPrefix(text, "#EXT-X-MEDIA:"), strings.HasPrefix(text, "#EXT-X-I-FRAME-STREAM-INF:"), strings.HasPrefix(text, "#EXT-X-RENDITION-REPORT:"):
					contentType = "application/vnd.apple.mpegurl"
				}
				updated := rewrite(reference, key, contentType)
				if updated == "" {
					invalid = true
				}
				return "URI=\"" + updated + "\""
			})
			if invalid {
				return "", errors.New("播放列表中的附属地址无效")
			}
		}
		if strings.HasPrefix(text, "#EXT-X-STREAM-INF:") {
			nextPlaylist = true
		}
		output = append(output, line)
	}
	return strings.Join(output, "\n"), nil
}

func (stream *nativeStreamServer) nativeServe(writer http.ResponseWriter, request *http.Request) {
	info := nativeStreamDiagnostic{request: nativeStreamRequestID.Add(1), started: time.Now(), method: request.Method, asset: "unknown"}
	observed := &nativeDiagnosticWriter{ResponseWriter: writer}
	writer = observed
	stream.nativeLog(info, "local_request", "", "", 0, "")
	defer func() {
		status := observed.status
		if status == 0 {
			status = http.StatusOK
		}
		stream.nativeLog(info, "local_response", "", fmt.Sprintf("bytes=%d write_error=%s client_context=%s", observed.bytes, nativeStreamError(observed.err), nativeStreamError(request.Context().Err())), status, writer.Header().Get("Content-Type"))
	}()
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 2 {
		http.NotFound(writer, request)
		return
	}
	info.session = nativeStreamID(parts[0])
	stream.mu.Lock()
	session := stream.sessions[parts[0]]
	if session != nil {
		session.lastUsed = time.Now()
	}
	stream.mu.Unlock()
	if session == nil {
		http.Error(writer, "播放已结束", http.StatusGone)
		return
	}
	session.mu.Lock()
	asset, found := session.assets[parts[1]]
	session.mu.Unlock()
	info.session = session.diagnosticID
	info.asset = asset.kind
	stream.nativeLog(info, "asset_request", asset.address, "", 0, asset.contentType)
	if !found {
		http.NotFound(writer, request)
		return
	}
	ctx, cancel := context.WithCancel(providerMediaContext(request.Context(), session.credentials))
	defer cancel()
	ctx = context.WithValue(ctx, nativeStreamDiagnosticKey{}, info)
	stop := context.AfterFunc(session.ctx, cancel)
	defer stop()
	writer.Header().Set("Cache-Control", "no-store")
	if len(asset.data) > 0 && asset.total > int64(len(asset.data)) {
		if stream.nativeServePrefix(ctx, writer, request, session, asset) {
			return
		}
		asset.data = nil
	}
	if len(asset.data) > 0 {
		if strings.Contains(asset.contentType, "mpegurl") {
			body, err := stream.nativeRewrite(parts[0], session, string(asset.data), asset.address)
			stream.nativeLog(info, "playlist_rewrite", asset.address, "kind="+nativePlaylistKind(string(asset.data))+" error="+nativeStreamError(err), 0, "")
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadGateway)
				return
			}
			writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			if request.Method == http.MethodGet {
				_, _ = io.WriteString(writer, body)
			}
		} else {
			writer.Header().Set("Content-Type", asset.contentType)
			if asset.etag != "" {
				writer.Header().Set("ETag", asset.etag)
			}
			http.ServeContent(writer, request, parts[1], time.Time{}, bytes.NewReader(asset.data))
		}
		return
	}
	upstream, err := http.NewRequestWithContext(ctx, request.Method, asset.address, nil)
	if err != nil {
		http.Error(writer, "媒体地址无效", http.StatusBadGateway)
		return
	}
	upstream.Header.Set("User-Agent", userAgent)
	upstream.Header.Set("Referer", session.referer)
	for _, name := range []string{"Range", "If-Range"} {
		if value := request.Header.Get(name); value != "" {
			upstream.Header.Set(name, value)
		}
	}
	response, err := stream.nativeRequest(upstream)
	if err != nil {
		http.Error(writer, "读取媒体失败，请重试", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
		failedAddress := upstream.URL.String()
		if response.Request != nil && response.Request.URL != nil {
			failedAddress = response.Request.URL.String()
		}
		stream.downloader.rememberNativeStreamHostFailure(failedAddress, "upstream_http")
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		err := stream.downloader.catalogResponseError(upstream, response, body)
		http.Error(writer, err.Error(), response.StatusCode)
		return
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	finalURL := upstream.URL
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL
	}
	contentIsMP4 := strings.Contains(contentType, "video/mp4")
	playlist := strings.Contains(contentType, "mpegurl") ||
		(strings.HasSuffix(strings.ToLower(finalURL.Path), ".m3u8") && !contentIsMP4)
	reader := bufio.NewReader(response.Body)
	// Avoid waiting for an arbitrary 512-byte peek before forwarding MP4 data.
	if request.Method == http.MethodGet && !playlist && !contentIsMP4 {
		peek, _ := reader.Peek(10)
		bodyIsPlaylist := strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(string(peek), "\ufeff")), "#EXTM3U")
		if bodyIsPlaylist {
			playlist = true
		}
	}
	if playlist && request.Method == http.MethodHead {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		writer.WriteHeader(http.StatusOK)
		return
	}
	if playlist && request.Method == http.MethodGet {
		body, err := io.ReadAll(io.LimitReader(reader, 4<<20+1))
		if err != nil || len(body) > 4<<20 {
			stream.nativeLog(info, "playlist_read_failed", finalURL.String(), "error="+nativeStreamError(err), 0, contentType)
			http.Error(writer, "播放列表过大或读取失败", http.StatusBadGateway)
			return
		}
		text := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
		if !strings.HasPrefix(text, "#EXTM3U") {
			stream.nativeLog(info, "playlist_invalid", finalURL.String(), "", 0, contentType)
			http.Error(writer, "播放列表无效", http.StatusBadGateway)
			return
		}
		rewritten, err := stream.nativeRewrite(parts[0], session, text, finalURL.String())
		stream.nativeLog(info, "playlist_rewrite", finalURL.String(), "kind="+nativePlaylistKind(text)+" error="+nativeStreamError(err), 0, contentType)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadGateway)
			return
		}
		stream.downloader.clearNativeStreamHostFailure(finalURL.String())
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(writer, rewritten)
		return
	}
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := response.Header.Get(name); value != "" {
			writer.Header().Set(name, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	if request.Method == http.MethodGet {
		// Flush the response headers immediately so the player does not wait
		// for net/http's small-write buffer to fill while the CDN is streaming.
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
			forwarded, err := stream.nativeCopyMediaWithResume(
				ctx,
				&nativeInitialFlushWriter{destination: writer, flusher: flusher},
				upstream,
				finalURL.String(),
				response,
				reader,
				info,
			)
			if err != nil {
				stream.nativeLog(info, "transfer_error", finalURL.String(),
					fmt.Sprintf("error=%s bytes_forwarded=%d local_response_bytes=%d %s",
						nativeStreamError(err), forwarded, observed.bytes, nativeStreamErrorDetail(err)),
					0, contentType)
			}
		}
	}
}


func nativeParseContentRange(value string) (start, end, total int64, ok bool) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "bytes") {
		return 0, 0, 0, false
	}
	parts := strings.SplitN(fields[1], "/", 2)
	if len(parts) != 2 || parts[1] == "*" {
		return 0, 0, 0, false
	}
	bounds := strings.SplitN(parts[0], "-", 2)
	if len(bounds) != 2 {
		return 0, 0, 0, false
	}
	start, errStart := strconv.ParseInt(bounds[0], 10, 64)
	end, errEnd := strconv.ParseInt(bounds[1], 10, 64)
	total, errTotal := strconv.ParseInt(parts[1], 10, 64)
	if errStart != nil || errEnd != nil || errTotal != nil ||
		start < 0 || end < start || total <= end {
		return 0, 0, 0, false
	}
	return start, end, total, true
}

func nativeMediaResponseSpan(response *http.Response) (start, end, total int64, resumable bool) {
	if response == nil {
		return 0, 0, 0, false
	}
	if response.StatusCode == http.StatusPartialContent {
		start, end, total, ok := nativeParseContentRange(response.Header.Get("Content-Range"))
		if !ok {
			return 0, 0, 0, false
		}
		if response.ContentLength > end-start+1 {
			return 0, 0, 0, false
		}
		return start, end, total, true
	}
	if response.StatusCode == http.StatusOK &&
		response.ContentLength > 0 &&
		strings.Contains(strings.ToLower(response.Header.Get("Accept-Ranges")), "bytes") {
		return 0, response.ContentLength - 1, response.ContentLength, true
	}
	return 0, 0, 0, false
}

// nativeCopyMediaWithResume keeps the local response open while repairing an
// upstream body that ended before its advertised byte range was complete.
// MPV receives one continuous byte stream; retries happen only upstream.
func (stream *nativeStreamServer) nativeCopyMediaWithResume(
	ctx context.Context,
	destination io.Writer,
	originalRequest *http.Request,
	address string,
	response *http.Response,
	firstReader io.Reader,
	info nativeStreamDiagnostic,
) (int64, error) {
	if response == nil || response.Body == nil {
		return 0, errors.New("media response body is unavailable")
	}
	start, end, total, resumable := nativeMediaResponseSpan(response)
	validator := response.Header.Get("ETag")
	if validator == "" {
		validator = response.Header.Get("Last-Modified")
	}

	currentResponse := response
	currentReader := firstReader
	var written int64
	reportIncomplete := func(cause error, attempt int, stage string) (int64, error) {
		expected := int64(0)
		if resumable {
			expected = end - start + 1
		}
		stream.nativeLog(info, "transfer_incomplete", address,
			fmt.Sprintf("stage=%s error=%s bytes_forwarded=%d expected_bytes=%d resumable=%t range_start=%d range_end=%d total_bytes=%d retry_attempt=%d %s",
				stage, nativeStreamError(cause), written, expected, resumable, start, end, total, attempt,
				nativeStreamErrorDetail(cause)),
			0, currentResponse.Header.Get("Content-Type"))
		return written, cause
	}
	for attempt := 0; ; attempt++ {
		n, copyErr := io.Copy(destination, currentReader)
		written += n
		_ = currentResponse.Body.Close()

		expected := int64(0)
		if resumable {
			expected = end - start + 1
		}
		incomplete := copyErr != nil || resumable && written < expected
		if !incomplete {
			stream.downloader.clearNativeStreamHostFailure(address)
			return written, nil
		}
		if ctx.Err() != nil {
			// A user switch or session release is an intentional cancellation, not a
			// damaged transfer. Avoid counting it as a failed CDN attempt.
			return written, ctx.Err()
		}
		// One idle-timeout rescue is worthwhile: some CDNs send headers and a
		// small prefix, then pause long enough to trip the per-read watchdog.
		// Resume from the next byte once, but don't loop on a CDN that stays idle.
		if errors.Is(copyErr, errNativeStreamIdleTimeout) && attempt >= 1 {
			return reportIncomplete(copyErr, attempt, "idle_timeout_retry_exhausted")
		}
		if !resumable || attempt >= nativeStreamResumeAttempts || written >= expected {
			if copyErr == nil {
				copyErr = io.ErrUnexpectedEOF
			}
			return reportIncomplete(copyErr, attempt, "resume_unavailable_or_exhausted")
		}

		nextStart := start + written
		if nextStart < start || nextStart > end {
			return reportIncomplete(io.ErrUnexpectedEOF, attempt, "invalid_resume_offset")
		}
		stream.nativeLog(info, "resume_attempt", address,
			fmt.Sprintf("attempt=%d start=%d end=%d total=%d forwarded=%d",
				attempt+1, nextStart, end, total, written),
			0, currentResponse.Header.Get("Content-Type"))

		retryRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return reportIncomplete(err, attempt, "resume_request_build")
		}
		for _, name := range []string{"User-Agent", "Referer"} {
			if value := originalRequest.Header.Get(name); value != "" {
				retryRequest.Header.Set(name, value)
			}
		}
		retryRequest.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", nextStart, end))
		if validator != "" {
			retryRequest.Header.Set("If-Range", validator)
		}
		retryResponse, err := stream.nativeRequest(retryRequest)
		if err != nil {
			stream.nativeLog(info, "resume_failed", address,
				"attempt="+strconv.Itoa(attempt+1)+" error="+nativeStreamError(err)+" "+nativeStreamErrorDetail(err),
				0, "")
			return reportIncomplete(err, attempt, "resume_request_failed")
		}
		resumeStart, resumeEnd, resumeTotal, rangeOK := nativeParseContentRange(retryResponse.Header.Get("Content-Range"))
		if retryResponse.StatusCode != http.StatusPartialContent ||
			!rangeOK || resumeStart != nextStart || resumeEnd > end || resumeTotal != total {
			_ = retryResponse.Body.Close()
			stream.nativeLog(info, "resume_rejected", address,
				fmt.Sprintf("attempt=%d status=%d requested_start=%d requested_end=%d expected_total=%d response_range=%s",
					attempt+1, retryResponse.StatusCode, nextStart, end, total, retryResponse.Header.Get("Content-Range")),
				retryResponse.StatusCode, retryResponse.Header.Get("Content-Type"))
			return reportIncomplete(fmt.Errorf("media range resume rejected: HTTP %d", retryResponse.StatusCode), attempt, "resume_range_rejected")
		}
		stream.nativeLog(info, "resume_response", address,
			fmt.Sprintf("attempt=%d range_start=%d range_end=%d total=%d",
				attempt+1, resumeStart, resumeEnd, resumeTotal),
			retryResponse.StatusCode, retryResponse.Header.Get("Content-Type"))
		currentResponse = retryResponse
		currentReader = retryResponse.Body
	}
}
