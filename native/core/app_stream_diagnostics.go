package core

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

var nativeStreamRequestID atomic.Uint64

type nativeStreamDiagnosticKey struct{}

type nativeStreamDiagnostic struct {
	request uint64
	session string
	asset   string
	method  string
	started time.Time
}

func nativeStreamID(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:6])
}

func nativeStreamTarget(address string) (string, string) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", "invalid"
	}
	parts := strings.Split(parsed.EscapedPath(), "/")
	for i, part := range parts {
		if len(part) > 48 {
			parts[i] = "[redacted-" + nativeStreamID(part) + "]"
		}
	}
	return parsed.Hostname(), truncate(strings.Join(parts, "/"), 512)
}

func nativeStreamError(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, errNativeStreamIdleTimeout) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	var wrappedURL *url.Error
	if errors.As(err, &wrappedURL) && wrappedURL.Err != nil {
		err = wrappedURL.Err
	}
	if strings.Contains(strings.ToLower(err.Error()), "tls handshake timeout") {
		return "tls"
	}
	var certificate *tls.CertificateVerificationError
	if errors.As(err, &certificate) {
		return "tls"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return "connect"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "unexpected_eof"
	}
	return "io_error"
}

var nativeStreamURLInError = regexp.MustCompile(`(?i)https?://[^\s"<>]+`)

// nativeStreamErrorDetail records the underlying network cause without leaking
// signed playback URLs, query credentials, or long media tokens into exported logs.
func nativeStreamErrorDetail(err error) string {
	if err == nil {
		return "error_type=<nil> cause=none"
	}
	var wrappedURL *url.Error
	if errors.As(err, &wrappedURL) && wrappedURL.Err != nil {
		err = wrappedURL.Err
	}
	var op *net.OpError
	if errors.As(err, &op) {
		cause := "<nil>"
		if op.Err != nil {
			cause = op.Err.Error()
		}
		cause = nativeStreamURLInError.ReplaceAllString(cause, "[redacted-url]")
		return fmt.Sprintf("error_type=%T op=%s net=%s cause_type=%T cause=%q", err, op.Op, op.Net, op.Err, truncate(cause, 180))
	}
	cause := nativeStreamURLInError.ReplaceAllString(err.Error(), "[redacted-url]")
	return fmt.Sprintf("error_type=%T cause=%q", err, truncate(cause, 180))
}

const (
	nativeStreamHostFailureCooldown      = 90 * time.Second
	nativeStreamHostFailureMaxCooldown   = 15 * time.Minute
	nativeStreamHostFailureResetAfter    = 30 * time.Minute
)

type nativeStreamHostFailureState struct {
	consecutive int
	until      time.Time
	lastFailure time.Time
	reason     string
}

func nativeStreamHostCooldownForFailures(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	cooldown := nativeStreamHostFailureCooldown
	for i := 1; i < failures && cooldown < nativeStreamHostFailureMaxCooldown; i++ {
		cooldown *= 2
		if cooldown > nativeStreamHostFailureMaxCooldown {
			cooldown = nativeStreamHostFailureMaxCooldown
		}
	}
	return cooldown
}

// rememberNativeStreamHostFailure temporarily deprioritizes hosts that failed
// during connection setup or media transfer. Repeated failures after a cooldown
// expires increase the penalty; a successful media body clears the streak.
func (downloader *Downloader) rememberNativeStreamHostFailure(address, reason string) {
	if downloader == nil {
		return
	}
	switch reason {
	case "timeout", "connect", "tls", "dns", "deadline", "unexpected_eof", "upstream_http", "io_error":
	default:
		return
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return
	}

	downloader.streamHostMu.Lock()
	if downloader.streamHostFailures == nil {
		downloader.streamHostFailures = make(map[string]nativeStreamHostFailureState)
	}
	now := time.Now()
	for failedHost, state := range downloader.streamHostFailures {
		if !now.Before(state.until) && (state.lastFailure.IsZero() || now.Sub(state.lastFailure) > nativeStreamHostFailureResetAfter) {
			delete(downloader.streamHostFailures, failedHost)
		}
	}
	state := downloader.streamHostFailures[host]
	if now.Before(state.until) {
		// Concurrent failures during one cooldown window are one failure episode.
		// Do not let parallel range requests immediately force the host to max backoff.
		downloader.streamHostMu.Unlock()
		return
	}
	if state.consecutive > 0 && !state.lastFailure.IsZero() && now.Sub(state.lastFailure) <= nativeStreamHostFailureResetAfter {
		state.consecutive++
	} else {
		state.consecutive = 1
	}
	cooldown := nativeStreamHostCooldownForFailures(state.consecutive)
	state.until = now.Add(cooldown)
	state.lastFailure = now
	state.reason = reason
	downloader.streamHostFailures[host] = state
	downloader.streamHostMu.Unlock()

	downloader.recordDiagnostic(diagnosticEvent{
		Level: "warn", Event: "stream.host_cooldown", Host: host,
		Message: fmt.Sprintf("reason=%s consecutive_failures=%d cooldown_seconds=%d",
			reason, state.consecutive, int(cooldown/time.Second)),
	})
}

func (downloader *Downloader) clearNativeStreamHostFailure(address string) {
	if downloader == nil {
		return
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return
	}
	downloader.streamHostMu.Lock()
	delete(downloader.streamHostFailures, host)
	downloader.streamHostMu.Unlock()
}

func (downloader *Downloader) nativeStreamHostCoolingDown(address string) bool {
	if downloader == nil {
		return false
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	downloader.streamHostMu.Lock()
	defer downloader.streamHostMu.Unlock()
	state, exists := downloader.streamHostFailures[host]
	return exists && time.Now().Before(state.until)
}

func (stream *nativeStreamServer) nativeLog(info nativeStreamDiagnostic, event, address, detail string, status int, contentType string) {
	host, target := nativeStreamTarget(address)
	mediaType, _, _ := mime.ParseMediaType(contentType)
	stream.downloader.recordDiagnostic(diagnosticEvent{
		Event: "stream." + event, Level: "info", Host: host, HTTPStatus: status, ResponseType: mediaType,
		Message: fmt.Sprintf("request=%d session=%s asset=%s method=%s path=%s elapsed_ms=%d %s",
			info.request, info.session, info.asset, info.method, target, time.Since(info.started).Milliseconds(), detail),
	})
}

func nativePlaylistKind(body string) string {
	if strings.Contains(body, "#EXT-X-STREAM-INF:") || strings.Contains(body, "#EXT-X-I-FRAME-STREAM-INF:") {
		return "master_playlist"
	}
	return "media_playlist"
}

type nativeDiagnosticBody struct {
	io.ReadCloser
	stream  *nativeStreamServer
	info    nativeStreamDiagnostic
	address string
}

func (body *nativeDiagnosticBody) Read(buffer []byte) (int, error) {
	n, err := body.ReadCloser.Read(buffer)
	if err != nil && err != io.EOF {
		reason := nativeStreamError(err)
		body.stream.downloader.rememberNativeStreamHostFailure(body.address, reason)
		body.stream.nativeLog(body.info, "upstream_read_error", body.address, "error="+reason+" "+nativeStreamErrorDetail(err), 0, "")
	}
	return n, err
}

type nativeDiagnosticWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	err    error
}

func (writer *nativeDiagnosticWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *nativeDiagnosticWriter) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	n, err := writer.ResponseWriter.Write(data)
	writer.bytes += int64(n)
	if err != nil {
		writer.err = err
	}
	return n, err
}

func (writer *nativeDiagnosticWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// nativeInitialFlushWriter flushes the first forwarded media bytes immediately.
// Go's HTTP server can otherwise buffer a tiny initial MP4 chunk while the
// upstream waits for more data, making the local player appear stuck at startup.
type nativeInitialFlushWriter struct {
	destination io.Writer
	flusher     http.Flusher
	flushed     bool
}

func (writer *nativeInitialFlushWriter) Write(data []byte) (int, error) {
	n, err := writer.destination.Write(data)
	if n > 0 && !writer.flushed {
		writer.flusher.Flush()
		writer.flushed = true
	}
	return n, err
}

func (stream *nativeStreamServer) nativeTrace(info nativeStreamDiagnostic, address string) *httptrace.ClientTrace {
	phase := func(name string, err error, detail string) {
		message := "phase=" + name + " error=" + nativeStreamError(err)
		if detail != "" {
			message += " " + detail
		}
		stream.nativeLog(info, "network_phase", address, message, 0, "")
	}
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			phase("dns_start", nil, "")
		},
		DNSDone: func(value httptrace.DNSDoneInfo) {
			phase("dns_done", value.Err, "")
		},
		ConnectStart: func(network, remote string) {
			phase("connect_start", nil, fmt.Sprintf("network=%s remote=%s", network, remote))
		},
		ConnectDone: func(network, remote string, err error) {
			phase("connect_done", err, fmt.Sprintf("network=%s remote=%s", network, remote))
		},
		TLSHandshakeStart: func() {
			phase("tls_start", nil, "")
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			phase("tls_done", err, "")
		},
		GotConn: func(value httptrace.GotConnInfo) {
			remote := ""
			if value.Conn != nil && value.Conn.RemoteAddr() != nil {
				remote = value.Conn.RemoteAddr().String()
			}
			phase("got_connection", nil, fmt.Sprintf("reused=%t remote=%s", value.Reused, remote))
		},
		GotFirstResponseByte: func() {
			phase("response_headers_first_byte", nil, "")
		},
	}
}


type nativeStreamIdleTimeoutError struct{}

func (*nativeStreamIdleTimeoutError) Error() string   { return "media stream idle timeout" }
func (*nativeStreamIdleTimeoutError) Timeout() bool   { return true }
func (*nativeStreamIdleTimeoutError) Temporary() bool { return true }

var errNativeStreamIdleTimeout = &nativeStreamIdleTimeoutError{}

// nativeStreamIdleBody bounds inactivity between upstream reads without setting
// a total playback duration. A timeout cancels only the stalled HTTP request.
type nativeStreamIdleBody struct {
	io.ReadCloser
	timeout   time.Duration
	cancel    context.CancelFunc
	onTimeout func()
}

func (body *nativeStreamIdleBody) Read(buffer []byte) (int, error) {
	fired := make(chan struct{})
	timer := time.AfterFunc(body.timeout, func() {
		body.cancel()
		if body.onTimeout != nil {
			body.onTimeout()
		}
		close(fired)
	})
	n, err := body.ReadCloser.Read(buffer)
	if !timer.Stop() {
		<-fired
		return n, errNativeStreamIdleTimeout
	}
	return n, err
}

func (body *nativeStreamIdleBody) Close() error {
	body.cancel()
	return body.ReadCloser.Close()
}
