package core

import (
	"context"
	"testing"
	"time"
)

func TestNativeDetailUsesCachedEpisodeList(t *testing.T) {
	id := sourceHongguo + ":12345"
	cachedDrama := nativeDrama{
		ID: id, Source: sourceHongguo, SourceID: "12345", Title: "快取劇名", Episodes: 1,
	}
	chapter := Chapter{
		ID: providerChapterID(sourceHongguo, "12345", "67890"),
		Source: sourceHongguo, Title: "第1集", VideoURL: "hongguo-cenc://67890",
		CurrentEpisode: rawEpisode(1),
	}
	engine := &nativeEngine{
		catalogs: map[string][]nativeDrama{
			sourceHongguo: {cachedDrama},
		},
		detailCache: map[string]nativeDetailCacheEntry{
			id: {
				Drama: cachedDrama,
				Chapters: []Chapter{chapter},
				ExpiresAt: time.Now().Add(time.Minute),
			},
		},
	}

	result, err := engine.nativeDetail(context.Background(), nativeDrama{
		ID: id, Source: sourceHongguo, SourceID: "12345", Title: "來源的劇名",
	})
	if err != nil {
		t.Fatalf("nativeDetail returned error: %v", err)
	}
	value, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	drama, ok := value["drama"].(nativeDrama)
	if !ok || drama.ID != id || drama.Title != "來源的劇名" {
		t.Fatalf("cached drama was not merged with latest catalog data: %#v", value["drama"])
	}
	chapters, ok := value["chapters"].([]Chapter)
	if !ok || len(chapters) != 1 || chapters[0].VideoURL != chapter.VideoURL {
		t.Fatalf("cached episodes were not returned: %#v", value["chapters"])
	}
}
