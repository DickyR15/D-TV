package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveDetailMetadataSkipsUnchangedCatalogWrite(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)

	drama := nativeDrama{
		MetadataSchema: 1,
		ID:              "hongguo:fixture",
		Source:          sourceHongguo,
		SourceID:        "fixture",
		Title:           "合成測試短劇",
		Description:     "原始簡介",
		Cover:           "https://example.test/cover.jpg",
		Episodes:        10,
		Category:        "真人劇",
		Tags:            []string{"合成"},
		OnlineDate:      "2026-10-23",
	}
	engine.mu.Lock()
	engine.catalogs[sourceHongguo] = []nativeDrama{drama}
	err = engine.writeCatalogDiskLocked()
	engine.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(engine.directory, "catalogs.json")
	marker := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, marker, marker); err != nil {
		t.Fatal(err)
	}
	if err := engine.saveDetailMetadata(drama); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(marker) {
		t.Fatal("unchanged metadata unexpectedly rewrote the full catalog cache")
	}

	updated := drama
	updated.Description = "更新後的合成簡介"
	if err := engine.saveDetailMetadata(updated); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk nativeCatalogDisk
	if err := json.Unmarshal(body, &disk); err != nil {
		t.Fatal(err)
	}
	items := disk.Catalogs[sourceHongguo]
	if len(items) != 1 || items[0].Description != updated.Description {
		t.Fatal("actual metadata changes were not persisted")
	}
}
