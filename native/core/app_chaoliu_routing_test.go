package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const chaoliuRoutingFixtureHTML = `<!doctype html>
<html><head><title>Chaoliu fixture catalogue</title><meta property="og:title" content="Chaoliu fixture"></head>
<body><div class="post"><h3><a href="/zh/uncensored_content/19/sample-title.html">測試作品</a></h3>
<img data-original="/images/test.jpg" alt="測試作品"><p>fixture catalogue card</p></div></body></html>`

func TestNativeCatalogRoutesChaoliuCatalogAndSearch(t *testing.T) {
	var requestedPaths []string
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		if !isChaoliuSiteHost(request.URL.Hostname()) {
			return nil, fmt.Errorf("unexpected chaoliu host: %s", request.URL.Hostname())
		}
		requestedPaths = append(requestedPaths, request.URL.EscapedPath())
		return sourceFixtureResponse(request, http.StatusOK, chaoliuRoutingFixtureHTML), nil
	})

	catalog, err := engine.nativeCatalog(context.Background(), nativeInput{
		Source: sourceChaoliu, Category: "uncensored", Page: 1, Force: true,
	})
	if err != nil {
		t.Fatalf("chaoliu category catalog failed: %v", err)
	}
	if len(catalog.Items) != 1 || catalog.Items[0].Source != sourceChaoliu ||
		catalog.Items[0].SourceID != "uncensored_content/19/sample-title" {
		t.Fatalf("chaoliu category catalog parsed unexpected items: %#v", catalog.Items)
	}
	if len(requestedPaths) != 1 || requestedPaths[0] != "/zh/uncensored_list/all/1.html" {
		t.Fatalf("chaoliu category request paths = %v", requestedPaths)
	}

	search, err := engine.nativeCatalog(context.Background(), nativeInput{
		Source: sourceChaoliu, Query: "sample", Page: 1,
	})
	if err != nil {
		t.Fatalf("chaoliu search failed: %v", err)
	}
	if len(search.Items) != 1 || search.Items[0].Source != sourceChaoliu {
		t.Fatalf("chaoliu search parsed unexpected items: %#v", search.Items)
	}
	if len(requestedPaths) != 2 || requestedPaths[1] != "/zh/fc_search/all/sample/1.html" {
		t.Fatalf("chaoliu search request paths = %v", requestedPaths)
	}
}

func TestChaoliuEmptyCatalogueParseDoesNotReplaceCachedItems(t *testing.T) {
	const emptyPage = `<!doctype html><html><head><title>Chaoliu</title>
<meta property="og:title" content="Chaoliu"></head><body><main><p>Catalogue shell without any post cards.</p></main></body></html>`
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		return sourceFixtureResponse(request, http.StatusOK, emptyPage), nil
	})
	key := nativeCatalogKey(sourceChaoliu, "uncensored")
	previous := nativeDrama{
		ID: providerDramaID(sourceChaoliu, "uncensored_content/19/previous-title"),
		Source: sourceChaoliu, SourceID: "uncensored_content/19/previous-title",
		Title: "先前快取作品",
	}
	engine.saveCatalogCache(key, &nativeCatalogResult{Items: []nativeDrama{previous}, Page: 1})

	_, err := engine.nativeCatalog(context.Background(), nativeInput{
		Source: sourceChaoliu, Category: "uncensored", Page: 1, Force: true,
	})
	if err == nil || !strings.Contains(err.Error(), "未解析到作品卡片") {
		t.Fatalf("empty chaoliu parse should fail with a cache-preservation error, got %v", err)
	}
	cached := engine.nativeCached(key)
	if len(cached.Items) != 1 || cached.Items[0].ID != previous.ID {
		t.Fatalf("empty chaoliu parse replaced the previous cache: %#v", cached.Items)
	}
}
