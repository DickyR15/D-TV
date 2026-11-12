package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type nativeResourceSettings struct {
	ProxyMode           string `json:"proxyMode"`
	ProxyURL            string `json:"proxyUrl"`
	CatalogConcurrency  int    `json:"catalogConcurrency"`
	CatalogIntervalMS   int    `json:"catalogIntervalMs"`
	Warning             string `json:"warning,omitempty"`
	SystemProxyStatus   string `json:"systemProxyStatus,omitempty"`
}

func defaultResourceSettings() nativeResourceSettings {
	return nativeResourceSettings{
		ProxyMode: "auto",
		CatalogConcurrency: 3,
		CatalogIntervalMS: 250,
	}
}

func (settings nativeResourceSettings) validate() error {
	if settings.CatalogConcurrency < 1 || settings.CatalogConcurrency > 6 ||
		settings.CatalogIntervalMS < 0 || settings.CatalogIntervalMS > 5000 {
		return errors.New("并发应为 1 至 6，请求间隔应为 0 至 5000 毫秒")
	}
	if settings.ProxyMode != "auto" && settings.ProxyMode != "direct" && settings.ProxyMode != "manual" {
		return errors.New("代理模式无效")
	}
	if settings.ProxyMode == "manual" || settings.ProxyURL != "" {
		if len(settings.ProxyURL) > 2048 {
			return errors.New("代理伺服器位址過長")
		}
		parsed, err := url.Parse(settings.ProxyURL)
		if err != nil || parsed.Hostname() == "" || parsed.Fragment != "" || parsed.RawQuery != "" ||
			(parsed.Path != "" && parsed.Path != "/") ||
			(parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "socks5" && parsed.Scheme != "socks5h") {
			return errors.New("請輸入有效的 HTTP、HTTPS 或 SOCKS5 代理伺服器位址")
		}
		if port := parsed.Port(); port != "" {
			number, err := strconv.Atoi(port)
			if err != nil || number < 1 || number > 65535 {
				return errors.New("代理端口无效")
			}
		}
	}
	return nil
}

type proxyRouter struct {
	mu             sync.RWMutex
	mode           string
	manual         *url.URL
	system         nativeSystemProxy
	systemRead     time.Time
	systemOverride bool
}

func (router *proxyRouter) proxy(request *http.Request) (*url.URL, error) {
	if request.URL.Hostname() == "127.0.0.1" || request.URL.Hostname() == "localhost" || request.URL.Hostname() == "::1" {
		return nil, nil
	}
	router.mu.RLock()
	mode, manual := router.mode, router.manual
	router.mu.RUnlock()
	switch mode {
	case "direct":
		return nil, nil
	case "manual":
		return manual, nil
	}
	return router.systemSettings().resolve(request)
}

func (engine *nativeEngine) loadResourceSettings() {
	settings := defaultResourceSettings()
	file := filepath.Join(engine.directory, "resource-settings.json")
	info, err := os.Stat(file)
	if err == nil {
		var stored nativeResourceSettings
		if info.Mode().IsRegular() && info.Size() <= 16384 {
			if data, readErr := os.ReadFile(file); readErr == nil && json.Unmarshal(data, &stored) == nil && stored.validate() == nil {
				settings = stored
			} else {
				settings.Warning = "網路與資源設定無法讀取，已使用預設值，請重新儲存設定"
			}
		} else {
			settings.Warning = "網路與資源設定無效，已使用預設值，請重新儲存設定"
		}
	} else if !os.IsNotExist(err) {
		settings.Warning = "網路與資源設定無法讀取，已使用預設值，請重新儲存設定"
	}
	engine.settings = settings
	engine.applyResourceSettings(settings)
}

func (engine *nativeEngine) resourceSettings() nativeResourceSettings {
	engine.settingsMu.Lock()
	defer engine.settingsMu.Unlock()
	if engine.settings.ProxyMode == "" {
		return defaultResourceSettings()
	}
	settings := engine.settings
	system := engine.downloader.proxyRouter.systemSettings()
	settings.SystemProxyStatus = "自动读取系統靜態代理及環境變數"
	if system.PACURL != "" {
		settings.SystemProxyStatus = "已讀取系統自動代理腳本"
	} else if system.PAC && system.HTTP == "" && system.HTTPS == "" {
		settings.SystemProxyStatus = "偵測到系統自動代理腳本；請設定手動代理或選擇直連"
	} else if system.HTTP != "" || system.HTTPS != "" {
		settings.SystemProxyStatus = "已读取系统静态代理"
	}
	return settings
}

func (engine *nativeEngine) applyResourceSettings(settings nativeResourceSettings) {
	router := engine.downloader.proxyRouter
	manual, _ := url.Parse(settings.ProxyURL)
	router.mu.Lock()
	router.mode, router.manual = settings.ProxyMode, manual
	router.mu.Unlock()
	limiter := engine.downloader.limiter
	limiter.mu.Lock()
	limiter.concurrency = settings.CatalogConcurrency
	limiter.interval = time.Duration(settings.CatalogIntervalMS) * time.Millisecond
	limiter.notifyLocked()
	limiter.mu.Unlock()
}

func (engine *nativeEngine) saveResourceSettings(settings nativeResourceSettings) (nativeResourceSettings, error) {
	settings.ProxyURL = strings.TrimSpace(settings.ProxyURL)
	settings.Warning = ""
	settings.SystemProxyStatus = ""
	if err := settings.validate(); err != nil {
		return nativeResourceSettings{}, err
	}
	engine.settingsMu.Lock()
	defer engine.settingsMu.Unlock()
	data, err := json.Marshal(settings)
	if err == nil {
		err = nativeDownloadWrite(filepath.Join(engine.directory, "resource-settings.json"), data)
	}
	if err != nil {
		return nativeResourceSettings{}, errors.New("未能儲存網路與資源設定，原設定保持不變")
	}
	engine.settings = settings
	engine.applyResourceSettings(settings)
	engine.downloader.client.CloseIdleConnections()
	return settings, nil
}
