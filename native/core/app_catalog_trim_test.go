package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestNativeCatalogCacheTrimsEveryListOnLoad(t *testing.T) {
	engine := sourceFixtureEngine(t, nil)
	items := make([]nativeDrama, nativeCatalogMaxItems+37)
	for index := range items {
		id := strconv.Itoa(index + 1)
		items[index] = nativeDrama{
			ID: "hongguo:" + id, Source: sourceHongguo,
			SourceID: id, Title: "合成劇 " + id,
		}
	}
	path := filepath.Join(engine.directory, "catalogs.json")
	disk := nativeCatalogDisk{
		Version: 3,
		Catalogs: map[string][]nativeDrama{
			sourceHongguo: items,
			"hongguo|short_play": items,
		},
		States: map[string]nativeCatalogState{
			sourceHongguo: {UpdatedAt: time.Now(), Page: 501, HasMore: true},
		},
	}
	body, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	engine.loadCatalogCache()
	for _, key := range []string{sourceHongguo, "hongguo|short_play"} {
		cached := engine.catalogs[key]
		if len(cached) != nativeCatalogMaxItems {
			t.Fatalf("%s cache was not trimmed: got %d items", key, len(cached))
		}
		if cached[0].ID != "hongguo:1" || cached[nativeCatalogMaxItems-1].ID != "hongguo:5000" {
			t.Fatalf("%s did not preserve the first 5,000 entries", key)
		}
	}
	if state := engine.catalogStates[sourceHongguo]; state.Page != 501 || !state.HasMore {
		t.Fatalf("cache trimming lost the saved pagination cursor: %+v", state)
	}
	if err := engine.writeCatalogDiskLocked(); err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved nativeCatalogDisk
	if err := json.Unmarshal(persisted, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Catalogs[sourceHongguo]) != nativeCatalogMaxItems ||
		len(saved.Catalogs["hongguo|short_play"]) != nativeCatalogMaxItems {
		t.Fatal("trimmed cache was not persisted within the per-list cap")
	}
}
