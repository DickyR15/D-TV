package core

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

type nativeSystemProxy struct {
	HTTP   string   `json:"http"`
	HTTPS  string   `json:"https"`
	Bypass []string `json:"bypass"`
	PAC    bool     `json:"pac"`
	PACURL string   `json:"-"`
}

func (proxy nativeSystemProxy) resolve(request *http.Request) (*url.URL, error) {
	if proxy.PACURL != "" {
		if address, err := platformPACProxy(proxy.PACURL, request.URL); err == nil {
			return address, nil
		}
	}
	host := strings.ToLower(request.URL.Hostname())
	for _, exception := range proxy.Bypass {
		exception = strings.ToLower(strings.TrimSpace(exception))
		if exception == "<local>" && !strings.Contains(host, ".") && net.ParseIP(host) == nil {
			return nil, nil
		}
		if matched, _ := path.Match(exception, host); exception != "" && matched {
			return nil, nil
		}
	}
	address := proxy.HTTP
	if request.URL.Scheme == "https" {
		address = proxy.HTTPS
	}
	if address == "" {
		if proxy.PAC && proxy.PACURL == "" {
			return nil, errors.New("系統使用自動代理設定檔，請在網路設定中設定手動代理或選擇直接連線")
		}
		return http.ProxyFromEnvironment(request)
	}
	return url.Parse(address)
}

func (router *proxyRouter) systemSettings() nativeSystemProxy {
	router.mu.Lock()
	defer router.mu.Unlock()
	if !router.systemOverride && time.Since(router.systemRead) > 30*time.Second {
		router.system = platformSystemProxy()
		router.systemRead = time.Now()
	}
	return router.system
}

// sameNativeSystemProxy compares every Windows system-proxy input that can
// change proxy resolution. PACURL must be included: changing the PAC script
// while the PAC-enabled flag remains true must invalidate the previous
// in-memory system-proxy snapshot.
func sameNativeSystemProxy(left, right nativeSystemProxy) bool {
	return left.HTTP == right.HTTP &&
		left.HTTPS == right.HTTPS &&
		left.PAC == right.PAC &&
		left.PACURL == right.PACURL &&
		slices.Equal(left.Bypass, right.Bypass)
}

func (engine *nativeEngine) updateSystemProxy(proxy nativeSystemProxy) error {
	for _, address := range []string{proxy.HTTP, proxy.HTTPS} {
		if address == "" {
			continue
		}
		settings := defaultResourceSettings()
		settings.ProxyMode, settings.ProxyURL = "manual", address
		if err := settings.validate(); err != nil {
			return err
		}
	}
	if len(proxy.Bypass) > 1000 {
		return errors.New("系统代理排除列表过长")
	}
	router := engine.downloader.proxyRouter
	router.mu.Lock()
	if router.systemOverride && sameNativeSystemProxy(router.system, proxy) {
		router.mu.Unlock()
		return nil
	}
	router.system, router.systemOverride = proxy, true
	router.mu.Unlock()
	engine.downloader.client.CloseIdleConnections()
	return nil
}
