package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// clearSourceCache clears only provider-owned cache data. LocalStore data such
// as profiles, favorites, watch history and playback progress is stored by
// Flutter in a separate store and is deliberately never touched here.
func (engine *nativeEngine) clearSourceCache() error {
	if engine == nil || engine.downloader == nil || engine.covers == nil {
		return errors.New("片源快取服務尚未就緒")
	}

	// Hold the read gate so no tracked catalogue request can enter while
	// cache files and the in-memory indexes are being replaced.
	engine.readMu.Lock()
	defer engine.readMu.Unlock()
	if engine.readCount > 0 {
		return errors.New("目前仍有片源讀取作業，請稍後再清除快取")
	}

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.sourceTasks) > 0 {
		return errors.New("片源更新作業進行中，請等候完成後再清除快取")
	}

	rankings := &engine.downloader.rankings
	rankings.mu.Lock()
	defer rankings.mu.Unlock()
	if len(rankings.pending) > 0 {
		return errors.New("排行榜仍在載入，請稍後再清除快取")
	}

	covers := engine.covers
	if !covers.mu.TryLock() {
		return errors.New("海報仍在載入，請稍後再清除快取")
	}
	defer covers.mu.Unlock()
	if len(covers.pending) > 0 {
		return errors.New("海報仍在載入，請稍後再清除快取")
	}
	for _, repair := range covers.repairs {
		if repair != nil && repair.retryAt.IsZero() {
			return errors.New("海報修復作業進行中，請稍後再清除快取")
		}
	}

	// Red Fruit keeps device registration in the same file as its catalogue
	// cache. Preserve identifiers and registration flags, but discard feed
	// cursors so this is a cache reset rather than a device reset.
	client := engine.downloader.hongguoClient()
	if !client.catalogMu.TryLock() {
		return errors.New("紅果目錄仍在更新，請稍後再清除快取")
	}
	defer client.catalogMu.Unlock()
	if !client.mu.TryLock() {
		return errors.New("紅果資料仍在載入，請稍後再清除快取")
	}
	defer client.mu.Unlock()
	if len(client.pending) > 0 || len(client.searchPending) > 0 || len(client.suggestPending) > 0 {
		return errors.New("紅果資料仍在載入，請稍後再清除快取")
	}

	clearedHongguo := cloneHongguoCatalogState(&client.state)
	if clearedHongguo == nil {
		return errors.New("紅果設備狀態尚未就緒")
	}
	clearedHongguo.Feeds = map[string]hongguoCatalogCursor{}
	disk := nativeCatalogDisk{
		Version:         3,
		Catalogs:        map[string][]nativeDrama{},
		States:          map[string]nativeCatalogState{},
		Categories:      map[string][]nativeCategory{},
		HongguoApp:      clearedHongguo,
		Recommendations: map[string]nativeRecommendationState{},
	}
	catalogBody, err := json.Marshal(disk)
	if err != nil {
		return fmt.Errorf("無法準備清除後的片源快取：%w", err)
	}

	if err := os.Remove(filepath.Join(engine.directory, "rankings.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("無法清除排行榜快取：%w", err)
	}
	if err := os.RemoveAll(covers.directory); err != nil {
		return fmt.Errorf("無法清除海報快取：%w", err)
	}
	// Keep a compact catalogs.json to persist the existing Red Fruit device
	// registration across process restarts.
	if err := writeNativeCacheFile(filepath.Join(engine.directory, "catalogs.json"), catalogBody); err != nil {
		return fmt.Errorf("無法儲存清理後的片源狀態：%w", err)
	}

	engine.catalogs = map[string][]nativeDrama{}
	engine.catalogStates = map[string]nativeCatalogState{}
	engine.categoryOptions = map[string][]nativeCategory{}
	engine.recommendations = map[string]nativeRecommendationState{}
	engine.detailCache = map[string]nativeDetailCacheEntry{}
	engine.hongguoCatalog = clearedHongguo
	engine.catalogSaveError = nil

	rankings.pages = map[string]rankingPage{}
	rankings.pending = map[string]*rankingCall{}

	covers.entries = map[string]nativeCoverEntry{}
	covers.pending = map[string]*nativeCoverCall{}
	covers.failures = map[string]time.Time{}
	covers.nodeFailures = map[string]time.Time{}
	covers.repairs = map[string]*nativeCoverRepair{}
	covers.size = 0

	client.state.Feeds = map[string]hongguoCatalogCursor{}
	client.details = map[string]hongguoDetailEntry{}
	client.pending = map[string]*hongguoDetailCall{}
	client.searches = map[string]hongguoSearchEntry{}
	client.searchPending = map[string]*hongguoSearchCall{}
	client.searchCursors = map[string]hongguoSearchCursor{}
	client.suggestions = map[string]hongguoSuggestionEntry{}
	client.suggestPending = map[string]*hongguoSuggestionCall{}

	// sources.json contains task history/status rather than provider content.
	// It stays intact, as do all Flutter LocalStore user records.
	return nil
}
