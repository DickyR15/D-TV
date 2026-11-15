package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const castTestDescription = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <device>
    <friendlyName>客廳電視</friendlyName>
    <deviceList>
      <device>
        <serviceList>
          <service>
            <serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType>
            <controlURL>/rendering</controlURL>
          </service>
          <service>
            <serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType>
            <controlURL>/transport</controlURL>
          </service>
        </serviceList>
      </device>
    </deviceList>
  </device>
</root>`

func castTestRenderer(t *testing.T, calls *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/description.xml" {
			_, _ = io.WriteString(writer, castTestDescription)
			return
		}
		if request.URL.Path != "/transport" {
			t.Errorf("控制請求發送到錯誤路徑：%s", request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		action := strings.Trim(request.Header.Get("SOAPACTION"), `"`)
		*calls = append(*calls, action+"\n"+string(body))
		switch {
		case strings.HasSuffix(action, "#GetTransportInfo"):
			_, _ = io.WriteString(writer, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetTransportInfoResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><CurrentTransportState>PAUSED_PLAYBACK</CurrentTransportState></u:GetTransportInfoResponse></s:Body></s:Envelope>`)
		case strings.HasSuffix(action, "#GetPositionInfo"):
			_, _ = io.WriteString(writer, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetPositionInfoResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><TrackDuration>0:02:05</TrackDuration><RelTime>00:01:03.500</RelTime></u:GetPositionInfoResponse></s:Body></s:Envelope>`)
		case strings.HasSuffix(action, "#Pause"):
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(writer, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>701</errorCode><errorDescription>Transition not available</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
		default:
			_, _ = io.WriteString(writer, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body/></s:Envelope>`)
		}
	}))
}

func TestCastControlsRenderer(t *testing.T) {
	var calls []string
	renderer := castTestRenderer(t, &calls)
	defer renderer.Close()
	ctx := context.Background()
	device, err := castDescribe(ctx, renderer.URL+"/description.xml")
	if err != nil {
		t.Fatal(err)
	}
	if device.Name != "客廳電視" || device.control != renderer.URL+"/transport" {
		t.Fatalf("裝置描述解析錯誤：%+v", device)
	}
	engine := &nativeEngine{}
	engine.cast.device, engine.cast.active = device, true
	status, err := engine.nativeCast(ctx, nativeInput{Command: "status"})
	if err != nil {
		t.Fatal(err)
	}
	values := status.(map[string]any)
	if values["state"] != "paused" || values["positionMs"] != int64(63500) || values["durationMs"] != int64(125000) {
		t.Fatalf("投屏狀態解析錯誤：%v", values)
	}
	if _, err := engine.nativeCast(ctx, nativeInput{Command: "seek", StartMS: 3725000}); err != nil {
		t.Fatal(err)
	}
	seek := calls[len(calls)-1]
	if !strings.Contains(seek, "#Seek") || !strings.Contains(seek, "<Unit>REL_TIME</Unit><Target>1:02:05</Target>") {
		t.Fatalf("進度定位 SOAP 請求錯誤：%s", seek)
	}
	if _, err := engine.nativeCast(ctx, nativeInput{Command: "pause"}); err == nil || !strings.Contains(err.Error(), "701") {
		t.Fatalf("裝置拒絕暫停時未回報 UPnP 錯誤：%v", err)
	}
	if _, err := engine.nativeCast(ctx, nativeInput{Command: "stop"}); err != nil {
		t.Fatal(err)
	}
	if engine.cast.active {
		t.Fatal("停止投屏後仍標記為啟用")
	}
	if _, err := engine.nativeCast(ctx, nativeInput{Command: "status"}); err == nil {
		t.Fatal("停止投屏後仍可查詢活動狀態")
	}
}

func TestCastMetadataEscapesTitleAndAddress(t *testing.T) {
	metadata := castMetadata("劇 <1> & 2", "http://192.168.1.2:8000/a/b.m3u8?x=1&y=2", "application/vnd.apple.mpegurl")
	if !strings.Contains(metadata, "劇 &lt;1&gt; &amp; 2") || !strings.Contains(metadata, "?x=1&amp;y=2</res>") {
		t.Fatalf("DIDL-Lite 元資料未正確跳脫：%s", metadata)
	}
}

func TestCastStreamServesRewrittenPlaylistsOnLAN(t *testing.T) {
	stream, err := newNativeStreamServer(&Downloader{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()
	origin, err := stream.lanOrigin("127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.lanClose()
	if origin == stream.address {
		t.Fatal("區域網路投屏服務不應使用本機 loopback 位址")
	}
	local, token := stream.nativeOpen(providerMedia{
		URL: "https://media.example/video/index.m3u8",
		Playlist: "#EXTM3U\n#EXTINF:4,\nsegment-1.ts\n#EXT-X-ENDLIST",
	})
	defer stream.nativeRelease(token)
	for address, prefix := range map[string]string{
		local: stream.address,
		origin + strings.TrimPrefix(local, stream.address): origin,
	} {
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		lines := strings.Split(string(body), "\n")
		if response.StatusCode != http.StatusOK || len(lines) < 3 || !strings.HasPrefix(lines[2], prefix+"/"+token+"/") {
			t.Fatalf("從 %s 取得的播放清單不正確：HTTP %d %s", address, response.StatusCode, body)
		}
	}
}
