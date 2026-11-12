package core

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type huangguoEdgeRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper huangguoEdgeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func huangguoEdgeTestResponse(request *http.Request, status int, payload any) (*http.Response, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Header: make(http.Header),
		Body: io.NopCloser(bytes.NewReader(raw)),
		Request: request,
	}, nil
}

func TestHuangguoAIEncryptedPlaybackUsesNewEdgeProtocol(t *testing.T) {
	serverPrivate, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var requestKey, responseKey []byte
	var info string
	var clientSID, clientKID string

	d := &Downloader{
		cfg: Config{HuangguoAIURL: "https://huangguoai.com"},
		providerHosts: make(map[string]string),
	}
	d.client = &http.Client{Transport: huangguoEdgeRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/playback/request" {
			t.Fatalf("unexpected edge request: %s %s", request.Method, request.URL)
		}
		if request.Header.Get("Origin") != "https://huangguoai.com" ||
			request.Header.Get("Referer") != "https://huangguoai.com/" ||
			request.Header.Get("Sec-Fetch-Mode") != "cors" ||
			request.Header.Get("Sec-Fetch-Dest") != "empty" {
			t.Fatalf("missing browser request headers: %#v", request.Header)
		}
		var outer map[string]any
		if err := json.NewDecoder(request.Body).Decode(&outer); err != nil {
			t.Fatal(err)
		}
		if outer["type"] == "hello" {
			clientPubRaw, err := base64.RawURLEncoding.DecodeString(outer["client_pub"].(string))
			if err != nil {
				t.Fatal(err)
			}
			clientPub, err := ecdh.P256().NewPublicKey(clientPubRaw)
			if err != nil {
				t.Fatal(err)
			}
			shared, err := serverPrivate.ECDH(clientPub)
			if err != nil {
				t.Fatal(err)
			}
			clientKID, clientSID = "test-kid", "0123456789abcdef0123456789abcdef"
			const site = "huangguoai.com"
			info = strings.Join([]string{huangguoEdgeProtocol, huangguoEdgeScope, site, clientKID, clientSID}, "\n")
			salt := make([]byte, 32)
			for i := range salt {
				salt[i] = byte(i)
			}
			requestKey = huangguoEdgeHKDF(shared, salt, []byte(info+"\nreq"))
			responseKey = huangguoEdgeHKDF(shared, salt, []byte(info+"\nres"))
			hello := map[string]any{
				"v": 1, "type": "hello", "kid": clientKID, "sid": clientSID, "site": site,
				"server_pub": base64.RawURLEncoding.EncodeToString(serverPrivate.PublicKey().Bytes()),
				"salt": base64.RawURLEncoding.EncodeToString(salt),
				"server_time": int64(1800000000), "expires_at": int64(1800000300),
			}
			return huangguoEdgeTestResponse(request, http.StatusOK, hello)
		}

		seq := uint32(huangguoEdgeMapNumber(outer, "seq"))
		timestamp := huangguoEdgeMapNumber(outer, "ts")
		ciphertext, err := base64.RawURLEncoding.DecodeString(outer["ciphertext"].(string))
		if err != nil {
			t.Fatal(err)
		}
		requestGCM, _ := huangguoEdgeNewGCM(requestKey)
		plain, err := requestGCM.Open(nil, huangguoEdgeNonce(seq), ciphertext,
			huangguoEdgeAAD(info, "req", seq, timestamp))
		if err != nil {
			t.Fatal(err)
		}
		var packet map[string]any
		if err := json.Unmarshal(plain, &packet); err != nil {
			t.Fatal(err)
		}
		innerRequest := packet["request"].(map[string]any)
		decodedBody, _ := base64.StdEncoding.DecodeString(innerRequest["body"].(string))
		if innerRequest["method"] != http.MethodPost ||
			innerRequest["path"] != "/api/videos/123/episodes/2/playback" ||
			string(decodedBody) != "{}" || len(innerRequest["visitor_id"].(string)) != 32 {
			t.Fatalf("wrong encrypted business request: %#v", innerRequest)
		}

		responseBody, _ := json.Marshal(map[string]any{
			"data": map[string]any{
				"streams": map[string]any{
					"main": "https://media.example.test/123/2/index.m3u8",
				},
			},
		})
		inner, _ := json.Marshal(map[string]any{
			"status": http.StatusOK,
			"body": base64.StdEncoding.EncodeToString(responseBody),
		})
		responseGCM, _ := huangguoEdgeNewGCM(responseKey)
		encrypted := responseGCM.Seal(nil, huangguoEdgeNonce(seq), inner,
			huangguoEdgeAAD(info, "res", seq, timestamp))
		result := map[string]any{
			"v": 1, "type": "result", "kid": clientKID, "sid": clientSID,
			"seq": seq, "ts": timestamp,
			"ciphertext": base64.RawURLEncoding.EncodeToString(encrypted),
		}
		return huangguoEdgeTestResponse(request, http.StatusOK, result)
	})}

	address, err := d.fetchHuangguoAIPlayURL(context.Background(), "123", 2)
	if err != nil {
		t.Fatal(err)
	}
	if address != "https://media.example.test/123/2/index.m3u8" {
		t.Fatalf("unexpected playback address: %q", address)
	}
}
