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
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
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

const nativeStreamHostFailureCooldown = 90 * time.Second

// rememberNativeStreamHostFailure temporarily deprioritizes hosts that failed
// during connection setup or media transfer. The cooldown is shared by the
// playback resolver and stream proxy through Downloader.
func (downloader *Downloader) rememberNativeStreamHostFailure(address, reason string) {
	switch reason {
	case "timeout", "connect", "tls", "dns", "deadline", "unexpected_eof", "upstream_http":
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
		downloader.streamHostFailures = make(map[string]time.Time)
	}
	now := time.Now()
	for failedHost, until := range downloader.streamHostFailures {
		if !now.Before(until) {
			delete(downloader.streamHostFailures, failedHost)
		}
	}
	previousUntil, wasCooling := downloader.streamHostFailures[host]
	previouslyCooling := wasCooling && now.Before(previousUntil)
	until := now.Add(nativeStreamHostFailureCooldown)
	if current, exists := downloader.streamHostFailures[host]; !exists || current.Before(until) {
		downloader.streamHostFailures[host] = until
	}
	downloader.streamHostMu.Unlock()
	if !previouslyCooling {
		downloader.recordDiagnostic(diagnosticEvent{
			Level: "warn", Event: "stream.host_cooldown", Host: host,
			Message: fmt.Sprintf("reason=%s cooldown_seconds=%d", reason, int(nativeStreamHostFailureCooldown/time.Second)),
		})
	}
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
	until, exists := downloader.streamHostFailures[host]
	if !exists {
		return false
	}
	if !time.Now().Before(until) {
		delete(downloader.streamHostFailures, host)
		return false
	}
	return true
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
		body.stream.nativeLog(body.info, "upstream_read_error", body.address, "error="+reason, 0, "")
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
	phase := func(name string, err error) {
		stream.nativeLog(info, "network_phase", address, "phase="+name+" error="+nativeStreamError(err), 0, "")
	}
	return &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { phase("dns_start", nil) },
		DNSDone:           func(value httptrace.DNSDoneInfo) { phase("dns_done", value.Err) },
		ConnectStart:      func(string, string) { phase("connect_start", nil) },
		ConnectDone:       func(_, _ string, err error) { phase("connect_done", err) },
		TLSHandshakeStart: func() { phase("tls_start", nil) },
		TLSHandshakeDone:  func(_ tls.ConnectionState, err error) { phase("tls_done", err) },
		GotConn: func(value httptrace.GotConnInfo) {
			stream.nativeLog(info, "network_phase", address, fmt.Sprintf("phase=got_connection reused=%t", value.Reused), 0, "")
		},
		GotFirstResponseByte: func() { phase("response_headers_first_byte", nil) },
	}
}
