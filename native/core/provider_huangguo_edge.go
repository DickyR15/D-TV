package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	huangguoEdgeProtocol    = "dx150-edge-v1"
	huangguoEdgeScope       = "playback"
	huangguoEdgeMaxSequence = uint32(16384)
	huangguoEdgeMaxBody     = 1 << 20
)

var errHuangguoEdgeSessionExpired = errors.New("黃果 AI 播放會話已失效")

type huangguoEdgeSession struct {
	baseURL     string
	info        string
	kid         string
	sid         string
	visitorID   string
	requestKey  []byte
	responseKey []byte
	clockOffset int64
	expiresAt   time.Time
	sequence    uint32
}

func huangguoEdgeNumber(value any) int64 {
	switch number := value.(type) {
	case float64:
		return int64(number)
	case int:
		return int64(number)
	case int64:
		return number
	case json.Number:
		result, _ := number.Int64()
		return result
	case string:
		result, _ := strconv.ParseInt(number, 10, 64)
		return result
	default:
		return 0
	}
}

func huangguoEdgeString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func huangguoEdgeMapNumber(values map[string]any, key string) int64 {
	return huangguoEdgeNumber(values[key])
}

func huangguoEdgeTokenValid(value string, min, max int) bool {
	if len(value) < min || len(value) > max {
		return false
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func huangguoEdgeDecodeURL(value string) ([]byte, error) {
	if value == "" || len(value) > huangguoEdgeMaxBody*2 {
		return nil, errors.New("黃果 AI 加密回應格式無效")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("黃果 AI 加密回應格式無效")
	}
	return decoded, nil
}

func huangguoEdgeHKDF(secret, salt, info []byte) []byte {
	extract := hmac.New(sha256.New, salt)
	_, _ = extract.Write(secret)
	prk := extract.Sum(nil)

	expand := hmac.New(sha256.New, prk)
	_, _ = expand.Write(info)
	_, _ = expand.Write([]byte{1})
	return expand.Sum(nil)
}

func huangguoEdgeNonce(sequence uint32) []byte {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint32(nonce[8:], sequence)
	return nonce
}

func huangguoEdgeAAD(info, direction string, sequence uint32, timestamp int64) []byte {
	return []byte(fmt.Sprintf("%s\n%s\n%d\n%d", info, direction, sequence, timestamp))
}

func huangguoEdgeNewGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("黃果 AI 加密金鑰長度無效")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (session *huangguoEdgeSession) usable(now time.Time) bool {
	return session != nil &&
		now.Add(5*time.Second).Before(session.expiresAt) &&
		session.sequence < huangguoEdgeMaxSequence
}

func (d *Downloader) huangguoEdgePost(ctx context.Context, endpoint string, payload any) ([]byte, int, error) {
	if d.client == nil {
		return nil, 0, errors.New("黃果 AI 網路用戶端尚未初始化")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return nil, 0, errors.New("黃果 AI 加密接口網址無效")
	}
	origin := parsed.Scheme + "://" + parsed.Host
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Origin", origin)
	request.Header.Set("Referer", origin+"/")
	request.Header.Set("Sec-Fetch-Dest", "empty")
	request.Header.Set("Sec-Fetch-Mode", "cors")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36")

	response, err := d.client.Do(request)
	if err != nil {
		return nil, 0, publicError(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, huangguoEdgeMaxBody+1))
	if err != nil {
		return nil, response.StatusCode, err
	}
	if len(raw) > huangguoEdgeMaxBody {
		return nil, response.StatusCode, errors.New("黃果 AI 加密接口回應過大")
	}
	return raw, response.StatusCode, nil
}

func (d *Downloader) openHuangguoEdgeSession(ctx context.Context) (*huangguoEdgeSession, error) {
	base := strings.TrimRight(d.providerBaseURL(sourceHuangguoAI), "/")
	if base == "" {
		return nil, errors.New("黃果 AI 站點網址尚未設定")
	}
	endpoint := base + "/api/playback/request"
	candidates := providerURLCandidates(endpoint)
	var lastErr error

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		origin := strings.TrimSuffix(candidate, "/api/playback/request")
		privateKey, err := ecdh.P256().GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("產生黃果 AI 握手金鑰失敗：%w", err)
		}
		helloRequest := map[string]any{
			"v": 1, "type": "hello",
			"client_pub": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()),
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		raw, status, err := d.huangguoEdgePost(attemptCtx, candidate, helloRequest)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		if status != http.StatusOK {
			var failure map[string]any
			_ = json.Unmarshal(raw, &failure)
			lastErr = fmt.Errorf("黃果 AI 加密握手失敗（HTTP %d）：%s", status,
				firstNonEmpty(huangguoEdgeString(failure, "error"), huangguoEdgeString(failure, "msg"), "站點拒絕請求"))
			continue
		}

		var hello map[string]any
		if err := json.Unmarshal(raw, &hello); err != nil {
			lastErr = fmt.Errorf("黃果 AI 加密握手回應無效：%w", err)
			continue
		}
		kid, sid := huangguoEdgeString(hello, "kid"), huangguoEdgeString(hello, "sid")
		site := huangguoEdgeString(hello, "site")
		serverTime, expiresAt := huangguoEdgeMapNumber(hello, "server_time"), huangguoEdgeMapNumber(hello, "expires_at")
		if huangguoEdgeMapNumber(hello, "v") != 1 ||
			huangguoEdgeString(hello, "type") != "hello" ||
			!huangguoEdgeTokenValid(kid, 1, 64) ||
			!huangguoEdgeTokenValid(sid, 16, 128) ||
			site == "" || len(site) > 256 ||
			serverTime <= 0 || expiresAt <= serverTime || expiresAt-serverTime > 600 {
			lastErr = errors.New("黃果 AI 加密握手資料不完整")
			continue
		}
		salt, err := huangguoEdgeDecodeURL(huangguoEdgeString(hello, "salt"))
		if err != nil || len(salt) != 32 {
			lastErr = errors.New("黃果 AI 握手鹽值無效")
			continue
		}
		serverPublicBytes, err := huangguoEdgeDecodeURL(huangguoEdgeString(hello, "server_pub"))
		if err != nil || len(serverPublicBytes) != 65 || serverPublicBytes[0] != 4 {
			lastErr = errors.New("黃果 AI 伺服器公鑰無效")
			continue
		}
		serverPublic, err := ecdh.P256().NewPublicKey(serverPublicBytes)
		if err != nil {
			lastErr = fmt.Errorf("黃果 AI 伺服器公鑰無效：%w", err)
			continue
		}
		sharedSecret, err := privateKey.ECDH(serverPublic)
		if err != nil {
			lastErr = fmt.Errorf("黃果 AI ECDH 金鑰協商失敗：%w", err)
			continue
		}

		info := strings.Join([]string{huangguoEdgeProtocol, huangguoEdgeScope, site, kid, sid}, "\n")
		requestKey := huangguoEdgeHKDF(sharedSecret, salt, []byte(info+"\nreq"))
		responseKey := huangguoEdgeHKDF(sharedSecret, salt, []byte(info+"\nres"))
		visitorBytes := make([]byte, 16)
		if _, err := rand.Read(visitorBytes); err != nil {
			lastErr = fmt.Errorf("產生黃果 AI 訪客識別碼失敗：%w", err)
			continue
		}
		session := &huangguoEdgeSession{
			baseURL: origin, info: info, kid: kid, sid: sid,
			visitorID: hex.EncodeToString(visitorBytes),
			requestKey: requestKey, responseKey: responseKey,
			clockOffset: serverTime - time.Now().Unix(),
			expiresAt: time.Now().Add(time.Duration(expiresAt-serverTime) * time.Second),
		}
		d.providerMu.Lock()
		if d.providerHosts == nil {
			d.providerHosts = make(map[string]string)
		}
		d.providerHosts[sourceHuangguoAI] = origin
		d.providerMu.Unlock()
		return session, nil
	}
	if lastErr == nil {
		lastErr = errors.New("黃果 AI 沒有可用的加密接口網域")
	}
	return nil, lastErr
}

func (d *Downloader) exchangeHuangguoEdge(ctx context.Context, session *huangguoEdgeSession, path string, body []byte) ([]byte, error) {
	if session == nil || !session.usable(time.Now()) {
		return nil, errHuangguoEdgeSessionExpired
	}
	if !strings.HasPrefix(path, "/api/") || strings.Contains(path, "://") {
		return nil, errors.New("黃果 AI 加密接口路徑無效")
	}
	session.sequence++
	sequence := session.sequence
	timestamp := time.Now().Unix() + session.clockOffset
	nonce := huangguoEdgeNonce(sequence)

	requestPacket := map[string]any{
		"method": http.MethodPost,
		"path": path,
		"body": base64.StdEncoding.EncodeToString(body),
		"content_type": "application/json",
		"visitor_id": session.visitorID,
	}
	plain, err := json.Marshal(map[string]any{"request": requestPacket})
	if err != nil {
		return nil, err
	}
	requestGCM, err := huangguoEdgeNewGCM(session.requestKey)
	if err != nil {
		return nil, err
	}
	ciphertext := requestGCM.Seal(nil, nonce, plain, huangguoEdgeAAD(session.info, "req", sequence, timestamp))
	outer := map[string]any{
		"v": 1, "type": "call", "kid": session.kid, "sid": session.sid,
		"seq": sequence, "ts": timestamp,
		"ciphertext": base64.RawURLEncoding.EncodeToString(ciphertext),
	}
	raw, status, err := d.huangguoEdgePost(ctx, session.baseURL+"/api/playback/request", outer)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		var failure map[string]any
		_ = json.Unmarshal(raw, &failure)
		if strings.EqualFold(huangguoEdgeString(failure, "error"), "session_expired") {
			return nil, errHuangguoEdgeSessionExpired
		}
		return nil, fmt.Errorf("黃果 AI 加密請求失敗（HTTP %d）：%s", status,
			firstNonEmpty(huangguoEdgeString(failure, "error"), huangguoEdgeString(failure, "msg"), "站點拒絕請求"))
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("黃果 AI 加密回應無法解析：%w", err)
	}
	if huangguoEdgeMapNumber(result, "v") != 1 ||
		huangguoEdgeString(result, "type") != "result" ||
		huangguoEdgeString(result, "kid") != session.kid ||
		huangguoEdgeString(result, "sid") != session.sid ||
		uint32(huangguoEdgeMapNumber(result, "seq")) != sequence ||
		huangguoEdgeMapNumber(result, "ts") != timestamp {
		if strings.Contains(string(raw), "session_expired") {
			return nil, errHuangguoEdgeSessionExpired
		}
		return nil, errors.New("黃果 AI 加密回應與目前會話不一致")
	}
	encrypted, err := huangguoEdgeDecodeURL(huangguoEdgeString(result, "ciphertext"))
	if err != nil {
		return nil, err
	}
	responseGCM, err := huangguoEdgeNewGCM(session.responseKey)
	if err != nil {
		return nil, err
	}
	decrypted, err := responseGCM.Open(nil, nonce, encrypted, huangguoEdgeAAD(session.info, "res", sequence, timestamp))
	if err != nil {
		return nil, errors.New("黃果 AI 播放回應解密驗證失敗")
	}
	var inner map[string]any
	if err := json.Unmarshal(decrypted, &inner); err != nil {
		return nil, fmt.Errorf("黃果 AI 內層回應格式無效：%w", err)
	}
	innerStatus := huangguoEdgeMapNumber(inner, "status")
	innerBody := huangguoEdgeString(inner, "body")
	if innerStatus != http.StatusOK {
		if decoded, decodeErr := base64.StdEncoding.DecodeString(innerBody); decodeErr == nil &&
			strings.Contains(string(decoded), "session_expired") {
			return nil, errHuangguoEdgeSessionExpired
		}
		return nil, fmt.Errorf("黃果 AI 播放接口回傳 HTTP %d", innerStatus)
	}
	if innerBody == "" {
		return nil, errors.New("黃果 AI 播放接口沒有回傳內容")
	}
	payload, err := base64.StdEncoding.DecodeString(innerBody)
	if err != nil {
		return nil, errors.New("黃果 AI 播放資料編碼無效")
	}
	if len(payload) > huangguoEdgeMaxBody {
		return nil, errors.New("黃果 AI 播放資料過大")
	}
	if !json.Valid(payload) {
		return nil, errors.New("黃果 AI 播放資料不是有效 JSON")
	}
	return payload, nil
}

func (d *Downloader) huangguoAIEncryptedCall(ctx context.Context, path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	d.huangguoEdgeMu.Lock()
	defer d.huangguoEdgeMu.Unlock()

	for attempt := 0; attempt < 2; attempt++ {
		if !d.huangguoEdgeSession.usable(time.Now()) {
			session, openErr := d.openHuangguoEdgeSession(ctx)
			if openErr != nil {
				return nil, openErr
			}
			d.huangguoEdgeSession = session
		}
		result, callErr := d.exchangeHuangguoEdge(ctx, d.huangguoEdgeSession, path, payload)
		if errors.Is(callErr, errHuangguoEdgeSessionExpired) {
			d.huangguoEdgeSession = nil
			continue
		}
		if callErr != nil {
			return nil, callErr
		}
		return result, nil
	}
	return nil, errors.New("黃果 AI 播放會話重建後仍無法取得播放資料")
}
