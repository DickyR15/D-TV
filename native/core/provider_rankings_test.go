package core

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

func rankingFixture(board rankingBoard, page int, rows []map[string]any) string {
	return rankingFixtureWithTotalPages(board, page, rows, 2)
}

func rankingFixtureWithTotalPages(board rankingBoard, page int, rows []map[string]any, totalPages int) string {
	key := "rank_" + board.path + "/page"
	loader, _ := json.Marshal(map[string]any{"loaderData": map[string]any{key: map[string]any{"rankKey": board.upstreamKey, "pageNum": page, "updatedText": "9月13日已更新", "content": map[string]any{}}}})
	args, _ := json.Marshal([]any{key, "content", map[string]any{"isSuccess": true, "rankList": rows, "pagination": map[string]int{"pageNum": page, "totalPages": totalPages}}})
	return "<script>window._ROUTER_DATA=" + string(loader) + "</script><script data-script-src='modern-run-router-data-fn' data-fn-name='r' data-fn-args='" + html.EscapeString(string(args)) + "'></script>"
}

func rankingRows(page int) []map[string]any {
	return []map[string]any{
		{"id": "7681903520873729048", "seriesId": "7681903520873729048", "rank": (page-1)*20 + 1, "title": "甲 & <乙> \"丙\"", "heatText": "1.2亿热度", "episodeVids": []string{"1111111111111111111", "2222222222222222222"}},
		{"id": "7680883072278989849", "seriesId": "7680883072278989849", "rank": (page-1)*20 + 3, "title": "另一部", "heatText": "9000万热度"},
	}
}

func TestHongguoRankingStreamAndValidation(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	result, err := parseHongguoRanking(rankingFixture(board, 2, rankingRows(2)), board, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].Rank != 21 || result.Items[1].Rank != 23 || result.HasMore || result.Items[0].Drama.Title != "甲 & <乙> \"丙\"" {
		t.Fatalf("streamed data lost its rank, title or pagination: %+v", result)
	}
	if result.Items[0].Drama.Views != "" || result.Items[0].Drama.Heat != "1.2亿热度" || result.Items[0].Drama.Cover != nil {
		t.Fatal("heat must stay separate from views, and ranking metadata must have no images")
	}
	if result.Items[0].Drama.TotalEpisode != nil || result.Items[0].Drama.EpisodesVerified {
		t.Fatal("episodeVids preview IDs must never be treated as the total episode count")
	}
	for _, test := range []struct {
		name   string
		change func([]map[string]any)
	}{
		{"duplicate ID", func(rows []map[string]any) { rows[1]["id"], rows[1]["seriesId"] = rows[0]["id"], rows[0]["seriesId"] }},
		{"duplicate rank", func(rows []map[string]any) { rows[1]["rank"] = 1 }},
		{"wrong page ranks", func(rows []map[string]any) { rows[0]["rank"] = 21 }},
		{"mismatched ID", func(rows []map[string]any) { rows[0]["id"] = "7777777777777777777" }},
		{"missing title", func(rows []map[string]any) { rows[0]["title"] = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := rankingRows(1)
			test.change(rows)
			if _, err := parseHongguoRanking(rankingFixture(board, 1, rows), board, 1); err == nil {
				t.Fatal("accepted invalid upstream ranks")
			}
		})
	}
	other, _ := findRankingBoard("hongguo-ai")
	if _, err := parseHongguoRanking(rankingFixture(other, 1, rankingRows(1)), board, 1); err == nil {
		t.Fatal("accepted a different board")
	}
	if _, err := parseHongguoRanking(rankingFixture(board, 1, rankingRows(1)), board, 2); err == nil {
		t.Fatal("accepted a repeated first page")
	}
	if _, err := parseHongguoRanking(rankingFixture(board, 1, []map[string]any{}), board, 1); err == nil {
		t.Fatal("empty page with hasMore must be treated as a failure")
	}
}

func TestHongguoRankingUsesOnlyExplicitTotalEpisodeMetadata(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	rows := rankingRows(1)
	rows[0]["episodeCount"] = 88
	result, err := parseHongguoRanking(rankingFixture(board, 1, rows), board, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) == 0 {
		t.Fatal("expected ranking rows")
	}
	drama := nativeNormalize(result.Items[0].Drama)
	if drama.Episodes != 88 || !drama.EpisodesVerified {
		t.Fatalf("explicit total episode count was not verified: %+v", drama)
	}
}

func TestHongguoRankingRecoversFullPageWhenLoaderIsShort(t *testing.T) {
	board, ok := findRankingBoard("hongguo-ai")
	if !ok {
		t.Fatal("AI hot board not found")
	}

	fullRows := make([]map[string]any, 0, 20)
	for rank := 1; rank <= 20; rank++ {
		id := strconv.FormatInt(7681903520873700000+int64(rank), 10)
		fullRows = append(fullRows, map[string]any{
			"id": id, "seriesId": id, "rank": rank, "title": "AI 熱播短劇 " + strconv.Itoa(rank),
		})
	}

	// Simulate the official page where both embedded data formats omit one
	// row, but the visible HTML still contains all twenty ranked cards.
	body := rankingFixtureWithTotalPages(board, 1, fullRows[:19], 5)
	jsonLDItems := make([]map[string]any, 0, 19)
	for _, row := range fullRows[:19] {
		id := row["id"].(string)
		jsonLDItems = append(jsonLDItems, map[string]any{
			"position": row["rank"], "name": row["title"],
			"url": "https://hongguoduanju.com/detail?series_id=" + id,
		})
	}
	jsonLD, err := json.Marshal(map[string]any{
		"@type": "ItemList",
		"url": "https://hongguoduanju.com/rank/hot-ai-drama",
		"itemListElement": jsonLDItems,
	})
	if err != nil {
		t.Fatal(err)
	}
	body += "<script type='application/ld+json'>" + string(jsonLD) + "</script>"
	var pageHTML strings.Builder
	pageHTML.WriteString(`<nav aria-label="榜单分页"><a href="/rank/hot-ai-drama" aria-current="page">1</a><a href="/rank/hot-ai-drama?page=2" rel="next">2</a></nav><ol aria-label="AI 熱播榜">`)
	for index, row := range fullRows {
		id := row["id"].(string)
		title := row["title"].(string)
		rank := index + 1
		pageHTML.WriteString(`<article aria-labelledby="rank-title-` + id + `"><a href="/detail?series_id=` + id + `"><h2 id="rank-title-` + id + `">` + title + `</h2></a><a aria-label="查看第 ` + strconv.Itoa(rank) + ` 部短劇" href="/detail?series_id=` + id + `"><span>` + strconv.Itoa(rank) + `</span></a></article>`)
	}
	pageHTML.WriteString("</ol>")
	body += pageHTML.String()

	document, parseErr := xhtml.Parse(strings.NewReader(body))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	lists := providerHTMLNodes(document, func(node *xhtml.Node) bool {
		return node.Data == "ol" && providerHTMLAttr(node, "aria-label") != ""
	})
	if len(lists) != 1 {
		t.Fatalf("expected one HTML ranking list, found %d", len(lists))
	}
	articles := providerHTMLNodes(lists[0], func(node *xhtml.Node) bool {
		return node.Data == "article" && strings.HasPrefix(providerHTMLAttr(node, "aria-labelledby"), "rank-title-")
	})
	if len(articles) != 20 {
		t.Fatalf("expected 20 visible ranking cards, found %d", len(articles))
	}
	for index, article := range articles {
		if _, valid := parseHongguoRankingArticle(article); !valid {
			t.Fatalf("visible HTML ranking card %d was not parseable: label=%q",
				index+1, providerHTMLAttr(article, "aria-labelledby"))
		}
	}

	result, err := parseHongguoRanking(body, board, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 20 {
		t.Fatalf("incomplete embedded payload should recover all twenty HTML ranks: items=%d hasMore=%t",
			len(result.Items), result.HasMore)
	}
	if result.Items[0].Rank != 1 || result.Items[19].Rank != 20 || !result.HasMore {
		t.Fatalf("recovered HTML ranks or pagination are incorrect: first=%d last=%d hasMore=%t",
			result.Items[0].Rank, result.Items[19].Rank, result.HasMore)
	}
}

func TestHuangdouRankingPreservesOrderAndMetrics(t *testing.T) {
	rows := []any{
		map[string]any{"id": "rp_first", "name": "低热度先返回", "hot_rate": "20", "click": "100"},
		map[string]any{"id": "second", "name": "高热度后返回", "hot_rate": "1000", "click": "2"},
	}
	result, err := parseHuangdouRanking(map[string]any{"data": map[string]any{"list": rows}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].Rank != 21 || result.Items[0].Drama.ID != "huangdou:first" || result.Items[0].Drama.Heat != "20" || result.Items[0].Drama.Views != "100次播放" || result.HasMore {
		t.Fatalf("wrong rank or source metric: %+v", result)
	}
	if _, err := parseHuangdouRanking(map[string]any{"error": "not a list"}, 1); err == nil {
		t.Fatal("an upstream error cannot become an empty successful board")
	}
	result, err = parseHuangdouRanking(map[string]any{"list": []any{}}, 1)
	if err != nil || len(result.Items) != 0 || result.HasMore {
		t.Fatal("explicitly empty lists must be supported")
	}
}

func TestHuangguoRankingStructuredData(t *testing.T) {
	board, _ := findRankingBoard("huangguo-potential")
	body := `<script type="application/ld+json">{"@graph":[{"@type":"BreadcrumbList","itemListElement":[]},{"@type":"ItemList","@id":"https://huangguoai.com/ranks/potential/#itemlist","itemListElement":[{"position":1,"name":"A & B","url":"https://huangguoai.com/detail/12/"},{"position":3,"name":"第二条","url":"https://huangguoai.com/detail/34/"}]}]}</script>`
	result, err := parseHuangguoRanking(body, board)
	if err != nil || len(result.Items) != 2 || result.Items[1].Rank != 3 || result.Items[0].Drama.ID != "huangguoai:12" || result.HasMore {
		t.Fatalf("could not preserve structured site ranks: %+v %v", result, err)
	}
	other, _ := findRankingBoard("huangguo-hot")
	if _, err := parseHuangguoRanking(body, other); err == nil {
		t.Fatal("accepted a different chart's structured data")
	}
	if _, err := parseHuangguoRanking(strings.ReplaceAll(body, "/detail/34/", "/detail/12/"), board); err == nil {
		t.Fatal("accepted duplicate IDs")
	}
}

type rankingTransport func(*http.Request) (*http.Response, error)

func (transport rankingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func rankingTestDownloader(t *testing.T, transport rankingTransport) *Downloader {
	t.Helper()
	d := sourceFixtureDownloader(t, sourceFixtureTransport(transport))
	d.cfg.dataDir = t.TempDir()
	d.limiter = newRequestLimiter(8, time.Nanosecond)
	return d
}

func rankingHTTPResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Request: request}
}

func TestHongguoRankingRetriesTransientRequestTimeout(t *testing.T) {
	board, _ := findRankingBoard("hongguo-comic")
	var calls atomic.Int32
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, context.DeadlineExceeded
		}
		return rankingHTTPResponse(request, http.StatusOK, rankingFixture(board, 1, rankingRows(1))), nil
	})

	result, err := d.fetchHongguoRankingPage(context.Background(), board, 1)
	if err != nil || len(result.Items) != 2 || calls.Load() != 2 {
		t.Fatalf("a transient ranking timeout should be retried once: items=%d calls=%d err=%v", len(result.Items), calls.Load(), err)
	}
}

func TestRankingCacheAndStaleRecovery(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	var calls atomic.Int32
	var fail atomic.Bool
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if fail.Load() {
			return rankingHTTPResponse(request, 503, "temporarily unavailable"), nil
		}
		page, err := strconv.Atoi(request.URL.Query().Get("page"))
		if err != nil {
			page = 1 // 第一页不带 page 参数
		}
		return rankingHTTPResponse(request, 200, rankingFixture(board, page, rankingRows(page))), nil
	})
	ctx := context.Background()
	first, err := d.loadRankingPage(ctx, board, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	first.Items[0].Rank = 999
	again, err := d.loadRankingPage(ctx, board, 1, false)
	if err != nil || again.Items[0].Rank != 1 || calls.Load() != 1 {
		t.Fatal("a fresh page was not safely reused")
	}
	if _, err = d.loadRankingPage(ctx, board, 2, false); err != nil {
		t.Fatal(err)
	}
	if _, err = d.loadRankingPage(ctx, board, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, found := d.rankings.pages[board.ID+":2"]; found {
		t.Fatal("refresh must invalidate older continuation pages")
	}
	previous := d.rankings.pages[board.ID+":1"].FetchedAt
	fail.Store(true)
	stale, err := d.loadRankingPage(ctx, board, 1, true)
	if err != nil || !stale.Stale || stale.Warning == "" || !stale.FetchedAt.Equal(previous) {
		t.Fatalf("last valid page must retain its original freshness: %+v %v", stale, err)
	}
	fail.Store(false)
	fresh, err := d.loadRankingPage(ctx, board, 1, true)
	if err != nil || fresh.Stale || fresh.Warning != "" {
		t.Fatal("a later successful refresh must clear the stale state")
	}
}


func TestRankingExpiredCacheServesImmediatelyAndRefreshesInBackground(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
		return rankingHTTPResponse(request, http.StatusOK, rankingFixture(board, 1, rankingRows(1))), nil
	})
	oldFetchedAt := time.Now().Add(-rankingCacheTTL - time.Minute)
	key := board.ID + ":1"
	d.rankings.pages = map[string]rankingPage{}
	d.rankings.pending = map[string]*rankingCall{}
	d.rankings.pages[key] = rankingPage{
		BoardID: board.ID,
		Page: 1,
		Items: []rankingItem{{
			Rank: 1,
			Drama: Drama{
				ID: "hongguo:7681903520873729048",
				Source: sourceHongguo,
				SourceID: "7681903520873729048",
				Title: "上次快取榜單",
			},
		}},
		FetchedAt: oldFetchedAt,
	}
	start := time.Now()
	result, err := d.loadRankingPage(context.Background(), board, 1, false)
	if err != nil || !result.Stale || len(result.Items) != 1 || result.Items[0].Drama.Title != "上次快取榜單" {
		t.Fatalf("expired ranking cache was not served immediately: %+v %v", result, err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("stale cache waited for network response: %s", elapsed)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not start")
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d.rankings.mu.Lock()
		refreshed := d.rankings.pages[key].FetchedAt.After(oldFetchedAt)
		d.rankings.mu.Unlock()
		if refreshed {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("background refresh did not replace the stale cache")
}

func TestRankingConcurrentRequestsShareFetch(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return rankingHTTPResponse(request, 200, rankingFixture(board, 1, rankingRows(1))), nil
	})
	var group sync.WaitGroup
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := d.loadRankingPage(context.Background(), board, 1, false)
			if err != nil || len(result.Items) != 2 {
				t.Errorf("concurrent request failed: %v", err)
			}
		}()
	}
	<-started
	close(release)
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("got %d upstream requests, want one", calls.Load())
	}
}

func TestCachedRankingTop100ReturnsLocalPagesImmediately(t *testing.T) {
	board, _ := findRankingBoard("hongguo-hot")
	d := &Downloader{}
	d.rankings.pages = map[string]rankingPage{}
	fetchedAt := time.Now().Add(-6 * time.Minute)
	for pageNo := 1; pageNo <= 5; pageNo++ {
		rows := make([]rankingItem, 0, 20)
		for offset := 1; offset <= 20; offset++ {
			rank := (pageNo-1)*20 + offset
			// Simulate an upstream page overlap: positions 21-35 repeat
			// the first 15 drama IDs, producing the reported 85 unique IDs
			// across 100 valid rank positions.
			idRank := rank
			if pageNo == 2 && offset <= 15 {
				idRank = offset
			}
			id := strconv.FormatInt(7681903520873720000+int64(idRank), 10)
			rows = append(rows, rankingItem{
				Rank: rank,
				Drama: Drama{
					ID: providerDramaID(sourceHongguo, id),
					Source: sourceHongguo,
					SourceID: id,
					Title: "快取短劇 " + strconv.Itoa(idRank),
				},
			})
		}
		d.rankings.pages[board.ID+":"+strconv.Itoa(pageNo)] = rankingPage{
			BoardID: board.ID,
			Page: pageNo,
			Items: rows,
			HasMore: pageNo < 5,
			FetchedAt: fetchedAt,
		}
	}

	result := d.cachedRankingTop100(board)
	if len(result.Items) != 100 || result.Page != 5 || result.HasMore || !result.Stale {
		t.Fatalf("expected stale cached top 100 without refreshing: items=%d page=%d more=%t stale=%t",
			len(result.Items), result.Page, result.HasMore, result.Stale)
	}
	if result.Items[0].Rank != 1 || result.Items[99].Rank != 100 {
		t.Fatalf("cached pages were not merged in ranking order: first=%d last=%d",
			result.Items[0].Rank, result.Items[99].Rank)
	}

	// If a continuation page is absent, return the known contiguous prefix
	// rather than blocking or stitching around a missing range.
	delete(d.rankings.pages, board.ID+":4")
	partial := d.cachedRankingTop100(board)
	if len(partial.Items) != 60 || partial.Page != 3 || !partial.HasMore {
		t.Fatalf("expected 60 cached items when page 4 is missing: items=%d page=%d more=%t",
			len(partial.Items), partial.Page, partial.HasMore)
	}
}
