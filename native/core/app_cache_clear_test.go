package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeClearSourceCacheRemovesProviderCachesAndKeepsRegistration(t *testing.T) {
	directory := t.TempDir()
	engine, err := newNativeEngine(directory)
	if err != nil {
		t.Fatal(err)
	}

	client := engine.downloader.hongguoClient()
	client.mu.Lock()
	client.state = hongguoCatalogState{
		Version: 1, DeviceID: "123456789", InstallID: "987654321", Registered: true,
		HelperDeviceID: "222222222", HelperInstallID: "333333333", HelperRegistered: true,
		Feeds: map[string]hongguoCatalogCursor{
			"hot-drama": {Offset: 20, Initialized: true, UpdatedAt: time.Now()},
		},
	}
	client.details["hongguo:1"] = hongguoDetailEntry{}
	client.searches["fixture"] = hongguoSearchEntry{}
	client.suggestions["fixture"] = hongguoSuggestionEntry{}
	client.searchCursors["fixture"] = hongguoSearchCursor{}
	client.mu.Unlock()

	engine.mu.Lock()
	engine.catalogs = map[string][]nativeDrama{sourceHongguo: {{ID: "hongguo:1", Title: "快取測試"}}}
	engine.catalogStates = map[string]nativeCatalogState{sourceHongguo: {Page: 1, UpdatedAt: time.Now()}}
	engine.categoryOptions = map[string][]nativeCategory{sourceMissAV: {{ID: "all", Name: "全部"}}}
	engine.recommendations = map[string]nativeRecommendationState{"real": {}}
	engine.detailCache = map[string]nativeDetailCacheEntry{"hongguo:1": {}}
	engine.sourceRecords = map[string]nativeSourceRecord{sourceHongguo: {Stage: "已完成"}}
	engine.mu.Unlock()

	engine.downloader.rankings.mu.Lock()
	engine.downloader.rankings.pages = map[string]rankingPage{
		"hongguo-hot:1": {BoardID: "hongguo-hot", Page: 1},
	}
	engine.downloader.rankings.mu.Unlock()

	if err := os.MkdirAll(engine.covers.directory, 0700); err != nil {
		t.Fatal(err)
	}
	coverPath := filepath.Join(engine.covers.directory, "fixture.img")
	if err := os.WriteFile(coverPath, []byte("poster-cache"), 0600); err != nil {
		t.Fatal(err)
	}
	engine.covers.mu.Lock()
	engine.covers.entries["fixture"] = nativeCoverEntry{size: int64(len("poster-cache"))}
	engine.covers.size = int64(len("poster-cache"))
	engine.covers.mu.Unlock()

	for _, name := range []string{"rankings.json", "catalogs.json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	if err := engine.clearSourceCache(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "rankings.json")); !os.IsNotExist(err) {
		t.Fatalf("rankings.json still exists after clearing: %v", err)
	}
	if _, err := os.Stat(engine.covers.directory); !os.IsNotExist(err) {
		t.Fatalf("cover cache directory still exists: %v", err)
	}
	if len(engine.catalogs) != 0 || len(engine.catalogStates) != 0 ||
		len(engine.categoryOptions) != 0 || len(engine.recommendations) != 0 ||
		len(engine.detailCache) != 0 || len(engine.downloader.rankings.pages) != 0 ||
		len(engine.covers.entries) != 0 || engine.covers.size != 0 {
		t.Fatal("provider cache remained in memory")
	}

	var disk nativeCatalogDisk
	body, err := os.ReadFile(filepath.Join(directory, "catalogs.json"))
	if err != nil || json.Unmarshal(body, &disk) != nil {
		t.Fatal("minimal device-state cache was not preserved", err)
	}
	if len(disk.Catalogs) != 0 || len(disk.States) != 0 || len(disk.Categories) != 0 ||
		disk.HongguoApp == nil || len(disk.HongguoApp.Feeds) != 0 {
		t.Fatal("catalogue disk cache was not cleared cleanly", disk)
	}
	if disk.HongguoApp.DeviceID != "123456789" || disk.HongguoApp.InstallID != "987654321" ||
		!disk.HongguoApp.Registered || disk.HongguoApp.HelperDeviceID != "222222222" ||
		disk.HongguoApp.HelperInstallID != "333333333" || !disk.HongguoApp.HelperRegistered {
		t.Fatalf("Red Fruit device registration was changed: %+v", disk.HongguoApp)
	}
	if record := engine.sourceRecords[sourceHongguo]; record.Stage != "已完成" {
		t.Fatal("source task records should be retained", record)
	}

	restarted, err := newNativeEngine(directory)
	if err != nil {
		t.Fatal(err)
	}
	restored := restarted.downloader.hongguoCatalogSnapshot()
	if restored.DeviceID != "123456789" || restored.InstallID != "987654321" ||
		!restored.Registered || len(restored.Feeds) != 0 {
		t.Fatalf("device registration did not survive an application restart: %+v", restored)
	}
	if len(restarted.catalogs) != 0 || len(restarted.categoryOptions) != 0 {
		t.Fatal("catalogue cache was restored after a clear")
	}
	if err := engine.clearSourceCache(); err != nil {
		t.Fatal("clear should be idempotent", err)
	}
}

func TestNativeClearSourceCacheRefusesWhileSourceTaskIsRunning(t *testing.T) {
	directory := t.TempDir()
	engine, err := newNativeEngine(directory)
	if err != nil {
		t.Fatal(err)
	}
	cacheFile := filepath.Join(directory, "catalogs.json")
	if err := os.WriteFile(cacheFile, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	engine.sourceTasks = map[string]*nativeSourceTask{sourceHongguo: &nativeSourceTask{}}
	engine.mu.Unlock()

	if err := engine.clearSourceCache(); err == nil {
		t.Fatal("cache clear should wait for a running source task")
	}
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatal("cache must remain untouched when clearing is rejected", err)
	}
}

func TestNativeClearSourceCacheRefusesWhileCatalogueReadIsRunning(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine.readMu.Lock()
	engine.readCount = 1
	engine.readMu.Unlock()

	if err := engine.clearSourceCache(); err == nil {
		t.Fatal("cache clear should wait for active catalogue reads")
	}
}
